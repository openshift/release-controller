package jobstatus

import (
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
)

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

func SetJobStatus(results *[]v1alpha1.JobStatus, newResult v1alpha1.JobStatus) {
	if results == nil {
		results = &[]v1alpha1.JobStatus{}
	}
	existingResult := FindJobStatus(*results, newResult.CIConfigurationName, newResult.CIConfigurationJobName)
	if existingResult == nil {
		*results = append(*results, newResult)
		return
	}

	existingResult.AggregateState = newResult.AggregateState
	existingResult.AnalysisJobCount = newResult.AnalysisJobCount
	existingResult.JobRunResults = newResult.JobRunResults
	existingResult.MaxRetries = newResult.MaxRetries
}

func RemoveJobStatus(results *[]v1alpha1.JobStatus, ciConfigurationName, ciConfigurationJobName string) {
	if results == nil {
		results = &[]v1alpha1.JobStatus{}
	}
	newResults := []v1alpha1.JobStatus{}
	for _, result := range *results {
		if result.CIConfigurationName != ciConfigurationName && result.CIConfigurationJobName != ciConfigurationJobName {
			newResults = append(newResults, result)
		}
	}

	*results = newResults
}

func FindJobStatus(results []v1alpha1.JobStatus, ciConfigurationName, ciConfigurationJobName string) *v1alpha1.JobStatus {
	for i := range results {
		if results[i].CIConfigurationName == ciConfigurationName && results[i].CIConfigurationJobName == ciConfigurationJobName {
			return &results[i]
		}
	}

	return nil
}

func ComputeJobState(jobs []v1alpha1.JobStatus) v1alpha1.JobState {
	totalJobs := len(jobs)
	var pendingJobs, successfulJobs, failedJobs []v1alpha1.JobStatus

	for _, job := range jobs {
		switch job.AggregateState {
		case v1alpha1.JobStatePending:
			pendingJobs = append(pendingJobs, job)
		case v1alpha1.JobStateSuccess:
			successfulJobs = append(successfulJobs, job)
		case v1alpha1.JobStateFailure:
			failedJobs = append(failedJobs, job)
		}
	}

	switch {
	// Any failed jobs mean the release cannot be Accepted
	case len(failedJobs) > 0:
		return v1alpha1.JobStateFailure
	// If everything is successful, then we can Accept the payload
	case len(successfulJobs) == totalJobs && totalJobs > 0:
		return v1alpha1.JobStateSuccess
	// If there are any pending jobs, then we're still working on validating the release
	case len(pendingJobs) > 0:
		return v1alpha1.JobStatePending
	default:
		return v1alpha1.JobStateUnknown
	}
}
