package release_payload_controller

import (
	"context"
	"fmt"
	"reflect"

	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/releasepayload/qualifiers"
	releasepayloadhelpers "github.com/openshift/release-controller/pkg/releasepayload/v1alpha1helpers"
	releasequalifierslib "github.com/openshift/release-controller/pkg/releasequalifiers"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
)

// QualifiersSummaryController updates the .status.jobQualifiersSummary field
// based on job statuses and global release qualifiers configuration.
// The QualifiersSummaryController reads the following pieces of information:
//   - .spec.payloadVerificationConfig.blockingJobs[]
//   - .spec.payloadVerificationConfig.informingJobs[]
//   - .spec.payloadVerificationConfig.upgradeJobs[]
//   - .status.blockingJobResults[]
//   - .status.informingJobResults[]
//   - .status.upgradeJobResults[]
//   - Global release qualifiers configuration
//
// and populates the following field:
//   - .status.jobQualifiersSummary
type QualifiersSummaryController struct {
	*ReleasePayloadController
	configAccessor releasequalifierslib.ConfigAccessor
}

func NewJobQualifiersSummaryController(
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	eventRecorder events.Recorder,
	configAccessor releasequalifierslib.ConfigAccessor,
) (*QualifiersSummaryController, error) {
	c := &QualifiersSummaryController{
		ReleasePayloadController: NewReleasePayloadController(
			"Qualifiers Summary Controller",
			releasePayloadInformer,
			releasePayloadClient,
			eventRecorder.WithComponentSuffix("qualifiers-summary-controller"),
			workqueue.NewTypedRateLimitingQueueWithConfig(workqueue.DefaultTypedControllerRateLimiter[string](), workqueue.TypedRateLimitingQueueConfig[string]{Name: "QualifiersSummaryController"})),
		configAccessor: configAccessor,
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

func (c *QualifiersSummaryController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting QualifiersSummaryController sync")
	defer klog.V(4).Infof("QualifiersSummaryController sync done")

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	originalReleasePayload, err := c.releasePayloadLister.ReleasePayloads(namespace).Get(name)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	releasePayload := originalReleasePayload.DeepCopy()

	// Generate job qualifiers summary (reads existing JiraNotifications from payload status)
	releasePayload.Status.QualifiersSummary = qualifiers.GenerateQualifiersSummary(
		releasePayload,
		c.configAccessor,
	)

	releasepayloadhelpers.CanonicalizeReleasePayloadStatus(releasePayload)

	if reflect.DeepEqual(originalReleasePayload, releasePayload) {
		return nil
	}

	klog.V(4).Infof("Syncing Qualifiers Summary for ReleasePayload: %s/%s", releasePayload.Namespace, releasePayload.Name)
	_, err = c.releasePayloadClient.ReleasePayloads(releasePayload.Namespace).UpdateStatus(ctx, releasePayload, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return nil
}
