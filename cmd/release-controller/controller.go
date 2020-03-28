package main

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	lru "github.com/hashicorp/golang-lru"
	"golang.org/x/time/rate"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	batchclient "k8s.io/client-go/kubernetes/typed/batch/v1"
	kv1core "k8s.io/client-go/kubernetes/typed/core/v1"
	batchlisters "k8s.io/client-go/listers/batch/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	imagev1 "github.com/openshift/api/image/v1"
	imagescheme "github.com/openshift/client-go/image/clientset/versioned/scheme"
	imageclient "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions/image/v1"
	imagelisters "github.com/openshift/client-go/image/listers/image/v1"

	"github.com/openshift/release-controller/pkg/signer"
	prowconfig "k8s.io/test-infra/prow/config"
)

// Controller ensures that OpenShift update payload images (also known as
// release images) are created whenever an image stream representing the images
// in a release is updated. A consumer sets the release.openshift.io/config
// annotation on an image stream in the release namespace and the controller will
//
// 1. Create a tag in the "release" image stream that uses the release name +
//    current timestamp.
// 2. Mirror all of the tags in the input image stream so that they can't be
//    pruned.
// 3. Launch a job in the job namespace to invoke 'oc adm release new' from
//    the mirror pointing to the release tag we created in step 1.
// 4. If the job succeeds in pushing the tag, set an annotation on that tag
//    release.openshift.io/phase = "Ready", indicating that the release can be
//    used by other steps
//
// TODO:
//
// 5. Perform a number of manual and automated tasks on the release - if all are
//    successful, set the phase to "Verified" and then promote the tag to external
//    locations.
//
// Invariants:
//
// 1. ...
//
type Controller struct {
	eventRecorder record.EventRecorder

	imageClient       imageclient.ImageV1Interface
	imageStreamLister *multiImageStreamLister
	jobClient         batchclient.JobsGetter
	jobLister         batchlisters.JobLister

	podClient kv1core.PodsGetter

	performGC bool

	// syncs are the items that must return true before the queue can be processed
	syncs []cache.InformerSynced
	// syncFn is the function that the controller loop will handle
	syncFn func(queueKey) error

	// queue is the list of namespace keys that must be synced.
	queue workqueue.RateLimitingInterface
	// qcQueue is a trigger to performing cleanup for deleted resources.
	gcQueue workqueue.RateLimitingInterface
	// auditQueue is inputs that must be audited
	auditQueue workqueue.RateLimitingInterface

	// auditTracker keeps track of when tags were audited
	auditTracker *AuditTracker
	// auditStore holds metadata on releases
	auditStore AuditStore
	// signer, if set, will be used against audited releases
	signer signer.Interface
	// cliImageForAudit tightly controls which tooling image to use to verify releases
	cliImageForAudit string

	// expectations track upcoming changes that we have not yet observed
	expectations *expectations
	// expectationDelay controls how long the controller waits to observe its
	// own creates. Exposed only for testing.
	expectationDelay time.Duration

	// releaseNamespace is the namespace where the "release" image stream is expected
	// to be found.
	releaseNamespace string
	// jobNamespace is the namespace where temporary job and image stream mirror objects
	// are created.
	jobNamespace string
	// prowNamespace is the namespace where ProwJobs are created.
	prowNamespace string
	// artifactsHost if set is the location to build download links for client tools from
	artifactsHost string

	prowConfigLoader ProwConfigLoader
	prowClient       dynamic.ResourceInterface
	prowLister       cache.Indexer

	// onlySources if set controls which image stream names can be synced
	onlySources sets.String

	releaseInfo ReleaseInfo

	graph *UpgradeGraph

	// parsedReleaseConfigCache caches the parsed release config object for any release
	// config serialized json.
	parsedReleaseConfigCache *lru.Cache
}

// NewController instantiates a Controller to manage release objects.
func NewController(
	eventsClient kv1core.EventsGetter,
	imageClient imageclient.ImageV1Interface,
	jobClient batchclient.JobsGetter,
	jobs batchinformers.JobInformer,
	podClient kv1core.PodsGetter,
	prowConfigLoader ProwConfigLoader,
	prowClient dynamic.ResourceInterface,
	releaseNamespace string,
	jobNamespace string,
	artifactsHost string,
	releaseInfo ReleaseInfo,
	graph *UpgradeGraph,
) *Controller {

	// log events at v2 and send them to the server
	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.V(2).Infof)
	broadcaster.StartRecordingToSink(&kv1core.EventSinkImpl{Interface: eventsClient.Events("")})
	recorder := broadcaster.NewRecorder(imagescheme.Scheme, corev1.EventSource{Component: "release-controller"})

	// we cache parsed release configs to avoid the deserialization cost
	parsedReleaseConfigCache, err := lru.New(50)
	if err != nil {
		panic(err)
	}

	c := &Controller{
		eventRecorder: recorder,
		queue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "releases"),
		gcQueue:       workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "gc"),

		// rate limit the audit queue severely
		auditQueue: workqueue.NewNamedRateLimitingQueue(workqueue.NewMaxOfRateLimiter(
			workqueue.NewItemExponentialFailureRateLimiter(5*time.Second, 2*time.Hour),
			&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Every(5), 2)},
		), "audit"),

		expectations:     newExpectations(),
		expectationDelay: 2 * time.Second,

		imageClient:       imageClient,
		imageStreamLister: &multiImageStreamLister{listers: make(map[string]imagelisters.ImageStreamNamespaceLister)},

		jobClient: jobClient,
		jobLister: jobs.Lister(),

		podClient: podClient,

		syncs: []cache.InformerSynced{},

		prowConfigLoader: prowConfigLoader,
		prowClient:       prowClient,

		releaseNamespace: releaseNamespace,
		jobNamespace:     jobNamespace,

		artifactsHost: artifactsHost,

		releaseInfo: releaseInfo,

		graph: graph,

		parsedReleaseConfigCache: parsedReleaseConfigCache,
	}

	c.auditTracker = NewAuditTracker(c.auditQueue)

	// handle job changes
	jobs.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.processJobIfComplete,
		DeleteFunc: c.processJob,
		UpdateFunc: func(oldObj, newObj interface{}) { c.processJobIfComplete(newObj) },
	})

	return c
}

func (c *Controller) LimitSources(names ...string) {
	c.onlySources = sets.NewString(names...)
}

type ProwConfigLoader interface {
	Config() *prowconfig.Config
}

// multiImageStreamLister uses multiple independent namespace listers
// to simulate a full lister so that multiple namespaces can be watched
// for image streams.
type multiImageStreamLister struct {
	listers map[string]imagelisters.ImageStreamNamespaceLister
}

func (l *multiImageStreamLister) List(label labels.Selector) ([]*imagev1.ImageStream, error) {
	var streams []*imagev1.ImageStream
	for _, ns := range l.listers {
		is, err := ns.List(label)
		if err != nil {
			return nil, err
		}
		streams = append(streams, is...)
	}
	return streams, nil
}

func (l *multiImageStreamLister) ImageStreams(ns string) imagelisters.ImageStreamNamespaceLister {
	return l.listers[ns]
}

// AddNamespacedImageStreamInformer adds a new namespace scoped informer to the controller.
// All namespaces are treated equally.
func (c *Controller) AddNamespacedImageStreamInformer(ns string, imagestreams imageinformers.ImageStreamInformer) {
	c.imageStreamLister.listers[ns] = imagestreams.Lister().ImageStreams(ns)

	imagestreams.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.processImageStream,
		DeleteFunc: c.processImageStream,
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.processImageStream(newObj)
		},
	})
}

// AddProwInformer sets the controller up to watch for changes to prow jobs created by the
// controller.
func (c *Controller) AddProwInformer(ns string, informer cache.SharedIndexInformer) {
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.processProwJob,
		DeleteFunc: c.processProwJob,
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.processProwJob(newObj)
		},
	})
	c.prowNamespace = ns
	c.prowLister = informer.GetIndexer()
}

type queueKey struct {
	namespace string
	name      string
}

func (c *Controller) addQueueKey(key queueKey) {
	// only image streams in the release namespace may be release inputs
	if key.namespace != c.releaseNamespace {
		return
	}
	c.queue.Add(key)
}

func (c *Controller) processJob(obj interface{}) {
	switch t := obj.(type) {
	case *batchv1.Job:
		// this job should wake the audit queue
		if t.Annotations[releaseAnnotationJobPurpose] == "audit" {
			if name, ok := t.Annotations[releaseAnnotationReleaseTag]; ok {
				c.auditQueue.Add(name)
			}
			return
		}

		key, ok := queueKeyFor(t.Annotations[releaseAnnotationSource])
		if !ok {
			return
		}
		if glog.V(6) {
			success, complete := jobIsComplete(t)
			glog.Infof("Job %s updated, complete=%t success=%t", t.Name, complete, success)
		}
		c.addQueueKey(key)
	default:
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %T", obj))
	}
}

func (c *Controller) processJobIfComplete(obj interface{}) {
	switch t := obj.(type) {
	case *batchv1.Job:
		if _, complete := jobIsComplete(t); !complete {
			return
		}
		c.processJob(obj)
	default:
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %T", obj))
	}
}

func (c *Controller) processProwJob(obj interface{}) {
	switch t := obj.(type) {
	case *unstructured.Unstructured:
		key, ok := queueKeyFor(t.GetAnnotations()[releaseAnnotationSource])
		if !ok {
			return
		}
		c.addQueueKey(key)
	default:
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %T", obj))
	}
}

func (c *Controller) processImageStream(obj interface{}) {
	switch t := obj.(type) {
	case *imagev1.ImageStream:
		// when we see a change to an image stream, reset our expectations
		// this also allows periodic purging of the expectation list in the event
		// we miss one or more events.
		c.expectations.Clear(t.Namespace, t.Name)

		// if this image stream is a mirror for releases, requeue any that it touches
		if _, ok := t.Annotations[releaseAnnotationConfig]; ok {
			glog.V(6).Infof("Image stream %s is a release input and will be queued", t.Name)
			c.addQueueKey(queueKey{namespace: t.Namespace, name: t.Name})
			return
		}
		if key, ok := queueKeyFor(t.Annotations[releaseAnnotationSource]); ok {
			glog.V(6).Infof("Image stream %s was created by %v, queuing source", t.Name, key)
			c.addQueueKey(key)
			return
		}
		if _, ok := t.Annotations[releaseAnnotationHasReleases]; ok {
			// if the release image stream is modified, tags might have been deleted so retrigger
			// everything
			glog.V(6).Infof("Image stream %s is a release target, requeue release namespace", t.Name)
			c.addQueueKey(queueKey{namespace: c.releaseNamespace})
			return
		}
	default:
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %T", obj))
	}
}

func (c *Controller) RunSync(workers int, stopCh <-chan struct{}) {
	c.syncFn = c.sync
	c.performGC = c.onlySources.Len() == 0
	c.run(workers, stopCh)
}

func (c *Controller) RunAudit(workers int, stopCh <-chan struct{}) {
	c.syncFn = c.syncAudit
	c.run(workers, stopCh)
}

// run begins watching and syncing.
func (c *Controller) run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	glog.Infof("Starting controller")

	if !cache.WaitForCacheSync(stopCh, c.syncs...) {
		utilruntime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	go wait.Until(c.gcWorker, time.Second, stopCh)

	for i := 0; i < workers; i++ {
		go wait.Until(c.auditWorker, time.Second, stopCh)
	}

	<-stopCh
	glog.Infof("Shutting down controller")
}

func (c *Controller) worker() {
	for c.processNext() {
	}
	glog.V(4).Infof("Worker stopped")
}

func (c *Controller) processNext() bool {
	obj, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(obj)

	// queue all release inputs in the namespace on a namespace sync
	// this allows us to batch changes together when calculating which resource
	// would be affected is inefficient
	key := obj.(queueKey)
	if len(key.name) == 0 {
		err := c.processNextNamespace(key.namespace)
		c.handleNamespaceErr(c.queue, err, key)
		return true
	}
	if c.onlySources.Len() > 0 && !c.onlySources.Has(key.name) {
		glog.V(4).Infof("Ignored %s", key.name)
		return true
	}

	glog.V(5).Infof("processing %v begin", key)
	err := c.syncFn(key)
	c.handleNamespaceErr(c.queue, err, key)
	glog.V(5).Infof("processing %v end", key)

	return true
}

func (c *Controller) processNextNamespace(ns string) error {
	imageStreams, err := c.imageStreamLister.ImageStreams(ns).List(labels.Everything())
	if err != nil {
		return err
	}
	for _, imageStream := range imageStreams {
		if _, ok := imageStream.Annotations[releaseAnnotationConfig]; ok {
			c.addQueueKey(queueKey{namespace: imageStream.Namespace, name: imageStream.Name})
		}
	}
	c.gcQueue.AddAfter("", 10*time.Second)
	return nil
}

func (c *Controller) gcWorker() {
	for c.processNextGC() {
	}
	glog.V(4).Infof("Worker stopped")
}

func (c *Controller) processNextGC() bool {
	key, quit := c.gcQueue.Get()
	if quit {
		return false
	}
	defer c.gcQueue.Done(key)

	glog.V(5).Infof("processing %v begin", key)
	err := c.garbageCollectSync()
	c.handleNamespaceErr(c.gcQueue, err, key)
	glog.V(5).Infof("processing %v end", key)

	return true
}

func (c *Controller) auditWorker() {
	for c.processNextAudit() {
	}
	glog.V(4).Infof("Worker stopped")
}

func (c *Controller) processNextAudit() bool {
	key, quit := c.auditQueue.Get()
	if quit {
		return false
	}
	defer c.auditQueue.Done(key)

	glog.V(5).Infof("processing %v begin", key)
	err := c.syncAuditTag(key.(string))
	c.handleNamespaceErr(c.auditQueue, err, key)
	glog.V(5).Infof("processing %v end", key)

	return true
}

// terminalError is a wrapper that indicates the error should be logged but the queue
// key should not be requeued.
type terminalError struct {
	error
}

func (c *Controller) handleNamespaceErr(queue workqueue.RateLimitingInterface, err error, key interface{}) {
	if err == nil {
		queue.Forget(key)
		return
	}

	if _, ok := err.(terminalError); ok {
		glog.V(2).Infof("Unable to sync %v, no retry: %v", key, err)
		return
	}

	glog.V(2).Infof("Error syncing %v: %v", key, err)
	queue.AddRateLimited(key)
}
