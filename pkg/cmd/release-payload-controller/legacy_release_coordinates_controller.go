package release_payload_controller

import (
	"context"
	"fmt"

	imagev1 "github.com/openshift/api/image/v1"
	imagev1informer "github.com/openshift/client-go/image/informers/externalversions/image/v1"
	imagev1lister "github.com/openshift/client-go/image/listers/image/v1"
	imagereference "github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

// LegacyReleaseCoordinatesController temporarily backfills empty
// spec.releaseCoordinates for local releases. Reference releases should already
// have coordinates and are skipped with a warning if they do not.
// Remove this controller once the backfill is complete.
type LegacyReleaseCoordinatesController struct {
	*ReleasePayloadController

	imageStreamLister imagev1lister.ImageStreamLister
}

func NewLegacyReleaseCoordinatesController(
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	imageStreamInformer imagev1informer.ImageStreamInformer,
	eventRecorder events.Recorder,
) (*LegacyReleaseCoordinatesController, error) {
	c := &LegacyReleaseCoordinatesController{
		ReleasePayloadController: NewReleasePayloadController("Legacy Release Coordinates Controller",
			releasePayloadInformer,
			releasePayloadClient,
			eventRecorder.WithComponentSuffix("legacy-release-coordinates-controller"),
			workqueue.NewTypedRateLimitingQueueWithConfig(workqueue.DefaultTypedControllerRateLimiter[string](), workqueue.TypedRateLimitingQueueConfig[string]{Name: "LegacyReleaseCoordinatesController"})),
		imageStreamLister: imageStreamInformer.Lister(),
	}
	c.syncFn = c.sync
	c.cachesToSync = append(c.cachesToSync, imageStreamInformer.Informer().HasSynced)

	enqueue := func(obj any) {
		if payload, ok := obj.(*v1alpha1.ReleasePayload); ok && len(payload.Spec.ReleaseCoordinates) == 0 {
			c.Enqueue(payload)
		}
	}
	if _, err := releasePayloadInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    enqueue,
		UpdateFunc: func(old, new any) { enqueue(new) },
	}); err != nil {
		return nil, fmt.Errorf("failed to add release payload event handler: %v", err)
	}

	return c, nil
}

func (c *LegacyReleaseCoordinatesController) sync(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	original, err := c.releasePayloadLister.ReleasePayloads(namespace).Get(name)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(original.Spec.ReleaseCoordinates) > 0 {
		return nil
	}
	if original.Spec.PayloadType == v1alpha1.PayloadTypeReference {
		klog.Warningf("skipping release coordinates backfill for reference ReleasePayload %s: expected release coordinates to be populated at creation", key)
		return nil
	}

	coordinates := original.Spec.PayloadCoordinates
	if coordinates.Namespace == "" || coordinates.ImagestreamName == "" || coordinates.ImagestreamTagName == "" {
		klog.Warningf("unable to backfill release coordinates for ReleasePayload %s: incomplete payload coordinates", key)
		return nil
	}
	imageStream, err := c.imageStreamLister.ImageStreams(coordinates.Namespace).Get(coordinates.ImagestreamName)
	if errors.IsNotFound(err) {
		klog.Warningf("unable to backfill release coordinates for ReleasePayload %s: %v", key, err)
		return nil
	}
	if err != nil {
		return err
	}

	var tag *imagev1.TagReference
	for i := range imageStream.Spec.Tags {
		if imageStream.Spec.Tags[i].Name == coordinates.ImagestreamTagName {
			tag = &imageStream.Spec.Tags[i]
			break
		}
	}
	if tag == nil {
		klog.Warningf("unable to backfill release coordinates for ReleasePayload %s: missing spec tag %s/%s:%s", key, imageStream.Namespace, imageStream.Name, coordinates.ImagestreamTagName)
		return nil
	}

	if tag.Reference {
		klog.Warningf("skipping release coordinates backfill for reference ImageStream tag %s/%s:%s (ReleasePayload %s): expected release coordinates to be populated at creation", imageStream.Namespace, imageStream.Name, tag.Name, key)
		return nil
	}
	releaseCoordinates := v1alpha1.ReleaseCoordinates{
		Repository: imageStream.Status.PublicDockerImageRepository,
		Tag:        coordinates.ImagestreamTagName,
	}
	if releaseCoordinates.Repository == "" {
		klog.Warningf("unable to backfill release coordinates for ReleasePayload %s: empty repository for ImageStream tag %s/%s:%s", key, imageStream.Namespace, imageStream.Name, tag.Name)
		return nil
	}
	repository, err := imagereference.Parse(releaseCoordinates.Repository)
	if err != nil || repository.Name == "" || repository.Tag != "" || repository.ID != "" {
		klog.Warningf("unable to backfill release coordinates for ReleasePayload %s: invalid repository %q", key, releaseCoordinates.Repository)
		return nil
	}

	payload := original.DeepCopy()
	payload.Spec.ReleaseCoordinates = []v1alpha1.ReleaseCoordinates{releaseCoordinates}
	klog.V(4).Infof("Backfilling release coordinates for ReleasePayload %s", key)
	_, err = c.releasePayloadClient.ReleasePayloads(namespace).Update(ctx, payload, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	return err
}
