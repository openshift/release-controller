package main

import (
	"fmt"

	"github.com/golang/glog"
	v1 "github.com/openshift/api/image/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

// syncBugzilla checks whether fixed bugs in a release had their
// PR reviewed and approved by the QA contact for the bug
func (c *Controller) syncBugzilla(key queueKey) error {
	defer func() {
		err := recover()
		panic(err)
	}()

	release, err := c.loadReleaseForSync(key.namespace, key.name)
	if err != nil || release == nil {
		return err
	}

	glog.V(6).Infof("checking if %v (%s) has verifyBugs set", key, release.Config.Name)

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
		glog.V(6).Infof("%v (%s) does not have verifyBugs set", key, release.Config.Name)
		return nil
	}

	glog.V(4).Infof("Verifying fixed bugs in %s", release.Config.Name)

	// get accepted tags
	acceptedTags := sortedRawReleaseTags(release, releasePhaseAccepted)
	// We need at least 2 accepted releases to compare
	if len(acceptedTags) < 2 {
		return nil
	}
	var errs []error
	// get latest accepted tag that has not been bugzilla verified
	var tag, prevTag *v1.TagReference
	for index, currTag := range acceptedTags {
		if tag.Annotations[releaseAnnotationBugsVerified] == "true" {
			// if we've already checked the latest tag, return
			if index == 0 {
				return nil
			}
			prevTag = currTag
			tag = acceptedTags[index-1]
			break
		}
	}

	bugs, err := c.releaseInfo.Bugs(prevTag.Name, tag.Name)
	if err != nil {
		return fmt.Errorf("Unable to generate changelog from %s to %s: %v", prevTag.Name, tag.Name, err)
	}
	if errs := append(errs, c.bugzillaVerifier.VerifyBugs(bugs)...); len(errs) != 0 {
		return utilerrors.NewAggregate(errs)
	}

	target := release.Target.DeepCopy()
	tagToBeUpdated := findTagReference(target, tag.Name)
	if tagToBeUpdated == nil {
		return fmt.Errorf("release %s no longer exists, cannot set annotation %s=true", tag.Name, releaseAnnotationBugsVerified)
	}
	if tagToBeUpdated.Annotations == nil {
		tagToBeUpdated.Annotations = make(map[string]string)
	}
	tagToBeUpdated.Annotations[releaseAnnotationBugsVerified] = "true"
	if _, err := c.imageClient.ImageStreams(target.Namespace).Update(target); err != nil {
		return err
	}

	return nil
}
