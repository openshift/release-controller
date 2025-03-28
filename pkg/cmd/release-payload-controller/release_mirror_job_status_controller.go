package release_payload_controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"github.com/openshift/release-controller/pkg/releasepayload/controller"
	releasepayloadhelpers "github.com/openshift/release-controller/pkg/releasepayload/v1alpha1helpers"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	batchv1informers "k8s.io/client-go/informers/batch/v1"
	batchv1listers "k8s.io/client-go/listers/batch/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
)

const (
	// ReleaseMirrorJobUnknownMessage release mirror job unknown message
	ReleaseMirrorJobUnknownMessage = "Release mirror job unknown"

	// ReleaseMirrorJobPendingMessage release mirror job pending message
	ReleaseMirrorJobPendingMessage = "Release mirror job pending"

	// ReleaseMirrorJobFailureMessage release mirror job failure message
	ReleaseMirrorJobFailureMessage = "Release mirror Job failed"

	// ReleaseMirrorJobSuccessMessage release mirror job success message
	ReleaseMirrorJobSuccessMessage = "Release mirror Job completed"
)

var ErrMirrorCoordinatesNotSet = errors.New("unable to lookup release mirror job: coordinates not set")

// ReleaseMirrorJobStatusController is responsible for watching batchv1.Jobs, in the job-namespace, and
// updating the respective ReleasePayload with the status, of the job, when it completes.
// The ReleaseMirrorJobStatusController watches for changes to the following resources:
//   - batchv1.Jobs
//
// and write the following information:
//   - .status.releaseMirrorJobResult.status
//   - .status.releaseMirrorJobResult.message
type ReleaseMirrorJobStatusController struct {
	*ReleasePayloadController

	batchJobLister batchv1listers.JobLister
}

func NewReleaseMirrorJobStatusController(
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	batchJobInformer batchv1informers.JobInformer,
	eventRecorder events.Recorder,
) (*ReleaseMirrorJobStatusController, error) {
	c := &ReleaseMirrorJobStatusController{
		ReleasePayloadController: NewReleasePayloadController("Release Mirror Job Status Controller",
			releasePayloadInformer,
			releasePayloadClient,
			eventRecorder.WithComponentSuffix("release-mirror-job-status-controller"),
			workqueue.NewRateLimitingQueueWithConfig(workqueue.DefaultControllerRateLimiter(), workqueue.RateLimitingQueueConfig{Name: "ReleaseMirrorJobStatusController"})),
		batchJobLister: batchJobInformer.Lister(),
	}

	c.syncFn = c.sync
	c.cachesToSync = append(c.cachesToSync, batchJobInformer.Informer().HasSynced)

	batchJobFilter := func(obj any) bool {
		if batchJob, ok := obj.(*batchv1.Job); ok {
			if _, ok := batchJob.Annotations[releasecontroller.ReleaseAnnotationReleaseTag]; ok {
				return true
			}
		}
		return false
	}

	if _, err := batchJobInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: batchJobFilter,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.lookupReleasePayload,
			UpdateFunc: func(old, new any) { c.lookupReleasePayload(new) },
			DeleteFunc: c.lookupReleasePayload,
		},
	}); err != nil {
		return nil, fmt.Errorf("Failed to add batch job event handler: %v", err)
	}

	// In case someone/something deletes the ReleaseMirrorJobResult.Status, try and rectify it...
	releasePayloadFilter := func(obj any) bool {
		if releasePayload, ok := obj.(*v1alpha1.ReleasePayload); ok {
			switch {
			// Check that we have the necessary information to proceed
			case len(releasePayload.Status.ReleaseMirrorJobResult.Coordinates.Namespace) == 0 || len(releasePayload.Status.ReleaseMirrorJobResult.Coordinates.Name) == 0:
				return false
			// Check if we need to process this ReleasePayload at all
			case len(releasePayload.Status.ReleaseMirrorJobResult.Status) == 0 || len(releasePayload.Status.ReleaseMirrorJobResult.Message) == 0:
				return true
			}
		}
		return false
	}

	if _, err := releasePayloadInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: releasePayloadFilter,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.Enqueue,
			UpdateFunc: func(old, new any) { c.Enqueue(new) },
			DeleteFunc: c.Enqueue,
		},
	}); err != nil {
		return nil, fmt.Errorf("Failed to add release payload event handler: %v", err)
	}

	return c, nil
}

func (c *ReleaseMirrorJobStatusController) lookupReleasePayload(obj any) {
	object, ok := obj.(runtime.Object)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unable to cast obj: %v", obj))
		return
	}
	target, err := controller.GetAnnotation(object, releasecontroller.ReleaseAnnotationTarget)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to determine releasepayload key: %v", err))
		return
	}
	parts := strings.Split(target, "/")
	if len(parts) != 2 {
		utilruntime.HandleError(fmt.Errorf("invalid target with %d parts: %q", len(parts), target))
		return
	}
	release, err := controller.GetAnnotation(object, releasecontroller.ReleaseAnnotationReleaseTag)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to determine releasepayload key: %v", err))
		return
	}
	releasePayloadKey := fmt.Sprintf("%s/%s", parts[0], release)
	klog.V(4).Infof("Queueing ReleasePayload: %s", releasePayloadKey)
	c.queue.Add(releasePayloadKey)
}

func (c *ReleaseMirrorJobStatusController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting ReleaseMirrorJobStatusController sync")
	defer klog.V(4).Infof("ReleaseMirrorJobStatusController sync done")

	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	klog.V(4).Infof("Processing ReleasePayload: '%s/%s' from workQueue", namespace, name)

	// Get the ReleasePayload resource with this namespace/name
	originalReleasePayload, err := c.releasePayloadLister.ReleasePayloads(namespace).Get(name)
	// The ReleasePayload resource may no longer exist, in which case we stop processing.
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// If the release mirror job status is terminal (Success or Failure) and message is set, then we have nothing else to do
	if (originalReleasePayload.Status.ReleaseMirrorJobResult.Status == v1alpha1.ReleaseMirrorJobSuccess || originalReleasePayload.Status.ReleaseMirrorJobResult.Status == v1alpha1.ReleaseMirrorJobFailed) &&
		originalReleasePayload.Status.ReleaseMirrorJobResult.Message != "" {
		return nil
	}

	if len(originalReleasePayload.Status.ReleaseMirrorJobResult.Coordinates.Namespace) == 0 || len(originalReleasePayload.Status.ReleaseMirrorJobResult.Coordinates.Name) == 0 {
		return ErrMirrorCoordinatesNotSet
	}

	// Lookup the job. If not found, then the status should be unknown...
	// TODO: consider a timeout...
	jobNotFound := false
	job, err := c.batchJobLister.Jobs(originalReleasePayload.Status.ReleaseMirrorJobResult.Coordinates.Namespace).Get(originalReleasePayload.Status.ReleaseMirrorJobResult.Coordinates.Name)
	if k8serrors.IsNotFound(err) {
		klog.V(4).Infof("Unable to locate release mirror job: %s/%s", originalReleasePayload.Status.ReleaseMirrorJobResult.Coordinates.Namespace, originalReleasePayload.Status.ReleaseMirrorJobResult.Coordinates.Name)
		// Reset the error to allow for further processing
		err = nil
		// Set flag to force the logic to set status to "Unknown"
		jobNotFound = true
	}
	if err != nil {
		return err
	}

	releasePayload := originalReleasePayload.DeepCopy()

	// Update the Status and Message of the ReleaseMirrorJobResult
	switch {
	case jobNotFound:
		releasePayload.Status.ReleaseMirrorJobResult.Status = v1alpha1.ReleaseMirrorJobUnknown
		releasePayload.Status.ReleaseMirrorJobResult.Message = ReleaseMirrorJobUnknownMessage
	default:
		releasePayload.Status.ReleaseMirrorJobResult.Status = computeReleaseMirrorJobStatus(job)
		releasePayload.Status.ReleaseMirrorJobResult.Message = computeReleaseMirrorJobMessage(job)
	}

	releasepayloadhelpers.CanonicalizeReleasePayloadStatus(releasePayload)

	if reflect.DeepEqual(originalReleasePayload, releasePayload) {
		return nil
	}

	klog.V(4).Infof("Syncing release mirror job status for ReleasePayload: %s/%s", releasePayload.Namespace, releasePayload.Name)
	_, err = c.releasePayloadClient.ReleasePayloads(releasePayload.Namespace).UpdateStatus(ctx, releasePayload, metav1.UpdateOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return nil
}

func computeReleaseMirrorJobStatus(job *batchv1.Job) v1alpha1.ReleaseMirrorJobStatus {
	if job.Status.CompletionTime != nil {
		return v1alpha1.ReleaseMirrorJobSuccess
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return v1alpha1.ReleaseMirrorJobFailed
		}
	}
	return v1alpha1.ReleaseMirrorJobUnknown
}

func computeReleaseMirrorJobMessage(job *batchv1.Job) string {
	if job.Status.CompletionTime != nil {
		return ReleaseMirrorJobSuccessMessage
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			switch {
			case len(condition.Reason) > 0 && len(condition.Message) > 0:
				return fmt.Sprintf("%s: %s", condition.Reason, condition.Message)
			default:
				return ReleaseMirrorJobFailureMessage
			}
		}
	}
	if (job.Status.Ready != nil && *job.Status.Ready >= 1) || job.Status.Active >= 1 {
		return ReleaseMirrorJobPendingMessage
	}
	return ReleaseMirrorJobUnknownMessage
}
