package release_payload_controller

import (
	"context"
	"fmt"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/releasepayload/conditions"
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

// These are valid Reason values for the ReleasePayloadStatus Conditions
const (
	// ReleasePayloadAcceptedReason programmatic identifier indicating that the ReleasePayload was Accepted
	ReleasePayloadAcceptedReason string = "ReleasePayloadAccepted"

	// ReleasePayloadManuallyAcceptedReason programmatic identifier indicating that the ReleasePayload was manually Accepted
	ReleasePayloadManuallyAcceptedReason string = "ReleasePayloadManuallyAccepted"
)

// PayloadAcceptedController is responsible for Accepting ReleasePayloads when any of the following scenarios
// occur:
//   1) all Blocking Jobs complete successfully
//   2) the payload is manually accepted
// The PayloadAcceptedController reads the following pieces of information:
//   - .spec.payloadOverride.override
//   - .status.blockingJobResults
//   - .status.releaseCreationJobResult.status
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
			workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "PayloadAcceptedController")),
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
	conditions.SetCondition(&releasePayload.Status.Conditions, acceptedCondition)
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

	// If the release creation job failed, then the payload will never be Accepted
	if payload.Status.ReleaseCreationJobResult.Status == v1alpha1.ReleaseCreationJobFailed {
		acceptedCondition.Status = metav1.ConditionFalse
		acceptedCondition.Reason = ReleasePayloadCreationFailedReason
		acceptedCondition.Message = payload.Status.ReleaseCreationJobResult.Message
		return acceptedCondition
	}

	// Check for "Accepted" PayloadOverride
	switch payload.Spec.PayloadOverride.Override {
	case v1alpha1.ReleasePayloadOverrideAccepted:
		acceptedCondition.Status = metav1.ConditionTrue
		acceptedCondition.Message = payload.Spec.PayloadOverride.Reason
		acceptedCondition.Reason = ReleasePayloadManuallyAcceptedReason
		return acceptedCondition
	case v1alpha1.ReleasePayloadOverrideRejected:
		// If a payload has already been rejected, then we do not want to process it any further...
		acceptedCondition.Status = metav1.ConditionFalse
		acceptedCondition.Message = payload.Spec.PayloadOverride.Reason
		acceptedCondition.Reason = ReleasePayloadManuallyRejectedReason
		return acceptedCondition
	}

	// Check that all Blocking jobs have completed successfully...
	status := jobstatus.ComputeAggregatedJobState(payload.Status.BlockingJobResults)
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
