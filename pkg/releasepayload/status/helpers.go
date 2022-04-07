package status

import "github.com/openshift/release-controller/pkg/apis/release/v1alpha1"

func GetJobs(status v1alpha1.ReleasePayloadStatus) []v1alpha1.JobStatus {
	var jobs []v1alpha1.JobStatus
	jobs = append(jobs, status.BlockingJobResults...)
	jobs = append(jobs, status.InformingJobResults...)
	return jobs
}
