package main

import (
	"fmt"
	imagev1 "github.com/openshift/api/image/v1"
	"k8s.io/klog"
)

const (
	maximumAnalysisJobCount = 20
)

// ensureAnalysisJobs creates and instantiates the specified number of occurrences of the release verification job for
// a given release.  The jobs can be tracked via its labels and/or annotations:
// Labels:
//     "release.openshift.io/analysis": the name of the release tag (i.e. 4.9.0-0.nightly-2021-07-12-202251)
// Annotations:
//     "release.openshift.io/image": the SHA value of the release
//     "release.openshift.io/dockerImageReference": the pull spec of the release
func (c *Controller) ensureAnalysisJobs(release *Release, releaseTag *imagev1.TagReference) error {
	statusTag := findImportedCurrentStatusTag(release.Target, releaseTag.Name)
	if statusTag == nil {
		klog.V(2).Infof("Waiting for release %s to be imported before we can retrieve metadata", releaseTag.Name)
		return nil
	}
	for name, analysisType := range release.Config.Analysis {
		if analysisType.Disabled {
			klog.V(2).Infof("%s: Release analysis step %s is disabled, ignoring", release.Config.MirrorPrefix, name)
			continue
		}
		if analysisType.AnalysisJobCount <= 0 {
			klog.Warningf("%s: Release analysis step %s configured without analysisJobCount, ignoring", release.Config.MirrorPrefix, name)
			continue
		}
		if analysisType.AnalysisJobCount > maximumAnalysisJobCount {
			klog.Warningf("%s: Release analysis step %s analysisJobCount (%d) exceeds the maximum number of jobs (%d), ignoring ", release.Config.MirrorPrefix, name, analysisType.AnalysisJobCount, maximumAnalysisJobCount)
			continue
		}

		// TODO: How do we want to track these jobs...
		jobLabels := map[string]string{
			"release.openshift.io/analysis": releaseTag.Name,
		}
		jobAnnotations := map[string]string{
			"release.openshift.io/image":                statusTag.Image,
			"release.openshift.io/dockerImageReference": statusTag.DockerImageReference,
		}

		switch {
		case analysisType.ProwJob != nil:
			// if this is an upgrade job, find the appropriate source for the upgrade job
			var previousTag, previousReleasePullSpec string
			if analysisType.Upgrade {
				var err error
				previousTag, previousReleasePullSpec, err = c.getUpgradeTagAndPullSpec(release, releaseTag, name, analysisType.UpgradeFrom, analysisType.UpgradeFromRelease, false)
				if err != nil {
					return err
				}
			}
			for i := 1; i <= analysisType.AnalysisJobCount; i++ {
				jobName := fmt.Sprintf("%s-analysis-%d", name, i)
				_, err := c.ensureProwJobForReleaseTag(release, jobName, analysisType, releaseTag, previousTag, previousReleasePullSpec, jobLabels, jobAnnotations)
				if err != nil {
					return err
				}
			}
		default:
			// manual verification
		}
	}
	return nil
}
