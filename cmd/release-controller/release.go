package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/blang/semver"
	lru "github.com/hashicorp/golang-lru"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog"

	imagev1 "github.com/openshift/api/image/v1"
)

func (c *Controller) releaseDefinition(is *imagev1.ImageStream) (*Release, bool, error) {
	src, ok := is.Annotations[releaseAnnotationConfig]
	if !ok {
		return nil, false, nil
	}
	cfg, err := parseReleaseConfig(src, c.parsedReleaseConfigCache)
	if err != nil {
		err = fmt.Errorf("the %s annotation for %s is invalid: %v", releaseAnnotationConfig, is.Name, err)
		c.eventRecorder.Eventf(is, corev1.EventTypeWarning, "InvalidReleaseDefinition", "%v", err)
		return nil, false, terminalError{err}
	}

	if len(is.Status.Tags) == 0 {
		klog.V(4).Infof("The release input has no status tags, waiting")
		return nil, false, nil
	}

	switch cfg.As {
	case releaseConfigModeStable:
		r := &Release{
			Source: is,
			Target: is,
			Config: cfg,
		}
		return r, true, nil
	default:
		targetImageStream, err := c.releaseLister.ImageStreams(is.Namespace).Get(cfg.To)
		if errors.IsNotFound(err) {
			// TODO: something special here?
			klog.V(2).Infof("The release image stream %s/%s does not exist", is.Namespace, cfg.To)
			return nil, false, terminalError{fmt.Errorf("the output release image stream %s/%s does not exist", is.Namespace, cfg.To)}
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

func parseReleaseConfig(data string, configCache *lru.Cache) (*ReleaseConfig, error) {
	if len(data) > 8*1024 {
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
	if len(cfg.To) == 0 && cfg.As != releaseConfigModeStable {
		return nil, fmt.Errorf("release must specify 'to' unless 'as' is 'Stable'")
	}
	for name, verify := range cfg.Verify {
		if len(name) == 0 {
			return nil, fmt.Errorf("verify config has no name")
		}
		switch verify.UpgradeFrom {
		case releaseUpgradeFromPreviousMinor, releaseUpgradeFromPreviousPatch, releaseUpgradeFromPrevious, "":
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

func releaseGenerationFromObject(name string, annotations map[string]string) (int64, bool) {
	_, ok := annotations[releaseAnnotationSource]
	if !ok {
		return 0, false
	}
	s, ok := annotations[releaseAnnotationGeneration]
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

func hashSpecTagImageDigests(is *imagev1.ImageStream) string {
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

func queueKeyFor(annotation string) (queueKey, bool) {
	if len(annotation) == 0 {
		return queueKey{}, false
	}
	parts := strings.SplitN(annotation, "/", 2)
	if len(parts) != 2 {
		return queueKey{}, false
	}
	return queueKey{namespace: parts[0], name: parts[1]}, true
}

func tagNames(refs []*imagev1.TagReference) []string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Name)
	}
	return names
}

func int32p(i int32) *int32 {
	return &i
}

func containsTagReference(tags []*imagev1.TagReference, name string) bool {
	for _, tag := range tags {
		if name == tag.Name {
			return true
		}
	}
	return false
}

func findTagReference(is *imagev1.ImageStream, name string) *imagev1.TagReference {
	for i := range is.Spec.Tags {
		tag := &is.Spec.Tags[i]
		if tag.Name == name {
			return tag
		}
	}
	return nil
}

func findImageIDForTag(is *imagev1.ImageStream, name string) string {
	for i := range is.Status.Tags {
		tag := &is.Status.Tags[i]
		if tag.Tag == name {
			if len(tag.Items) == 0 {
				return ""
			}
			if len(tag.Conditions) > 0 {
				if isTagEventConditionNotImported(tag) {
					return ""
				}
			}
			if specTag := findSpecTag(is.Spec.Tags, name); specTag != nil && (specTag.Generation == nil || *specTag.Generation > tag.Items[0].Generation) {
				return ""
			}
			return tag.Items[0].Image
		}
	}
	return ""
}

func isTagEventConditionNotImported(event *imagev1.NamedTagEventList) bool {
	for _, condition := range event.Conditions {
		if condition.Type == imagev1.ImportSuccess {
			if condition.Status == corev1.ConditionFalse {
				return true
			}
		}
	}
	return false
}

// find ImagePullSpec will return a fully-resolved pull spec for an image tag
// when such a thing exists for the local registry. When the source image is
// known but an import has not yet succeeded, we return the empty string.
func findImagePullSpec(is *imagev1.ImageStream, name string) string {
	for i := range is.Status.Tags {
		tag := &is.Status.Tags[i]
		if tag.Tag == name {
			if len(tag.Items) == 0 {
				if specTag := findSpecTag(is.Spec.Tags, name); specTag != nil {
					if from := specTag.From; from != nil && from.Kind == "DockerImage" {
						return "" // the ImageStream controller needs to import this, we can check again later
					}
				}
				return "" // the ImageStream controller has not yet imported this, we can check again later
			}
			return fmt.Sprintf("%s@%s", is.Status.PublicDockerImageRepository, tag.Items[0].Image)
		}
	}
	return "" // no such tag
}

// findPublicImagePullSpec will return fully-resolved pull spec for an image tag
// when such a thing exists for the local registry. When the source image is
// known but an import has not yet succeeded, we return the empty string.
//
// TODO: it looks like this func is now very similar to findImagePullSpec - but
// consumers of findImagePullSpec use the output to configure e.g. PodSpecs, so
// clearly consumers are not correctly determining if they need the _public_
// pull-spec or "any" pull-spec, or perhaps there is no meaningful distinction
// between these functions.
func findPublicImagePullSpec(is *imagev1.ImageStream, name string) string {
	for i := range is.Status.Tags {
		tag := &is.Status.Tags[i]
		if tag.Tag == name {
			if specTag := findSpecTag(is.Spec.Tags, name); specTag != nil {
				if from := specTag.From; from != nil && from.Kind == "DockerImage" {
					if len(tag.Items) == 0 || (specTag.Generation != nil && *specTag.Generation > tag.Items[0].Generation) {
						return "" // the ImageStream controller needs to import this, we can check again later
					}
				}
			}
			if len(tag.Items) == 0 {
				return "" // the ImageStream controller has not yet imported this, we can check again later
			}
			if len(is.Status.PublicDockerImageRepository) > 0 {
				return fmt.Sprintf("%s:%s", is.Status.PublicDockerImageRepository, name)
			}
			// the rest of this code is not important as CI registries are always public
			if strings.HasPrefix(tag.Items[0].DockerImageReference, is.Status.DockerImageRepository) {
				return "" // why?
			}
			return tag.Items[0].DockerImageReference // this is likely to not be a useful pullspec for someone calling this func...
		}
	}
	return "" // no such tag
}

// unsortedSemanticReleaseTags returns the tags in the release as a sortable array, but
// does not sort the array. If phases is specified only tags in the provided phases
// are returned.
func unsortedSemanticReleaseTags(release *Release, phases ...string) SemanticVersions {
	is := release.Target
	sourceName := fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name)
	versions := make(SemanticVersions, 0, len(is.Spec.Tags))
	for i := range is.Spec.Tags {
		tag := &is.Spec.Tags[i]
		if tag.Annotations[releaseAnnotationSource] != sourceName {
			continue
		}

		// if the name has changed, consider the tag abandoned (admin is responsible for cleaning it up)
		if tag.Annotations[releaseAnnotationName] != release.Config.Name {
			continue
		}
		if len(phases) > 0 && !stringSliceContains(phases, tag.Annotations[releaseAnnotationPhase]) {
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

func firstTagWithMajorMinorSemanticVersion(versions SemanticVersions, version semver.Version) *SemanticVersion {
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

// sortedReleaseTags returns the tags for a given release in the most appropriate order -
// by creation date for iterative streams, by semantic version for stable streams. If
// phase is specified the list will be filtered.
func sortedReleaseTags(release *Release, phases ...string) []*imagev1.TagReference {
	versions := unsortedSemanticReleaseTags(release, phases...)
	switch release.Config.As {
	case releaseConfigModeStable:
		sort.Sort(versions)
		return versions.Tags()
	default:
		tags := versions.Tags()
		sort.Sort(tagReferencesByAge(tags))
		return tags
	}
}

func stringSliceContains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// sortedRawReleaseTags returns the tags for the given release in order of their creation
// if they are in one of the provided phases. Use sortedReleaseTags if you are trying to get the
// most appropriate recent tag. Intended for use only within the release.
func sortedRawReleaseTags(release *Release, phases ...string) []*imagev1.TagReference {
	var tags []*imagev1.TagReference
	for i := range release.Target.Spec.Tags {
		tag := &release.Target.Spec.Tags[i]
		if tag.Annotations[releaseAnnotationName] != release.Config.Name {
			continue
		}
		if tag.Annotations[releaseAnnotationSource] != fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name) {
			continue
		}
		if stringSliceContains(phases, tag.Annotations[releaseAnnotationPhase]) {
			tags = append(tags, tag)
		}
	}
	sort.Sort(tagReferencesByAge(tags))
	return tags
}
