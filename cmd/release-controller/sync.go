package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"

	imagev1 "github.com/openshift/api/image/v1"
	imagereference "github.com/openshift/library-go/pkg/image/reference"

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
				c.addQueueKey(queueKey{namespace: imageStream.Namespace, name: imageStream.Name})
			}
		}
		return c.garbageCollectUnreferencedObjects()
	}

	// if we are waiting to observe the result of our previous actions, simply delay
	// if c.expectations.Expecting(key.namespace, key.name) {
	// 	c.queue.AddAfter(key, c.expectationDelay)
	// 	glog.V(5).Infof("Release %s has unsatisfied expectations", key.name)
	// 	return nil
	// }

	// locate the release definition off the image stream, or clean up any remaining
	// artifacts if the release no longer points to those
	isLister := c.imageStreamLister.ImageStreams(key.namespace)
	imageStream, err := isLister.Get(key.name)
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
	if err := c.syncPending(release, pendingTags, inputImageHash); err != nil {
		c.eventRecorder.Eventf(imageStream, corev1.EventTypeWarning, "UnableToProcessRelease", "%v", err)
		return err
	}

	// ensure verification steps are run on the ready tags
	if err := c.syncReady(release); err != nil {
		c.eventRecorder.Eventf(imageStream, corev1.EventTypeWarning, "UnableToVerifyRelease", "%v", err)
		return err
	}

	// ensure publish steps are run on the accepted tags
	if err := c.syncAccepted(release); err != nil {
		c.eventRecorder.Eventf(imageStream, corev1.EventTypeWarning, "UnableToVerifyRelease", "%v", err)
		return err
	}

	return nil
}

func (c *Controller) ensureProwJobForReleaseTag(release *Release, verifyName string, verifyType ReleaseVerification, releaseTag *imagev1.TagReference) (*unstructured.Unstructured, error) {
	jobName := verifyType.ProwJob.Name
	prowJobName := fmt.Sprintf("%s-%s", releaseTag.Name, verifyName)
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
	periodicConfig, ok := hasProwJob(config, jobName)
	if !ok {
		err := fmt.Errorf("the prow job %s is not valid: no job with that name", jobName)
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", err.Error())
		return nil, terminalError{err}
	}

	spec := prowSpecForPeriodicConfig(periodicConfig, config.Plank.DefaultDecorationConfig)
	mirror, _ := c.getMirror(release, releaseTag.Name)
	var previousReleasePullSpec string
	var previousTag string
	if tags := findTagReferencesByPhase(release, releasePhaseAccepted); len(tags) > 0 {
		previousTag = tags[0].Name
		previousReleasePullSpec = release.Target.Status.PublicDockerImageRepository + ":" + previousTag
	}
	ok, err = addReleaseEnvToProwJobSpec(spec, release, mirror, releaseTag, previousReleasePullSpec)
	if err != nil {
		return nil, err
	}
	if !ok {
		now := metav1.Now()
		// return a synthetic job to indicate that this test is impossible to run (no spec, or
		// this is an upgrade job and no upgrade is possible)
		return objectToUnstructured(&prowapiv1.ProwJob{
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
				StartTime:      now,
				CompletionTime: &now,
				Description:    "Job was not defined or does not have any inputs",
				State:          prowapiv1.SuccessState,
			},
		}), nil
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
			StartTime: metav1.Now(),
			State:     prowapiv1.TriggeredState,
		},
	}
	pj.Annotations[releaseAnnotationToTag] = releaseTag.Name
	if verifyType.Upgrade && len(previousTag) > 0 {
		pj.Annotations[releaseAnnotationFromTag] = previousTag
	}
	out, err := c.prowClient.Create(objectToUnstructured(pj), metav1.CreateOptions{})
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
	var (
		removeFailures tagReferencesByAge
		removeAccepted tagReferencesByAge
		removeRejected tagReferencesByAge
	)
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
		case releasePhaseFailed:
			removeFailures = append(removeFailures, tag)
		case releasePhaseRejected:
			removeRejected = append(removeRejected, tag)
		case releasePhaseAccepted:
			removeAccepted = append(removeAccepted, tag)
		}
	}

	keepTagsOfType := 5

	if len(removeFailures) > keepTagsOfType {
		sort.Sort(removeFailures)
		removeTags = append(removeTags, removeFailures[keepTagsOfType:]...)
	}
	if len(removeRejected) > keepTagsOfType {
		sort.Sort(removeRejected)
		removeTags = append(removeTags, removeRejected[keepTagsOfType:]...)
	}

	// always keep at least one accepted tag, but remove any that are past expiration
	if expires := release.Config.Expires.Duration(); expires > 0 && len(removeAccepted) > keepTagsOfType {
		glog.V(5).Infof("Checking for tags that are more than %s old", expires)
		sort.Sort(removeAccepted)
		for _, tag := range removeAccepted[keepTagsOfType:] {
			created, err := time.Parse(time.RFC3339, tag.Annotations[releaseAnnotationCreationTimestamp])
			if err != nil {
				glog.Errorf("Unparseable timestamp on release tag %s:%s: %v", release.Target.Name, tag.Name, err)
				continue
			}
			if created.Add(expires).Before(now) {
				removeTags = append(removeTags, tag)
			}
		}
	}

	// arrange the pendingTags in alphabetic order (which should be based on date)
	sort.Sort(tagReferencesByAge(pendingTags))
	return pendingTags, removeTags, hasNewImages, inputImageHash
}

func (c *Controller) syncPending(release *Release, pendingTags []*imagev1.TagReference, inputImageHash string) (err error) {
	if len(pendingTags) > 1 {
		if err := c.transitionReleasePhaseFailure(release, []string{releasePhasePending}, releasePhaseFailed, reasonAndMessage("Aborted", "Multiple releases were found simultaneously running."), tagNames(pendingTags[1:])...); err != nil {
			return err
		}
	}

	if len(pendingTags) > 0 {
		// we only process the first tag
		tag := pendingTags[0]
		mirror, err := c.ensureReleaseMirror(release, tag.Name, inputImageHash)
		if err != nil {
			return err
		}
		if len(tag.Annotations[releaseAnnotationImageHash]) == 0 {
			return c.setReleaseAnnotation(release, releasePhasePending, map[string]string{releaseAnnotationImageHash: mirror.Annotations[releaseAnnotationImageHash]}, tag.Name)
		}
		if mirror.Annotations[releaseAnnotationImageHash] != tag.Annotations[releaseAnnotationImageHash] {
			return fmt.Errorf("mirror hash for %q does not match, release cannot be created", tag.Name)
		}

		job, err := c.ensureReleaseJob(release, tag.Name, mirror)
		if err != nil {
			return err
		}
		success, complete := jobIsComplete(job)
		switch {
		case !complete:
			return nil
		case !success:
			// TODO: extract termination message from the job
			if err := c.transitionReleasePhaseFailure(release, []string{releasePhasePending}, releasePhaseFailed, reasonAndMessage("CreateReleaseFailed", "Could not create the release image"), tag.Name); err != nil {
				return err
			}
		default:
			if err := c.markReleaseReady(release, nil, tag.Name); err != nil {
				return err
			}
			if tags := findTagReferencesByPhase(release, releasePhaseAccepted); len(tags) > 0 {
				go func() {
					if _, err := c.releaseInfo.ChangeLog(tags[0].Name, tag.Name); err != nil {
						glog.V(4).Infof("Unable to pre-cache changelog for new ready release %s: %v", tag.Name, err)
					}
				}()
			}
		}
	}

	return nil
}

func (c *Controller) syncReady(release *Release) error {
	readyTags := findTagReferencesByPhase(release, releasePhaseReady)

	if glog.V(4) {
		glog.Infof("ready=%v", tagNames(readyTags))
	}

	for _, releaseTag := range readyTags {
		status, err := c.ensureVerificationJobs(release, releaseTag)
		if err != nil {
			return err
		}

		if names, ok := status.Incomplete(release.Config.Verify); ok {
			glog.V(4).Infof("Verification jobs for %s are still running: %s", releaseTag.Name, strings.Join(names, ", "))
			if err := c.markReleaseReady(release, map[string]string{releaseAnnotationVerify: toJSONString(status)}, releaseTag.Name); err != nil {
				return err
			}
			continue
		}

		if names, ok := status.Failures(); ok {
			if allOptional(release.Config.Verify, names...) {
				glog.V(4).Infof("Release %s had only optional job failures: %v", releaseTag.Name, strings.Join(names, ", "))
			} else {
				glog.V(4).Infof("Release %s was rejected", releaseTag.Name)
				annotations := reasonAndMessage("VerificationFailed", fmt.Sprintf("release verification step failed: %s", strings.Join(names, ", ")))
				annotations[releaseAnnotationVerify] = toJSONString(status)
				if err := c.transitionReleasePhaseFailure(release, []string{releasePhaseReady}, releasePhaseRejected, annotations, releaseTag.Name); err != nil {
					return err
				}
				continue
			}
		}

		// if all jobs are complete and there are no failures, this is accepted
		if err := c.markReleaseAccepted(release, map[string]string{releaseAnnotationVerify: toJSONString(status)}, releaseTag.Name); err != nil {
			return err
		}
		glog.V(4).Infof("Release %s accepted", releaseTag.Name)
	}

	return nil
}

func (c *Controller) ensureVerificationJobs(release *Release, releaseTag *imagev1.TagReference) (VerificationStatusMap, error) {
	var verifyStatus VerificationStatusMap
	for name, verifyType := range release.Config.Verify {
		if verifyType.Disabled {
			glog.V(2).Infof("Release verification step %s is disabled, ignoring", name)
			continue
		}

		switch {
		case verifyType.ProwJob != nil:
			if verifyStatus == nil {
				if data := releaseTag.Annotations[releaseAnnotationVerify]; len(data) > 0 {
					verifyStatus = make(VerificationStatusMap)
					if err := json.Unmarshal([]byte(data), &verifyStatus); err != nil {
						glog.Errorf("Release %s has invalid verification status, ignoring: %v", releaseTag.Name, err)
					}
				}
			}

			if status, ok := verifyStatus[name]; ok {
				switch status.State {
				case releaseVerificationStateFailed, releaseVerificationStateSucceeded:
					// we've already processed this, continue
					continue
				case releaseVerificationStatePending:
					// we need to process this
				default:
					glog.V(2).Infof("Unrecognized verification status %q for type %s on release %s", status.State, name, releaseTag.Name)
				}
			}

			job, err := c.ensureProwJobForReleaseTag(release, name, verifyType, releaseTag)
			if err != nil {
				return nil, err
			}
			status, ok := prowJobVerificationStatus(job)
			if !ok {
				return nil, fmt.Errorf("unexpected error accessing prow job definition")
			}
			if status.State == releaseVerificationStateSucceeded {
				glog.V(2).Infof("Prow job %s for release %s succeeded, logs at %s", name, releaseTag.Name, status.Url)
			}
			if verifyStatus == nil {
				verifyStatus = make(VerificationStatusMap)
			}
			verifyStatus[name] = status

		default:
			// manual verification
		}
	}
	return verifyStatus, nil
}

func (c *Controller) syncAccepted(release *Release) error {
	acceptedTags := findTagReferencesByPhase(release, releasePhaseAccepted)

	if glog.V(4) {
		glog.Infof("release=%s accepted=%v", release.Config.Name, tagNames(acceptedTags))
	}

	if len(release.Config.Publish) == 0 || len(acceptedTags) == 0 {
		return nil
	}
	var errs []error
	newestAccepted := acceptedTags[0]
	for name, publishType := range release.Config.Publish {
		switch {
		case publishType.TagRef != nil:
			if err := c.ensureTagPointsToRelease(release, publishType.TagRef.Name, newestAccepted.Name); err != nil {
				errs = append(errs, fmt.Errorf("unable to update tag for publish step %s: %v", name, err))
				continue
			}
		case publishType.ImageStreamRef != nil:
			ns := publishType.ImageStreamRef.Namespace
			if len(ns) == 0 {
				ns = release.Target.Namespace
			}
			if err := c.ensureImageStreamMatchesRelease(release, ns, publishType.ImageStreamRef.Name, newestAccepted.Name); err != nil {
				errs = append(errs, fmt.Errorf("unable to update image stream for publish step %s: %v", name, err))
				continue
			}
		}
	}
	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}
	return nil
}

func (c *Controller) createReleaseTag(release *Release, now time.Time, inputImageHash string) (*imagev1.TagReference, error) {
	target := release.Target.DeepCopy()
	now = now.UTC().Truncate(time.Second)
	t := now.Format("2006-01-02-150405")
	tag := imagev1.TagReference{
		Name: fmt.Sprintf("%s-%s", release.Config.Name, t),
		Annotations: map[string]string{
			releaseAnnotationName:              release.Config.Name,
			releaseAnnotationSource:            fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
			releaseAnnotationCreationTimestamp: now.Format(time.RFC3339),
			releaseAnnotationPhase:             releasePhasePending,
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
		// use delete imagestreamtag so that status tags are removed as well
		glog.V(2).Infof("Removing release tag %s", tag.Name)
		if err := c.imageClient.ImageStreamTags(release.Target.Namespace).Delete(fmt.Sprintf("%s:%s", release.Target.Name, tag.Name), nil); err != nil {
			if !errors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

func mirrorName(release *Release, releaseTagName string) string {
	suffix := strings.TrimPrefix(releaseTagName, release.Config.Name)
	if len(release.Config.MirrorPrefix) > 0 {
		return fmt.Sprintf("%s%s", release.Config.MirrorPrefix, suffix)
	}
	return fmt.Sprintf("%s%s", release.Source.Name, suffix)
}

func (c *Controller) getMirror(release *Release, releaseTagName string) (*imagev1.ImageStream, error) {
	return c.imageStreamLister.ImageStreams(c.releaseNamespace).Get(mirrorName(release, releaseTagName))
}

func (c *Controller) ensureReleaseMirror(release *Release, releaseTagName, inputImageHash string) (*imagev1.ImageStream, error) {
	mirrorName := mirrorName(release, releaseTagName)
	is, err := c.imageStreamLister.ImageStreams(c.releaseNamespace).Get(mirrorName)
	if err == nil {
		return is, nil
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}

	is = &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mirrorName,
			Namespace: c.releaseNamespace,
			Annotations: map[string]string{
				releaseAnnotationSource:     fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
				releaseAnnotationReleaseTag: releaseTagName,
				releaseAnnotationImageHash:  inputImageHash,
				releaseAnnotationGeneration: strconv.FormatInt(release.Target.Generation, 10),
			},
		},
	}

	// this block is mostly identical to the logic in openshift/origin pkg/oc/cli/admin/release/new which
	// calculates the spec tags - it preserves the desired source location of the image and errors when
	// we can't resolve or the result might be ambiguous
	forceExternal := release.Config.ReferenceMode == "public" || release.Config.ReferenceMode == ""
	internal := release.Source.Status.DockerImageRepository
	external := release.Source.Status.PublicDockerImageRepository
	if forceExternal && len(external) == 0 {
		return nil, fmt.Errorf("only image streams with public image repositories can be the source for releases when using the default referenceMode")
	}

	for _, tag := range release.Source.Status.Tags {
		if len(tag.Items) == 0 {
			continue
		}
		source := tag.Items[0].DockerImageReference
		latest := tag.Items[0]
		if len(latest.Image) == 0 {
			continue
		}
		// eliminate status tag references that point to the outside
		if len(source) > 0 {
			if len(internal) > 0 && strings.HasPrefix(latest.DockerImageReference, internal) {
				glog.V(2).Infof("Can't use tag %q source %s because it points to the internal registry", tag.Tag, source)
				source = ""
			}
		}
		ref := findSpecTag(release.Source.Spec.Tags, tag.Tag)
		if ref == nil {
			ref = &imagev1.TagReference{Name: tag.Tag}
		} else {
			// prevent unimported images from being skipped
			if ref.Generation != nil && *ref.Generation != tag.Items[0].Generation {
				return nil, fmt.Errorf("the tag %q in the source input stream has not been imported yet", tag.Tag)
			}
			// use the tag ref as the source
			if ref.From != nil && ref.From.Kind == "DockerImage" && !strings.HasPrefix(ref.From.Name, internal) {
				if from, err := imagereference.Parse(ref.From.Name); err == nil {
					from.Tag = ""
					from.ID = tag.Items[0].Image
					source = from.Exact()
				} else {
					glog.V(2).Infof("Can't use tag %q from %s because it isn't a valid image reference", tag.Tag, ref.From.Name)
				}
			}
			ref = ref.DeepCopy()
		}
		// default to the external registry name
		if (forceExternal || len(source) == 0) && len(external) > 0 {
			source = external + "@" + tag.Items[0].Image
		}
		if len(source) == 0 {
			return nil, fmt.Errorf("Can't use tag %q because we cannot locate or calculate a source location", tag.Tag)
		}
		sourceRef, err := imagereference.Parse(source)
		if err != nil {
			return nil, fmt.Errorf("the tag %q points to source %q which is not valid", tag.Tag, source)
		}
		sourceRef.Tag = ""
		sourceRef.ID = tag.Items[0].Image
		source = sourceRef.Exact()

		if strings.HasPrefix(source, external+"@") {
			ref.From = &corev1.ObjectReference{
				Kind:      "ImageStreamImage",
				Namespace: release.Source.Namespace,
				Name:      fmt.Sprintf("%s@%s", release.Source.Name, latest.Image),
			}
		} else {
			ref.From = &corev1.ObjectReference{
				Kind: "DockerImage",
				Name: source,
			}
		}
		ref.ImportPolicy.Scheduled = false
		is.Spec.Tags = append(is.Spec.Tags, *ref)
	}

	glog.V(2).Infof("Mirroring release images in %s/%s to %s/%s", release.Source.Namespace, release.Source.Name, is.Namespace, is.Name)
	is, err = c.imageClient.ImageStreams(is.Namespace).Create(is)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return nil, err
		}
		// perform a live read
		is, err = c.imageClient.ImageStreams(is.Namespace).Get(is.Name, metav1.GetOptions{})
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
	// TODO: this should be the default
	toImageBase := fmt.Sprintf("%s:cluster-version-operator", mirror.Status.PublicDockerImageRepository)
	cliImage := fmt.Sprintf("%s:cli", mirror.Status.PublicDockerImageRepository)

	job = newReleaseJob(name, mirror.Name, mirror.Namespace, cliImage, toImage, toImageBase, release.Config.ReferenceMode, release.Config.PullSecretName)
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

func newReleaseJob(name, mirrorName, mirrorNamespace, cliImage, toImage, toImageBase, referenceMode, pullSecretName string) *batchv1.Job {
	if len(pullSecretName) > 0 {
		return &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Annotations: map[string]string{},
			},
			Spec: batchv1.JobSpec{
				Parallelism:  int32p(1),
				BackoffLimit: int32p(3),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: "builder",
						RestartPolicy:      corev1.RestartPolicyNever,
						Volumes: []corev1.Volume{
							{
								Name: "pull-secret",
								VolumeSource: corev1.VolumeSource{
									Secret: &corev1.SecretVolumeSource{
										SecretName: pullSecretName,
									},
								},
							},
						},
						Containers: []corev1.Container{
							{
								Name:  "build",
								Image: cliImage,

								ImagePullPolicy: corev1.PullAlways,

								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "pull-secret",
										MountPath: "/tmp/pull-secret",
									},
								},
								Env: []corev1.EnvVar{
									{Name: "HOME", Value: "/tmp"},
								},
								Command: []string{
									"/bin/bash", "-c", `
									set -eu
									mkdir $HOME/.docker/
									cp -Lf /tmp/pull-secret/* $HOME/.docker/
									oc registry login
									oc adm release new "--name=$1" "--from-image-stream=$2" "--namespace=$3" "--to-image=$4" "--to-image-base=$5" "--reference-mode=$6"
									`, "",
									name, mirrorName, mirrorNamespace, toImage, toImageBase, referenceMode},
								TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
							},
						},
					},
				},
			},
		}
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{},
		},
		Spec: batchv1.JobSpec{
			Parallelism:  int32p(1),
			BackoffLimit: int32p(3),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: "builder",
					RestartPolicy:      corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "build",
							Image: cliImage,

							ImagePullPolicy: corev1.PullAlways,

							Env: []corev1.EnvVar{
								{Name: "HOME", Value: "/tmp"},
							},
							Command: []string{
								"/bin/bash", "-c", `
								set -eu
								oc registry login
								oc adm release new "--name=$1" "--from-image-stream=$2" "--namespace=$3" "--to-image=$4" "--to-image-base=$5" "--reference-mode=$6"
								`, "",
								name, mirrorName, mirrorNamespace, toImage, toImageBase, referenceMode},
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

func (c *Controller) markReleaseReady(release *Release, annotations map[string]string, names ...string) error {
	return c.transitionReleasePhase(release, []string{releasePhasePending, releasePhaseReady}, releasePhaseReady, annotations, names...)
}

func (c *Controller) markReleaseAccepted(release *Release, annotations map[string]string, names ...string) error {
	return c.transitionReleasePhase(release, []string{releasePhaseReady}, releasePhaseAccepted, annotations, names...)
}

func (c *Controller) setReleaseAnnotation(release *Release, phase string, annotations map[string]string, names ...string) error {
	if len(names) == 0 {
		return nil
	}

	changes := 0
	target := release.Target.DeepCopy()
	for _, name := range names {
		tag := findTagReference(target, name)
		if tag == nil {
			return fmt.Errorf("release %s no longer exists, cannot be put into %s", name, phase)
		}

		if current := tag.Annotations[releaseAnnotationPhase]; current != phase {
			return fmt.Errorf("release %s is not in phase %v (%s)", name, phase, current)
		}
		for k, v := range annotations {
			if len(v) == 0 {
				delete(tag.Annotations, k)
			} else {
				tag.Annotations[k] = v
			}
		}

		// if there is no change, avoid updating anything
		if oldTag := findTagReference(release.Target, name); oldTag != nil {
			if reflect.DeepEqual(oldTag.Annotations, tag.Annotations) {
				continue
			}
		}
		changes++
		glog.V(2).Infof("Adding annotations to release %s", name)
	}

	if changes == 0 {
		return nil
	}

	is, err := c.imageClient.ImageStreams(target.Namespace).Update(target)
	if err != nil {
		return err
	}
	release.Target = is
	return nil
}

func (c *Controller) transitionReleasePhase(release *Release, preconditionPhases []string, phase string, annotations map[string]string, names ...string) error {
	if len(names) == 0 {
		return nil
	}

	changes := 0
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
			if len(v) == 0 {
				delete(tag.Annotations, k)
			} else {
				tag.Annotations[k] = v
			}
		}

		// if there is no change, avoid updating anything
		if oldTag := findTagReference(release.Target, name); oldTag != nil {
			if reflect.DeepEqual(oldTag.Annotations, tag.Annotations) {
				continue
			}
		}
		changes++
		glog.V(2).Infof("Marking release %s %s", name, phase)
	}

	if changes == 0 {
		return nil
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

func (c *Controller) ensureTagPointsToRelease(release *Release, to, from string) error {
	if to == from {
		return nil
	}
	fromTag := findTagReference(release.Target, from)
	toTag := findTagReference(release.Target, to)
	if fromTag == nil {
		// tag was deleted
		return nil
	}
	if toTag != nil {
		if toTag.From != nil && toTag.From.Kind == "ImageStreamTag" && toTag.From.Name == from && toTag.From.Namespace == "" {
			// already set to the correct location
			return nil
		}
	}
	target := release.Target.DeepCopy()
	toTag = findTagReference(target, to)
	if toTag == nil {
		target.Spec.Tags = append(target.Spec.Tags, imagev1.TagReference{
			Name: to,
		})
		toTag = &target.Spec.Tags[len(target.Spec.Tags)-1]
	}
	toTag.From = &corev1.ObjectReference{Kind: "ImageStreamTag", Name: from}
	toTag.ImportPolicy = imagev1.TagImportPolicy{}

	is, err := c.imageClient.ImageStreams(target.Namespace).Update(target)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	glog.V(2).Infof("Updated image stream tag %s/%s:%s to point to %s", release.Target.Namespace, release.Target.Name, to, from)
	release.Target = is
	return nil
}

func (c *Controller) ensureImageStreamMatchesRelease(release *Release, toNamespace, toName, from string) error {
	glog.V(4).Infof("Ensure image stream %s/%s has contents of %s", toNamespace, toName, from)
	if toNamespace == release.Source.Namespace && toName == release.Source.Name {
		return nil
	}
	fromTag := findTagReference(release.Target, from)
	if fromTag == nil {
		// tag was deleted
		return nil
	}

	mirror, err := c.getMirror(release, from)
	if err != nil {
		glog.V(2).Infof("Error getting release mirror image stream: %v", err)
		return nil
	}

	target, err := c.imageStreamLister.ImageStreams(toNamespace).Get(toName)
	if errors.IsNotFound(err) {
		// TODO: create it?
		glog.V(2).Infof("Target image stream doesn't exist yet: %v", err)
		return nil
	}
	if err != nil {
		// TODO
		glog.V(2).Infof("Error getting publish image stream: %v", err)
		return nil
	}

	set := fmt.Sprintf("release.openshift.io/source-%s", release.Config.Name)
	if value, ok := target.Annotations[set]; ok && value == from {
		glog.V(2).Infof("Published image stream %s/%s is up to date", toNamespace, toName)
		return nil
	}

	processed := sets.NewString()
	finalRefs := make([]imagev1.TagReference, 0, len(mirror.Spec.Tags))
	for _, tag := range mirror.Spec.Tags {
		processed.Insert(tag.Name)
		finalRefs = append(finalRefs, tag)
	}
	for _, tag := range target.Spec.Tags {
		if processed.Has(tag.Name) {
			continue
		}
		finalRefs = append(finalRefs, tag)
	}
	sort.Slice(finalRefs, func(i, j int) bool {
		return finalRefs[i].Name < finalRefs[j].Name
	})

	target = target.DeepCopy()
	target.Spec.Tags = finalRefs
	if target.Annotations == nil {
		target.Annotations = make(map[string]string)
	}
	target.Annotations[set] = from

	_, err = c.imageClient.ImageStreams(target.Namespace).Update(target)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	glog.V(2).Infof("Updated image stream %s/%s to point to contents of %s", toNamespace, toName, from)
	return nil
}

func (c *Controller) garbageCollectUnreferencedObjects() error {
	is, err := c.imageStreamLister.ImageStreams(c.releaseNamespace).Get(c.releaseImageStream)
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
		if _, ok := validReleases[mirror.Annotations[releaseAnnotationReleaseTag]]; ok {
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

func addReleaseEnvToProwJobSpec(spec *prowapiv1.ProwJobSpec, release *Release, mirror *imagev1.ImageStream, releaseTag *imagev1.TagReference, previousReleasePullSpec string) (bool, error) {
	if spec.PodSpec == nil {
		// Jenkins jobs cannot be parameterized
		return true, nil
	}
	for i := range spec.PodSpec.Containers {
		c := &spec.PodSpec.Containers[i]
		for j := range c.Env {
			switch name := c.Env[j].Name; {
			case name == "RELEASE_IMAGE_LATEST":
				c.Env[j].Value = release.Target.Status.PublicDockerImageRepository + ":" + releaseTag.Name
			case name == "RELEASE_IMAGE_INITIAL":
				if len(previousReleasePullSpec) == 0 {
					return false, nil
				}
				c.Env[j].Value = previousReleasePullSpec
			case name == "IMAGE_FORMAT":
				if mirror == nil {
					return false, fmt.Errorf("unable to determine IMAGE_FORMAT for prow job %s", spec.Job)
				}
				c.Env[j].Value = mirror.Status.PublicDockerImageRepository + ":${component}"
			case strings.HasPrefix(name, "IMAGE_"):
				suffix := strings.TrimPrefix(name, "IMAGE_")
				if len(suffix) == 0 {
					break
				}
				if mirror == nil {
					return false, fmt.Errorf("unable to determine IMAGE_FORMAT for prow job %s", spec.Job)
				}
				suffix = strings.ToLower(strings.Replace(suffix, "_", "-", -1))
				c.Env[j].Value = mirror.Status.PublicDockerImageRepository + ":" + suffix
			}
		}
	}
	return true, nil
}

func hasProwJob(config *prowapiv1.Config, name string) (*prowapiv1.PeriodicConfig, bool) {
	for i := range config.Periodics {
		if config.Periodics[i].Name == name {
			return &config.Periodics[i], true
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
	if string(out) == "null" {
		return ""
	}
	return string(out)
}

func prowSpecForPeriodicConfig(config *prowapiv1.PeriodicConfig, decorationConfig *prowapiv1.DecorationConfig) *prowapiv1.ProwJobSpec {
	spec := &prowapiv1.ProwJobSpec{
		Type:  prowapiv1.PeriodicJob,
		Job:   config.Name,
		Agent: prowapiv1.KubernetesAgent,

		Refs: &prowapiv1.Refs{},

		PodSpec: config.Spec.DeepCopy(),
	}

	if decorationConfig != nil {
		spec.DecorationConfig = decorationConfig.DeepCopy()
	} else {
		spec.DecorationConfig = &prowapiv1.DecorationConfig{}
	}
	spec.DecorationConfig.SkipCloning = true

	return spec
}

func findSpecTag(tags []imagev1.TagReference, name string) *imagev1.TagReference {
	for i, tag := range tags {
		if tag.Name != name {
			continue
		}
		return &tags[i]
	}
	return nil
}
