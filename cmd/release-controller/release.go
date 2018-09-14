package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/golang/glog"
	lru "github.com/hashicorp/golang-lru"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	kv1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	imagev1 "github.com/openshift/api/image/v1"
	imagescheme "github.com/openshift/client-go/image/clientset/versioned/scheme"
	imageclient "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions/image/v1"
	imagelisters "github.com/openshift/client-go/image/listers/image/v1"
)

// Controller ensures that zero or more routes exist to match any supported ingress. The
// controller creates a controller owner reference from the route to the parent ingress,
// allowing users to orphan their ingress. All owned routes have specific spec fields
// managed (those attributes present on the ingress), while any other fields may be
// modified by the user.
//
// Invariants:
//
// 1. For every ingress path rule with a non-empty backend statement, a route should
//    exist that points to that backend.
// 2. For every TLS hostname that has a corresponding path rule and points to a secret
//    that exists, a route should exist with a valid TLS config from that secret.
// 3. For every service referenced by the ingress path rule, the route should have
//    a target port based on the service.
// 4. A route owned by an ingress that is not described by any of the three invariants
//    above should be deleted.
//
// The controller also relies on the use of expectations to remind itself whether there
// are route creations it has not yet observed, which prevents the controller from
// creating more objects than it needs. The expectations are reset when the ingress
// object is modified. It is possible that expectations could leak if an ingress is
// deleted and its deletion is not observed by the cache, but such leaks are only expected
// if there is a bug in the informer cache which must be fixed anyway.
//
// Unsupported attributes:
//
// * the ingress class attribute
// * nginx annotations
// * the empty backend
// * paths with empty backends
// * creating a dynamic route spec.host
//
type Controller struct {
	eventRecorder record.EventRecorder

	imageClient imageclient.ImageStreamsGetter

	imagestreamLister imagelisters.ImageStreamLister

	// syncs are the items that must return true before the queue can be processed
	syncs []cache.InformerSynced

	// queue is the list of namespace keys that must be synced.
	queue workqueue.RateLimitingInterface

	// expectations track upcoming route creations that we have not yet observed
	expectations *expectations
	// expectationDelay controls how long the controller waits to observe its
	// own creates. Exposed only for testing.
	expectationDelay time.Duration

	sourceCache *lru.Cache
}

// expectations track an upcoming change to a named resource related
// to an ingress. This is a thread safe object but callers assume
// responsibility for ensuring expectations do not leak.
type expectations struct {
	lock   sync.Mutex
	expect map[queueKey]sets.String
}

// newExpectations returns a tracking object for upcoming events
// that the controller may expect to happen.
func newExpectations() *expectations {
	return &expectations{
		expect: make(map[queueKey]sets.String),
	}
}

// Expect that an event will happen in the future for the given ingress
// and a named resource related to that ingress.
func (e *expectations) Expect(namespace, ingressName, name string) {
	e.lock.Lock()
	defer e.lock.Unlock()
	key := queueKey{namespace: namespace, name: ingressName}
	set, ok := e.expect[key]
	if !ok {
		set = sets.NewString()
		e.expect[key] = set
	}
	set.Insert(name)
}

// Satisfied clears the expectation for the given resource name on an
// ingress.
func (e *expectations) Satisfied(namespace, ingressName, name string) {
	e.lock.Lock()
	defer e.lock.Unlock()
	key := queueKey{namespace: namespace, name: ingressName}
	set := e.expect[key]
	set.Delete(name)
	if set.Len() == 0 {
		delete(e.expect, key)
	}
}

// Expecting returns true if the provided ingress is still waiting to
// see changes.
func (e *expectations) Expecting(namespace, ingressName string) bool {
	e.lock.Lock()
	defer e.lock.Unlock()
	key := queueKey{namespace: namespace, name: ingressName}
	return e.expect[key].Len() > 0
}

// Clear indicates that all expectations for the given ingress should
// be cleared.
func (e *expectations) Clear(namespace, ingressName string) {
	e.lock.Lock()
	defer e.lock.Unlock()
	key := queueKey{namespace: namespace, name: ingressName}
	delete(e.expect, key)
}

type queueKey struct {
	namespace string
	name      string
}

// NewController instantiates a Controller
func NewController(eventsClient kv1core.EventsGetter, imageClient imageclient.ImageStreamsGetter, imagestreams imageinformers.ImageStreamInformer) *Controller {
	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.V(2).Infof)
	// TODO: remove the wrapper when every clients have moved to use the clientset.
	broadcaster.StartRecordingToSink(&kv1core.EventSinkImpl{Interface: eventsClient.Events("")})
	recorder := broadcaster.NewRecorder(imagescheme.Scheme, corev1.EventSource{Component: "release-controller"})

	sourceCache, err := lru.New(50)
	if err != nil {
		panic(err)
	}

	c := &Controller{
		eventRecorder: recorder,
		queue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "release"),

		expectations:     newExpectations(),
		expectationDelay: 2 * time.Second,

		imageClient: imageClient,

		imagestreamLister: imagestreams.Lister(),

		syncs: []cache.InformerSynced{
			imagestreams.Informer().HasSynced,
		},

		sourceCache: sourceCache,
	}

	// any change to a service in the namespace
	// services.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
	// 	AddFunc:    c.processNamespace,
	// 	DeleteFunc: c.processNamespace,
	// 	UpdateFunc: func(oldObj, newObj interface{}) {
	// 		c.processNamespace(newObj)
	// 	},
	// })

	// any change to a route that has the controller relationship to an ImageStream
	// routes.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
	// 	FilterFunc: func(obj interface{}) bool {
	// 		switch t := obj.(type) {
	// 		case *routev1.Route:
	// 			_, ok := hasIngressOwnerRef(t.OwnerReferences)
	// 			return ok
	// 		}
	// 		return true
	// 	},
	// 	Handler: cache.ResourceEventHandlerFuncs{
	// 		AddFunc:    c.processRoute,
	// 		DeleteFunc: c.processRoute,
	// 		UpdateFunc: func(oldObj, newObj interface{}) {
	// 			c.processRoute(newObj)
	// 		},
	// 	},
	// })

	// changes to ingresses
	imagestreams.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.processImageStream,
		DeleteFunc: c.processImageStream,
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.processImageStream(newObj)
		},
	})

	return c
}

func (c *Controller) processNamespace(obj interface{}) {
	switch t := obj.(type) {
	case metav1.Object:
		ns := t.GetNamespace()
		if len(ns) == 0 {
			utilruntime.HandleError(fmt.Errorf("object %T has no namespace", obj))
			return
		}
		c.queue.Add(queueKey{namespace: ns})
	default:
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %T", obj))
	}
}

func (c *Controller) processJob(obj interface{}) {
	// switch t := obj.(type) {
	// case *routev1.Route:
	// 	ingressName, ok := hasIngressOwnerRef(t.OwnerReferences)
	// 	if !ok {
	// 		return
	// 	}
	// 	c.expectations.Satisfied(t.Namespace, ingressName, t.Name)
	// 	c.queue.Add(queueKey{namespace: t.Namespace, name: ingressName})
	// default:
	// 	utilruntime.HandleError(fmt.Errorf("couldn't get key for object %T", obj))
	// }
}

func (c *Controller) processImageStream(obj interface{}) {
	switch t := obj.(type) {
	case *imagev1.ImageStream:
		// when we see a change to an image stream, reset our expectations
		// this also allows periodic purging of the expectation list in the event
		// we miss one or more events.
		c.expectations.Clear(t.Namespace, t.Name)
		c.queue.Add(queueKey{namespace: t.Namespace, name: t.Name})
	default:
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %T", obj))
	}
}

// Run begins watching and syncing.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
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

	<-stopCh
	glog.Infof("Shutting down controller")
}

func (c *Controller) worker() {
	for c.processNext() {
	}
	glog.V(4).Infof("Worker stopped")
}

func (c *Controller) processNext() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	glog.V(5).Infof("processing %v begin", key)
	err := c.sync(key.(queueKey))
	c.handleNamespaceErr(err, key)
	glog.V(5).Infof("processing %v end", key)

	return true
}

func (c *Controller) handleNamespaceErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	glog.V(4).Infof("Error syncing %v: %v", key, err)
	c.queue.AddRateLimited(key)
}

func (c *Controller) sync(key queueKey) error {
	// sync all image streams in the namespace
	if len(key.name) == 0 {
		ingresses, err := c.imagestreamLister.ImageStreams(key.namespace).List(labels.Everything())
		if err != nil {
			return err
		}
		for _, ingress := range ingresses {
			c.queue.Add(queueKey{namespace: ingress.Namespace, name: ingress.Name})
		}
		return nil
	}
	// if we are waiting to observe the result of our previous actions, simply delay
	if c.expectations.Expecting(key.namespace, key.name) {
		c.queue.AddAfter(key, c.expectationDelay)
		glog.V(5).Infof("Release %s has unsatisfied expectations", key.name)
		return nil
	}

	// locate the release definition off the image stream, or clean up any remaining
	// artifacts if the release no longer points to those
	is, err := c.imagestreamLister.ImageStreams(key.namespace).Get(key.name)
	if errors.IsNotFound(err) {
		return c.cleanupRelease(key.namespace, key.name)
	}
	if err != nil {
		return err
	}
	release, ok, err := c.releaseDefinition(is)
	if err != nil {
		c.eventRecorder.Eventf(is, corev1.EventTypeWarning, "InvalidReleaseDefinition", "Unable to read image stream for release definition: %v", err)
		return nil
	}
	if !ok {
		return c.cleanupRelease(key.namespace, key.name)
	}
	glog.V(4).Infof("Processing release image stream %s: %#v", key.name, release.Config)

	pendingReleases, pruneReleases := findPendingReleases(release)
	completedReleases, pendingReleases, err := c.syncPendingReleases(release, pendingReleases)
	if err != nil {
		return err
	}

	glog.V(4).Infof("pending  %#v", pendingReleases)
	glog.V(4).Infof("prune    %#v", pruneReleases)
	glog.V(4).Infof("complete %#v", completedReleases)

	var errs []error

	// if pruneErrs := c.pruneReleases(targetImageStream, pruneReleases); len(pruneErrs) > 0 {
	// 	errs = append(errs, pruneErrs...)
	// }
	// if syncErrs := c.resyncPendingReleases(targetImageStream, pendingReleases); len(syncErrs) > 0 {
	// 	errs = append(errs, syncErrs...)
	// }
	// if len(pendingReleases) == 0 {
	// 	if err :=  c.createRelease(targetImageStream, release); err != nil {
	// 		errs = append(errs, err)
	// 	}
	// }

	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}
	return nil
}

type Release struct {
	Source  *imagev1.ImageStream
	Target  *imagev1.ImageStream
	Config  *ReleaseConfig
	Expires time.Time
}

type ReleaseConfig struct {
	Name string `json:"name"`
}

func (c *Controller) releaseDefinition(is *imagev1.ImageStream) (*Release, bool, error) {
	src, ok := is.Annotations["release.openshift.io/config"]
	if !ok {
		return nil, false, nil
	}

	cfg, err := c.parseReleaseConfig(src)
	if err != nil {
		return nil, false, fmt.Errorf("the release config annotation for %s is invalid: %v", is.Name, err)
	}

	targetImageStream, err := c.imagestreamLister.ImageStreams(is.Namespace).Get("release")
	if errors.IsNotFound(err) {
		// TODO: something special here?
		return nil, false, fmt.Errorf("unable to locate target image stream %s for release %s: %v", "release", is.Name, err)
	}
	if err != nil {
		return nil, false, err
	}

	r := &Release{
		Source: is,
		Target: targetImageStream,
		Config: cfg,
	}
	return r, true, nil
}

func (c *Controller) parseReleaseConfig(data string) (*ReleaseConfig, error) {
	if len(data) > 4*1024 {
		return nil, fmt.Errorf("release config must be less than 4k")
	}
	obj, ok := c.sourceCache.Get(data)
	if ok {
		cfg := obj.(ReleaseConfig)
		return &cfg, nil
	}
	cfg := &ReleaseConfig{}
	if err := json.Unmarshal([]byte(data), cfg); err != nil {
		return nil, err
	}
	if len(cfg.Name) == 0 {
		return nil, fmt.Errorf("release config must have a valid name")
	}
	copied := *cfg
	c.sourceCache.Add(data, copied)
	return cfg, nil
}

func (c *Controller) cleanupRelease(namespace, name string) error {
	return nil
}

func findPendingReleases(release *Release) (pending []*imagev1.TagReference, prune []*imagev1.TagReference) {
	return nil, nil
}

func (c *Controller) syncPendingReleases(release *Release, pendingReleases []*imagev1.TagReference) (completed []*imagev1.TagReference, pending []*imagev1.TagReference, err error) {
	return nil, nil, nil
}
