package v1alpha1helpers

import (
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/releasepayload/conditions"
	"github.com/openshift/release-controller/pkg/releasepayload/jobrunresult"
	"github.com/openshift/release-controller/pkg/releasepayload/jobstatus"
	"sort"
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

// ByCIConfigurationCIConfigurationName sorts a list of CIConfiguration's by their CIConfigurationName
type ByCIConfigurationCIConfigurationName []v1alpha1.CIConfiguration

func (in ByCIConfigurationCIConfigurationName) Less(i, j int) bool {
	return in[i].CIConfigurationName < in[j].CIConfigurationName
}

func (in ByCIConfigurationCIConfigurationName) Len() int {
	return len(in)
}

func (in ByCIConfigurationCIConfigurationName) Swap(i, j int) {
	in[i], in[j] = in[j], in[i]
}
