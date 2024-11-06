package v1alpha1helpers

import (
	"sort"

	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/releasepayload/conditions"
	"github.com/openshift/release-controller/pkg/releasepayload/jobrunresult"
	"github.com/openshift/release-controller/pkg/releasepayload/jobstatus"
)

func CanonicalizeReleasePayloadStatus(in *v1alpha1.ReleasePayload) {
	sort.Sort(jobstatus.ByJobStatusCIConfigurationName(in.Status.BlockingJobResults))
	CanonicalizeJobRunResults(in.Status.BlockingJobResults)
	sort.Sort(jobstatus.ByJobStatusCIConfigurationName(in.Status.InformingJobResults))
	CanonicalizeJobRunResults(in.Status.InformingJobResults)
	sort.Sort(jobstatus.ByJobStatusCIConfigurationName(in.Status.UpgradeJobResults))
	CanonicalizeJobRunResults(in.Status.UpgradeJobResults)
	sort.Sort(conditions.ByReleasePayloadConditionType(in.Status.Conditions))
}

func CanonicalizeJobRunResults(jobs []v1alpha1.JobStatus) {
	for _, results := range jobs {
		sort.Sort(jobrunresult.ByCoordinatesName(results.JobRunResults))
	}
}
