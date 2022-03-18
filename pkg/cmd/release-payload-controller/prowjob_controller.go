package release_payload_controller

import (
	"context"
	"fmt"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	releasepayloadlister "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/test-infra/prow/apis/prowjobs/v1"
	prowjobinformer "k8s.io/test-infra/prow/client/informers/externalversions/prowjobs/v1"
	prowjoblister "k8s.io/test-infra/prow/client/listers/prowjobs/v1"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
)

type ProwJobStatusController struct {
	releasePayloadNamespace string
	releasePayloadLister    releasepayloadlister.ReleasePayloadLister

	prowJobLister prowjoblister.ProwJobLister

	eventRecorder events.Recorder

	cachesToSync []cache.InformerSynced

	queue workqueue.RateLimitingInterface
}

func NewProwJobStatusController(
	releasePayloadNamespace string,
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	prowJobInformer prowjobinformer.ProwJobInformer,
	eventRecorder events.Recorder,
) (*ProwJobStatusController, error) {
	c := &ProwJobStatusController{
		releasePayloadNamespace: releasePayloadNamespace,
		releasePayloadLister:    releasePayloadInformer.Lister(),
		prowJobLister:           prowJobInformer.Lister(),
		eventRecorder:           eventRecorder.WithComponentSuffix("prowjob-status-controller"),
		queue:                   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ProwJobStatusController"),
	}

	c.cachesToSync = append(c.cachesToSync, releasePayloadInformer.Informer().HasSynced)
	c.cachesToSync = append(c.cachesToSync, prowJobInformer.Informer().HasSynced)

	prowJobFilter := func(obj interface{}) bool {
		if prowJob, ok := obj.(*v1.ProwJob); ok {
			if _, ok := prowJob.Labels[releasecontroller.ReleaseAnnotationVerify]; ok {
				return true
			}
		}
		if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			if prowJob, ok := tombstone.Obj.(*v1.ProwJob); ok {
				if _, ok = prowJob.Labels[releasecontroller.ReleaseAnnotationVerify]; ok {
					return true
				}
			}
		}
		return false
	}

	prowJobInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: prowJobFilter,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.lookupReleasePayload,
			UpdateFunc: func(old, new interface{}) { c.lookupReleasePayload(new) },
			DeleteFunc: c.lookupReleasePayload,
		},
	})

	return c, nil
}

func (c *ProwJobStatusController) lookupReleasePayload(obj interface{}) {
	prowJob, ok := obj.(*v1.ProwJob)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unable to cast prowJob: %v", obj))
		return
	}

	klog.V(4).Infof("Processing prowjob: '%s/%s'", prowJob.Namespace, prowJob.Name)

	tag, ok := prowJob.Annotations[releasecontroller.ReleaseAnnotationToTag]
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unable to process prowjob, missing '%s' annotation", releasecontroller.ReleaseAnnotationToTag))
		return
	}

	releasePayloadKey := fmt.Sprintf("%s/%s", c.releasePayloadNamespace, tag)
	klog.V(4).Infof("Queueing ReleasePayload: %s", releasePayloadKey)
	c.queue.Add(releasePayloadKey)
}

func (c *ProwJobStatusController) Run(ctx context.Context) {
	defer utilruntime.HandleCrash()

	klog.Info("Starting ProwJob Status Controller")
	defer func() {
		klog.Info("Shutting down ProwJob Status Controller")
		c.queue.ShutDown()
		klog.Info("ProwJob Status Controller shut down")
	}()

	if !cache.WaitForNamedCacheSync("ProwJobStatusController", ctx.Done(), c.cachesToSync...) {
		return
	}

	go func() {
		wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}()

	<-ctx.Done()
}

func (c *ProwJobStatusController) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
	}
}

func (c *ProwJobStatusController) processNextItem(ctx context.Context) bool {
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

func (c *ProwJobStatusController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting ProwJobStatusController sync")
	defer klog.V(4).Infof("ProwJobStatusController sync done")

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

	for _, jobStatus := range releasePayload.Status.GetJobs() {
		klog.V(4).Infof("Syncing Job: %s", jobStatus.CIConfigurationName)
	}

	return nil
}
