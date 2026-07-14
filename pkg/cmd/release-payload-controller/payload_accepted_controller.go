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
	// ReleasePayloadAcceptedReason programmatic identifier indicating that the ReleasePayload was Accepted
	ReleasePayloadAcceptedReason string = "ReleasePayloadAccepted"

	// ReleasePayloadManuallyAcceptedReason programmatic identifier indicating that the ReleasePayload was manually Accepted
	ReleasePayloadManuallyAcceptedReason string = "ReleasePayloadManuallyAccepted"
)

// PayloadAcceptedController is responsible for Accepting ReleasePayloads when any of the following scenarios
// occur:
//  1. all Blocking Jobs complete successfully
//  2. the payload is manually accepted
//
// The PayloadAcceptedController reads the following pieces of information:
//   - .spec.payloadOverride.override
//   - .status.blockingJobResults
//   - .status.releaseCreationJobResult.status
//   - .status.releaseMirrorJobResult.status
//
// and populates the following condition:
//   - .status.conditions.PayloadAccepted
type PayloadAcceptedController struct {
	*ReleasePayloadController
}

func NewPayloadAcceptedController(
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	eventRecorder events.Recorder,
) (*PayloadAcceptedController, error) {
	c := &PayloadAcceptedController{
		ReleasePayloadController: NewReleasePayloadController("Payload Accepted Controller",
			releasePayloadInformer,
			releasePayloadClient,
			eventRecorder.WithComponentSuffix("payload-accepted-controller"),
			workqueue.NewTypedRateLimitingQueueWithConfig(workqueue.DefaultTypedControllerRateLimiter[string](), workqueue.TypedRateLimitingQueueConfig[string]{Name: "PayloadAcceptedController"})),
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

func (c *PayloadAcceptedController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting PayloadAcceptedController sync")
	defer klog.V(4).Infof("PayloadAcceptedController sync done")

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

	acceptedCondition := computeReleasePayloadAcceptedCondition(originalReleasePayload)

	releasePayload := originalReleasePayload.DeepCopy()
	v1helpers.SetCondition(&releasePayload.Status.Conditions, acceptedCondition)
	releasepayloadhelpers.CanonicalizeReleasePayloadStatus(releasePayload)

	if reflect.DeepEqual(originalReleasePayload, releasePayload) {
		return nil
	}

	klog.V(4).Infof("Syncing Payload Accepted for ReleasePayload: %s/%s", releasePayload.Namespace, releasePayload.Name)
	_, err = c.releasePayloadClient.ReleasePayloads(releasePayload.Namespace).UpdateStatus(ctx, releasePayload, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return nil
}

func computeReleasePayloadAcceptedCondition(payload *v1alpha1.ReleasePayload) metav1.Condition {
	acceptedCondition := metav1.Condition{
		Type:   v1alpha1.ConditionPayloadAccepted,
		Status: metav1.ConditionUnknown,
		Reason: ReleasePayloadAcceptedReason,
	}

	// The release creation job must complete successfully before acceptance can
	// be considered — including manual overrides, since there is no physical
	// release to accept until the creation job succeeds.
	if payload.Status.ReleaseCreationJobResult.Status == v1alpha1.ReleaseCreationJobFailed {
		acceptedCondition.Status = metav1.ConditionFalse
		acceptedCondition.Reason = ReleasePayloadCreationJobFailedReason
		acceptedCondition.Message = payload.Status.ReleaseCreationJobResult.Message
		return acceptedCondition
	}
	if payload.Status.ReleaseCreationJobResult.Status != v1alpha1.ReleaseCreationJobSuccess {
		return acceptedCondition
	}

	// When a mirror job is configured, it must reach a terminal state before
	// acceptance can be evaluated — including manual overrides, since the
	// images need to be backed up before any decision is made. A failed
	// mirror job does not block acceptance.
	// Skip this gate if the payload has already been accepted or rejected —
	// retroactively reverting a terminal release due to a stale mirror status
	// would be disruptive for payloads that predate this check.
	if payload.Spec.PayloadCreationConfig.ReleaseMirrorCoordinates.ReleaseMirrorJobName != "" &&
		!v1helpers.IsConditionTrue(payload.Status.Conditions, v1alpha1.ConditionPayloadAccepted) &&
		!v1helpers.IsConditionTrue(payload.Status.Conditions, v1alpha1.ConditionPayloadRejected) {
		if payload.Status.ReleaseMirrorJobResult.Status != v1alpha1.ReleaseMirrorJobSuccess &&
			payload.Status.ReleaseMirrorJobResult.Status != v1alpha1.ReleaseMirrorJobFailed {
			return acceptedCondition
		}
	}

	// Check for "Accepted" PayloadOverride
	switch payload.Spec.PayloadOverride.Override {
	case v1alpha1.ReleasePayloadOverrideAccepted:
		acceptedCondition.Status = metav1.ConditionTrue
		acceptedCondition.Message = payload.Spec.PayloadOverride.Reason
		acceptedCondition.Reason = ReleasePayloadManuallyAcceptedReason
		return acceptedCondition
	case v1alpha1.ReleasePayloadOverrideRejected:
		acceptedCondition.Status = metav1.ConditionFalse
		acceptedCondition.Message = payload.Spec.PayloadOverride.Reason
		acceptedCondition.Reason = ReleasePayloadManuallyRejectedReason
		return acceptedCondition
	}

	// When there are no blocking jobs, wait for all informing/upgrade jobs to
	// reach a terminal state before accepting. A job is terminal when its
	// aggregate state is Success or Failure. Any other state (Pending, Unknown,
	// or unset) means the job has not finished and acceptance must wait.
	if len(payload.Status.BlockingJobResults) == 0 {
		allJobs := append(payload.Status.InformingJobResults, payload.Status.UpgradeJobResults...)
		for _, job := range allJobs {
			if job.AggregateState != v1alpha1.JobStateSuccess && job.AggregateState != v1alpha1.JobStateFailure {
				acceptedCondition.Status = metav1.ConditionUnknown
				return acceptedCondition
			}
		}
		acceptedCondition.Status = metav1.ConditionTrue
		return acceptedCondition
	}

	// Check that all Blocking jobs have completed successfully...
	status := jobstatus.ComputeJobState(payload.Status.BlockingJobResults)
	switch status {
	case v1alpha1.JobStateSuccess:
		acceptedCondition.Status = metav1.ConditionTrue
	case v1alpha1.JobStateUnknown:
		acceptedCondition.Status = metav1.ConditionUnknown
	default:
		acceptedCondition.Status = metav1.ConditionFalse
	}

	return acceptedCondition
}
