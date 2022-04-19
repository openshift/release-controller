package release_payload_controller

import (
	"context"
	"fmt"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"github.com/openshift/release-controller/pkg/releasepayload/controller"
	"github.com/openshift/release-controller/pkg/releasepayload/status"
	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	v1 "k8s.io/test-infra/prow/apis/prowjobs/v1"
	prowjobinformer "k8s.io/test-infra/prow/client/informers/externalversions/prowjobs/v1"
	prowjoblister "k8s.io/test-infra/prow/client/listers/prowjobs/v1"

	"github.com/openshift/library-go/pkg/operator/events"
)

type ProwJobStatusController struct {
	*ReleasePayloadController

	prowJobNamespace string
	prowJobLister    prowjoblister.ProwJobLister
}

func NewProwJobStatusController(
	releasePayloadNamespace string,
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	prowJobNamespace string,
	prowJobInformer prowjobinformer.ProwJobInformer,
	eventRecorder events.Recorder,
) (*ProwJobStatusController, error) {
	c := &ProwJobStatusController{
		ReleasePayloadController: NewReleasePayloadController("ProwJob Status Controller",
			releasePayloadNamespace,
			releasePayloadInformer,
			releasePayloadClient,
			eventRecorder.WithComponentSuffix("prowjob-status-controller"),
			workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ProwJobStatusController")),
		prowJobNamespace: prowJobNamespace,
		prowJobLister:    prowJobInformer.Lister(),
	}

	c.syncFn = c.sync
	c.cachesToSync = append(c.cachesToSync, prowJobInformer.Informer().HasSynced)

	prowJobFilter := func(obj interface{}) bool {
		if prowJob, ok := obj.(*v1.ProwJob); ok {
			if _, ok := prowJob.Labels[releasecontroller.ReleaseAnnotationVerify]; ok {
				return true
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

	releasePayloadInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: c.Enqueue,
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.Enqueue(newObj)
		},
		DeleteFunc: c.Enqueue,
	})

	return c, nil
}

func (c *ProwJobStatusController) lookupReleasePayload(obj interface{}) {
	releasePayloadKey, err := controller.GetReleasePayloadQueueKeyFromAnnotation(obj, releasecontroller.ReleaseAnnotationToTag, c.releasePayloadNamespace)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to determine releasepayload key: %v", err))
		return
	}
	klog.V(4).Infof("Queueing ReleasePayload: %s", releasePayloadKey)
	c.queue.Add(releasePayloadKey)
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

	for _, jobStatus := range status.GetJobs(releasePayload.Status) {
		klog.V(4).Infof("Syncing Job: %s", jobStatus.CIConfigurationName)
	}

	return nil
}
