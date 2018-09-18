package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/golang/glog"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	imagev1 "github.com/openshift/api/image/v1"

	prowapiv1 "github.com/openshift/release-controller/pkg/prow/apiv1"
)

// sync expects to receive a queue key that points to a valid release image input
// or to the entire namespace.
func (c *Controller) sync(key queueKey) error {
	defer func() {
		err := recover()
		panic(err)
	}()
	// queue all release inputs in the namespace on a namespace sync
	// this allows us to batch changes together when calculating which resource
	// would be affected is inefficient
	if len(key.name) == 0 {
		imageStreams, err := c.imageStreamLister.ImageStreams(key.namespace).List(labels.Everything())
		if err != nil {
			return err
		}
		for _, imageStream := range imageStreams {
			if _, ok := imageStream.Annotations[releaseAnnotationConfig]; ok {
				c.queue.Add(queueKey{namespace: imageStream.Namespace, name: imageStream.Name})
			}
		}
		return c.garbageCollectUnreferencedObjects()
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
		return c.garbageCollectUnreferencedObjects()
	}
	if err != nil {
		return err
	}
	release, ok, err := c.releaseDefinition(imageStream)
	if err != nil {
		return err
	}
	if !ok {
		return c.garbageCollectUnreferencedObjects()
	}

	now := time.Now()
	pendingTags, removeTags, hasNewImages, inputImageHash := calculateSyncActions(release, now)

	if glog.V(4) {
		glog.Infof("name=%s hasNewImages=%t inputImageHash=%s removeTags=%v pendingTags=%v", release.Source.Name, hasNewImages, inputImageHash, tagNames(removeTags), tagNames(pendingTags))
	}

	// ensure old or unneeded tags are removed
	if len(removeTags) > 0 {
		// requeue this release image stream for safety
		c.queue.AddAfter(key, time.Second)
		return c.removeReleaseTags(release, removeTags)
	}

	// ensure that changes to the input image stream turn into a new release (if no current release is being processed)
	if len(pendingTags) == 0 && hasNewImages {
		releaseTag, err := c.createReleaseTag(release, now, inputImageHash)
		if err != nil {
			c.eventRecorder.Eventf(imageStream, corev1.EventTypeWarning, "UnableToCreateRelease", "%v", err)
			return err
		}
		pendingTags = []*imagev1.TagReference{releaseTag}
	}

	// ensure any pending tags have the necessary jobs/mirrors created
	acceptedReleaseTags, readyReleaseTags, err := c.syncPending(release, pendingTags, inputImageHash)
	if err != nil {
		c.eventRecorder.Eventf(imageStream, corev1.EventTypeWarning, "UnableToProcessRelease", "%v", err)
		return err
	}

	if len(readyReleaseTags) > 0 {
		for _, releaseTag := range readyReleaseTags {
			var missing, urls []string
			var failed string
			for name, verifyType := range release.Config.Verify {
				switch {
				case verifyType.ProwJob != nil:
					job, err := c.ensureProwJobForReleaseTag(release, name, verifyType.ProwJob.Name, releaseTag)
					if err != nil {
						return err
					}
					s, _, err := unstructured.NestedString(job.Object, "status", "state")
					if err != nil {
						return fmt.Errorf("unexpected error accessing prow job definition: %v", err)
					}
					url, _, _ := unstructured.NestedString(job.Object, "status", "url")
					if len(url) > 0 {
						urls = append(urls, url)
					}
					switch s {
					case prowapiv1.SuccessState:
						glog.V(2).Infof("Prow job %s for release %s succeeded, logs at %s", name, releaseTag.Name, url)
					case prowapiv1.FailureState:
						failed = name
						break
					case prowapiv1.AbortedState:
						failed = name
						break
					default:
						missing = append(missing, name)
					}

				default:
					// manual verification
				}
			}

			if len(failed) > 0 {
				glog.V(4).Infof("Release %s was rejected", releaseTag.Name)
				annotations := reasonAndMessage("VerificationFailed", fmt.Sprintf("release verification step %s failed", failed))
				annotations[releaseAnnotationVerifyURLs] = toJSONString(urls)
				if err := c.transitionReleasePhaseFailure(release, []string{releasePhaseReady}, releasePhaseRejected, annotations, releaseTag.Name); err != nil {
					return err
				}
				continue
			}
			if len(missing) > 0 {
				glog.V(4).Infof("Verification jobs for %s still running: %v", releaseTag.Name, missing)
				continue
			}
			if err := c.markReleaseAccepted(release, map[string]string{releaseAnnotationVerifyURLs: toJSONString(urls)}, releaseTag.Name); err != nil {
				return err
			}
			glog.V(4).Infof("Release %s accepted", releaseTag.Name)
			acceptedReleaseTags = append(acceptedReleaseTags, releaseTag)
		}
	}

	// TODO: tag accepted releases
	// TODO: promote accepted releases to external systems

	if glog.V(4) {
		glog.Infof("ready=%v accepted=%v", tagNames(readyReleaseTags), tagNames(acceptedReleaseTags))
	}

	return nil
}

func (c *Controller) ensureProwJobForReleaseTag(release *Release, name, jobName string, releaseTag *imagev1.TagReference) (*unstructured.Unstructured, error) {
	prowJobName := fmt.Sprintf("%s-%s", releaseTag.Name, name)
	obj, exists, err := c.prowLister.GetByKey(fmt.Sprintf("%s/%s", c.prowNamespace, prowJobName))
	if err != nil {
		return nil, err
	}
	if exists {
		// TODO: check metadata on object
		return obj.(*unstructured.Unstructured), nil
	}

	config := c.prowConfigLoader.Config()
	if config == nil {
		err := fmt.Errorf("the prow job %s is not valid: no prow jobs have been defined", jobName)
		c.eventRecorder.Event(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", err.Error())
		return nil, terminalError{err}
	}
	prowSpec, ok := hasProwJob(config, jobName)
	if !ok {
		err := fmt.Errorf("the prow job %s is not valid: no job with that name", jobName)
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", err.Error())
		return nil, terminalError{err}
	}
	spec := prowSpec.DeepCopy()
	mirror, _ := c.imageStreamLister.ImageStreams(c.jobNamespace).Get(releaseTag.Name)
	if err := addReleaseEnvToProwJobSpec(spec, release, mirror, releaseTag); err != nil {
		return nil, err
	}

	pj := &prowapiv1.ProwJob{
		TypeMeta: metav1.TypeMeta{APIVersion: "prow.k8s.io/v1", Kind: "ProwJob"},
		ObjectMeta: metav1.ObjectMeta{
			Name: prowJobName,
			Annotations: map[string]string{
				releaseAnnotationSource: fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),

				"prow.k8s.io/job": spec.Job,
			},
			Labels: map[string]string{
				"release.openshift.io/verify": "true",

				"prow.k8s.io/type": string(spec.Type),
				"prow.k8s.io/job":  spec.Job,
			},
		},
		Spec: *spec,
		Status: prowapiv1.ProwJobStatus{
			State: prowapiv1.PendingState,
		},
	}
	out, err := c.prowClient.Create(objectToUnstructured(pj))
	if errors.IsAlreadyExists(err) {
		// find a cached version or do a live call
		job, exists, err := c.prowLister.GetByKey(fmt.Sprintf("%s/%s", c.prowNamespace, prowJobName))
		if err != nil {
			return nil, err
		}
		if exists {
			return job.(*unstructured.Unstructured), nil
		}
		return c.prowClient.Get(prowJobName, metav1.GetOptions{})
	}
	if errors.IsInvalid(err) {
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", "the prow job %s is not valid: %v", jobName, err)
		return nil, terminalError{err}
	}
	if err != nil {
		return nil, err
	}
	glog.V(2).Infof("Created new prow job %s", pj.Name)
	return out, nil
}

func calculateSyncActions(release *Release, now time.Time) (pendingTags []*imagev1.TagReference, removeTags []*imagev1.TagReference, hasNewImages bool, inputImageHash string) {
	hasNewImages = true
	inputImageHash = hashSpecTagImageDigests(release.Source)
	for i := range release.Target.Spec.Tags {
		tag := &release.Target.Spec.Tags[i]
		if tag.Annotations[releaseAnnotationSource] != fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name) {
			continue
		}

		// if the name has changed, consider the tag abandoned (admin is responsible for cleaning it up)
		if tag.Annotations[releaseAnnotationName] != release.Config.Name {
			continue
		}

		if tag.Annotations[releaseAnnotationImageHash] == inputImageHash {
			hasNewImages = false
		}

		phase := tag.Annotations[releaseAnnotationPhase]
		switch phase {
		case releasePhasePending, "":
			pendingTags = append(pendingTags, tag)
		default:
			if release.Expires == 0 {
				continue
			}
			created, err := time.Parse(time.RFC3339, tag.Annotations[releaseAnnotationCreationTimestamp])
			if err != nil {
				glog.Errorf("Unparseable timestamp on release tag %s:%s: %v", release.Target.Name, tag.Name, err)
				continue
			}
			if created.Add(release.Expires).Before(now) {
				removeTags = append(removeTags)
			}
		}
	}
	// arrange the pendingTags in alphabetic order (which should be based on date)
	sort.Slice(pendingTags, func(i, j int) bool { return pendingTags[i].Name > pendingTags[j].Name })
	return pendingTags, removeTags, hasNewImages, inputImageHash
}

func (c *Controller) syncPending(release *Release, pendingTags []*imagev1.TagReference, inputImageHash string) (accepted []*imagev1.TagReference, ready []*imagev1.TagReference, err error) {
	if len(pendingTags) > 1 {
		if err := c.transitionReleasePhaseFailure(release, []string{releasePhasePending}, releasePhaseFailed, reasonAndMessage("Aborted", "Multiple releases were found simultaneously running."), tagNames(pendingTags[1:])...); err != nil {
			return nil, nil, err
		}
	}

	if len(pendingTags) > 0 {
		tag := pendingTags[0]
		mirror, err := c.ensureReleaseMirror(release, tag.Name, inputImageHash)
		if err != nil {
			return nil, nil, err
		}
		if mirror.Annotations[releaseAnnotationImageHash] != tag.Annotations[releaseAnnotationImageHash] {
			return nil, nil, fmt.Errorf("mirror hash for %q does not match, release cannot be created", tag.Name)
		}

		job, err := c.ensureReleaseJob(release, tag.Name, mirror)
		success, complete := jobIsComplete(job)
		switch {
		case !complete:
			return nil, nil, nil
		case !success:
			// TODO: extract termination message from the job
			if err := c.transitionReleasePhaseFailure(release, []string{releasePhasePending}, releasePhaseFailed, reasonAndMessage("CreateReleaseFailed", "Could not create the release image"), tag.Name); err != nil {
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
		switch tag.Annotations[releaseAnnotationPhase] {
		case releasePhaseReady:
			ready = append(ready, tag)
		case releasePhaseAccepted:
			accepted = append(accepted, tag)
		}
	}

	return accepted, ready, nil
}

func (c *Controller) createReleaseTag(release *Release, now time.Time, inputImageHash string) (*imagev1.TagReference, error) {
	target := release.Target.DeepCopy()
	now = now.UTC().Truncate(time.Second)
	t := now.Format("20060102150405")
	tag := imagev1.TagReference{
		Name: fmt.Sprintf("%s-%s", release.Config.Name, t),
		Annotations: map[string]string{
			releaseAnnotationName:              release.Config.Name,
			releaseAnnotationSource:            fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
			releaseAnnotationCreationTimestamp: now.Format(time.RFC3339),
			releaseAnnotationPhase:             releasePhasePending,
			releaseAnnotationImageHash:         inputImageHash,
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

func (c *Controller) removeReleaseTags(release *Release, removeTags []*imagev1.TagReference) error {
	for _, tag := range removeTags {
		if err := c.imageClient.ImageStreamTags(release.Target.Namespace).Delete(fmt.Sprintf("%s:%s", release.Target.Name, tag.Name), nil); err != nil {
			if !errors.IsNotFound(err) {
				return err
			}
		}
	}
	// TODO: loop over all mirror image streams in c.jobNamespace that come from a tag that doesn't exist and delete them
	return nil
}

func (c *Controller) ensureReleaseMirror(release *Release, name, inputImageHash string) (*imagev1.ImageStream, error) {
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
				releaseAnnotationSource:     fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
				releaseAnnotationImageHash:  inputImageHash,
				releaseAnnotationGeneration: strconv.FormatInt(release.Target.Generation, 10),
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
	job.Annotations[releaseAnnotationSource] = mirror.Annotations[releaseAnnotationSource]
	job.Annotations[releaseAnnotationGeneration] = strconv.FormatInt(release.Target.Generation, 10)

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

func (c *Controller) markReleaseReady(release *Release, names ...string) error {
	return c.transitionReleasePhase(release, []string{releasePhasePending}, releasePhaseReady, nil, names...)
}

func (c *Controller) markReleaseAccepted(release *Release, annotations map[string]string, names ...string) error {
	return c.transitionReleasePhase(release, []string{releasePhaseReady}, releasePhaseAccepted, annotations, names...)
}

func (c *Controller) transitionReleasePhase(release *Release, preconditionPhases []string, phase string, annotations map[string]string, names ...string) error {
	if len(names) == 0 {
		return nil
	}

	target := release.Target.DeepCopy()
	for _, name := range names {
		tag := findTagReference(target, name)
		if tag == nil {
			return fmt.Errorf("release %s no longer exists, cannot be put into %s", name, phase)
		}

		if current := tag.Annotations[releaseAnnotationPhase]; !containsString(preconditionPhases, current) {
			return fmt.Errorf("release %s is not in phase %v (%s), unable to mark %s", name, preconditionPhases, current, phase)
		}
		tag.Annotations[releaseAnnotationPhase] = phase
		delete(tag.Annotations, releaseAnnotationReason)
		delete(tag.Annotations, releaseAnnotationMessage)
		for k, v := range annotations {
			tag.Annotations[k] = v
		}
		glog.V(2).Infof("Marking release %s %s", name, phase)
	}

	is, err := c.imageClient.ImageStreams(target.Namespace).Update(target)
	if err != nil {
		return err
	}
	release.Target = is
	return nil
}

func (c *Controller) transitionReleasePhaseFailure(release *Release, preconditionPhases []string, phase string, annotations map[string]string, names ...string) error {
	target := release.Target.DeepCopy()
	changed := 0
	for _, name := range names {
		if tag := findTagReference(target, name); tag != nil {
			if current := tag.Annotations[releaseAnnotationPhase]; !containsString(preconditionPhases, current) {
				return fmt.Errorf("release %s is not in phase %v (%s), unable to mark %s", name, preconditionPhases, current, phase)
			}
			tag.Annotations[releaseAnnotationPhase] = phase
			for k, v := range annotations {
				tag.Annotations[k] = v
			}
			glog.V(2).Infof("Marking release %s failed: %v", name, annotations)
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

func (c *Controller) garbageCollectUnreferencedObjects() error {
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
			if err := c.jobClient.Jobs(job.Namespace).Delete(job.Name, nil); err != nil && !errors.IsNotFound(err) {
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
			if err := c.imageClient.ImageStreams(mirror.Namespace).Delete(mirror.Name, nil); err != nil && !errors.IsNotFound(err) {
				utilruntime.HandleError(fmt.Errorf("can't delete orphaned release mirror %s: %v", mirror.Name, err))
			}
		}
	}
	return nil
}

func objectToUnstructured(obj runtime.Object) *unstructured.Unstructured {
	buf := &bytes.Buffer{}
	if err := unstructured.UnstructuredJSONScheme.Encode(obj, buf); err != nil {
		panic(err)
	}
	u := &unstructured.Unstructured{}
	if _, _, err := unstructured.UnstructuredJSONScheme.Decode(buf.Bytes(), nil, u); err != nil {
		panic(err)
	}
	return u
}

func addReleaseEnvToProwJobSpec(spec *prowapiv1.ProwJobSpec, release *Release, mirror *imagev1.ImageStream, releaseTag *imagev1.TagReference) error {
	if spec.PodSpec == nil {
		return nil
	}
	for i := range spec.PodSpec.Containers {
		c := &spec.PodSpec.Containers[i]
		for j := range c.Env {
			switch name := c.Env[j].Name; {
			case name == "RELEASE_IMAGE_LATEST":
				c.Env[j].Value = release.Target.Status.PublicDockerImageRepository + releaseTag.Name
			case name == "IMAGE_FORMAT":
				if mirror == nil {
					return fmt.Errorf("unable to determine IMAGE_FORMAT for prow job %s", spec.Job)
				}
				c.Env[j].Value = mirror.Status.PublicDockerImageRepository + ":${component}"
			case strings.HasPrefix(name, "IMAGE_"):
				suffix := strings.TrimPrefix(name, "IMAGE_")
				if len(suffix) == 0 {
					break
				}
				if mirror == nil {
					return fmt.Errorf("unable to determine IMAGE_FORMAT for prow job %s", spec.Job)
				}
				suffix = strings.ToLower(strings.Replace(suffix, "_", "-", -1))
				c.Env[j].Value = mirror.Status.PublicDockerImageRepository + ":" + suffix
			}
		}
	}
	return nil
}

func hasProwJob(config *prowapiv1.Config, name string) (*prowapiv1.ProwJobSpec, bool) {
	for i := range config.Jobs {
		if config.Jobs[i].Job == name {
			return &config.Jobs[i], true
		}
	}
	return nil, false
}

func containsString(arr []string, s string) bool {
	for _, str := range arr {
		if s == str {
			return true
		}
	}
	return false
}

func reasonAndMessage(reason, message string) map[string]string {
	return map[string]string{
		releaseAnnotationReason:  reason,
		releaseAnnotationMessage: message,
	}
}

func toJSONString(data interface{}) string {
	out, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	return string(out)
}
