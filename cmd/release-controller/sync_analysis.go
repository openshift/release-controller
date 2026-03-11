package main

import (
	"fmt"
	"time"

	imagev1 "github.com/openshift/api/image/v1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	prowjobv1 "sigs.k8s.io/prow/pkg/apis/prowjobs/v1"
)

const (
	// defaultAggregateProwJobName the default ProwJob to call if no override is specified
	defaultAggregateProwJobName = "release-openshift-release-analysis-aggregator"
)

// launchAnalysisJobs creates analysis jobs for an aggregated verification step.
// It returns true if all analysis jobs have been created (or already exist), and
// false if some jobs were deferred by splay and still need to be created later.
func (c *Controller) launchAnalysisJobs(release *releasecontroller.Release, verifyName string, verifyType releasecontroller.ReleaseVerification, releaseTag *imagev1.TagReference, previousTag, previousReleasePullSpec string) (bool, error) {
	jobLabels := map[string]string{
		releasecontroller.ProwJobLabelCapability: "rce",
		"release.openshift.io/analysis":          releaseTag.Name,
		releasecontroller.ReleaseLabelPayload:    releaseTag.Name,
	}

	// Update the AnalysisJobCount to no trigger the analysis logic again
	copied := verifyType.DeepCopy()
	copied.AggregatedProwJob.AnalysisJobCount = 0

	allCreated := true
	for i := range verifyType.AggregatedProwJob.AnalysisJobCount {
		// Postfix the name to differentiate it from the aggregator job
		jobNameSuffix := fmt.Sprintf("analysis-%d", i)
		job, err := c.ensureProwJobForReleaseTag(release, verifyName, jobNameSuffix, *copied, releaseTag, previousTag, previousReleasePullSpec, jobLabels)
		if err != nil {
			return false, err
		}
		if job == nil {
			allCreated = false
		}
	}
	return allCreated, nil
}

func addAnalysisEnvToProwJobSpec(spec *prowjobv1.ProwJobSpec, payloadTag, verificationJobName string) (bool, error) {
	if spec.PodSpec == nil {
		// Jenkins jobs cannot be parameterized
		return true, nil
	}
	for i := range spec.PodSpec.Containers {
		c := &spec.PodSpec.Containers[i]
		for j := range c.Env {
			switch name := c.Env[j].Name; name {
			case "PAYLOAD_TAG":
				c.Env[j].Value = payloadTag
			case "VERIFICATION_JOB_NAME":
				c.Env[j].Value = verificationJobName
			case "JOB_START_TIME":
				c.Env[j].Value = time.Now().Format(time.RFC3339)
			}
		}
	}
	return true, nil
}
