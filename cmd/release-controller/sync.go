package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	"github.com/blang/semver"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog"

	"github.com/openshift/api/image/docker10"
	imagev1 "github.com/openshift/api/image/v1"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/releasepayload"
)

// sync expects to receive a queue key that points to a valid release image input
// or to the entire namespace.
func (c *Controller) sync(key queueKey) error {
	defer func() {
		if err := recover(); err != nil {
			panic(err)
		}
	}()

	// if we are waiting to observe the result of our previous actions, simply delay
	// if c.expectations.Expecting(key.namespace, key.name) {
	// 	c.queue.AddAfter(key, c.expectationDelay)
	// 	klog.V(5).Infof("Release %s has unsatisfied expectations", key.name)
	// 	return nil
	// }

	release, err := c.loadReleaseForSync(key.namespace, key.name)
	if err != nil || release == nil {
		return err
	}
	c.populatePayloadPhases(release)

	if release.Config.EndOfLife {
		klog.V(6).Infof("release %s has reached the end of life", release.Config.Name)
		return nil
	}

	// ensure that the target image stream always carries the annotation indicating it is
	// a target for backreferencing from GC and other check points
	if _, ok := release.Target.Annotations[releasecontroller.ReleaseAnnotationHasReleases]; !ok {
		target := release.Target.DeepCopy()
		if target.Annotations == nil {
			target.Annotations = make(map[string]string)
		}
		target.Annotations[releasecontroller.ReleaseAnnotationHasReleases] = "true"
		if _, err := c.imageClient.ImageStreams(target.Namespace).Update(context.TODO(), target, metav1.UpdateOptions{}); err != nil {
			return err
		}
		return nil
	}

	now := time.Now()
	adoptTags, pendingTags, removeTags, hasNewImages, inputImageHash, queueAfter := calculateSyncActions(release, now)

	if klog.V(4) {
		klog.Infof("name=%s hasNewImages=%t inputImageHash=%s adoptTags=%v removeTags=%v pendingTags=%v queueAfter=%s", release.Source.Name, hasNewImages, inputImageHash, releasecontroller.TagNames(adoptTags), releasecontroller.TagNames(removeTags), releasecontroller.TagNames(pendingTags), queueAfter)
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
		releaseTag, err := c.createReleaseTag(release, now)
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

	// if we're waiting for an interval to elapse, go ahead and queue to be woken
	if queueAfter > 0 {
		c.queue.AddAfter(key, queueAfter)
	}

	c.gcQueue.AddAfter("", 15*time.Second)
	return nil
}

func calculateSyncActions(release *releasecontroller.Release, now time.Time) (adoptTags, pendingTags, removeTags []*imagev1.TagReference, hasNewImages bool, inputImageHash string, queueAfter time.Duration) {
	hasNewImages = true
	inputImageHash = releasecontroller.HashSpecTagImageDigests(release.Source)
	var (
		removeFailures  releasecontroller.TagReferencesByAge
		removeAccepted  releasecontroller.TagReferencesByAge
		removeRejected  releasecontroller.TagReferencesByAge
		unreadyTagCount int
	)
	target := release.Target

	shouldAdopt := release.Config.As == releasecontroller.ReleaseConfigModeStable

	tags := make([]*imagev1.TagReference, 0, len(target.Spec.Tags))
	for i := range target.Spec.Tags {
		tags = append(tags, &target.Spec.Tags[i])
	}
	sort.Sort(releasecontroller.TagReferencesByAge(tags))

	var firstTag *imagev1.TagReference
	removeFailuresAfter, removeRejectedAfter := -1, -1
	for _, tag := range tags {
		if shouldAdopt {
			if len(tag.Annotations[releasecontroller.ReleaseAnnotationSource]) == 0 && len(tag.Annotations[releasecontroller.ReleaseAnnotationPhase]) == 0 {
				adoptTags = append(adoptTags, tag)
				continue
			}
		}

		// always skip pinned tags
		if _, ok := tag.Annotations[releasecontroller.ReleaseAnnotationKeep]; ok {
			continue
		}
		// check annotations when using the target as tag source
		if release.Config.As != releasecontroller.ReleaseConfigModeStable && tag.Annotations[releasecontroller.ReleaseAnnotationSource] != fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name) {
			continue
		}
		// if the name has changed, consider the tag abandoned (admin is responsible for cleaning it up)
		if tag.Annotations[releasecontroller.ReleaseAnnotationName] != release.Config.Name {
			continue
		}
		if firstTag == nil {
			firstTag = tag
		}
		if tag.Annotations[releasecontroller.ReleaseAnnotationImageHash] == inputImageHash {
			hasNewImages = false
		}

		phase := releasecontroller.GetTagPhase(release, tag)
		switch phase {
		case releasecontroller.ReleasePhasePending, "":
			unreadyTagCount++
			pendingTags = append(pendingTags, tag)
		case releasecontroller.ReleasePhaseFailed:
			removeFailures = append(removeFailures, tag)
		case releasecontroller.ReleasePhaseRejected:
			removeRejected = append(removeRejected, tag)
		case releasecontroller.ReleasePhaseAccepted:
			removeAccepted = append(removeAccepted, tag)
			removeRejectedAfter = len(removeRejected)
			removeFailuresAfter = len(removeFailures)
		default:
			unreadyTagCount++
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
		klog.V(5).Infof("Checking for tags that are more than %s old", expires)
		for _, tag := range removeAccepted[keepTagsOfType:] {
			created, err := time.Parse(time.RFC3339, tag.Annotations[releasecontroller.ReleaseAnnotationCreationTimestamp])
			if err != nil {
				klog.Errorf("Unparseable timestamp on release tag %s:%s: %v", release.Target.Name, tag.Name, err)
				continue
			}
			if created.Add(expires).Before(now) {
				removeTags = append(removeTags, tag)
			}
		}
	}

	switch release.Config.As {
	case releasecontroller.ReleaseConfigModeStable:
		hasNewImages = false
		inputImageHash = ""
		removeTags = nil
	default:
		// gate creating new releases when we already are at max unready or in the cooldown interval
		if release.Config.MaxUnreadyReleases > 0 && unreadyTagCount >= release.Config.MaxUnreadyReleases {
			klog.V(2).Infof("Release %s at max %d unready releases, will not launch new tags", release.Config.Name, release.Config.MaxUnreadyReleases)
			hasNewImages = false
		}
		if firstTag != nil {
			delay, msg, interval := releasecontroller.IsReleaseDelayedForInterval(release, firstTag)
			if delay {
				queueAfter = interval
				klog.V(2).Info(msg)
				hasNewImages = false
			}
		}
	}

	return adoptTags, pendingTags, removeTags, hasNewImages, inputImageHash, queueAfter
}

func (c *Controller) syncAdopted(release *releasecontroller.Release, adoptTags []*imagev1.TagReference, now time.Time) (changed bool, err error) {
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
		[]string{"", releasecontroller.ReleasePhasePending},
		releasecontroller.ReleasePhasePending,
		map[string]string{
			releasecontroller.ReleaseAnnotationName:              release.Config.Name,
			releasecontroller.ReleaseAnnotationSource:            fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
			releasecontroller.ReleaseAnnotationCreationTimestamp: now.Format(time.RFC3339),
		},
		names...,
	)
}

func (c *Controller) syncPending(release *releasecontroller.Release, pendingTags []*imagev1.TagReference, inputImageHash string) (err error) {
	switch release.Config.As {
	case releasecontroller.ReleaseConfigModeStable:
		for _, tag := range pendingTags {
			// wait for import, then determine whether the requested version (tag name) matches the source version (label on image)
			id := releasecontroller.FindImageIDForTag(release.Source, tag.Name)
			if len(id) == 0 {
				klog.V(2).Infof("Waiting for release %s to be imported before we can retrieve metadata", tag.Name)
				continue
			}
			klog.V(2).Infof("Processing pending release %s", tag.Name)
			rewriteValue := tag.Annotations[releasecontroller.ReleaseAnnotationRewrite]
			if len(rewriteValue) == 0 {
				klog.V(2).Infof("Rewriting pending release %s", tag.Name)
				isi, err := c.imageClient.ImageStreamImages(release.Source.Namespace).Get(context.TODO(), fmt.Sprintf("%s@%s", release.Source.Name, id), metav1.GetOptions{})
				if err != nil {
					return err
				}
				// Handle manifest list based releases...
				for _, m := range isi.Image.DockerImageManifests {
					if m.Architecture == "amd64" {
						isi, err = c.imageClient.ImageStreamImages(release.Source.Namespace).Get(context.TODO(), fmt.Sprintf("%s@%s", release.Source.Name, m.Digest), metav1.GetOptions{})
						if err != nil {
							return err
						}
						break
					}
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
				if err := c.setReleaseAnnotation(release, tag.Annotations[releasecontroller.ReleaseAnnotationPhase], map[string]string{releasecontroller.ReleaseAnnotationRewrite: rewriteValue}, tag.Name); err != nil {
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
			if len(tag.Annotations[releasecontroller.ReleaseAnnotationImageHash]) == 0 {
				if err := c.setReleaseAnnotation(release, tag.Annotations[releasecontroller.ReleaseAnnotationPhase], map[string]string{releasecontroller.ReleaseAnnotationImageHash: mirror.Annotations[releasecontroller.ReleaseAnnotationImageHash]}, tag.Name); err != nil {
					return err
				}
				continue
			}
			if mirror.Annotations[releasecontroller.ReleaseAnnotationImageHash] != tag.Annotations[releasecontroller.ReleaseAnnotationImageHash] {
				// delete the mirror and exit
				return fmt.Errorf("unimplemented, should regenerate contents of tag")
			}
			// Create the corresponding ReleasePayload object...
			_, err = c.ensureReleasePayload(release, tag)
			if err != nil {
				return err
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
			payload, err := c.releasePayloadLister.ReleasePayloads(release.Target.Namespace).Get(tag.Name)
			if err != nil {
				if !errors.IsNotFound(err) {
					return err
				}
				return c.ensureRewriteJobImageRetrieved(release, job, mirror)
			}
			phase := releasecontroller.GetReleasePhase(payload)
			switch phase {
			case releasecontroller.ReleasePhaseReady:
				if err := c.markReleaseReady(release, nil, tag.Name); err != nil {
					return err
				}
				c.precacheChangelog(release, tag)
			case releasecontroller.ReleasePhaseFailed:
				log, _, _ := ensureJobTerminationMessageRetrieved(c.podClient, job, "status.phase=Failed", "build", false)
				if err := c.transitionReleasePhaseFailure(release, []string{releasecontroller.ReleasePhasePending}, releasecontroller.ReleasePhaseFailed, withLog(reasonAndMessage("CreateReleaseFailed", "Could not create the release image"), log), tag.Name); err != nil {
					return err
				}
			default:
				return c.ensureRewriteJobImageRetrieved(release, job, mirror)
			}
		}
		return nil
	}

	if len(pendingTags) > 1 {
		if err := c.transitionReleasePhaseFailure(release, []string{releasecontroller.ReleasePhasePending}, releasecontroller.ReleasePhaseFailed, reasonAndMessage("Aborted", "Multiple releases were found simultaneously running."), releasecontroller.TagNames(pendingTags[1:])...); err != nil {
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
		if len(tag.Annotations[releasecontroller.ReleaseAnnotationImageHash]) == 0 {
			return c.setReleaseAnnotation(release, releasecontroller.ReleasePhasePending, map[string]string{releasecontroller.ReleaseAnnotationImageHash: mirror.Annotations[releasecontroller.ReleaseAnnotationImageHash]}, tag.Name)
		}
		if mirror.Annotations[releasecontroller.ReleaseAnnotationImageHash] != tag.Annotations[releasecontroller.ReleaseAnnotationImageHash] {
			return fmt.Errorf("mirror hash for %q does not match, release cannot be created", tag.Name)
		}

		var job *batchv1.Job
		if releasecontroller.IsReferenceReleaseTag(release, tag) {
			job, err = c.ensureReferenceReleaseJob(release, tag.Name, mirror)
		} else {
			job, err = c.ensureReleaseJob(release, tag.Name, mirror)
		}
		if err != nil || job == nil {
			return err
		}
		payload, err := c.releasePayloadLister.ReleasePayloads(release.Target.Namespace).Get(tag.Name)
		if err != nil {
			if !errors.IsNotFound(err) {
				return err
			}
			return nil
		}
		phase := releasecontroller.GetReleasePhase(payload)
		klog.V(4).Infof("Release creation for %s phase: %s", tag.Name, phase)
		switch phase {
		case releasecontroller.ReleasePhaseReady:
			if err := c.markReleaseReady(release, nil, tag.Name); err != nil {
				return err
			}
			c.precacheChangelog(release, tag)
		case releasecontroller.ReleasePhaseFailed:
			log, _, _ := ensureJobTerminationMessageRetrieved(c.podClient, job, "status.phase=Failed", "build", false)
			if err := c.transitionReleasePhaseFailure(release, []string{releasecontroller.ReleasePhasePending}, releasecontroller.ReleasePhaseFailed, withLog(reasonAndMessage("CreateReleaseFailed", "Could not create the release image"), log), tag.Name); err != nil {
				return err
			}
		default:
			return nil
		}
	}

	return nil
}

func (c *Controller) syncReady(release *releasecontroller.Release) error {
	readyTags := releasecontroller.SortedRawReleaseTags(release, releasecontroller.ReleasePhaseReady)

	if klog.V(5) && len(readyTags) > 0 {
		klog.Infof("ready=%v", releasecontroller.TagNames(readyTags))
	}

	for _, releaseTag := range readyTags {
		mirror, err := releasecontroller.GetMirror(release, releaseTag.Name, c.releaseLister)
		if err != nil {
			klog.Errorf("Failed to identify `from` mirror for creation of release mirror job: %v", err)
		} else if _, err := c.ensureReleaseMirrorJob(release, releaseTag.Name, mirror); err != nil {
			klog.Errorf("Failed to create release mirror job: %v", err)
		}

		if err := c.ensureReleaseUpgradeJobs(release, releaseTag); err != nil {
			klog.Errorf("unable to launch release upgrade jobs for %q: %v", releaseTag.Name, err)
		}

		payload, verifyStatus, err := c.getReleasePayloadVerificationState(release, releaseTag.Name)
		if err != nil {
			klog.Errorf("unable to get ReleasePayload for %q: %v", releaseTag.Name, err)
			continue
		}

		if err := c.ensureVerificationJobs(release, releaseTag, verifyStatus); err != nil {
			if errors.IsConflict(err) {
				return nil
			}
			return err
		}

		phase := releasecontroller.GetReleasePhase(payload)
		switch phase {
		case releasecontroller.ReleasePhaseAccepted:
			if err := c.markReleaseAccepted(release, nil, releaseTag.Name); err != nil {
				return err
			}
			klog.V(4).Infof("Release %s accepted", releaseTag.Name)

		case releasecontroller.ReleasePhaseRejected:
			reason, message := getRejectionDetails(payload)
			annotations := reasonAndMessage(reason, message)
			if err := c.transitionReleasePhaseFailure(release, []string{releasecontroller.ReleasePhaseReady}, releasecontroller.ReleasePhaseRejected, annotations, releaseTag.Name); err != nil {
				return err
			}
			klog.V(4).Infof("Release %s rejected", releaseTag.Name)

		default:
			klog.V(4).Infof("Verification jobs for %s are still running (phase=%s)", releaseTag.Name, phase)
		}
	}

	return nil
}

func (c *Controller) syncAccepted(release *releasecontroller.Release) error {
	acceptedTags := releasecontroller.SortedRawReleaseTags(release, releasecontroller.ReleasePhaseAccepted)

	if klog.V(5) && len(acceptedTags) > 0 {
		klog.Infof("release=%s accepted=%v", release.Config.Name, releasecontroller.TagNames(acceptedTags))
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

func (c *Controller) precacheChangelog(release *releasecontroller.Release, tag *imagev1.TagReference) {
	if tags := releasecontroller.SortedRawReleaseTags(release, releasecontroller.ReleasePhaseReady); len(tags) > 0 {
		go func() {
			fromPullSpec := releasecontroller.ReleasePullSpec(release, tags[0])
			if len(fromPullSpec) == 0 {
				klog.Errorf("Unable to determine pullspec for fromImage: %s", tags[0].Name)
				return
			}
			fromImage, err := releasecontroller.GetImageInfo(c.releaseInfo, c.architecture, fromPullSpec)
			if err != nil {
				klog.Errorf("Unable to get from image info for release %s: %v", tags[0].Name, err)
				return
			}

			toPullSpec := releasecontroller.ReleasePullSpec(release, tag)
			if len(toPullSpec) == 0 {
				klog.Errorf("Unable to determine pullspec for toImage: %s", tag.Name)
				return
			}
			toImage, err := releasecontroller.GetImageInfo(c.releaseInfo, c.architecture, toPullSpec)
			if err != nil {
				klog.Errorf("Unable to get to image info for release %s: %v", tag.Name, err)
				return
			}

			if _, err := c.releaseInfo.ChangeLog(fromImage.GenerateDigestPullSpec(), toImage.GenerateDigestPullSpec(), false); err != nil {
				klog.V(4).Infof("Unable to pre-cache changelog for new ready release %s: %v", tag.Name, err)
			}
		}()
	}
}

func (c *Controller) populatePayloadPhases(release *releasecontroller.Release) {
	releasecontroller.PopulatePayloadPhases(release, c.releasePayloadLister.ReleasePayloads(release.Target.Namespace))
}

func (c *Controller) loadReleaseForSync(namespace, name string) (*releasecontroller.Release, error) {
	// locate the release definition off the image stream, or clean up any remaining
	// artifacts if the release no longer points to those
	isLister := c.releaseLister.ImageStreams(namespace)
	if isLister == nil {
		return nil, nil
	}
	imageStream, err := isLister.Get(name)
	if errors.IsNotFound(err) {
		c.gcQueue.AddAfter("", 10*time.Second)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	release, ok, err := releasecontroller.ReleaseDefinition(imageStream, c.parsedReleaseConfigCache, c.eventRecorder, *c.releaseLister)
	if err != nil {
		return nil, err
	}
	if !ok {
		c.gcQueue.AddAfter("", 10*time.Second)
		return nil, nil
	}
	return release, nil
}

func (c *Controller) getReleasePayloadVerificationState(release *releasecontroller.Release, tagName string) (*v1alpha1.ReleasePayload, releasecontroller.VerificationStatusMap, error) {
	nsLister := c.releasePayloadLister.ReleasePayloads(release.Target.Namespace)
	if nsLister == nil {
		return nil, nil, fmt.Errorf("no ReleasePayload lister for namespace %s", release.Target.Namespace)
	}
	payload, err := nsLister.Get(tagName)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get ReleasePayload %s/%s: %v", release.Target.Namespace, tagName, err)
	}
	var verifyStatus releasecontroller.VerificationStatusMap
	releasepayload.GenerateVerificationStatusMap(payload, &verifyStatus)
	return payload, verifyStatus, nil
}

func getRejectionDetails(payload *v1alpha1.ReleasePayload) (string, string) {
	for _, cond := range payload.Status.Conditions {
		if cond.Type == v1alpha1.ConditionPayloadRejected && cond.Status == metav1.ConditionTrue {
			return cond.Reason, cond.Message
		}
	}
	return "VerificationFailed", "release verification failed"
}

