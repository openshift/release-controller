package release_payload_controller

import (
	"context"
	"errors"
	"fmt"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	releasepayloadlister "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"github.com/openshift/release-controller/pkg/releasepayload/controller"
	releasepayloadhelpers "github.com/openshift/release-controller/pkg/releasepayload/v1alpha1helpers"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	batchv1informers "k8s.io/client-go/informers/batch/v1"
	batchv1listers "k8s.io/client-go/listers/batch/v1"
	"reflect"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
)

const (
	// ReleaseCreationJobUnknownMessage release creation job unknown message
	ReleaseCreationJobUnknownMessage = "Release creation job unknown"

	// ReleaseCreationJobPendingMessage release creation job pending message
	ReleaseCreationJobPendingMessage = "Release creation job pending"

	// ReleaseCreationJobFailureMessage release creation job failure message
	ReleaseCreationJobFailureMessage = "Release creation Job failed"

	// ReleaseCreationJobSuccessMessage release creation job success message
	ReleaseCreationJobSuccessMessage = "Release creation Job completed"
)

var ErrCoordinatesNotSet = errors.New("unable to lookup release creation job: coordinates not set")

// ReleaseCreationStatusController is responsible for watching batchv1.Jobs, in the job-namespace, and
// updating the respective ReleasePayload with the status, of the job, when it completes.
// The ReleaseCreationStatusController watches for changes to the following resources:
//   - batchv1.Jobs
// and write the following information:
//   - .status.releaseCreationJobResult.status
//   - .status.releaseCreationJobResult.message
type ReleaseCreationStatusController struct {
	releasePayloadNamespace string
	releasePayloadLister    releasepayloadlister.ReleasePayloadLister
	releasePayloadClient    releasepayloadclient.ReleaseV1alpha1Interface

	batchJobNamespace string
	batchJobLister    batchv1listers.JobLister

	eventRecorder events.Recorder

	cachesToSync []cache.InformerSynced

	queue workqueue.RateLimitingInterface

	accessor meta.MetadataAccessor
}

func NewReleaseCreationStatusController(
	releasePayloadNamespace string,
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	batchJobNamespace string,
	batchJobInformer batchv1informers.JobInformer,
	eventRecorder events.Recorder,
) (*ReleaseCreationStatusController, error) {
	c := &ReleaseCreationStatusController{
		releasePayloadNamespace: releasePayloadNamespace,
		releasePayloadLister:    releasePayloadInformer.Lister(),
		releasePayloadClient:    releasePayloadClient,
		batchJobNamespace:       batchJobNamespace,
		batchJobLister:          batchJobInformer.Lister(),
		eventRecorder:           eventRecorder.WithComponentSuffix("release-creation-status-controller"),
		queue:                   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ReleaseCreationStatusController"),
	}

	c.accessor = meta.NewAccessor()

	c.cachesToSync = append(c.cachesToSync, releasePayloadInformer.Informer().HasSynced)
	c.cachesToSync = append(c.cachesToSync, batchJobInformer.Informer().HasSynced)

	batchJobFilter := func(obj interface{}) bool {
		if batchJob, ok := obj.(*batchv1.Job); ok {
			if _, ok := batchJob.Annotations[releasecontroller.ReleaseAnnotationReleaseTag]; ok {
				return true
			}
		}
		return false
	}

	batchJobInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: batchJobFilter,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.lookupReleasePayload,
			UpdateFunc: func(old, new interface{}) { c.lookupReleasePayload(new) },
			DeleteFunc: c.lookupReleasePayload,
		},
	})

	// In case someone/something deletes the ReleaseCreationJobResult.Status, try and rectify it...
	releasePayloadFilter := func(obj interface{}) bool {
		if releasePayload, ok := obj.(*v1alpha1.ReleasePayload); ok {
			switch {
			// Check that we have the necessary information to proceed
			case len(releasePayload.Status.ReleaseCreationJobResult.Coordinates.Namespace) == 0 || len(releasePayload.Status.ReleaseCreationJobResult.Coordinates.Name) == 0:
				return false
			// Check if we need to process this ReleasePayload at all
			case len(releasePayload.Status.ReleaseCreationJobResult.Status) == 0 || len(releasePayload.Status.ReleaseCreationJobResult.Message) == 0:
				return true
			}
		}
		return false
	}

	releasePayloadInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: releasePayloadFilter,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.Enqueue,
			UpdateFunc: func(old, new interface{}) { c.Enqueue(new) },
			DeleteFunc: c.Enqueue,
		},
	})

	return c, nil
}

func (c *ReleaseCreationStatusController) Enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid queue key '%v': %v", obj, err))
		return
	}
	c.queue.Add(key)
}

func (c *ReleaseCreationStatusController) lookupReleasePayload(obj interface{}) {
	releasePayloadKey, err := controller.GetReleasePayloadQueueKeyFromAnnotation(obj, releasecontroller.ReleaseAnnotationReleaseTag, c.releasePayloadNamespace)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to determine releasepayload key: %v", err))
		return
	}
	klog.V(4).Infof("Queueing ReleasePayload: %s", releasePayloadKey)
	c.queue.Add(releasePayloadKey)
}

func (c *ReleaseCreationStatusController) Run(ctx context.Context, workers int) {
	defer utilruntime.HandleCrash()

	klog.Info("Starting Release Creation Status Controller")
	defer func() {
		klog.Info("Shutting down Release Creation Status Controller")
		c.queue.ShutDown()
		klog.Info("Release Creation Status Controller shut down")
	}()

	if !cache.WaitForNamedCacheSync("ReleaseCreationStatusController", ctx.Done(), c.cachesToSync...) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	<-ctx.Done()
}

func (c *ReleaseCreationStatusController) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
	}
}

func (c *ReleaseCreationStatusController) processNextItem(ctx context.Context) bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.sync(ctx, key.(string))

	if err == nil {
		c.queue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("%v failed with : %w", key, err))
	c.queue.AddRateLimited(key)

	return true
}

func (c *ReleaseCreationStatusController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting ReleaseCreationStatusController sync")
	defer klog.V(4).Infof("ReleaseCreationStatusController sync done")

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

	// If the release creation job status is terminal (Success), then we have noting else to do
	if originalReleasePayload.Status.ReleaseCreationJobResult.Status == v1alpha1.ReleaseCreationJobSuccess {
		return nil
	}

	if len(originalReleasePayload.Status.ReleaseCreationJobResult.Coordinates.Namespace) == 0 || len(originalReleasePayload.Status.ReleaseCreationJobResult.Coordinates.Name) == 0 {
		return ErrCoordinatesNotSet
	}

	// Lookup the job. If not found, then the status should be unknown...
	// TODO: consider a timeout...
	jobNotFound := false
	job, err := c.batchJobLister.Jobs(originalReleasePayload.Status.ReleaseCreationJobResult.Coordinates.Namespace).Get(originalReleasePayload.Status.ReleaseCreationJobResult.Coordinates.Name)
	if k8serrors.IsNotFound(err) {
		klog.V(4).Infof("Unable to locate release creation job: %s/%s", c.batchJobNamespace, originalReleasePayload.Name)
		// Reset the error to allow for further processing
		err = nil
		// Set flag to force the logic to set status to "Unknown"
		jobNotFound = true
	}
	if err != nil {
		return err
	}

	releasePayload := originalReleasePayload.DeepCopy()

	// Update the Status and Message of the ReleaseCreationJobResult
	switch {
	case jobNotFound:
		releasePayload.Status.ReleaseCreationJobResult.Status = v1alpha1.ReleaseCreationJobUnknown
		releasePayload.Status.ReleaseCreationJobResult.Message = ReleaseCreationJobUnknownMessage
	default:
		releasePayload.Status.ReleaseCreationJobResult.Status = computeReleaseCreationJobStatus(job)
		releasePayload.Status.ReleaseCreationJobResult.Message = computeReleaseCreationJobMessage(job)
	}

	releasepayloadhelpers.CanonicalizeReleasePayloadStatus(releasePayload)

	if reflect.DeepEqual(originalReleasePayload, releasePayload) {
		return nil
	}

	klog.V(4).Infof("Syncing release creation job status for ReleasePayload: %s/%s", releasePayload.Namespace, releasePayload.Name)
	_, err = c.releasePayloadClient.ReleasePayloads(releasePayload.Namespace).UpdateStatus(ctx, releasePayload, metav1.UpdateOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return nil
}

func computeReleaseCreationJobStatus(job *batchv1.Job) v1alpha1.ReleaseCreationJobStatus {
	if job.Status.CompletionTime != nil {
		return v1alpha1.ReleaseCreationJobSuccess
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return v1alpha1.ReleaseCreationJobFailed
		}
	}
	return v1alpha1.ReleaseCreationJobUnknown
}

func computeReleaseCreationJobMessage(job *batchv1.Job) string {
	if job.Status.CompletionTime != nil {
		return ReleaseCreationJobSuccessMessage
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			switch {
			case len(condition.Reason) > 0 && len(condition.Message) > 0:
				return fmt.Sprintf("%s: %s", condition.Reason, condition.Message)
			default:
				return ReleaseCreationJobFailureMessage
			}
		}
	}
	if (job.Status.Ready != nil && *job.Status.Ready >= 1) || job.Status.Active >= 1 {
		return ReleaseCreationJobPendingMessage
	}
	return ReleaseCreationJobUnknownMessage
}
