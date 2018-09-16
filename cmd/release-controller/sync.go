package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	imagev1 "github.com/openshift/api/image/v1"
)

const (
	releaseImageStreamName = "release"
)

// sync expects to receive a queue key that points to a valid release image input
// or to the entire namespace.
func (c *Controller) sync(key queueKey) error {
	// queue all release inputs in the namespace on a namespace sync
	// this allows us to batch changes together when calculating which resource
	// would be affected is inefficient
	if len(key.name) == 0 {
		imageStreams, err := c.imageStreamLister.ImageStreams(key.namespace).List(labels.Everything())
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
	imageStream, err := c.imageStreamLister.ImageStreams(key.namespace).Get(key.name)
	if errors.IsNotFound(err) {
		return c.cleanupInvariants()
	}
	if err != nil {
		return err
	}
	release, ok, err := c.releaseDefinition(imageStream)
	if err != nil {
		c.eventRecorder.Eventf(imageStream, corev1.EventTypeWarning, "InvalidReleaseDefinition", "%v", err)
		return err
	}
	if !ok {
		return c.cleanupInvariants()
	}

	now := time.Now()
	pending, toPrune, hasNewImages, inputHash := determineNeededActions(release, now)

	if glog.V(4) {
		glog.Infof("name=%s hasNewImages=%t imageHash=%s prune=%v pending=%v", release.Source.Name, hasNewImages, inputHash, tagNames(toPrune), tagNames(pending))
	}

	if len(toPrune) > 0 {
		c.queue.AddAfter(key, time.Second)
		return c.pruneRelease(release, toPrune)
	}

	if len(pending) == 0 && hasNewImages {
		releaseTag, err := c.createReleaseTag(release, now, inputHash)
		if err != nil {
			c.eventRecorder.Eventf(imageStream, corev1.EventTypeWarning, "UnableToCreateRelease", "%v", err)
			return err
		}
		pending = []*imagev1.TagReference{releaseTag}
	}

	verifiedReleases, readyReleases, err := c.syncPending(release, pending, inputHash)
	if err != nil {
		c.eventRecorder.Eventf(imageStream, corev1.EventTypeWarning, "UnableToProcessRelease", "%v", err)
		return err
	}

	if glog.V(4) {
		glog.Infof("ready=%v verified=%v", tagNames(readyReleases), tagNames(verifiedReleases))
	}

	return nil
}

func (c *Controller) releaseDefinition(is *imagev1.ImageStream) (*Release, bool, error) {
	src, ok := is.Annotations["release.openshift.io/config"]
	if !ok {
		return nil, false, nil
	}
	cfg, err := c.parseReleaseConfig(src)
	if err != nil {
		return nil, false, fmt.Errorf("the release.openshift.io/config annotation for %s is invalid: %v", is.Name, err)
	}

	targetImageStream, err := c.imageStreamLister.ImageStreams(c.releaseNamespace).Get(releaseImageStreamName)
	if errors.IsNotFound(err) {
		// TODO: something special here?
		glog.V(2).Infof("The release image stream %s/%s does not exist", c.releaseNamespace, releaseImageStreamName)
		return nil, false, terminalError{fmt.Errorf("the output release image stream %s/%s does not exist", releaseImageStreamName, is.Name)}
	}
	if err != nil {
		return nil, false, fmt.Errorf("unable to lookup release image stream: %v", err)
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
	is, err := c.imageStreamLister.ImageStreams(c.releaseNamespace).Get(releaseImageStreamName)
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
	mirrors, err := c.imageStreamLister.List(labels.Everything())
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

func (c *Controller) syncPending(release *Release, pending []*imagev1.TagReference, inputHash string) (verified []*imagev1.TagReference, ready []*imagev1.TagReference, err error) {
	if len(pending) > 1 {
		if err := c.markReleaseFailed(release, "Aborted", "Multiple releases were found simultaneously running.", tagNames(pending[1:])...); err != nil {
			return nil, nil, err
		}
	}

	if len(pending) > 0 {
		tag := pending[0]
		mirror, err := c.ensureReleaseMirror(release, tag.Name, inputHash)
		if err != nil {
			return nil, nil, err
		}
		if mirror.Annotations["release.openshift.io/hash"] != tag.Annotations["release.openshift.io/hash"] {
			return nil, nil, fmt.Errorf("mirror hash for %q does not match, release cannot be created", tag.Name)
		}

		job, err := c.ensureReleaseJob(release, tag.Name, mirror)
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

func (c *Controller) ensureReleaseJob(release *Release, name string, mirror *imagev1.ImageStream) (*batchv1.Job, error) {
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

func (c *Controller) pruneRelease(release *Release, toPrune []*imagev1.TagReference) error {
	for _, tag := range toPrune {
		if err := c.imageClient.ImageStreamTags(release.Target.Namespace).Delete(fmt.Sprintf("%s:%s", release.Target.Name, tag.Name), nil); err != nil {
			if !errors.IsNotFound(err) {
				return err
			}
		}
	}
	// TODO: loop over all mirror image streams in c.jobNamespace that come from a tag that doesn't exist and delete them
	return nil
}

func (c *Controller) createReleaseTag(release *Release, now time.Time, inputHash string) (*imagev1.TagReference, error) {
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

func (c *Controller) ensureReleaseMirror(release *Release, name, inputHash string) (*imagev1.ImageStream, error) {
	is, err := c.imageStreamLister.ImageStreams(c.jobNamespace).Get(name)
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
