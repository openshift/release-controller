package release_payload_controller

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/operator/events"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	releasepayloadlister "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type Controller interface {
	sync(ctx context.Context, key string) error
}

type ReleasePayloadController struct {
	name string

	releasePayloadLister releasepayloadlister.ReleasePayloadLister
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface

	eventRecorder events.Recorder

	cachesToSync []cache.InformerSynced

	queue workqueue.RateLimitingInterface

	syncFn func(ctx context.Context, key string) error
}

func NewReleasePayloadController(
	name string,
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	eventRecorder events.Recorder,
	queue workqueue.RateLimitingInterface) *ReleasePayloadController {
	c := &ReleasePayloadController{
		name:                 name,
		releasePayloadLister: releasePayloadInformer.Lister(),
		releasePayloadClient: releasePayloadClient,
		eventRecorder:        eventRecorder,
		queue:                queue,
	}

	c.cachesToSync = append(c.cachesToSync, releasePayloadInformer.Informer().HasSynced)

	return c
}

func (c *ReleasePayloadController) Enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid queue key '%v': %v", obj, err))
		return
	}
	c.queue.Add(key)
}

func (c *ReleasePayloadController) RunWorkers(ctx context.Context, workers int) {
	defer utilruntime.HandleCrash()

	klog.Infof("Starting %s", c.name)
	defer func() {
		klog.Infof("Shutting down %s", c.name)
		c.queue.ShutDown()
		klog.Infof("%s shut down", c.name)
	}()

	if !cache.WaitForNamedCacheSync(c.name, ctx.Done(), c.cachesToSync...) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	<-ctx.Done()
}

func (c *ReleasePayloadController) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
	}
}

func (c *ReleasePayloadController) processNextItem(ctx context.Context) bool {
	if c.syncFn == nil {
		panic(fmt.Errorf("%s's syncFn() not set", c.name))
	}

	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncFn(ctx, key.(string))

	if err == nil {
		c.queue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("%v failed with : %w", key, err))
	c.queue.AddRateLimited(key)

	return true
}
