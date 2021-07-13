package main

import (
	"fmt"
	imagev1 "github.com/openshift/api/image/v1"
	prowjobv1 "k8s.io/test-infra/prow/apis/prowjobs/v1"
	"time"
)

const (
	// defaultAggregateProwJobName the default ProwJob to call if no override is specified
	defaultAggregateProwJobName = "release-openshift-release-analysis-aggregator"
)

func (c *Controller) launchAnalysisJobs(release *Release, verifyName string, verifyType ReleaseVerification, releaseTag *imagev1.TagReference, previousTag, previousReleasePullSpec string) error {
	jobLabels := map[string]string{
		"release.openshift.io/analysis": releaseTag.Name,
	}

	// Update the AnalysisJobCount to no trigger the analysis logic again
	copied := verifyType.DeepCopy()
	copied.AggregatedProwJob.AnalysisJobCount = 0

	for i := 0; i < verifyType.AggregatedProwJob.AnalysisJobCount; i++ {
		// Postfix the name to differentiate it from the aggregator job
		jobName := fmt.Sprintf("%s-analysis-%d", verifyName, i)
		_, err := c.ensureProwJobForReleaseTag(release, jobName, *copied, releaseTag, previousTag, previousReleasePullSpec, jobLabels)
		if err != nil {
			return err
		}
	}
	return true, nil
}

func findStatusTag(is *imagev1.ImageStream, name string) *imagev1.TagEvent {
	for i := range is.Status.Tags {
		tag := &is.Status.Tags[i]
		if tag.Tag == name {
			if len(tag.Items) == 0 {
				return nil
			}
			if len(tag.Conditions) > 0 {
				if isTagEventConditionNotImported(tag) {
					return nil
				}
			}
			if specTag := findSpecTag(is.Spec.Tags, name); specTag != nil && (specTag.Generation == nil || *specTag.Generation > tag.Items[0].Generation) {
				return nil
			}
			return &tag.Items[0]
		}
	}
	return nil
}

// ensureAnalysisJobs creates and instantiates the specified number of occurrences of the release verification job for
// a given release.  The jobs can be tracked via its labels and/or annotations:
// Labels:
//     "release.openshift.io/analysis": the name of the release tag (i.e. 4.9.0-0.nightly-2021-07-12-202251)
// Annotations:
//     "release.openshift.io/image": the SHA value of the release
//     "release.openshift.io/dockerImageReference": the pull spec of the release
func (c *Controller) ensureAnalysisJobs(release *Release, releaseTag *imagev1.TagReference) error {
	statusTag := findStatusTag(release.Target, releaseTag.Name)
	if statusTag == nil {
		klog.V(2).Infof("Waiting for release %s to be imported before we can retrieve metadata", releaseTag.Name)
		return nil
	}
	for name, analysisType := range release.Config.Analysis {
		if analysisType.Disabled {
			klog.V(2).Infof("Release %s analysis step %s is disabled, ignoring", releaseTag.Name, name)
			continue
		}
		if analysisType.AnalysisJobCount <= 0 {
			klog.Warningf("Release %s analysis step %s configured without analysisJobCount, ignoring", releaseTag.Name, name)
			continue
		}
		jobCount := analysisType.AnalysisJobCount
		if jobCount > 20 {
			jobCount = 20
			klog.Warningf("Release %s analysis step %s configured with greater than maximum number of jobs: %d.  Running %d instances", releaseTag.Name, name, analysisType.AnalysisJobCount, jobCount)
		}
	}
	return nil
}

func addAnalysisEnvToProwJobSpec(spec *prowjobv1.ProwJobSpec, payloadTag, verificationJobName string) (bool, error) {
	if spec.PodSpec == nil {
		// Jenkins jobs cannot be parameterized
		return true, nil
	}
	for i := range spec.PodSpec.Containers {
		c := &spec.PodSpec.Containers[i]
		for j := range c.Env {
			switch name := c.Env[j].Name; {
			case name == "PAYLOAD_TAG":
				c.Env[j].Value = payloadTag
			case name == "VERIFICATION_JOB_NAME":
				c.Env[j].Value = verificationJobName
			case name == "JOB_START_TIME":
				c.Env[j].Value = time.Now().Format(time.RFC3339)
			}
		}
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
			for i := 1; i <= jobCount; i++ {
				jobName := fmt.Sprintf("%s-analysis-%d", name, i)
				_, err := c.ensureProwJobForReleaseTag(release, jobName, analysisType, releaseTag, previousTag, previousReleasePullSpec, jobLabels, jobAnnotations)
				if err != nil {
					return err
				}
			}
		}
	}
	return true, nil
}
