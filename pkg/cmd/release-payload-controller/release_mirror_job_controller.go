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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
)

const (
	// ReleasePayloadMirrorJobFailedReason programmatic identifier indicating that the ReleasePayload was not mirror successfully
	ReleasePayloadMirrorJobFailedReason string = "ReleasePayloadMirrorJobFailed"
)

// ReleaseMirrorJobController is responsible for writing the coordinates of the release mirror job.
// The jobsNamespace is populated from a command-line parameter and contains the namespace where
// the release-controller creates the release mirror batch/v1 jobs.
// The ReleaseMirrorJobController writes the following pieces of information:
//   - .status.ReleaseMirrorJobResult.ReleaseMirrorJobCoordinates.Namespace
//   - .status.ReleaseMirrorJobResult.ReleaseMirrorJobCoordinates.Name
type ReleaseMirrorJobController struct {
	*ReleasePayloadController
}

func NewReleaseMirrorJobController(
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	eventRecorder events.Recorder,
) (*ReleaseMirrorJobController, error) {
	c := &ReleaseMirrorJobController{
		ReleasePayloadController: NewReleasePayloadController("Release Mirror Job Controller",
			releasePayloadInformer,
			releasePayloadClient,
			eventRecorder.WithComponentSuffix("release-mirror-job-controller"),
			workqueue.NewRateLimitingQueueWithConfig(workqueue.DefaultControllerRateLimiter(), workqueue.RateLimitingQueueConfig{Name: "ReleaseMirrorJobController"})),
	}

	c.syncFn = c.sync

	releasePayloadFilter := func(obj any) bool {
		if releasePayload, ok := obj.(*v1alpha1.ReleasePayload); ok {
			// If the Coordinates are already set, then don't do anything...
			if len(releasePayload.Status.ReleaseMirrorJobResult.Coordinates.Namespace) > 0 && len(releasePayload.Status.ReleaseMirrorJobResult.Coordinates.Name) > 0 {
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

func (c *ReleaseMirrorJobController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting ReleaseMirrorJobController sync")
	defer klog.V(4).Infof("ReleaseMirrorJobController sync done")

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

	// If the Coordinates are already set, then don't do anything...
	if len(originalReleasePayload.Status.ReleaseMirrorJobResult.Coordinates.Namespace) > 0 && len(originalReleasePayload.Status.ReleaseMirrorJobResult.Coordinates.Name) > 0 {
		return nil
	}

	klog.V(4).Infof("Syncing ReleaseMirrorJobResult for ReleasePayload: %s/%s", originalReleasePayload.Namespace, originalReleasePayload.Name)

	releasePayload := originalReleasePayload.DeepCopy()

	// Updating the ReleaseMirrorJobResult.  Blanking out the Status and the Message forces the
	// release_mirror_job_status_controller to rediscover and set them accordingly.
	releasePayload.Status.ReleaseMirrorJobResult = v1alpha1.ReleaseMirrorJobResult{
		Coordinates: v1alpha1.ReleaseMirrorJobCoordinates{
			Name:      originalReleasePayload.Spec.PayloadCreationConfig.ReleaseMirrorCoordinates.ReleaseMirrorJobName,
			Namespace: originalReleasePayload.Spec.PayloadCreationConfig.ReleaseMirrorCoordinates.Namespace,
		},
	}

	releasepayloadhelpers.CanonicalizeReleasePayloadStatus(releasePayload)

	if reflect.DeepEqual(originalReleasePayload, releasePayload) {
		return nil
	}

	_, err = c.releasePayloadClient.ReleasePayloads(releasePayload.Namespace).UpdateStatus(ctx, releasePayload, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return nil
}
