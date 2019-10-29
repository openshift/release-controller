package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/blang/semver"
	"github.com/golang/glog"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	"github.com/openshift/api/image/docker10"
	imagev1 "github.com/openshift/api/image/v1"
)

// sync expects to receive a queue key that points to a valid release image input
// or to the entire namespace.
func (c *Controller) sync(key queueKey) error {
	defer func() {
		err := recover()
		panic(err)
	}()

	// if we are waiting to observe the result of our previous actions, simply delay
	// if c.expectations.Expecting(key.namespace, key.name) {
	// 	c.queue.AddAfter(key, c.expectationDelay)
	// 	glog.V(5).Infof("Release %s has unsatisfied expectations", key.name)
	// 	return nil
	// }

	release, err := c.loadReleaseForSync(key.namespace, key.name)
	if err != nil || release == nil {
		return err
	}

	// ensure that the target image stream always carries the annotation indicating it is
	// a target for backreferencing from GC and other check points
	if _, ok := release.Target.Annotations[releaseAnnotationHasReleases]; !ok {
		target := release.Target.DeepCopy()
		if target.Annotations == nil {
			target.Annotations = make(map[string]string)
		}
		target.Annotations[releaseAnnotationHasReleases] = "true"
		if _, err := c.imageClient.ImageStreams(target.Namespace).Update(target); err != nil {
			return err
		}
		return nil
	}

	now := time.Now()
	adoptTags, pendingTags, removeTags, hasNewImages, inputImageHash := calculateSyncActions(release, now)

	if glog.V(4) {
		glog.Infof("name=%s hasNewImages=%t inputImageHash=%s adoptTags=%v removeTags=%v pendingTags=%v", release.Source.Name, hasNewImages, inputImageHash, tagNames(adoptTags), tagNames(removeTags), tagNames(pendingTags))
	}

	// take any tags that need to be given annotations now
	if len(adoptTags) > 0 {
		changed, err := c.syncAdopted(release, adoptTags, now)
		if err != nil {
			return err
		}
		if changed {
			return nil
		}
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
			c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "UnableToCreateRelease", "%v", err)
			return err
		}
		pendingTags = []*imagev1.TagReference{releaseTag}
	}

	// ensure any pending tags have the necessary jobs/mirrors created
	if err := c.syncPending(release, pendingTags, inputImageHash); err != nil {
		if errors.IsConflict(err) {
			return nil
		}
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "UnableToProcessRelease", "%v", err)
		return err
	}

	// ensure verification steps are run on the ready tags
	if err := c.syncReady(release); err != nil {
		if errors.IsConflict(err) {
			return nil
		}
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "UnableToVerifyRelease", "%v", err)
		return err
	}

	// ensure publish steps are run on the accepted tags
	if err := c.syncAccepted(release); err != nil {
		if errors.IsConflict(err) {
			return nil
		}
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "UnableToVerifyRelease", "%v", err)
		return err
	}

	c.gcQueue.AddAfter("", 15*time.Second)
	return nil
}

func calculateSyncActions(release *Release, now time.Time) (adoptTags, pendingTags, removeTags []*imagev1.TagReference, hasNewImages bool, inputImageHash string) {
	hasNewImages = true
	inputImageHash = hashSpecTagImageDigests(release.Source)
	var (
		removeFailures tagReferencesByAge
		removeAccepted tagReferencesByAge
		removeRejected tagReferencesByAge
	)
	target := release.Target

	shouldAdopt := release.Config.As == releaseConfigModeStable

	tags := make([]*imagev1.TagReference, 0, len(target.Spec.Tags))
	for i := range target.Spec.Tags {
		tags = append(tags, &target.Spec.Tags[i])
	}
	sort.Sort(tagReferencesByAge(tags))

	removeFailuresAfter, removeRejectedAfter := -1, -1
	for _, tag := range tags {
		if shouldAdopt {
			if len(tag.Annotations[releaseAnnotationSource]) == 0 && len(tag.Annotations[releaseAnnotationPhase]) == 0 {
				adoptTags = append(adoptTags, tag)
				continue
			}
		}

		// always skip pinned tags
		if _, ok := tag.Annotations[releaseAnnotationKeep]; ok {
			continue
		}
		// check annotations when using the target as tag source
		if release.Config.As != releaseConfigModeStable && tag.Annotations[releaseAnnotationSource] != fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name) {
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
			removeRejectedAfter = len(removeRejected)
			removeFailuresAfter = len(removeFailures)
		}
	}

	// remove failures and rejections after the last accepted
	if removeRejectedAfter != -1 {
		removeTags = append(removeTags, removeRejected[removeRejectedAfter:]...)
		removeRejected = removeRejected[:removeRejectedAfter]
	}
	if removeFailuresAfter != -1 {
		removeTags = append(removeTags, removeFailures[removeFailuresAfter:]...)
		removeFailures = removeFailures[:removeFailuresAfter]
	}

	// keep only five failures and five rejections
	keepTagsOfType := 5
	if len(removeFailures) > keepTagsOfType {
		removeTags = append(removeTags, removeFailures[keepTagsOfType:]...)
	}
	if len(removeRejected) > keepTagsOfType {
		removeTags = append(removeTags, removeRejected[keepTagsOfType:]...)
	}

	// always keep at least one accepted tag, but remove any that are past expiration
	if expires := release.Config.Expires.Duration(); expires > 0 && len(removeAccepted) > keepTagsOfType {
		glog.V(5).Infof("Checking for tags that are more than %s old", expires)
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

	switch release.Config.As {
	case releaseConfigModeStable:
		hasNewImages = false
		inputImageHash = ""
		removeTags = nil
	}

	return adoptTags, pendingTags, removeTags, hasNewImages, inputImageHash
}

func (c *Controller) syncAdopted(release *Release, adoptTags []*imagev1.TagReference, now time.Time) (changed bool, err error) {
	names := make([]string, 0, len(adoptTags))
	for _, tag := range adoptTags {
		if tag.Name == "next" {
			// changes the list of tags, so needs to exit
			return true, c.replaceReleaseTagWithNext(release, tag)
		}
		if _, err := semver.Parse(tag.Name); err == nil {
			names = append(names, tag.Name)
		}
	}
	if len(names) == 0 {
		return false, nil
	}
	return true, c.ensureReleaseTagPhase(
		release,
		[]string{"", releasePhasePending},
		releasePhasePending,
		map[string]string{
			releaseAnnotationName:              release.Config.Name,
			releaseAnnotationSource:            fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
			releaseAnnotationCreationTimestamp: now.Format(time.RFC3339),
		},
		names...,
	)
}

func (c *Controller) syncPending(release *Release, pendingTags []*imagev1.TagReference, inputImageHash string) (err error) {
	switch release.Config.As {
	case releaseConfigModeStable:
		for _, tag := range pendingTags {
			// wait for import, then determine whether the requested version (tag name) matches the source version (label on image)
			id := findImageIDForTag(release.Source, tag.Name)
			if len(id) == 0 {
				glog.V(2).Infof("Waiting for release %s to be imported before we can retrieve metadata", tag.Name)
				continue
			}
			rewriteValue := tag.Annotations[releaseAnnotationRewrite]
			if len(rewriteValue) == 0 {
				isi, err := c.imageClient.ImageStreamImages(release.Source.Namespace).Get(fmt.Sprintf("%s@%s", release.Source.Name, id), metav1.GetOptions{})
				if err != nil {
					return err
				}
				metadata := &docker10.DockerImage{}
				if len(isi.Image.DockerImageMetadata.Raw) == 0 {
					return fmt.Errorf("could not fetch Docker image metadata for release %s", tag.Name)
				}
				if err := json.Unmarshal(isi.Image.DockerImageMetadata.Raw, metadata); err != nil {
					return fmt.Errorf("malformed Docker image metadata on ImageStreamTag: %v", err)
				}
				var name string
				if metadata.Config != nil {
					name = metadata.Config.Labels["io.openshift.release"]
				}
				rewriteValue = fmt.Sprintf("%t", name != tag.Name)
				if err := c.setReleaseAnnotation(release, tag.Annotations[releaseAnnotationPhase], map[string]string{releaseAnnotationRewrite: rewriteValue}, tag.Name); err != nil {
					return err
				}
				continue
			}
			rewrite := rewriteValue == "true"

			hash := fmt.Sprintf("%s-%d", tag.Name, *tag.Generation)
			// mirror any internal images
			// rewrite payload for internal images with metadata
			mirror, err := c.ensureReleaseMirror(release, tag.Name, hash)
			if err != nil {
				return err
			}
			if len(tag.Annotations[releaseAnnotationImageHash]) == 0 {
				if err := c.setReleaseAnnotation(release, tag.Annotations[releaseAnnotationPhase], map[string]string{releaseAnnotationImageHash: mirror.Annotations[releaseAnnotationImageHash]}, tag.Name); err != nil {
					return err
				}
				continue
			}
			if mirror.Annotations[releaseAnnotationImageHash] != tag.Annotations[releaseAnnotationImageHash] {
				// delete the mirror and exit
				return fmt.Errorf("unimplemented, should regenerate contents of tag")
			}
			// get metadata about the release
			//   get upgrade graph edges
			//   check to see any required edges are missing?  wait for latest edge?  wait for pending edges?
			//     how do we calculate required edge set?
			var job *batchv1.Job
			if rewrite {
				job, err = c.ensureRewriteJob(release, tag.Name, mirror, `{}`)
			} else {
				job, err = c.ensureImportJob(release, tag.Name, mirror)
			}
			if err != nil || job == nil {
				return err
			}
			success, complete := jobIsComplete(job)
			switch {
			case !complete:
				return c.ensureRewriteJobImageRetrieved(release, job, mirror)
			case !success:
				// TODO: extract termination message from the job
				if err := c.transitionReleasePhaseFailure(release, []string{releasePhasePending}, releasePhaseFailed, reasonAndMessage("CreateReleaseFailed", "Could not create the release image"), tag.Name); err != nil {
					return err
				}
			default:
				if err := c.markReleaseReady(release, nil, tag.Name); err != nil {
					return err
				}
				if tags := findTagReferencesByPhase(release, releasePhaseReady); len(tags) > 0 {
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
		if err != nil || job == nil {
			return err
		}
		success, complete := jobIsComplete(job)
		switch {
		case !complete:
			return nil
		case !success:
			// try to get the last termination message
			log, _, _ := ensureJobTerminationMessageRetrieved(c.podClient, job, "status.phase=Failed", "build", false)
			if err := c.transitionReleasePhaseFailure(release, []string{releasePhasePending}, releasePhaseFailed, withLog(reasonAndMessage("CreateReleaseFailed", "Could not create the release image"), log), tag.Name); err != nil {
				return err
			}
		default:
			if err := c.markReleaseReady(release, nil, tag.Name); err != nil {
				return err
			}
			if tags := findTagReferencesByPhase(release, releasePhaseReady); len(tags) > 0 {
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

	if glog.V(4) && len(readyTags) > 0 {
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

func (c *Controller) syncAccepted(release *Release) error {
	acceptedTags := findTagReferencesByPhase(release, releasePhaseAccepted)

	if glog.V(4) && len(acceptedTags) > 0 {
		glog.Infof("release=%s accepted=%v", release.Config.Name, tagNames(acceptedTags))
	}

	if len(release.Config.Publish) == 0 || len(acceptedTags) == 0 {
		return nil
	}
	var errs []error
	newestAccepted := acceptedTags[0]
	for name, publishType := range release.Config.Publish {
		if publishType.Disabled {
			continue
		}
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
			if err := c.ensureImageStreamMatchesRelease(release, ns, publishType.ImageStreamRef.Name, newestAccepted.Name, publishType.ImageStreamRef.Tags, publishType.ImageStreamRef.ExcludeTags); err != nil {
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

func (c *Controller) loadReleaseForSync(namespace, name string) (*Release, error) {
	// locate the release definition off the image stream, or clean up any remaining
	// artifacts if the release no longer points to those
	isLister := c.imageStreamLister.ImageStreams(namespace)
	imageStream, err := isLister.Get(name)
	if errors.IsNotFound(err) {
		c.gcQueue.AddAfter("", 10*time.Second)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	release, ok, err := c.releaseDefinition(imageStream)
	if err != nil {
		return nil, err
	}
	if !ok {
		c.gcQueue.AddAfter("", 10*time.Second)
		return nil, nil
	}
	return release, nil
}

func containsString(arr []string, s string) bool {
	for _, str := range arr {
		if s == str {
			return true
		}
	}
	return false
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

func findSpecTag(tags []imagev1.TagReference, name string) *imagev1.TagReference {
	for i, tag := range tags {
		if tag.Name != name {
			continue
		}
		return &tags[i]
	}
	return nil
}
