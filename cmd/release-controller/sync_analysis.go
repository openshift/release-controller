package main

import (
	"fmt"
	imagev1 "github.com/openshift/api/image/v1"
	"k8s.io/klog"
)

func (c *Controller) ensureAnalysisJobs(release *Release, releaseTag *imagev1.TagReference) error {
	for name, analysisType := range release.Config.Analysis {
		if analysisType.Disabled {
			klog.V(2).Infof("Release %s analysis step %s is disabled, ignoring", releaseTag.Name, name)
			continue
		}
		if analysisType.AnalysisJobCount == 0 {
			klog.Warningf("Release %s analysis step %s configured without analysisJobCount, ignoring", releaseTag.Name, name)
			continue
		}

		// TODO: How do we want to track these jobs...
		jobLabels := map[string]string{
			"release.openshift.io/analysis": releaseTag.Name,
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
				_, err := c.ensureProwJobForReleaseTag(release, jobName, analysisType, releaseTag, previousTag, previousReleasePullSpec, jobLabels)
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
