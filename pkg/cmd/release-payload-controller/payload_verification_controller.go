package release_payload_controller

import (
	"context"
	"fmt"
	"reflect"

	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	releasepayloadhelpers "github.com/openshift/release-controller/pkg/releasepayload/v1alpha1helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
)

// PayloadVerificationController is responsible for the initial population of the following pieces of
// the ReleasePayload:
//   - .status.blockingJobResults
//   - .status.informingJobResults
//   - .status.upgradeJobResults
//
// This information is obtained by reading `.spec.payloadVerification` and populating the respective
// JobResults accordingly.
type PayloadVerificationController struct {
	*ReleasePayloadController
}

func NewPayloadVerificationController(
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	eventRecorder events.Recorder,
) (*PayloadVerificationController, error) {
	c := &PayloadVerificationController{
		ReleasePayloadController: NewReleasePayloadController("Payload Verification Controller",
			releasePayloadInformer,
			releasePayloadClient,
			eventRecorder.WithComponentSuffix("payload-verification-controller"),
			workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "PayloadVerificationController")),
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

func (c *PayloadVerificationController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting PayloadVerificationController sync")
	defer klog.V(4).Infof("PayloadVerificationController sync done")

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

	// If there are any JobResults defined, then we don't need to do anything else here...
	if len(originalReleasePayload.Status.BlockingJobResults) != 0 || len(originalReleasePayload.Status.InformingJobResults) != 0 || len(originalReleasePayload.Status.UpgradeJobResults) != 0 {
		klog.V(5).Infof("ReleasePayload: '%s/%s' already synced", originalReleasePayload.Namespace, originalReleasePayload.Name)
		return nil
	}

	releasePayload := originalReleasePayload.DeepCopy()

	klog.V(4).Infof("Syncing Payload Verification for ReleasePayload: %s/%s", releasePayload.Namespace, releasePayload.Name)
	for _, verify := range releasePayload.Spec.PayloadVerificationConfig.BlockingJobs {
		releasePayload.Status.BlockingJobResults = append(releasePayload.Status.BlockingJobResults, generateJobStatus(verify))
	}

	for _, verify := range releasePayload.Spec.PayloadVerificationConfig.InformingJobs {
		releasePayload.Status.InformingJobResults = append(releasePayload.Status.InformingJobResults, generateJobStatus(verify))
	}

	for _, upgrade := range releasePayload.Spec.PayloadVerificationConfig.UpgradeJobs {
		releasePayload.Status.UpgradeJobResults = append(releasePayload.Status.UpgradeJobResults, generateUpgradeJobStatus(upgrade))
	}

	releasepayloadhelpers.CanonicalizeReleasePayloadStatus(releasePayload)

	if reflect.DeepEqual(originalReleasePayload, releasePayload) {
		return nil
	}

	_, err = c.releasePayloadClient.ReleasePayloads(releasePayload.Namespace).UpdateStatus(ctx, releasePayload, v1.UpdateOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return nil
}

func generateJobStatus(config v1alpha1.CIConfiguration) v1alpha1.JobStatus {
	JobStatus := v1alpha1.JobStatus{
		CIConfigurationName:    config.CIConfigurationName,
		CIConfigurationJobName: config.CIConfigurationJobName,
		MaxRetries:             config.MaxRetries,
		AnalysisJobCount:       config.AnalysisJobCount,
	}
	return JobStatus
}

func generateUpgradeJobStatus(config v1alpha1.CIConfiguration) v1alpha1.JobStatus {
	JobStatus := v1alpha1.JobStatus{
		CIConfigurationName:    config.CIConfigurationName,
		CIConfigurationJobName: config.CIConfigurationJobName,
	}
	return JobStatus
}
