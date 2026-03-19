package releasepayload

import (
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"github.com/openshift/release-controller/pkg/releasepayload/jobrunresult"
	"sort"
)

func GenerateVerificationStatusMap(payload *v1alpha1.ReleasePayload, status *releasecontroller.VerificationStatusMap) bool {
	if status == nil {
		return false
	}
	if *status == nil {
		*status = map[string]*releasecontroller.VerificationStatus{}
	}

	for _, job := range payload.Status.InformingJobResults {
		if result, ok := convertToVerificationStatusMapResult(job); ok {
			(*status)[job.CIConfigurationName] = result
		}
	}

	for _, job := range payload.Status.BlockingJobResults {
		if result, ok := convertToVerificationStatusMapResult(job); ok {
			(*status)[job.CIConfigurationName] = result
		}
	}

	return true
}

func convertToVerificationStatusMapResult(job v1alpha1.JobStatus) (*releasecontroller.VerificationStatus, bool) {
	url := getVerificationStatusUrl(job.JobRunResults)
	state := getVerificationStatusState(job.AggregateState)
	// If the job appears successful but has no URL, it's likely a freshly created job
	// that hasn't had results populated yet — treat it as pending rather than successful.
	if state == releasecontroller.ReleaseVerificationStateSucceeded && url == "" {
		state = releasecontroller.ReleaseVerificationStatePending
	}
	status := &releasecontroller.VerificationStatus{
		Retries:             getVerificationStatusRetries(job),
		PreviousAttemptURLs: getVerificationStatusPreviousAttemptURLs(job.JobRunResults, url),
		State:               state,
		URL:                 url,
	}
	return status, true
}

func getVerificationStatusRetries(job v1alpha1.JobStatus) int {
	// For any individual job, there should always be at least a single JobRunResult.  Retries are consecutive
	// attempts of the initial job that ended in a failure state.  Therefore, the maximum number of JobRunResults
	// should amount to 1 + job.MaxRetries.  This *must* be enforced by the ReleaseController because the
	// ReleasePayloadController does not care.  It simply keeps track of all the results of a job that are
	// attributable back to a particular ReleasePayload.
	results := len(job.JobRunResults)
	maxResults := job.MaxRetries + 1
	switch {
	case results == 0:
		// Nothing to figure out
		return 0
	case results == 1:
		// A singular result without any retries
		return 0
	case results <= maxResults:
		// First result and at least 1 retry
		return results - 1
	case results > job.MaxRetries:
		// More results than are allowed by the ReleaseController
		return job.MaxRetries
	default:
		return 0
	}
}

func getVerificationStatusState(state v1alpha1.JobState) string {
	switch state {
	case v1alpha1.JobStateSuccess:
		return releasecontroller.ReleaseVerificationStateSucceeded
	case v1alpha1.JobStateFailure:
		return releasecontroller.ReleaseVerificationStateFailed
	case v1alpha1.JobStateUnknown:
		// I don't like this, but the existing behavior of the release-controller is to create a "synthetic" job
		// if/when it's impossible to generate the prowjob for the specified verification.  Those jobs will never
		// have any JobRunResults and therefore an "Unknown" JobState. Ultimately, they are considered a "Success"
		// to not Reject the respective release for an invalid configuration...
		return releasecontroller.ReleaseVerificationStateSucceeded
	default:
		return releasecontroller.ReleaseVerificationStatePending
	}
}

func getVerificationStatusPreviousAttemptURLs(jobRunResults []v1alpha1.JobRunResult, primaryURL string) []string {
	if len(jobRunResults) <= 1 {
		return nil
	}
	sorted := make([]v1alpha1.JobRunResult, len(jobRunResults))
	copy(sorted, jobRunResults)
	sort.Sort(jobrunresult.ByStartTime(sorted))

	var urls []string
	for _, result := range sorted {
		if result.HumanProwResultsURL != "" && result.HumanProwResultsURL != primaryURL {
			urls = append(urls, result.HumanProwResultsURL)
		}
	}
	return urls
}

func getVerificationStatusUrl(jobRunResults []v1alpha1.JobRunResult) string {
	// If there was any successful run, then return that URL
	for _, result := range jobRunResults {
		if result.State == v1alpha1.JobRunStateSuccess {
			return result.HumanProwResultsURL
		}
	}
	// Otherwise, return the URL of the "last" attempt
	if len(jobRunResults) >= 1 {
		sort.Sort(jobrunresult.ByStartTime(jobRunResults))
		return jobRunResults[len(jobRunResults)-1].HumanProwResultsURL
	}
	return ""
}
