package release_payload_controller

import (
	"context"
	"fmt"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/releasepayload/jobstatus"
	releasepayloadhelpers "github.com/openshift/release-controller/pkg/releasepayload/v1alpha1helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"reflect"

	"github.com/openshift/library-go/pkg/operator/events"
)

// JobStateController is responsible for updating the JobState for every entry in the
// BlockingJobResults and InformingJobResults lists.
// The JobStateController reads the following pieces of information:
//   - .status.blockingJobResults[] | .results[]
//   - .status.informingJobResults[] | .results[]
//   - .status.upgradeJobResults[] | .results[]
//
// and populates the following condition:
//   - .status.blockingJobResults[] | .state
//   - .status.informingJobResults[] | .state
//   - .status.upgradeJobResults[] | .state
type JobStateController struct {
	*ReleasePayloadController
}

func NewJobStateController(
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	eventRecorder events.Recorder,
) (*JobStateController, error) {
	c := &JobStateController{
		ReleasePayloadController: NewReleasePayloadController("Job State Controller",
			releasePayloadInformer,
			releasePayloadClient,
			eventRecorder.WithComponentSuffix("job-state-controller"),
			workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "JobStateController")),
	}

	c.syncFn = c.sync

	releasePayloadInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: c.Enqueue,
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.Enqueue(newObj)
		},
		DeleteFunc: c.Enqueue,
	})

	return c, nil
}

func (c *JobStateController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting JobStateController sync")
	defer klog.V(4).Infof("JobStateController sync done")

	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the ReleasePayload resource with this namespace/name
	originalReleasePayload, err := c.releasePayloadLister.ReleasePayloads(namespace).Get(name)
	// The ReleasePayload resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// TODO: at larger scales, we need to figure out if we need to change a value before the deepcopy
	releasePayload := originalReleasePayload.DeepCopy()

	// Update the BlockingJobResults
	for _, job := range releasePayload.Status.BlockingJobResults {
		job.AggregateState = computeJobState(job)
		jobstatus.SetJobStatus(&releasePayload.Status.BlockingJobResults, job)
	}

	// Update the InformingJobResults
	for _, job := range releasePayload.Status.InformingJobResults {
		job.AggregateState = computeJobState(job)
		jobstatus.SetJobStatus(&releasePayload.Status.InformingJobResults, job)
	}

	// Update the UpgradeJobResults
	for _, job := range releasePayload.Status.UpgradeJobResults {
		job.AggregateState = computeJobState(job)
		jobstatus.SetJobStatus(&releasePayload.Status.UpgradeJobResults, job)
	}

	releasepayloadhelpers.CanonicalizeReleasePayloadStatus(releasePayload)

	if reflect.DeepEqual(originalReleasePayload, releasePayload) {
		return nil
	}

	klog.V(4).Infof("Syncing Job State for ReleasePayload: %s/%s", releasePayload.Namespace, releasePayload.Name)
	_, err = c.releasePayloadClient.ReleasePayloads(releasePayload.Namespace).UpdateStatus(ctx, releasePayload, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return nil
}

// computeJobState analyzes the specified job to determine which type of JobRunResults to process and returns
// the corresponding JobState.
func computeJobState(job v1alpha1.JobStatus) v1alpha1.JobState {
	attempts := len(job.JobRunResults)

	// If there are no results, then there is nothing to do...
	if attempts == 0 {
		return v1alpha1.JobStateUnknown
	}

	switch {
	case job.AnalysisJobCount > 0:
		return processAnalysisJobResults(job.JobRunResults, job.AnalysisJobCount)
	case job.MaxRetries > 0:
		return processRetryJobResults(job.JobRunResults, job.MaxRetries)
	default:
		return processJobResults(job.JobRunResults)
	}
}

// interrogateJobResults organizes JobRunResults into categorized slices for further processing.  The individual jobs
// are sorted into the following categories: Pending, Successful, Failed, and Unknown.
func interrogateJobResults(results []v1alpha1.JobRunResult) (pendingJobs, successfulJobs, failedJobs, unknownJobs []v1alpha1.JobRunResult) {
	for _, result := range results {
		switch result.State {
		case v1alpha1.JobRunStatePending, v1alpha1.JobRunStateTriggered, v1alpha1.JobRunStateScheduling:
			pendingJobs = append(pendingJobs, result)
		case v1alpha1.JobRunStateSuccess:
			successfulJobs = append(successfulJobs, result)
		case v1alpha1.JobRunStateFailure, v1alpha1.JobRunStateAborted, v1alpha1.JobRunStateError:
			failedJobs = append(failedJobs, result)
		default:
			unknownJobs = append(unknownJobs, result)
		}
	}

	return pendingJobs, successfulJobs, failedJobs, unknownJobs
}

// processJobResults processes JobRunResults for jobs that are not configured as analysis jobs or
// that have retries.  Any Successful result of this class of jobs returns a "Success" JobState.
func processJobResults(results []v1alpha1.JobRunResult) v1alpha1.JobState {
	var pendingJobs, successfulJobs, failedJobs, _ = interrogateJobResults(results)

	switch {
	case len(successfulJobs) > 0:
		return v1alpha1.JobStateSuccess
	case len(pendingJobs) > 0:
		return v1alpha1.JobStatePending
	case len(failedJobs) > 0:
		return v1alpha1.JobStateFailure
	default:
		return v1alpha1.JobStateUnknown
	}
}

const (
	// analysisSuccessThreshold represents the threshold that must be reached or exceeded for the
	// corresponding analysis jobs to be marked successful
	analysisSuccessThreshold = 0.6
)

// processAnalysisJobResults processes JobRunResults for analysis jobs.  The JobState will be
// "Unknown" until the number of JobRunResults equals count.  While any of the JobRunResults are
// still running (pending), the JobState will be "Pending".  Once all the JobRunResults are complete,
// a success rate, that is greater-than or equal to analysisSuccessThreshold, will return a "Success"
// JobState.  Anything less-than analysisSuccessThreshold will return a "Failure" JobState.
func processAnalysisJobResults(results []v1alpha1.JobRunResult, count int) v1alpha1.JobState {
	var pendingJobs, successfulJobs, _, _ = interrogateJobResults(results)

	switch {
	case len(results) < count:
		return v1alpha1.JobStateUnknown
	case (float64(len(successfulJobs)) / float64(count)) >= analysisSuccessThreshold:
		return v1alpha1.JobStateSuccess
	case len(pendingJobs) > 0:
		return v1alpha1.JobStatePending
	default:
		return v1alpha1.JobStateFailure
	}
}

// processRetryJobResults processes JobRunResults for jobs that can be retried.  Any successful
// JobRunResult will return a "Success" JobStatus.  As long as the number of attempts is less than
// count and each attempt results in a failure, the JobStatus will return "Pending".  If all retry
// attempts have failed, the JobStatus will be "Failed".
func processRetryJobResults(results []v1alpha1.JobRunResult, count int) v1alpha1.JobState {
	attempts := len(results)
	// For any individual job, there should always be at least a single JobRunResult.  Retries are consecutive
	// attempts of the initial job that ended in a failure state.  Therefore, the maximum number of JobRunResults
	// should amount to 1 + job.MaxRetries.  This *must* be enforced by the ReleaseController because the
	// ReleasePayloadController does not care.  It simply keeps track of all the results of a job that are
	// attributable back to a particular ReleasePayload.
	maxResults := count + 1

	var pendingJobs, successfulJobs, failedJobs, _ = interrogateJobResults(results)

	switch {
	case len(successfulJobs) > 0:
		return v1alpha1.JobStateSuccess
	case len(pendingJobs) > 0:
		return v1alpha1.JobStatePending
	case len(failedJobs) > 0:
		switch {
		case attempts < maxResults:
			return v1alpha1.JobStatePending
		default:
			return v1alpha1.JobStateFailure
		}
	default:
		return v1alpha1.JobStateUnknown
	}
}
