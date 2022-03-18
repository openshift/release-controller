package v1alpha1helpers

import (
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sort"
)

// byJobStatusCIConfigurationName sorts a list of JobStatus' by their CIConfigurationName
type byJobStatusCIConfigurationName []v1alpha1.JobStatus

func (in byJobStatusCIConfigurationName) Less(i, j int) bool {
	return in[i].CIConfigurationName < in[j].CIConfigurationName
}

func (in byJobStatusCIConfigurationName) Len() int {
	return len(in)
}

func (in byJobStatusCIConfigurationName) Swap(i, j int) {
	in[i], in[j] = in[j], in[i]
}

// byCoordinatesName sorts a list of JobRunResults by their Coordinates Name
type byCoordinatesName []v1alpha1.JobRunResult

func (in byCoordinatesName) Less(i, j int) bool {
	return in[i].Coordinates.Name < in[j].Coordinates.Name
}

func (in byCoordinatesName) Len() int {
	return len(in)
}

func (in byCoordinatesName) Swap(i, j int) {
	in[i], in[j] = in[j], in[i]
}

// byLastTransitionTime sorts a list of ReleasePayloadConditions, descending, by their LastTransitionTime
type byLastTransitionTime []metav1.Condition

func (in byLastTransitionTime) Less(i, j int) bool {
	return in[j].LastTransitionTime.Before(&in[i].LastTransitionTime)
}

func (in byLastTransitionTime) Len() int {
	return len(in)
}

func (in byLastTransitionTime) Swap(i, j int) {
	in[i], in[j] = in[j], in[i]
}

func CanonicalizeReleasePayloadStatus(in *v1alpha1.ReleasePayload) {
	sort.Sort(byJobStatusCIConfigurationName(in.Status.BlockingJobResults))
	CanonicalizeJobRunResults(in.Status.BlockingJobResults)
	sort.Sort(byJobStatusCIConfigurationName(in.Status.InformingJobResults))
	CanonicalizeJobRunResults(in.Status.InformingJobResults)
	sort.Sort(byLastTransitionTime(in.Status.Conditions))
}

func CanonicalizeJobRunResults(jobs []v1alpha1.JobStatus) {
	for _, results := range jobs {
		sort.Sort(byCoordinatesName(results.JobRunResults))
	}
}
