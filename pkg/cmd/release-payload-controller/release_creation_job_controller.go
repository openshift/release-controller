package release_payload_controller

import (
	"context"
	"fmt"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	releasepayloadlister "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"k8s.io/apimachinery/pkg/api/errors"
	batchinformer "k8s.io/client-go/informers/batch/v1"
	batchlister "k8s.io/client-go/listers/batch/v1"
	"k8s.io/kubernetes/pkg/apis/batch"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
)

type ReleaseCreationJobController struct {
	releasePayloadNamespace string
	releasePayloadLister    releasepayloadlister.ReleasePayloadLister

	jobsNamespace string
	jobsLister    batchlister.JobLister

	eventRecorder events.Recorder

	cachesToSync []cache.InformerSynced

	queue workqueue.RateLimitingInterface
}

func NewReleaseCreationJobController(
	releasePayloadNamespace string,
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	jobsNamespace string,
	jobsInformer batchinformer.JobInformer,
	eventRecorder events.Recorder,
) (*ReleaseCreationJobController, error) {
	c := &ReleaseCreationJobController{
		releasePayloadNamespace: releasePayloadNamespace,
		releasePayloadLister:    releasePayloadInformer.Lister(),
		jobsNamespace:           jobsNamespace,
		jobsLister:              jobsInformer.Lister(),
		eventRecorder:           eventRecorder.WithComponentSuffix("release-creation-job-controller"),
		queue:                   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ReleaseCreationJobController"),
	}

	c.cachesToSync = append(c.cachesToSync, releasePayloadInformer.Informer().HasSynced)
	c.cachesToSync = append(c.cachesToSync, jobsInformer.Informer().HasSynced)

	jobFilter := func(obj interface{}) bool {
		if job, ok := obj.(*batch.Job); ok {
			if _, ok := job.Annotations[releasecontroller.ReleaseAnnotationReleaseTag]; ok {
				return true
			}
		}
		if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			if job, ok := tombstone.Obj.(*batch.Job); ok {
				if _, ok = job.Annotations[releasecontroller.ReleaseAnnotationReleaseTag]; ok {
					return true
				}
			}
		}
		return false
	}

	jobsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: jobFilter,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.lookupReleasePayload,
			UpdateFunc: func(old, new interface{}) { c.lookupReleasePayload(new) },
			DeleteFunc: c.lookupReleasePayload,
		},
	})

	return c, nil
}

func (c *ReleaseCreationJobController) lookupReleasePayload(obj interface{}) {
	job, ok := obj.(*batch.Job)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unable to cast Job: %v", obj))
		return
	}

	klog.V(4).Infof("Processing batch job: '%s/%s'", job.Namespace, job.Name)

	tag, ok := job.Annotations[releasecontroller.ReleaseAnnotationReleaseTag]
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unable to process batch job, missing '%s' annotation", releasecontroller.ReleaseAnnotationReleaseTag))
		return
	}

	releasePayloadKey := fmt.Sprintf("%s/%s", c.releasePayloadNamespace, tag)
	klog.V(4).Infof("Queueing ReleasePayload: %s", releasePayloadKey)
	c.queue.Add(releasePayloadKey)
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

	releasePayload := originalReleasePayload.DeepCopy()

	klog.V(4).Infof("Syncing ReleasePayload: %s/%s", releasePayload.Namespace, releasePayload.Name)

	return nil
}
