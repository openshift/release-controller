package release_payload_controller

import (
	"context"
	"fmt"
	"github.com/openshift/release-controller/pkg/client/clientset/versioned"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	releasepayloadlister "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
)

type PayloadAcceptedController struct {
	releasePayloadNamespace string
	releasePayloadLister    releasepayloadlister.ReleasePayloadLister
	releasePayloadClientSet *versioned.Clientset

	eventRecorder events.Recorder

	cachesToSync []cache.InformerSynced

	queue workqueue.RateLimitingInterface
}

func NewPayloadAcceptedController(
	releasePayloadNamespace string,
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClientSet *versioned.Clientset,
	eventRecorder events.Recorder,
) (*PayloadAcceptedController, error) {
	c := &PayloadAcceptedController{
		releasePayloadNamespace: releasePayloadNamespace,
		releasePayloadLister:    releasePayloadInformer.Lister(),
		releasePayloadClientSet: releasePayloadClientSet,
		eventRecorder:           eventRecorder.WithComponentSuffix("payload-accepted-controller"),
		queue:                   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "PayloadAcceptedController"),
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

func (c *PayloadAcceptedController) Enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid queue key '%v': %v", obj, err))
		return
	}
	c.queue.Add(key)
}

func (c *PayloadAcceptedController) Run(ctx context.Context) {
	defer utilruntime.HandleCrash()

	klog.Info("Starting Payload Accepted Controller")
	defer func() {
		klog.Info("Shutting down Payload Accepted Controller")
		c.queue.ShutDown()
		klog.Info("Payload Accepted Controller shut down")
	}()

	if !cache.WaitForNamedCacheSync("PayloadAcceptedController", ctx.Done(), c.cachesToSync...) {
		return
	}

	go func() {
		wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}()

	<-ctx.Done()
}

func (c *PayloadAcceptedController) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
	}
}

func (c *PayloadAcceptedController) processNextItem(ctx context.Context) bool {
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
	if err != nil {
		// The ReleasePayload resource may no longer exist, in which case we stop processing.
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	releasePayload := originalReleasePayload.DeepCopy()

	klog.V(4).Infof("Syncing Payload Accepted for ReleasePayload: %s/%s", releasePayload.Namespace, releasePayload.Name)

	return nil
}
