package release_payload_controller

import (
	"context"
	"fmt"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	releasepayloadlister "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/releasepayload/conditions"
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
	// ReleasePayloadCreatedReason programmatic identifier indicating that the ReleasePayload created successfully
	ReleasePayloadCreatedReason string = "ReleasePayloadCreated"

	// ReleasePayloadFailedReason programmatic identifier indicating that the ReleasePayload creation failed
	ReleasePayloadFailedReason string = "ReleasePayloadFailed"
)

// PayloadCreationController is responsible for setting the PayloadCreated and PayloadFailed conditions based
// on whether the release payload creation job completed successfully or not.
// The PayloadCreationController reads the following pieces of information:
//   - .status.releaseCreationJobResult.status
// and populates the following conditions:
//   - .status.conditions.PayloadCreated
//   - .status.conditions.PayloadFailed
type PayloadCreationController struct {
	releasePayloadNamespace string
	releasePayloadLister    releasepayloadlister.ReleasePayloadLister
	releasePayloadClient    releasepayloadclient.ReleaseV1alpha1Interface

	eventRecorder events.Recorder

	cachesToSync []cache.InformerSynced

	queue workqueue.RateLimitingInterface
}

func NewPayloadCreationController(
	releasePayloadNamespace string,
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	eventRecorder events.Recorder,
) (*PayloadCreationController, error) {
	c := &PayloadCreationController{
		releasePayloadNamespace: releasePayloadNamespace,
		releasePayloadLister:    releasePayloadInformer.Lister(),
		releasePayloadClient:    releasePayloadClient,
		eventRecorder:           eventRecorder.WithComponentSuffix("payload-creation-controller"),
		queue:                   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "PayloadCreationController"),
	}

	c.cachesToSync = append(c.cachesToSync, releasePayloadInformer.Informer().HasSynced)

	releasePayloadFilter := func(obj interface{}) bool {
		if releasePayload, ok := obj.(*v1alpha1.ReleasePayload); ok {
			// If the conditions are both in their respective terminal states, then there is nothing else to do...
			if (conditions.IsConditionTrue(releasePayload.Status.Conditions, v1alpha1.ConditionPayloadCreated) ||
				conditions.IsConditionFalse(releasePayload.Status.Conditions, v1alpha1.ConditionPayloadCreated)) &&
				(conditions.IsConditionTrue(releasePayload.Status.Conditions, v1alpha1.ConditionPayloadFailed) ||
					conditions.IsConditionFalse(releasePayload.Status.Conditions, v1alpha1.ConditionPayloadFailed)) {
				return false
			}
			return true
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

func (c *PayloadCreationController) Enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid queue key '%v': %v", obj, err))
		return
	}
	c.queue.Add(key)
}

func (c *PayloadCreationController) Run(ctx context.Context) {
	defer utilruntime.HandleCrash()

	klog.Info("Starting Payload Creation Controller")
	defer func() {
		klog.Info("Shutting down Payload Creation Controller")
		c.queue.ShutDown()
		klog.Info("Payload Creation Controller shut down")
	}()

	if !cache.WaitForNamedCacheSync("PayloadCreationController", ctx.Done(), c.cachesToSync...) {
		return
	}

	go func() {
		wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}()

	<-ctx.Done()
}

func (c *PayloadCreationController) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
	}
}

func (c *PayloadCreationController) processNextItem(ctx context.Context) bool {
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

func (c *PayloadCreationController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting PayloadCreationController sync")
	defer klog.V(4).Infof("PayloadCreationController sync done")

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
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// If the conditions are both in their respective terminal states, then there is nothing else to do...
	if (conditions.IsConditionTrue(originalReleasePayload.Status.Conditions, v1alpha1.ConditionPayloadCreated) ||
		conditions.IsConditionFalse(originalReleasePayload.Status.Conditions, v1alpha1.ConditionPayloadCreated)) &&
		(conditions.IsConditionTrue(originalReleasePayload.Status.Conditions, v1alpha1.ConditionPayloadFailed) ||
			conditions.IsConditionFalse(originalReleasePayload.Status.Conditions, v1alpha1.ConditionPayloadFailed)) {
		return nil
	}

	createdCondition := &metav1.Condition{
		Type:   v1alpha1.ConditionPayloadCreated,
		Status: metav1.ConditionUnknown,
		Reason: ReleasePayloadCreatedReason,
	}
	failedCondition := &metav1.Condition{
		Type:   v1alpha1.ConditionPayloadFailed,
		Status: metav1.ConditionUnknown,
		Reason: ReleasePayloadFailedReason,
	}

	switch originalReleasePayload.Status.ReleaseCreationJobResult.Status {
	case v1alpha1.ReleaseCreationJobSuccess:
		createdCondition.Status = metav1.ConditionTrue
		createdCondition.Message = ReleaseCreationJobSuccessMessage
		failedCondition.Status = metav1.ConditionFalse
		failedCondition.Message = ReleaseCreationJobSuccessMessage
	case v1alpha1.ReleaseCreationJobFailed:
		createdCondition.Status = metav1.ConditionFalse
		createdCondition.Message = ReleaseCreationJobFailureMessage
		failedCondition.Status = metav1.ConditionTrue
		failedCondition.Message = ReleaseCreationJobFailureMessage
	default:
		// Nothing to do here
	}

	releasePayload := originalReleasePayload.DeepCopy()
	conditions.SetCondition(&releasePayload.Status.Conditions, *createdCondition)
	conditions.SetCondition(&releasePayload.Status.Conditions, *failedCondition)
	releasepayloadhelpers.CanonicalizeReleasePayloadStatus(releasePayload)

	if reflect.DeepEqual(originalReleasePayload, releasePayload) {
		return nil
	}

	klog.V(4).Infof("Syncing payload creation for ReleasePayload: %s/%s", releasePayload.Namespace, releasePayload.Name)
	_, err = c.releasePayloadClient.ReleasePayloads(releasePayload.Namespace).UpdateStatus(ctx, releasePayload, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return nil
}
