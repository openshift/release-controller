package release_payload_controller

import (
	"context"
	"fmt"
	"reflect"

	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	releasepayloadhelpers "github.com/openshift/release-controller/pkg/releasepayload/v1alpha1helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
)

// These are valid Reason values for the ReleasePayloadStatus Conditions
const (
	// ReleasePayloadMirroredReason programmatic identifier indicating that the ReleasePayload mirrored successfully
	ReleasePayloadMirroredReason string = "ReleasePayloadMirrored"

	// ReleasePayloadMirrorFailedReason programmatic identifier indicating that the ReleasePayload mirroring failed
	ReleasePayloadMirrorFailedReason string = "ReleasePayloadMirrorFailed"
)

// PayloadMirrorController is responsible for setting the PayloadMirrored and PayloadMirrorFailed conditions based
// on whether the release payload mirror job completed successfully or not.
// The PayloadMirrorController reads the following pieces of information:
//   - .status.releaseMirrorJobResult.status
//
// and populates the following conditions:
//   - .status.conditions.PayloadMirrored
//   - .status.conditions.PayloadMirrorFailed
type PayloadMirrorController struct {
	*ReleasePayloadController
}

func NewPayloadMirrorController(
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	eventRecorder events.Recorder,
) (*PayloadMirrorController, error) {
	c := &PayloadMirrorController{
		ReleasePayloadController: NewReleasePayloadController("Payload Mirror Controller",
			releasePayloadInformer,
			releasePayloadClient,
			eventRecorder.WithComponentSuffix("payload-mirror-controller"),
			workqueue.NewRateLimitingQueueWithConfig(workqueue.DefaultControllerRateLimiter(), workqueue.RateLimitingQueueConfig{Name: "PayloadMirrorController"})),
	}

	c.syncFn = c.sync

	releasePayloadFilter := func(obj any) bool {
		if releasePayload, ok := obj.(*v1alpha1.ReleasePayload); ok {
			// If the conditions are both in their respective terminal states, then there is nothing else to do...
			if (v1helpers.IsConditionTrue(releasePayload.Status.Conditions, v1alpha1.ConditionPayloadMirrored) ||
				v1helpers.IsConditionFalse(releasePayload.Status.Conditions, v1alpha1.ConditionPayloadMirrored)) &&
				(v1helpers.IsConditionTrue(releasePayload.Status.Conditions, v1alpha1.ConditionPayloadMirrorFailed) ||
					v1helpers.IsConditionFalse(releasePayload.Status.Conditions, v1alpha1.ConditionPayloadMirrorFailed)) {
				return false
			}
			return true
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

func (c *PayloadMirrorController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting PayloadMirrorController sync")
	defer klog.V(4).Infof("PayloadMirrorController sync done")

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
	if (v1helpers.IsConditionTrue(originalReleasePayload.Status.Conditions, v1alpha1.ConditionPayloadMirrored) ||
		v1helpers.IsConditionFalse(originalReleasePayload.Status.Conditions, v1alpha1.ConditionPayloadMirrored)) &&
		(v1helpers.IsConditionTrue(originalReleasePayload.Status.Conditions, v1alpha1.ConditionPayloadMirrorFailed) ||
			v1helpers.IsConditionFalse(originalReleasePayload.Status.Conditions, v1alpha1.ConditionPayloadMirrorFailed)) {
		return nil
	}

	createdCondition := &metav1.Condition{
		Type:   v1alpha1.ConditionPayloadMirrored,
		Status: metav1.ConditionUnknown,
		Reason: ReleasePayloadMirroredReason,
	}
	failedCondition := &metav1.Condition{
		Type:   v1alpha1.ConditionPayloadMirrorFailed,
		Status: metav1.ConditionUnknown,
		Reason: ReleasePayloadMirrorFailedReason,
	}

	switch originalReleasePayload.Status.ReleaseMirrorJobResult.Status {
	case v1alpha1.ReleaseMirrorJobSuccess:
		createdCondition.Status = metav1.ConditionTrue
		createdCondition.Message = ReleaseMirrorJobSuccessMessage
		failedCondition.Status = metav1.ConditionFalse
		failedCondition.Message = ReleaseMirrorJobSuccessMessage
	case v1alpha1.ReleaseMirrorJobFailed:
		createdCondition.Status = metav1.ConditionFalse
		createdCondition.Message = ReleaseMirrorJobFailureMessage
		failedCondition.Status = metav1.ConditionTrue
		failedCondition.Message = ReleaseMirrorJobFailureMessage
	default:
		// Nothing to do here
	}

	releasePayload := originalReleasePayload.DeepCopy()
	v1helpers.SetCondition(&releasePayload.Status.Conditions, *createdCondition)
	v1helpers.SetCondition(&releasePayload.Status.Conditions, *failedCondition)
	releasepayloadhelpers.CanonicalizeReleasePayloadStatus(releasePayload)

	if reflect.DeepEqual(originalReleasePayload, releasePayload) {
		return nil
	}

	klog.V(4).Infof("Syncing payload mirror for ReleasePayload: %s/%s", releasePayload.Namespace, releasePayload.Name)
	_, err = c.releasePayloadClient.ReleasePayloads(releasePayload.Namespace).UpdateStatus(ctx, releasePayload, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return nil
}
