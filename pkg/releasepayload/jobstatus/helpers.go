package jobstatus

import "github.com/openshift/release-controller/pkg/apis/release/v1alpha1"

// ByJobStatusCIConfigurationName sorts a list of JobStatus' by their CIConfigurationName
type ByJobStatusCIConfigurationName []v1alpha1.JobStatus

func (in ByJobStatusCIConfigurationName) Less(i, j int) bool {
	return in[i].CIConfigurationName < in[j].CIConfigurationName
}

func (in ByJobStatusCIConfigurationName) Len() int {
	return len(in)
}

func (in ByJobStatusCIConfigurationName) Swap(i, j int) {
	in[i], in[j] = in[j], in[i]
}

func ComputeAggregatedJobState(jobs []v1alpha1.JobStatus) v1alpha1.JobState {
	totalJobs := len(jobs)
	var pendingJobs, successfulJobs, failedJobs, unknownJobs []v1alpha1.JobStatus

	for _, job := range jobs {
		switch job.AggregateState {
		case v1alpha1.JobStatePending:
			pendingJobs = append(pendingJobs, job)
		case v1alpha1.JobStateSuccess:
			successfulJobs = append(successfulJobs, job)
		case v1alpha1.JobStateFailure:
			failedJobs = append(failedJobs, job)
		default:
			unknownJobs = append(unknownJobs, job)
		}
	}

	switch {
	// Any failed jobs mean the release cannot be Accepted
	case len(failedJobs) > 0:
		return v1alpha1.JobStateFailure
	// If everything is successful, then we can Accept the payload
	case len(successfulJobs) == totalJobs:
		return v1alpha1.JobStateSuccess
	// If there are any pending jobs, then we're still working on validating the release
	case len(pendingJobs) > 0:
		return v1alpha1.JobStatePending
	default:
		return v1alpha1.JobStateUnknown
	}
}
