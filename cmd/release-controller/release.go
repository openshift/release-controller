package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	lru "github.com/hashicorp/golang-lru"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
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
)

// Controller ensures ...
//
// Invariants:
//
// 1. ...
//
type Controller struct {
	eventRecorder record.EventRecorder

	imageClient       imageclient.ImageV1Interface
	imagestreamLister imagelisters.ImageStreamLister

	jobClient batchclient.JobsGetter
	jobLister batchlisters.JobLister

	// syncs are the items that must return true before the queue can be processed
	syncs []cache.InformerSynced

	// queue is the list of namespace keys that must be synced.
	queue workqueue.RateLimitingInterface

	// expectations track upcoming route creations that we have not yet observed
	expectations *expectations
	// expectationDelay controls how long the controller waits to observe its
	// own creates. Exposed only for testing.
	expectationDelay time.Duration

	releaseNamespace string
	jobNamespace     string

	sourceCache *lru.Cache
}

// NewController instantiates a Controller
func NewController(
	eventsClient kv1core.EventsGetter,
	imageClient imageclient.ImageV1Interface,
	imagestreams imageinformers.ImageStreamInformer,
	jobClient batchclient.JobsGetter,
	jobs batchinformers.JobInformer,
	releaseNamespace string,
	jobNamespace string,
) *Controller {
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

		imageClient:       imageClient,
		imagestreamLister: imagestreams.Lister(),

		jobClient: jobClient,
		jobLister: jobs.Lister(),

		syncs: []cache.InformerSynced{
			imagestreams.Informer().HasSynced,
		},

		releaseNamespace: releaseNamespace,
		jobNamespace:     jobNamespace,

		sourceCache: sourceCache,
	}

	// any change to a job
	jobs.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.processJobIfComplete,
		DeleteFunc: c.processJob,
		UpdateFunc: func(oldObj, newObj interface{}) { c.processJobIfComplete(newObj) },
	})

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

	// changes to image streams
	imagestreams.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.processImageStream,
		DeleteFunc: c.processImageStream,
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.processImageStream(newObj)
		},
	})

	return c
}

// expectations track an upcoming change to a named resource related
// to a release. This is a thread safe object but callers assume
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

// Expect that an event will happen in the future for the given release
// and a named resource related to that release.
func (e *expectations) Expect(namespace, parentName, name string) {
	e.lock.Lock()
	defer e.lock.Unlock()
	key := queueKey{namespace: namespace, name: parentName}
	set, ok := e.expect[key]
	if !ok {
		set = sets.NewString()
		e.expect[key] = set
	}
	set.Insert(name)
}

// Satisfied clears the expectation for the given resource name on an
// release.
func (e *expectations) Satisfied(namespace, parentName, name string) {
	e.lock.Lock()
	defer e.lock.Unlock()
	key := queueKey{namespace: namespace, name: parentName}
	set := e.expect[key]
	set.Delete(name)
	if set.Len() == 0 {
		delete(e.expect, key)
	}
}

// Expecting returns true if the provided release is still waiting to
// see changes.
func (e *expectations) Expecting(namespace, parentName string) bool {
	e.lock.Lock()
	defer e.lock.Unlock()
	key := queueKey{namespace: namespace, name: parentName}
	return e.expect[key].Len() > 0
}

// Clear indicates that all expectations for the given release should
// be cleared.
func (e *expectations) Clear(namespace, parentName string) {
	e.lock.Lock()
	defer e.lock.Unlock()
	key := queueKey{namespace: namespace, name: parentName}
	delete(e.expect, key)
}

type queueKey struct {
	namespace string
	name      string
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
	switch t := obj.(type) {
	case *batchv1.Job:
		key, ok := queueKeyFor(t.Annotations["release.openshift.io/source"])
		if !ok {
			return
		}
		if glog.V(4) {
			success, complete := jobIsComplete(t)
			glog.Infof("Job %s updated, complete=%t success=%t", t.Name, complete, success)
		}
		c.queue.Add(key)
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

func (c *Controller) processImageStream(obj interface{}) {
	switch t := obj.(type) {
	case *imagev1.ImageStream:
		// when we see a change to an image stream, reset our expectations
		// this also allows periodic purging of the expectation list in the event
		// we miss one or more events.
		c.expectations.Clear(t.Namespace, t.Name)

		// if this image stream is a mirror for releases, requeue any that it touches
		if _, ok := t.Annotations["release.openshift.io/config"]; ok {
			glog.V(5).Infof("Image stream %s is a release input and will be queued", t.Name)
			c.queue.Add(queueKey{namespace: t.Namespace, name: t.Name})
			return
		}
		if key, ok := queueKeyFor(t.Annotations["release.openshift.io/source"]); ok {
			glog.V(5).Infof("Image stream %s was created by %v, queuing source", t.Name, key)
			c.queue.Add(key)
			return
		}
		if t.Name == "release" {
			// if the release image stream is modified, just requeue everything in the event a tag
			// has been deleted
			glog.V(5).Infof("Image stream %s is a release target, requeue the entire namespace", t.Name)
			c.queue.Add(queueKey{namespace: t.Namespace})
			return
		}
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

// sync expects to receive a queue key that points to a valid release image input
// or to the entire namespace.
func (c *Controller) sync(key queueKey) error {
	// queue all release inputs in the namespace on a namespace sync
	// this allows us to batch changes together when calculating which resource
	// would be affected is inefficient
	if len(key.name) == 0 {
		imageStreams, err := c.imagestreamLister.ImageStreams(key.namespace).List(labels.Everything())
		if err != nil {
			return err
		}
		for _, imageStream := range imageStreams {
			if _, ok := imageStream.Annotations["release.openshift.io/config"]; ok {
				c.queue.Add(queueKey{namespace: imageStream.Namespace, name: imageStream.Name})
			}
		}
		return c.cleanupInvariants()
	}

	// if we are waiting to observe the result of our previous actions, simply delay
	if c.expectations.Expecting(key.namespace, key.name) {
		c.queue.AddAfter(key, c.expectationDelay)
		glog.V(5).Infof("Release %s has unsatisfied expectations", key.name)
		return nil
	}

	// locate the release definition off the image stream, or clean up any remaining
	// artifacts if the release no longer points to those
	imageStream, err := c.imagestreamLister.ImageStreams(key.namespace).Get(key.name)
	if errors.IsNotFound(err) {
		return c.cleanupInvariants()
	}
	if err != nil {
		return err
	}
	release, ok, err := c.releaseDefinition(imageStream)
	if err != nil {
		c.eventRecorder.Eventf(imageStream, corev1.EventTypeWarning, "InvalidReleaseDefinition", "The release is not valid: %v", err)
		return nil
	}
	if !ok {
		return c.cleanupInvariants()
	}

	now := time.Now()
	pendingReleases, pruneReleases, newImages, inputHash := determineNeededActions(release, now)

	if glog.V(4) {
		glog.Infof("name=%s newImages=%t imageHash=%s prune=%v pending=%v", release.Source.Name, newImages, inputHash, tagNames(pruneReleases), tagNames(pendingReleases))
	}

	if len(pruneReleases) > 0 {
		c.queue.AddAfter(key, time.Second)
		return c.pruneRelease(release, pruneReleases)
	}

	if len(pendingReleases) == 0 && newImages {
		pending, err := c.createRelease(release, now, inputHash)
		if err != nil {
			c.eventRecorder.Eventf(imageStream, corev1.EventTypeWarning, "UnableToCreateRelease", "%v", err)
			return err
		}
		pendingReleases = []*imagev1.TagReference{pending}
	}

	verifiedReleases, readyReleases, err := c.syncPending(release, pendingReleases, inputHash)
	if err != nil {
		c.eventRecorder.Eventf(imageStream, corev1.EventTypeWarning, "UnableToProcessRelease", "%v", err)
		return err
	}

	if glog.V(4) {
		glog.Infof("ready=%v verified=%v", tagNames(readyReleases), tagNames(verifiedReleases))
	}

	return nil
}

type Release struct {
	Source  *imagev1.ImageStream
	Target  *imagev1.ImageStream
	Config  *ReleaseConfig
	Expires time.Duration
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

	if len(is.Status.Tags) == 0 {
		glog.V(4).Infof("The release input has no status tags, waiting")
		return nil, false, nil
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

func (c *Controller) cleanupInvariants() error {
	is, err := c.imagestreamLister.ImageStreams(c.releaseNamespace).Get("release")
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	validReleases := make(map[string]struct{})
	for _, tag := range is.Spec.Tags {
		validReleases[tag.Name] = struct{}{}
	}

	// all jobs created for a release that no longer exists should be deleted
	jobs, err := c.jobLister.List(labels.Everything())
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if _, ok := validReleases[job.Name]; ok {
			continue
		}
		generation, ok := releaseGenerationFromObject(job.Name, job.Annotations)
		if !ok {
			continue
		}
		if generation < is.Generation {
			glog.V(2).Infof("Removing orphaned release job %s", job.Name)
			if err := c.jobClient.Jobs(job.Namespace).Delete(job.Name, nil); err != nil {
				utilruntime.HandleError(fmt.Errorf("can't delete orphaned release job %s: %v", job.Name, err))
			}
		}
	}

	// all image mirrors created for a release that no longer exists should be deleted
	mirrors, err := c.imagestreamLister.List(labels.Everything())
	if err != nil {
		return err
	}
	for _, mirror := range mirrors {
		if _, ok := validReleases[mirror.Name]; ok {
			continue
		}
		generation, ok := releaseGenerationFromObject(mirror.Name, mirror.Annotations)
		if !ok {
			continue
		}
		if generation < is.Generation {
			glog.V(2).Infof("Removing orphaned release mirror %s", mirror.Name)
			if err := c.imageClient.ImageStreams(mirror.Namespace).Delete(mirror.Name, nil); err != nil {
				utilruntime.HandleError(fmt.Errorf("can't delete orphaned release mirror %s: %v", mirror.Name, err))
			}
		}
	}
	return nil
}

func releaseGenerationFromObject(name string, annotations map[string]string) (int64, bool) {
	_, ok := annotations["release.openshift.io/source"]
	if !ok {
		return 0, false
	}
	s, ok := annotations["release.openshift.io/generation"]
	if !ok {
		glog.V(4).Infof("Can't check job %s, no generation", name)
		return 0, false
	}
	generation, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		glog.V(4).Infof("Can't check job %s, generation is invalid: %v", name, err)
		return 0, false
	}
	return generation, true
}

func determineNeededActions(release *Release, now time.Time) (pending []*imagev1.TagReference, prune []*imagev1.TagReference, pendingImages bool, inputHash string) {
	pendingImages = true
	inputHash = calculateSourceHash(release.Source)
	for i := range release.Target.Spec.Tags {
		tag := &release.Target.Spec.Tags[i]
		if tag.Annotations["release.openshift.io/source"] != fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name) {
			continue
		}

		if tag.Annotations["release.openshift.io/name"] != release.Config.Name {
			continue
		}

		if tag.Annotations["release.openshift.io/hash"] == inputHash {
			pendingImages = false
		}

		phase := tag.Annotations["release.openshift.io/phase"]
		switch phase {
		case "Pending", "":
			pending = append(pending, tag)
		case "Verified", "Ready":
			if release.Expires == 0 {
				continue
			}
			created, err := time.Parse(time.RFC3339, tag.Annotations["release.openshift.io/creationTimestamp"])
			if err != nil {
				glog.Errorf("Unparseable timestamp on release tag %s:%s: %v", release.Target.Name, tag.Name, err)
				continue
			}
			if created.Add(release.Expires).Before(now) {
				prune = append(prune)
			}
		}
	}
	// arrange the pending by rough alphabetic order
	sort.Slice(pending, func(i, j int) bool { return pending[i].Name > pending[j].Name })
	return pending, prune, pendingImages, inputHash
}

func (c *Controller) syncPending(release *Release, pendingReleases []*imagev1.TagReference, inputHash string) (verified []*imagev1.TagReference, ready []*imagev1.TagReference, err error) {
	if len(pendingReleases) > 1 {
		if err := c.markReleaseFailed(release, "Aborted", "Multiple releases were found simultaneously running.", tagNames(pendingReleases[1:])...); err != nil {
			return nil, nil, err
		}
	}

	if len(pendingReleases) > 0 {
		tag := pendingReleases[0]
		mirror, err := c.getOrCreateReleaseMirror(release, tag.Name, inputHash)
		if err != nil {
			return nil, nil, err
		}
		if mirror.Annotations["release.openshift.io/hash"] != tag.Annotations["release.openshift.io/hash"] {
			return nil, nil, fmt.Errorf("mirror hash for %q does not match, release cannot be created", tag.Name)
		}

		job, err := c.getOrCreateReleaseJob(release, tag.Name, mirror)
		success, complete := jobIsComplete(job)
		switch {
		case !complete:
			return nil, nil, nil
		case !success:
			// TODO: extract termination message from the job
			if err := c.markReleaseFailed(release, "CreateReleaseFailed", "Could not create the release image", tag.Name); err != nil {
				return nil, nil, err
			}
		default:
			if err := c.markReleaseReady(release, tag.Name); err != nil {
				return nil, nil, err
			}
		}
	}

	for i := range release.Target.Spec.Tags {
		tag := &release.Target.Spec.Tags[i]
		if tag.Annotations["release.openshift.io/phase"] == "Ready" {
			ready = append(ready, tag)
		}
	}

	return nil, ready, nil
}

func (c *Controller) getOrCreateReleaseJob(release *Release, name string, mirror *imagev1.ImageStream) (*batchv1.Job, error) {
	job, err := c.jobLister.Jobs(c.jobNamespace).Get(name)
	if err == nil {
		return job, nil
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}

	toImage := fmt.Sprintf("%s:%s", release.Target.Status.PublicDockerImageRepository, name)
	toImageBase := fmt.Sprintf("%s:cluster-version-operator", mirror.Status.PublicDockerImageRepository)

	job = newReleaseJob(name, c.jobNamespace, toImage, toImageBase)
	job.Annotations["release.openshift.io/source"] = mirror.Annotations["release.openshift.io/source"]
	job.Annotations["release.openshift.io/generation"] = strconv.FormatInt(release.Target.Generation, 10)

	glog.V(2).Infof("Running release creation job for %s", name)
	job, err = c.jobClient.Jobs(c.jobNamespace).Create(job)
	if err == nil {
		return job, nil
	}
	if !errors.IsAlreadyExists(err) {
		return nil, err
	}

	// perform a live lookup if we are racing to create the job
	return c.jobClient.Jobs(c.jobNamespace).Get(name, metav1.GetOptions{})
}

func newReleaseJob(name, namespace, toImage, toImageBase string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: int32p(3),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: "builder",
					RestartPolicy:      corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "build",
							Image: "openshift/origin-cli:v4.0",
							Env: []corev1.EnvVar{
								{Name: "HOME", Value: "/tmp"},
							},
							Command: []string{
								"/bin/bash", "-c", `
								set -e
								oc registry login
								oc adm release new --name $1 --from-image-stream $2 --namespace $3 --to-image $4 --to-image-base $5
								`, "",
								name, name, namespace, toImage, toImageBase,
							},
							TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
						},
					},
				},
			},
		},
	}
}

func (c *Controller) pruneRelease(release *Release, pruneReleases []*imagev1.TagReference) error {
	for _, tag := range pruneReleases {
		if err := c.imageClient.ImageStreamTags(release.Target.Namespace).Delete(fmt.Sprintf("%s:%s", release.Target.Name, tag.Name), nil); err != nil {
			if !errors.IsNotFound(err) {
				return err
			}
		}
	}
	// TODO: loop over all mirror image streams in c.jobNamespace that come from a tag that doesn't exist and delete them
	return nil
}

func (c *Controller) createRelease(release *Release, now time.Time, inputHash string) (*imagev1.TagReference, error) {
	target := release.Target.DeepCopy()
	now = now.UTC().Truncate(time.Second)
	t := now.Format("20060102150405")
	tag := imagev1.TagReference{
		Name: fmt.Sprintf("%s-%s", release.Config.Name, t),
		Annotations: map[string]string{
			"release.openshift.io/name":              release.Config.Name,
			"release.openshift.io/source":            fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
			"release.openshift.io/creationTimestamp": now.Format(time.RFC3339),
			"release.openshift.io/phase":             "Pending",
			"release.openshift.io/hash":              inputHash,
		},
	}
	target.Spec.Tags = append(target.Spec.Tags, tag)

	glog.V(2).Infof("Starting new release %s", tag.Name)

	is, err := c.imageClient.ImageStreams(target.Namespace).Update(target)
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	release.Target = is

	return &is.Spec.Tags[len(is.Spec.Tags)-1], nil
}

func (c *Controller) markReleaseReady(release *Release, names ...string) error {
	if len(names) == 0 {
		return nil
	}

	target := release.Target.DeepCopy()
	for _, name := range names {
		tag := findTagReference(target, name)
		if tag == nil {
			return fmt.Errorf("release %s no longer exists, cannot be made ready", name)
		}

		if phase := tag.Annotations["release.openshift.io/phase"]; phase != "Pending" {
			return fmt.Errorf("release %s is not Pending (%s), unable to mark ready", name, phase)
		}
		tag.Annotations["release.openshift.io/phase"] = "Ready"
		delete(tag.Annotations, "release.openshift.io/reason")
		delete(tag.Annotations, "release.openshift.io/message")
		glog.V(2).Infof("Marking release %s ready", name)
	}

	is, err := c.imageClient.ImageStreams(target.Namespace).Update(target)
	if err != nil {
		return err
	}
	release.Target = is
	return nil
}

func (c *Controller) markReleaseFailed(release *Release, reason, message string, names ...string) error {
	target := release.Target.DeepCopy()
	changed := 0
	for _, name := range names {
		if tag := findTagReference(target, name); tag != nil {
			tag.Annotations["release.openshift.io/phase"] = "Failed"
			tag.Annotations["release.openshift.io/reason"] = reason
			tag.Annotations["release.openshift.io/message"] = message
			glog.V(2).Infof("Marking release %s failed: %s %s", name, reason, message)
			changed++
		}
	}
	if changed == 0 {
		// release tags have all been deleted
		return nil
	}

	is, err := c.imageClient.ImageStreams(target.Namespace).Update(target)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	release.Target = is
	return nil
}

func (c *Controller) getOrCreateReleaseMirror(release *Release, name, inputHash string) (*imagev1.ImageStream, error) {
	is, err := c.imagestreamLister.ImageStreams(c.jobNamespace).Get(name)
	if err == nil {
		return is, nil
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}

	is = &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				"release.openshift.io/source":     fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
				"release.openshift.io/hash":       inputHash,
				"release.openshift.io/generation": strconv.FormatInt(release.Target.Generation, 10),
			},
		},
	}
	for _, statusTag := range release.Source.Status.Tags {
		if len(statusTag.Items) == 0 {
			continue
		}
		latest := statusTag.Items[0]
		if len(latest.Image) == 0 {
			continue
		}

		is.Spec.Tags = append(is.Spec.Tags, imagev1.TagReference{
			Name: statusTag.Tag,
			From: &corev1.ObjectReference{
				Kind:      "ImageStreamImage",
				Namespace: release.Source.Namespace,
				Name:      fmt.Sprintf("%s@%s", release.Source.Name, latest.Image),
			},
		})
	}
	glog.V(2).Infof("Mirroring release images in %s/%s to %s/%s", release.Source.Namespace, release.Source.Name, c.jobNamespace, is.Name)
	is, err = c.imageClient.ImageStreams(c.jobNamespace).Create(is)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return nil, err
		}
		// perform a live read
		is, err = c.imageClient.ImageStreams(c.jobNamespace).Get(is.Name, metav1.GetOptions{})
	}
	return is, err
}

func calculateSourceHash(is *imagev1.ImageStream) string {
	h := sha256.New()
	for _, tag := range is.Status.Tags {
		if len(tag.Items) == 0 {
			continue
		}
		latest := tag.Items[0]
		input := latest.Image
		if len(input) == 0 {
			input = latest.DockerImageReference
		}
		h.Write([]byte(input))
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}

func queueKeyFor(annotation string) (queueKey, bool) {
	if len(annotation) == 0 {
		return queueKey{}, false
	}
	parts := strings.SplitN(annotation, "/", 2)
	if len(parts) != 2 {
		return queueKey{}, false
	}
	return queueKey{namespace: parts[0], name: parts[1]}, true
}

func tagNames(refs []*imagev1.TagReference) []string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Name)
	}
	return names
}

func int32p(i int32) *int32 {
	return &i
}

func containsTagReference(tags []*imagev1.TagReference, name string) bool {
	for _, tag := range tags {
		if name == tag.Name {
			return true
		}
	}
	return false
}

func jobIsComplete(job *batchv1.Job) (succeeded bool, complete bool) {
	if job.Status.CompletionTime != nil {
		return true, true
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return false, true
		}
	}
	return false, false
}

func findTagReference(is *imagev1.ImageStream, name string) *imagev1.TagReference {
	for i := range is.Spec.Tags {
		tag := &is.Spec.Tags[i]
		if tag.Name == name {
			return tag
		}
	}
	return nil
}
