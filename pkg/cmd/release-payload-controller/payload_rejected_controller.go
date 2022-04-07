package release_payload_controller

import (
	"context"
	"fmt"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	releasepayloadlister "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/releasepayload/conditions"
	"github.com/openshift/release-controller/pkg/releasepayload/jobstatus"
	releasepayloadhelpers "github.com/openshift/release-controller/pkg/releasepayload/v1alpha1helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
)

// These are valid Reason values for the ReleasePayloadStatus Conditions
const (
	// ReleasePayloadRejectedReason programmatic identifier indicating that the ReleasePayload was Rejected
	ReleasePayloadRejectedReason string = "ReleasePayloadRejected"

	// ReleasePayloadManuallyRejectedReason programmatic identifier indicating that the ReleasePayload was manually Rejected
	ReleasePayloadManuallyRejectedReason string = "ReleasePayloadManuallyRejected"
)

// PayloadRejectedController is responsible for Rejecting ReleasePayloads when either of the following scenarios
// occur:
//   1) any Blocking Job fails
//   2) the payload is manually rejected
// The PayloadRejectedController reads the following pieces of information:
//   - .spec.payloadOverride.override
//   - .status.blockingJobResults
// and populates the following condition:
//   - .status.conditions.PayloadRejected
type PayloadRejectedController struct {
	releasePayloadNamespace string
	releasePayloadLister    releasepayloadlister.ReleasePayloadLister
	releasePayloadClient    releasepayloadclient.ReleaseV1alpha1Interface

	eventRecorder events.Recorder

	cachesToSync []cache.InformerSynced

	queue workqueue.RateLimitingInterface
}

func NewPayloadRejectedController(
	releasePayloadNamespace string,
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	eventRecorder events.Recorder,
) (*PayloadRejectedController, error) {
	c := &PayloadRejectedController{
		releasePayloadNamespace: releasePayloadNamespace,
		releasePayloadLister:    releasePayloadInformer.Lister(),
		releasePayloadClient:    releasePayloadClient,
		eventRecorder:           eventRecorder.WithComponentSuffix("payload-rejected-controller"),
		queue:                   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "PayloadRejectedController"),
	}

	c.cachesToSync = append(c.cachesToSync, releasePayloadInformer.Informer().HasSynced)

	releasePayloadInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.Enqueue,
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.Enqueue(newObj)
		},
		DeleteFunc: c.Enqueue,
	})

	return c, nil
}

func (c *PayloadRejectedController) Enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid queue key '%v': %v", obj, err))
		return
	}
	c.queue.Add(key)
}

func (c *PayloadRejectedController) Run(ctx context.Context) {
	defer utilruntime.HandleCrash()

	klog.Info("Starting Payload Rejected Controller")
	defer func() {
		klog.Info("Shutting down Payload Rejected Controller")
		c.queue.ShutDown()
		klog.Info("Payload Rejected Controller shut down")
	}()

	if !cache.WaitForNamedCacheSync("PayloadRejectedController", ctx.Done(), c.cachesToSync...) {
		return
	}

	go func() {
		wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}()

	<-ctx.Done()
}

func (c *PayloadRejectedController) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
	}
}

func (c *PayloadRejectedController) processNextItem(ctx context.Context) bool {
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

func (c *PayloadRejectedController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting PayloadRejectedController sync")
	defer klog.V(4).Infof("PayloadRejectedController sync done")

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

	rejectedCondition := computeReleasePayloadRejectedCondition(originalReleasePayload)

	releasePayload := originalReleasePayload.DeepCopy()
	conditions.SetCondition(&releasePayload.Status.Conditions, rejectedCondition)
	releasepayloadhelpers.CanonicalizeReleasePayloadStatus(releasePayload)

	if reflect.DeepEqual(originalReleasePayload, releasePayload) {
		return nil
	}

	klog.V(4).Infof("Syncing Payload Rejected for ReleasePayload: %s/%s", releasePayload.Namespace, releasePayload.Name)
	_, err = c.releasePayloadClient.ReleasePayloads(releasePayload.Namespace).UpdateStatus(ctx, releasePayload, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return nil
}

func computeReleasePayloadRejectedCondition(payload *v1alpha1.ReleasePayload) metav1.Condition {
	rejectedCondition := metav1.Condition{
		Type:   v1alpha1.ConditionPayloadRejected,
		Status: metav1.ConditionUnknown,
		Reason: ReleasePayloadRejectedReason,
	}

	// Check for "Accepted" PayloadOverride
	switch payload.Spec.PayloadOverride.Override {
	case v1alpha1.ReleasePayloadOverrideRejected:
		rejectedCondition.Status = metav1.ConditionTrue
		rejectedCondition.Message = payload.Spec.PayloadOverride.Reason
		rejectedCondition.Reason = ReleasePayloadManuallyRejectedReason
		return rejectedCondition
	case v1alpha1.ReleasePayloadOverrideAccepted:
		// If a payload has already been accepted, then we do not want to process it any further...
		rejectedCondition.Status = metav1.ConditionFalse
		rejectedCondition.Message = payload.Spec.PayloadOverride.Reason
		rejectedCondition.Reason = ReleasePayloadManuallyAcceptedReason
		return rejectedCondition
	}

	// Check that all Blocking jobs have completed successfully...
	status := jobstatus.ComputeAggregatedJobState(payload.Status.BlockingJobResults)
	switch status {
	case v1alpha1.JobStateFailure:
		rejectedCondition.Status = metav1.ConditionTrue
	case v1alpha1.JobStateUnknown:
		rejectedCondition.Status = metav1.ConditionUnknown
	default:
		rejectedCondition.Status = metav1.ConditionFalse
	}

	return rejectedCondition
}
