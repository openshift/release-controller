package release_payload_controller

import (
	"context"
	"fmt"
	"reflect"

	"github.com/openshift/library-go/pkg/operator/v1helpers"
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

	"github.com/openshift/library-go/pkg/operator/events"
)

// These are valid Reason values for the ReleasePayloadStatus Conditions
const (
	// ReleasePayloadRejectedReason programmatic identifier indicating that the ReleasePayload was Rejected
	ReleasePayloadRejectedReason string = "ReleasePayloadRejected"

	// ReleasePayloadManuallyRejectedReason programmatic identifier indicating that the ReleasePayload was manually Rejected
	ReleasePayloadManuallyRejectedReason string = "ReleasePayloadManuallyRejected"
)

// PayloadRejectedController is responsible for Rejecting ReleasePayloads when any of the following scenarios
// occur:
//  1. any Blocking Job fails
//  2. the payload is manually rejected
//  2. the release creation job fails
//
// The PayloadRejectedController reads the following pieces of information:
//   - .spec.payloadOverride.override
//   - .status.blockingJobResults
//   - .status.releaseCreationJobResult.status
//   - .status.releaseMirrorJobResult.status
//
// and populates the following condition:
//   - .status.conditions.PayloadRejected
type PayloadRejectedController struct {
	*ReleasePayloadController
}

func NewPayloadRejectedController(
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	eventRecorder events.Recorder,
) (*PayloadRejectedController, error) {
	c := &PayloadRejectedController{
		ReleasePayloadController: NewReleasePayloadController("Payload Rejected Controller",
			releasePayloadInformer,
			releasePayloadClient,
			eventRecorder.WithComponentSuffix("payload-rejected-controller"),
			workqueue.NewTypedRateLimitingQueueWithConfig(workqueue.DefaultTypedControllerRateLimiter[string](), workqueue.TypedRateLimitingQueueConfig[string]{Name: "PayloadRejectedController"})),
	}

	c.syncFn = c.sync

	if _, err := releasePayloadInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: c.Enqueue,
		UpdateFunc: func(oldObj, newObj any) {
			c.Enqueue(newObj)
		},
		DeleteFunc: c.Enqueue,
	}); err != nil {
		return nil, fmt.Errorf("failed to add release payload event handler: %v", err)
	}

	return c, nil
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
	v1helpers.SetCondition(&releasePayload.Status.Conditions, rejectedCondition)
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

	// If the release creation job failed, then the payload should be Rejected
	if payload.Status.ReleaseCreationJobResult.Status == v1alpha1.ReleaseCreationJobFailed {
		rejectedCondition.Status = metav1.ConditionTrue
		rejectedCondition.Reason = ReleasePayloadCreationJobFailedReason
		rejectedCondition.Message = payload.Status.ReleaseCreationJobResult.Message
		return rejectedCondition
	}

	// The release creation job must succeed before rejection can be evaluated,
	// including manual overrides — there is no physical release to reject until
	// the creation job completes.
	if payload.Status.ReleaseCreationJobResult.Status != v1alpha1.ReleaseCreationJobSuccess {
		return rejectedCondition
	}

	// When a mirror job is configured, it must reach a terminal state before
	// rejection can be evaluated — including manual overrides, since the
	// images need to be backed up before any decision is made. A failed
	// mirror job does not cause rejection.
	// Skip this gate if the payload has already been accepted or rejected —
	// retroactively reverting a terminal release due to a stale mirror status
	// would be disruptive for payloads that predate this check.
	if payload.Spec.PayloadCreationConfig.ReleaseMirrorCoordinates.ReleaseMirrorJobName != "" &&
		!v1helpers.IsConditionTrue(payload.Status.Conditions, v1alpha1.ConditionPayloadAccepted) &&
		!v1helpers.IsConditionTrue(payload.Status.Conditions, v1alpha1.ConditionPayloadRejected) {
		if payload.Status.ReleaseMirrorJobResult.Status != v1alpha1.ReleaseMirrorJobSuccess &&
			payload.Status.ReleaseMirrorJobResult.Status != v1alpha1.ReleaseMirrorJobFailed {
			return rejectedCondition
		}
	}

	// Check for PayloadOverride
	switch payload.Spec.PayloadOverride.Override {
	case v1alpha1.ReleasePayloadOverrideRejected:
		rejectedCondition.Status = metav1.ConditionTrue
		rejectedCondition.Message = payload.Spec.PayloadOverride.Reason
		rejectedCondition.Reason = ReleasePayloadManuallyRejectedReason
		return rejectedCondition
	case v1alpha1.ReleasePayloadOverrideAccepted:
		rejectedCondition.Status = metav1.ConditionFalse
		rejectedCondition.Message = payload.Spec.PayloadOverride.Reason
		rejectedCondition.Reason = ReleasePayloadManuallyAcceptedReason
		return rejectedCondition
	}

	// Check that all Blocking jobs have completed successfully...
	status := jobstatus.ComputeJobState(payload.Status.BlockingJobResults)
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
