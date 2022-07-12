package main

import (
	"fmt"
	imagev1 "github.com/openshift/api/image/v1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"k8s.io/klog"
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
	for _, previousTag := range supportedUpgrades {
		platform := platformDistribution.Get()
		previousReleasePullSpec := generatePullSpec(previousTag, c.architecture)
		klog.V(4).Infof("Testing upgrade from %q to %q on %q", previousReleasePullSpec, pullSpec, platform)
		jobLabels := map[string]string{
			releasecontroller.ReleaseLabelVerify:  "true",
			releasecontroller.ReleaseLabelPayload: releaseTag.Name,
		}
		name := fmt.Sprintf("upgrade-from-%s", previousTag)
		jobNameSuffix := platform
		verifyType := releasecontroller.ReleaseVerification{
			Upgrade: true,
			ProwJob: jobs[platform],
		}
		_, err := c.ensureProwJobForReleaseTag(release, name, jobNameSuffix, verifyType, releaseTag, previousTag, previousReleasePullSpec, jobLabels)
		if err != nil {
			return err
		}
	}
	return nil
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
