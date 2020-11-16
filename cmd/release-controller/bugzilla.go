package main

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/openshift/api/image/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog"
)

func getNonVerifiedTags(acceptedTags []*v1.TagReference) (current, previous *v1.TagReference) {
	// get oldest non-verified tag to make sure none are missed
	// accepted tags are returned sorted with the latest release first; reverse so we can get oldest non-verified release
	for i := 0; i < len(acceptedTags)/2; i++ {
		j := len(acceptedTags) - i - 1
		acceptedTags[i], acceptedTags[j] = acceptedTags[j], acceptedTags[i]
	}
	for index, tag := range acceptedTags {
		if anno, ok := tag.Annotations[releaseAnnotationBugsVerified]; !ok || anno != "true" {
			if index == 0 {
				return tag, nil
			}
			return tag, acceptedTags[index-1]
		}
	}
	return nil, nil
}

// syncBugzilla checks whether fixed bugs in a release had their
// PR reviewed and approved by the QA contact for the bug
func (c *Controller) syncBugzilla(key queueKey) error {
	defer func() {
		if err := recover(); err != nil {
			panic(err)
		}
	}()

	release, err := c.loadReleaseForSync(key.namespace, key.name)
	if err != nil || release == nil {
		klog.V(6).Infof("bugzilla: could not load release for sync for %s/%s", key.namespace, key.name)
		return err
	}

	klog.V(6).Infof("checking if %v (%s) has verifyBugs set", key, release.Config.Name)

	// check if verifyBugs publish step is enabled for release
	var verifyBugs *PublishVerifyBugs
	for _, publishType := range release.Config.Publish {
		if publishType.Disabled {
			continue
		}
		switch {
		case publishType.VerifyBugs != nil:
			verifyBugs = publishType.VerifyBugs
		}
		if verifyBugs != nil {
			break
		}
	}
	if verifyBugs == nil {
		klog.V(6).Infof("%v (%s) does not have verifyBugs set", key, release.Config.Name)
		return nil
	}

	klog.V(4).Infof("Verifying fixed bugs in %s", release.Config.Name)

	// get accepted tags
	acceptedTags := sortedRawReleaseTags(release, releasePhaseAccepted)
	tag, prevTag := getNonVerifiedTags(acceptedTags)
	if tag == nil {
		klog.V(6).Infof("bugzilla: All accepted tags for %s have already been verified", release.Config.Name)
		return nil
	}
	if prevTag == nil {
		if verifyBugs.PreviousReleaseTag == nil {
			klog.V(2).Infof("bugzilla error: previous release unset for %s", release.Config.Name)
			return fmt.Errorf("bugzilla error: previous release unset for %s", release.Config.Name)
		}
		stream, err := c.imageClient.ImageStreams(verifyBugs.PreviousReleaseTag.Namespace).Get(context.TODO(), verifyBugs.PreviousReleaseTag.Name, meta.GetOptions{})
		if err != nil {
			klog.V(2).Infof("bugzilla: failed to get imagestream (%s/%s) when getting previous release for %s: %v", verifyBugs.PreviousReleaseTag.Namespace, verifyBugs.PreviousReleaseTag.Name, release.Config.Name, err)
			return err
		}
		prevTag = findTagReference(stream, verifyBugs.PreviousReleaseTag.Tag)
		if prevTag == nil {
			klog.V(2).Infof("bugzilla: failed to get tag %s in imagestream (%s/%s) when getting previous release for %s", verifyBugs.PreviousReleaseTag.Tag, verifyBugs.PreviousReleaseTag.Namespace, verifyBugs.PreviousReleaseTag.Name, release.Config.Name)
			return fmt.Errorf("failed to find tag %s in imagestream %s/%s", verifyBugs.PreviousReleaseTag.Tag, verifyBugs.PreviousReleaseTag.Namespace, verifyBugs.PreviousReleaseTag.Name)
		}
	}
	// To make sure all tags are up-to-date, requeue when non-verified tags are found; this allows us to make
	// sure tags that may not have been passed to this function (such as older tags) get processed,
	// and it also allows us to handle the case where the imagestream fails to update below.
	defer c.queue.AddAfter(key, time.Second)

	dockerRepo := release.Target.Status.PublicDockerImageRepository
	if len(dockerRepo) == 0 {
		klog.V(4).Infof("bugzilla: release target %s does not have a configured registry", release.Target.Name)
		return fmt.Errorf("bugzilla: release target %s does not have a configured registry", release.Target.Name)
	}

	bugs, err := c.releaseInfo.Bugs(dockerRepo+":"+prevTag.Name, dockerRepo+":"+tag.Name)
	if err != nil {
		klog.V(4).Infof("Unable to generate bug list from %s to %s: %v", prevTag.Name, tag.Name, err)
		return fmt.Errorf("Unable to generate bug list from %s to %s: %v", prevTag.Name, tag.Name, err)
	}
	var errs []error
	if errs := append(errs, c.bugzillaVerifier.VerifyBugs(bugs)...); len(errs) != 0 {
		klog.V(4).Infof("Error(s) in bugzilla verifier: %v", utilerrors.NewAggregate(errs))
		return utilerrors.NewAggregate(errs)
	}

	target := release.Target.DeepCopy()
	tagToBeUpdated := findTagReference(target, tag.Name)
	if tagToBeUpdated == nil {
		klog.V(6).Infof("release %s no longer exists, cannot set annotation %s=true", tag.Name, releaseAnnotationBugsVerified)
		return fmt.Errorf("release %s no longer exists, cannot set annotation %s=true", tag.Name, releaseAnnotationBugsVerified)
	}
	if tagToBeUpdated.Annotations == nil {
		tagToBeUpdated.Annotations = make(map[string]string)
	}
	tagToBeUpdated.Annotations[releaseAnnotationBugsVerified] = "true"
	klog.V(6).Infof("Setting %s annotation to \"true\" for %s in imagestream %s/%s", releaseAnnotationBugsVerified, tag.Name, target.GetNamespace(), target.GetName())
	if _, err := c.imageClient.ImageStreams(target.Namespace).Update(context.TODO(), target, meta.UpdateOptions{}); err != nil {
		klog.V(4).Infof("Failed to update bugzilla annotation for tag %s in imagestream %s/%s: %v", tag.Name, target.GetNamespace(), target.GetName(), err)
		return err
	}

	return nil
}
