package releasecontroller

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/blang/semver"
	lru "github.com/hashicorp/golang-lru"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"

	imagev1 "github.com/openshift/api/image/v1"
)

var (
	ErrStreamNotFound    = fmt.Errorf("no release configuration exists with the requested name")
	ErrStreamTagNotFound = fmt.Errorf("no tags exist within the release that satisfy the request")
)

type StableReferences struct {
	Releases StableReleases
}

type StableReleases []StableRelease

func (v StableReleases) Less(i, j int) bool {
	c := v[i].Version.Compare(v[j].Version)
	if c > 0 {
		return true
	}
	return false
}

func (v StableReleases) Len() int      { return len(v) }
func (v StableReleases) Swap(i, j int) { v[i], v[j] = v[j], v[i] }

type StableRelease struct {
	Release  *Release
	Version  semver.Version
	Versions SemanticVersions
}

type dockerImageConfig struct {
	Architecture string `json:"architecture"`
	Os           string `json:"os"`
}

type imageInfoConfig struct {
	Config *dockerImageConfig `json:"config"`
	Digest string             `json:"digest"`
	Name   string             `json:"name"`
}

func (c imageInfoConfig) GenerateDigestPullSpec() string {
	if strings.Contains(c.Name, "@sha256:") {
		return fmt.Sprintf("%s@%s", strings.Split(c.Name, "@sha256:")[0], c.Digest)
	}
	return fmt.Sprintf("%s@%s", strings.Split(c.Name, ":")[0], c.Digest)
}

func (r *Release) HasInconsistencies() bool {
	for _, tag := range r.Source.Spec.Tags {
		if _, ok := tag.Annotations[ReleaseAnnotationInconsistency]; ok {
			return true
		}
	}
	if _, ok := r.Source.ObjectMeta.Annotations[ReleaseAnnotationInconsistency]; ok {
		return true
	}
	return false
}

func ReleaseDefinition(is *imagev1.ImageStream, releaseConfigCache *lru.Cache, eventRecorder record.EventRecorder, releaseLister MultiImageStreamLister) (*Release, bool, error) {
	src, ok := is.Annotations[ReleaseAnnotationConfig]
	if !ok {
		return nil, false, nil
	}

	cfg, err := ParseReleaseConfig(src, releaseConfigCache)
	if err != nil {
		err = fmt.Errorf("the %s annotation for %s is invalid: %v", ReleaseAnnotationConfig, is.Name, err)
		eventRecorder.Eventf(is, corev1.EventTypeWarning, "InvalidReleaseDefinition", "%v", err)
		return nil, false, TerminalError{err}
	}

	if is.Status.PublicDockerImageRepository == "" {
		klog.V(4).Infof("The release input has no public docker image repository, waiting")
		return nil, false, nil
	}

	if len(is.Status.Tags) == 0 {
		klog.V(4).Infof("The release input has no status tags, waiting")
		return nil, false, nil
	}

	switch cfg.As {
	case ReleaseConfigModeStable:
		r := &Release{
			Source: is,
			Target: is,
			Config: cfg,
		}
		return r, true, nil
	default:
		targetImageStream, err := releaseLister.ImageStreams(is.Namespace).Get(cfg.To)
		if errors.IsNotFound(err) {
			// TODO: something special here?
			klog.V(2).Infof("The release image stream %s/%s does not exist", is.Namespace, cfg.To)
			return nil, false, TerminalError{fmt.Errorf("the output release image stream %s/%s does not exist", is.Namespace, cfg.To)}
		}
		if err != nil {
			return nil, false, fmt.Errorf("unable to lookup release image stream: %v", err)
		}
		r := &Release{
			Source: is,
			Target: targetImageStream,
			Config: cfg,
		}
		return r, true, nil
	}
}

func ParseReleaseConfig(data string, configCache *lru.Cache) (*ReleaseConfig, error) {
	// TODO: bumping this from 8 to 12 to allow for current releases to continue processing.  We need to consider improvements in the space before bumping again.
	if len(data) > 12*1024 {
		return nil, fmt.Errorf("release config must be less than 8k")
	}
	if configCache != nil {
		obj, ok := configCache.Get(data)
		if ok {
			cfg := obj.(ReleaseConfig)
			return &cfg, nil
		}
	}
	cfg := &ReleaseConfig{}
	if err := json.Unmarshal([]byte(data), cfg); err != nil {
		return nil, err
	}
	if len(cfg.Name) == 0 {
		return nil, fmt.Errorf("release config must have a valid name")
	}
	if len(cfg.To) == 0 && cfg.As != ReleaseConfigModeStable {
		return nil, fmt.Errorf("release must specify 'to' unless 'as' is 'Stable'")
	}
	for name, verify := range cfg.Verify {
		if len(name) == 0 {
			return nil, fmt.Errorf("verify config has no name")
		}
		switch verify.UpgradeFrom {
		case ReleaseUpgradeFromPreviousMinor, ReleaseUpgradeFromPreviousPatch, ReleaseUpgradeFromPrevious, "":
		default:
			return nil, fmt.Errorf("verify config %s has an invalid upgradeFrom: %s", name, verify.UpgradeFrom)
		}
		if verify.ProwJob != nil {
			if len(verify.ProwJob.Name) == 0 {
				return nil, fmt.Errorf("prow job for %s has no name", name)
			}
		}
	}
	for name, publish := range cfg.Publish {
		if len(name) == 0 {
			return nil, fmt.Errorf("publish config has no name")
		}
		if publish.TagRef != nil {
			if len(publish.TagRef.Name) == 0 {
				return nil, fmt.Errorf("tagRef publish for %s has no name", name)
			}
		}
		if publish.ImageStreamRef != nil {
			if len(publish.ImageStreamRef.Name) == 0 {
				return nil, fmt.Errorf("imageStreamRef publish for %s has no name", name)
			}
		}
	}
	copied := *cfg
	if configCache != nil {
		configCache.Add(data, copied)
	}
	return cfg, nil
}

func ReleaseGenerationFromObject(name string, annotations map[string]string) (int64, bool) {
	_, ok := annotations[ReleaseAnnotationSource]
	if !ok {
		return 0, false
	}
	s, ok := annotations[ReleaseAnnotationGeneration]
	if !ok {
		klog.V(4).Infof("Can't check %s, no generation", name)
		return 0, false
	}
	generation, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		klog.V(4).Infof("Can't check %s, generation is invalid: %v", name, err)
		return 0, false
	}
	return generation, true
}

func HashSpecTagImageDigests(is *imagev1.ImageStream) string {
	h := sha256.New()
	for _, tag := range is.Status.Tags {
		if len(tag.Items) == 0 {
			continue
		}
		latest := tag.Items[0]
		input := latest.Image
		if len(input) == 0 {
			input = latest.DockerImageReference
		}
		h.Write([]byte(input))
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}

func TagNames(refs []*imagev1.TagReference) []string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Name)
	}
	return names
}

func Int32p(i int32) *int32 {
	return &i
}

func ContainsTagReference(tags []*imagev1.TagReference, name string) bool {
	for _, tag := range tags {
		if name == tag.Name {
			return true
		}
	}
	return false
}

func FindTagReference(is *imagev1.ImageStream, name string) *imagev1.TagReference {
	for i := range is.Spec.Tags {
		tag := &is.Spec.Tags[i]
		if tag.Name == name {
			return tag
		}
	}
	return nil
}

func FindImageIDForTag(is *imagev1.ImageStream, name string) string {
	for i := range is.Status.Tags {
		tag := &is.Status.Tags[i]
		if tag.Tag == name {
			if len(tag.Items) == 0 {
				return ""
			}
			if len(tag.Conditions) > 0 {
				if IsTagEventConditionNotImported(tag) {
					return ""
				}
			}
			if specTag := FindSpecTag(is.Spec.Tags, name); specTag != nil && (specTag.Generation == nil || *specTag.Generation > tag.Items[0].Generation) {
				return ""
			}
			return tag.Items[0].Image
		}
	}
	return ""
}

func IsTagEventConditionNotImported(event *imagev1.NamedTagEventList) bool {
	for _, condition := range event.Conditions {
		if condition.Type == imagev1.ImportSuccess {
			if condition.Status == corev1.ConditionFalse {
				return true
			}
		}
	}
	return false
}

func FindImagePullSpec(is *imagev1.ImageStream, name string) string {
	for i := range is.Status.Tags {
		tag := &is.Status.Tags[i]
		if tag.Tag == name {
			if len(tag.Items) == 0 {
				if specTag := FindSpecTag(is.Spec.Tags, name); specTag != nil {
					if from := specTag.From; from != nil && from.Kind == "DockerImage" {
						return from.Name
					}
				}
				return ""
			}
			return tag.Items[0].DockerImageReference
		}
	}
	return ""
}

func FindPublicImagePullSpec(is *imagev1.ImageStream, name string) string {
	for i := range is.Status.Tags {
		tag := &is.Status.Tags[i]
		if tag.Tag == name {
			if specTag := FindSpecTag(is.Spec.Tags, name); specTag != nil {
				if from := specTag.From; from != nil && from.Kind == "DockerImage" {
					if len(tag.Items) == 0 || (specTag.Generation != nil && *specTag.Generation >= tag.Items[0].Generation) {
						return from.Name
					}
				}
			}
			if len(tag.Items) == 0 {
				return ""
			}
			if len(is.Status.PublicDockerImageRepository) > 0 {
				return fmt.Sprintf("%s:%s", is.Status.PublicDockerImageRepository, name)
			}
			if strings.HasPrefix(tag.Items[0].DockerImageReference, is.Status.DockerImageRepository) {
				return ""
			}
			return tag.Items[0].DockerImageReference
		}
	}
	return ""
}

// UnsortedSemanticReleaseTags returns the tags in the release as a sortable array, but
// does not sort the array. If phases is specified only tags in the provided phases
// are returned.
func UnsortedSemanticReleaseTags(release *Release, phases ...string) SemanticVersions {
	is := release.Target
	sourceName := fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name)
	versions := make(SemanticVersions, 0, len(is.Spec.Tags))
	for i := range is.Spec.Tags {
		tag := &is.Spec.Tags[i]
		if tag.Annotations[ReleaseAnnotationSource] != sourceName {
			continue
		}

		// if the name has changed, consider the tag abandoned (admin is responsible for cleaning it up)
		if tag.Annotations[ReleaseAnnotationName] != release.Config.Name {
			continue
		}
		if len(phases) > 0 && !StringSliceContains(phases, tag.Annotations[ReleaseAnnotationPhase]) {
			continue
		}

		if version, err := semver.Parse(tag.Name); err == nil {
			versions = append(versions, SemanticVersion{Tag: tag, Version: &version})
		} else {
			versions = append(versions, SemanticVersion{Tag: tag})
		}
	}
	return versions
}

func FirstTagWithMajorMinorSemanticVersion(versions SemanticVersions, version semver.Version) *SemanticVersion {
	for i, v := range versions {
		if v.Version == nil {
			continue
		}
		if v.Version.Major == version.Major && v.Version.Minor == version.Minor {
			return &versions[i]
		}
	}
	return nil
}

// SortedReleaseTags returns the tags for a given release in the most appropriate order -
// by creation date for iterative streams, by semantic version for stable streams. If
// phase is specified the list will be filtered.
func SortedReleaseTags(release *Release, phases ...string) []*imagev1.TagReference {
	versions := UnsortedSemanticReleaseTags(release, phases...)
	switch release.Config.As {
	case ReleaseConfigModeStable:
		sort.Sort(versions)
		return versions.Tags()
	default:
		tags := versions.Tags()
		sort.Sort(TagReferencesByAge(tags))
		return tags
	}
}

// SortedRawReleaseTags returns the tags for the given release in order of their creation
// if they are in one of the provided phases. Use sortedReleaseTags if you are trying to get the
// most appropriate recent tag. Intended for use only within the release.
func SortedRawReleaseTags(release *Release, phases ...string) []*imagev1.TagReference {
	var tags []*imagev1.TagReference
	for i := range release.Target.Spec.Tags {
		tag := &release.Target.Spec.Tags[i]
		if tag.Annotations[ReleaseAnnotationName] != release.Config.Name {
			continue
		}
		if tag.Annotations[ReleaseAnnotationSource] != fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name) {
			continue
		}
		if StringSliceContains(phases, tag.Annotations[ReleaseAnnotationPhase]) {
			tags = append(tags, tag)
		}
	}
	sort.Sort(TagReferencesByAge(tags))
	return tags
}

func FindSpecTag(tags []imagev1.TagReference, name string) *imagev1.TagReference {
	for i, tag := range tags {
		if tag.Name != name {
			continue
		}
		return &tags[i]
	}
	return nil
}

func IsReleaseDelayedForInterval(release *Release, tag *imagev1.TagReference) (bool, string, time.Duration) {
	if release.Config.MinCreationIntervalSeconds == 0 {
		return false, "", 0
	}
	if tag == nil {
		return false, "", 0
	}
	created, err := time.Parse(time.RFC3339, tag.Annotations[ReleaseAnnotationCreationTimestamp])
	if err != nil {
		return false, "", 0
	}
	interval, minInterval := time.Now().Sub(created), time.Duration(release.Config.MinCreationIntervalSeconds)*time.Second
	if interval < minInterval {
		return true, fmt.Sprintf("Release %s last tag %s created %s ago (less than minimum interval %s), will not launch new tags", release.Config.Name, tag.Name, interval.Truncate(time.Second), minInterval), minInterval - interval
	}
	return false, "", 0
}

func CountUnreadyReleases(release *Release, tags []*imagev1.TagReference) int {
	unreadyTagCount := 0
	for _, tag := range tags {
		// always skip pinned tags
		if _, ok := tag.Annotations[ReleaseAnnotationKeep]; ok {
			continue
		}
		// check annotations when using the target as tag source
		if release.Config.As != ReleaseConfigModeStable && tag.Annotations[ReleaseAnnotationSource] != fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name) {
			continue
		}
		// if the name has changed, consider the tag abandoned (admin is responsible for cleaning it up)
		if tag.Annotations[ReleaseAnnotationName] != release.Config.Name {
			continue
		}

		phase := tag.Annotations[ReleaseAnnotationPhase]
		switch phase {
		case ReleasePhaseFailed, ReleasePhaseRejected, ReleasePhaseAccepted:
			// terminal don't count
		default:
			unreadyTagCount++
		}
	}
	return unreadyTagCount
}

func GetStableReleases(rcCache *lru.Cache, eventRecorder record.EventRecorder, lister *MultiImageStreamLister) (*StableReferences, error) {
	imageStreams, err := lister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	stable := &StableReferences{}

	for _, stream := range imageStreams {
		r, ok, err := ReleaseDefinition(stream, rcCache, eventRecorder, *lister)
		if err != nil || !ok {
			continue
		}

		if r.Config.As == ReleaseConfigModeStable {
			version, _ := SemverParseTolerant(r.Source.Name)
			stable.Releases = append(stable.Releases, StableRelease{
				Release: r,
				Version: version,
			})
		}
	}

	sort.Sort(stable.Releases)
	return stable, nil
}

func LatestForStream(rcCache *lru.Cache, eventRecorder record.EventRecorder, lister *MultiImageStreamLister, streamName string, constraint semver.Range, relativeIndex int) (*Release, *imagev1.TagReference, error) {
	imageStreams, err := lister.List(labels.Everything())
	if err != nil {
		return nil, nil, err
	}
	for _, stream := range imageStreams {
		r, ok, err := ReleaseDefinition(stream, rcCache, eventRecorder, *lister)
		if err != nil || !ok {
			continue
		}
		if r.Config.Name != streamName {
			continue
		}
		// find all accepted tags, then sort by semantic version
		tags := UnsortedSemanticReleaseTags(r, ReleasePhaseAccepted)
		sort.Sort(tags)
		for _, ver := range tags {
			if constraint != nil && (ver.Version == nil || !constraint(*ver.Version)) {
				continue
			}
			if relativeIndex > 0 {
				relativeIndex--
				continue
			}
			return r, ver.Tag, nil
		}
		return nil, nil, ErrStreamTagNotFound
	}
	return nil, nil, ErrStreamNotFound
}

func GetImageInfo(releaseInfo ReleaseInfo, architecture, pullSpec string) (*imageInfoConfig, error) {
	// Get the ImageInfo
	imageInfo, err := releaseInfo.ImageInfo(pullSpec, architecture)
	if err != nil {
		return nil, fmt.Errorf("could not get image info for from pullSpec %s: %v", pullSpec, err)
	}
	config := imageInfoConfig{}
	if err := json.Unmarshal([]byte(imageInfo), &config); err != nil {
		return nil, fmt.Errorf("could not unmarshal image info for from pullSpec %s: %v", pullSpec, err)
	}
	return &config, nil
}

func GetVerificationJobs(rcCache *lru.Cache, eventRecorder record.EventRecorder, lister *MultiImageStreamLister, release *Release, releaseTag *imagev1.TagReference, artSuffix string) (map[string]ReleaseVerification, error) {
	if release.Config.As != ReleaseConfigModeStable || artSuffix == "" {
		return release.Config.Verify, nil
	}
	jobs := make(map[string]ReleaseVerification)
	for k, v := range release.Config.Verify {
		jobs[k] = v
	}
	version, err := SemverParseTolerant(releaseTag.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get full stable verification jobs: %w", err)
	}
	isName := fmt.Sprintf("%d.%d%s", version.Major, version.Minor, artSuffix)
	klog.Infof("Getting imagestream %s/%s", release.Target.Namespace, isName)
	isLister := lister.ImageStreams(release.Target.Namespace)
	if isLister == nil {
		return nil, fmt.Errorf("failed to get imagestream %s", release.Target.Namespace)
	}
	imageStream, err := isLister.Get(isName)
	if err != nil {
		return nil, fmt.Errorf("failed to get imagestream %s/%s: %w", release.Target.Namespace, isName, err)
	}
	versionedRelease, ok, err := ReleaseDefinition(imageStream, rcCache, eventRecorder, *lister)
	if err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("failed to get release definition from stream %s/%s", release.Target.Namespace, isName)
	}
	for name, verify := range versionedRelease.Config.Verify {
		if _, ok := jobs[name]; !ok && !verify.Optional && verify.AggregatedProwJob == nil {
			verify.Optional = true
			jobs[name] = verify
		}
	}
	return jobs, nil
}
