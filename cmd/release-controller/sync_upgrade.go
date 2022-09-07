package main

import (
	"context"
	"fmt"
	"github.com/blang/semver"
	imagev1 "github.com/openshift/api/image/v1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
	prowapi "k8s.io/test-infra/prow/apis/prowjobs/v1"
	"math/rand"
	"sort"
	"strings"
	"time"
)

var (
	random = rand.New(rand.NewSource(time.Now().UnixNano()))
)

func (c *Controller) ensureReleaseUpgradeJobs(release *releasecontroller.Release, releaseTag *imagev1.TagReference) error {
	// To minimize the blast radius of these jobs, I'm limiting them to only the official ART releases (i.e. Stable)...
	if release.Config.As != releasecontroller.ReleaseConfigModeStable {
		klog.V(4).Infof("Supported upgrade testing is disabled for: %q", release.Config.Name)
		return nil
	}
	platforms, jobs := getUpgradeProwJobs(release)
	if len(platforms) == 0 {
		klog.V(4).Infof("No platforms, for supported upgrade testing, defined: %q", release.Config.Name)
		return nil
	}
	platformDistribution, err := NewRoundRobinClusterDistribution(platforms...)
	if err != nil {
		return fmt.Errorf("unable to determine platform distribution: %v", err)
	}
	pullSpec := releasecontroller.FindPublicImagePullSpec(release.Target, releaseTag.Name)
	if len(pullSpec) == 0 {
		return fmt.Errorf("unable to get determine pullSpec for for release: %s", releaseTag.Name)
	}
	toImage, err := releasecontroller.GetImageInfo(c.releaseInfo, c.architecture, pullSpec)
	if err != nil {
		return fmt.Errorf("unable to get to image info for release %s: %v", releaseTag.Name, err)
	}
	toPullSpec := toImage.GenerateDigestPullSpec()
	tagUpgradeInfo, err := c.releaseInfo.UpgradeInfo(toPullSpec)
	if err != nil {
		return fmt.Errorf("could not get release info for tag %s: %v", toPullSpec, err)
	}
	var supportedUpgrades []string
	if tagUpgradeInfo.Metadata != nil {
		supportedUpgrades = tagUpgradeInfo.Metadata.Previous
	}
	sortedUpgrades := SortedUpgradesByReleaseMap(supportedUpgrades)
	upgradesSample := sortedUpgrades.Sample(3)
	// Get all the currently running prowjobs for this release
	prowJobs := c.getProwJobsForTag(releaseTag.Name)
	for _, previousTag := range upgradesSample {
		verifyName := fmt.Sprintf("upgrade-from-%s", previousTag)
		prowJobNamePrefix := fmt.Sprintf("%s-%s", releaseTag.Name, verifyName)
		// Ensure that only a single upgrade job gets executed per release tag
		if releaseUpgradeJobExists(prowJobs, prowJobNamePrefix) {
			klog.V(6).Infof("Release upgrade job %q already exists.", prowJobNamePrefix)
			continue
		}
		platform := platformDistribution.Get()
		previousReleasePullSpec := generatePullSpec(previousTag, c.architecture)
		klog.V(4).Infof("Testing upgrade from %q to %q on %q", previousReleasePullSpec, pullSpec, platform)
		jobLabels := map[string]string{
			releasecontroller.ReleaseLabelVerify:  "true",
			releasecontroller.ReleaseLabelPayload: releaseTag.Name,
		}
		verifyType := releasecontroller.ReleaseVerification{
			Upgrade: true,
			ProwJob: jobs[platform],
		}
		_, err := c.ensureProwJobForReleaseTag(release, verifyName, platform, verifyType, releaseTag, previousTag, previousReleasePullSpec, jobLabels)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) getProwJobsForTag(tagName string) []prowapi.ProwJob {
	prowJobs := []prowapi.ProwJob{}
	labelSet := labels.Set{
		releasecontroller.ReleaseLabelVerify:  "true",
		releasecontroller.ReleaseLabelPayload: tagName,
	}
	list, err := c.prowClient.List(context.TODO(), metav1.ListOptions{LabelSelector: labels.SelectorFromSet(labelSet).String()})
	if err != nil {
		klog.Errorf("failed to list prowjobs: %v", err)
		return prowJobs
	}
	for _, job := range list.Items {
		prowjob := prowapi.ProwJob{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(job.UnstructuredContent(), &prowjob); err != nil {
			klog.Errorf("failed to convert unstructured prowjob to prowjob type object: %v", err)
			continue
		}
		prowJobs = append(prowJobs, prowjob)
	}
	return prowJobs
}

func releaseUpgradeJobExists(prowJobs []prowapi.ProwJob, prefix string) bool {
	for _, prowJob := range prowJobs {
		if strings.HasPrefix(prowJob.Name, prefix) {
			return true
		}
	}
	return false
}

func generatePullSpec(version, architecture string) string {
	arch := architecture
	switch architecture {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	}
	return fmt.Sprintf("quay.io/openshift-release-dev/ocp-release:%s-%s", version, arch)
}

func getUpgradeProwJobs(release *releasecontroller.Release) ([]string, map[string]*releasecontroller.ProwJobVerification) {
	var keys []string
	prowJobs := make(map[string]*releasecontroller.ProwJobVerification)
	for key, value := range release.Config.Upgrade {
		if value.Disabled {
			continue
		}
		keys = append(keys, key)
		if value.ProwJob != nil {
			prowJobs[key] = value.ProwJob
		}
	}
	return keys, prowJobs
}

type SemanticVersions []semver.Version

func (v SemanticVersions) Less(i, j int) bool {
	if v[i].GT(v[j]) {
		return true
	}
	if v[i].LT(v[j]) {
		return false
	}
	return v[i].String() > v[j].String()
}

func (v SemanticVersions) Len() int {
	return len(v)
}

func (v SemanticVersions) Swap(i, j int) {
	v[i], v[j] = v[j], v[i]
}

type SortedVersionsMap struct {
	SortedKeys []string
	VersionMap map[string]SemanticVersions
}

func (s SortedVersionsMap) Sample(size int) []string {
	var versions []string
	minSize := 3 * size
	for _, release := range s.SortedKeys {
		upgrades := s.VersionMap[release]
		// If there isn't enough edges, just return what we can...
		if len(upgrades) < minSize {
			for _, v := range upgrades {
				versions = append(versions, v.String())
			}
			continue
		}
		// First N versions
		for _, v := range upgrades[:size] {
			versions = append(versions, v.String())
		}
		// Last N versions
		for _, v := range upgrades[len(upgrades)-size:] {
			versions = append(versions, v.String())
		}
		// Random N versions from everything else
		remaining := upgrades[size : len(upgrades)-size]
		for i := 0; i < size; i++ {
			index := random.Intn(len(remaining))
			versions = append(versions, remaining[index].String())
		}
	}

	return versions
}

func SortedUpgradesByReleaseMap(supportedUpgrades []string) SortedVersionsMap {
	releaseBuckets := make(map[string]SemanticVersions)

	for _, v := range supportedUpgrades {
		if version, err := semver.Parse(v); err == nil {
			releaseName := fmt.Sprintf("%d.%02d", version.Major, version.Minor)
			if releaseBuckets[releaseName] == nil {
				releaseBuckets[releaseName] = SemanticVersions{version}
			} else {
				releaseBuckets[releaseName] = append(releaseBuckets[releaseName], version)
			}
		} else {
			klog.Errorf("Unable to parse upgrade version: %q", version)
		}
	}
	var sortedKeys []string
	for key, _ := range releaseBuckets {
		sortedKeys = append(sortedKeys, key)
		sort.Sort(releaseBuckets[key])
	}
	sort.Strings(sortedKeys)

	return SortedVersionsMap{
		SortedKeys: sortedKeys,
		VersionMap: releaseBuckets,
	}
}
