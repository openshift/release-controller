package release_payload_controller

import (
	"context"
	"fmt"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	releasepayloadlister "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
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

const (
	// ReleasePayloadCreationFailedReason programmatic identifier indicating that the ReleasePayload was not created successfully
	ReleasePayloadCreationFailedReason string = "ReleasePayloadCreationFailed"
)

// ReleaseCreationJobController is responsible for writing the coordinates of the release creation job.
// The jobsNamespace is populated from a command-line parameter and contains the namespace where
// the release-controller creates the release creation batch/v1 jobs.
// The ReleaseCreationJobController writes the following pieces of information:
//   - .status.ReleaseCreationJobResult.ReleaseCreationJobCoordinates.Namespace
//   - .status.ReleaseCreationJobResult.ReleaseCreationJobCoordinates.Name
type ReleaseCreationJobController struct {
	releasePayloadNamespace string
	releasePayloadLister    releasepayloadlister.ReleasePayloadLister
	releasePayloadClient    releasepayloadclient.ReleaseV1alpha1Interface

	jobsNamespace string

	eventRecorder events.Recorder

	cachesToSync []cache.InformerSynced

	queue workqueue.RateLimitingInterface
}

func NewReleaseCreationJobController(
	releasePayloadNamespace string,
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	jobsNamespace string,
	eventRecorder events.Recorder,
) (*ReleaseCreationJobController, error) {
	c := &ReleaseCreationJobController{
		releasePayloadNamespace: releasePayloadNamespace,
		releasePayloadLister:    releasePayloadInformer.Lister(),
		releasePayloadClient:    releasePayloadClient,
		jobsNamespace:           jobsNamespace,
		eventRecorder:           eventRecorder.WithComponentSuffix("release-creation-job-controller"),
		queue:                   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ReleaseCreationJobController"),
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

func (c *ReleaseCreationJobController) Enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid queue key '%v': %v", obj, err))
		return
	}
	c.queue.Add(key)
}

func (c *ReleaseCreationJobController) Run(ctx context.Context) {
	defer utilruntime.HandleCrash()

	klog.Info("Starting Release Creation Job Controller")
	defer func() {
		klog.Info("Shutting down Release Creation Job Controller")
		c.queue.ShutDown()
		klog.Info("Release Creation Job Controller shut down")
	}()

	if !cache.WaitForNamedCacheSync("ReleaseCreationJobController", ctx.Done(), c.cachesToSync...) {
		return
	}

	go func() {
		wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}()

	<-ctx.Done()
}

func (c *ReleaseCreationJobController) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
	}
}

func (c *ReleaseCreationJobController) processNextItem(ctx context.Context) bool {
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

func (c *ReleaseCreationJobController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting ReleaseCreationJobController sync")
	defer klog.V(4).Infof("ReleaseCreationJobController sync done")

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
	if len(originalReleasePayload.Status.ReleaseCreationJobResult.Coordinates.Namespace) > 0 && len(originalReleasePayload.Status.ReleaseCreationJobResult.Coordinates.Name) > 0 {
		return nil
	}

	klog.V(4).Infof("Syncing ReleaseCreationJobResult for ReleasePayload: %s/%s", originalReleasePayload.Namespace, originalReleasePayload.Name)

	releasePayload := originalReleasePayload.DeepCopy()

	// Updating the ReleaseCreationJobResult.  Blanking out the Status and the Message forces the
	// release_creation_status_controller to rediscover and set them accordingly.
	releasePayload.Status.ReleaseCreationJobResult = v1alpha1.ReleaseCreationJobResult{
		Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
			Name:      originalReleasePayload.Name,
			Namespace: c.jobsNamespace,
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
