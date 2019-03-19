package main

import (
	"fmt"
	"reflect"
	"time"

	"github.com/blang/semver"
	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/api/errors"

	imagev1 "github.com/openshift/api/image/v1"
)

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
	updateReleaseTarget(release, is)

	return &is.Spec.Tags[len(is.Spec.Tags)-1], nil
}

func (c *Controller) replaceReleaseTagWithNext(release *Release, tag *imagev1.TagReference) error {
	var semanticTags []semver.Version
	for _, tag := range tagsForRelease(release) {
		version, err := semver.Parse(tag.Name)
		if err != nil {
			continue
		}
		semanticTags = append(semanticTags, version)
	}

	// we don't want to leave the tag pending, because there is no tag to increment from and
	// a future update shouldn't change it
	if len(semanticTags) == 0 {
		return c.transitionReleasePhaseFailure(release, []string{"", releasePhasePending}, releasePhaseFailed, reasonAndMessage("NoExistingTags", "The next tag requires at least one existing semantic version tag in this image stream."), tag.Name)
	}

	// find the next tag
	semver.Sort(semanticTags)
	newest := semanticTags[len(semanticTags)-1]
	next, err := incrementSemanticVersion(newest)
	if err != nil {
		return err
	}

	// reset the nextTag with a new name
	target := release.Target.DeepCopy()
	origin := findTagReference(target, tag.Name)
	origin.Name = next.String()
	origin.Annotations = map[string]string{
		releaseAnnotationName:              release.Config.Name,
		releaseAnnotationCreationTimestamp: time.Now().Format(time.RFC3339),
		releaseAnnotationPhase:             releasePhasePending,
		releaseAnnotationSource:            fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
		releaseAnnotationRewrite:           "true",
	}
	if value, ok := tag.Annotations[releaseAnnotationMirrorImages]; ok {
		origin.Annotations[releaseAnnotationMirrorImages] = value
	}
	origin.From = tag.From
	origin.ImportPolicy = tag.ImportPolicy

	glog.V(2).Infof("Updating next tag for %s to be %s", release.Config.Name, next)
	is, err := c.imageClient.ImageStreams(target.Namespace).Update(target)
	if err != nil {
		return err
	}
	updateReleaseTarget(release, is)
	return nil
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

func (c *Controller) setReleaseAnnotation(release *Release, phase string, annotations map[string]string, names ...string) error {
	is := release.Target

	if len(names) == 0 {
		return nil
	}

	changes := 0
	target := is.DeepCopy()
	for _, name := range names {
		tag := findTagReference(target, name)
		if tag == nil {
			return fmt.Errorf("release %s no longer exists, cannot be put into %s", name, phase)
		}
		if tag.Annotations == nil {
			tag.Annotations = make(map[string]string)
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
	updateReleaseTarget(release, is)
	return nil
}

func (c *Controller) markReleaseReady(release *Release, annotations map[string]string, names ...string) error {
	return c.ensureReleaseTagPhase(release, []string{releasePhasePending, releasePhaseReady, ""}, releasePhaseReady, annotations, names...)
}

func (c *Controller) markReleaseAccepted(release *Release, annotations map[string]string, names ...string) error {
	return c.ensureReleaseTagPhase(release, []string{releasePhaseReady}, releasePhaseAccepted, annotations, names...)
}

func (c *Controller) ensureReleaseTagPhase(release *Release, preconditionPhases []string, phase string, annotations map[string]string, names ...string) error {
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
		if tag.Annotations == nil {
			tag.Annotations = make(map[string]string)
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
	updateReleaseTarget(release, is)
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
			if tag.Annotations == nil {
				tag.Annotations = make(map[string]string)
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
	updateReleaseTarget(release, is)
	return nil
}

func incrementSemanticVersion(v semver.Version) (semver.Version, error) {
	if len(v.Pre) == 0 {
		copied := v
		copied.Patch++
		return copied, nil
	}
	for i := len(v.Pre) - 1; i >= 0; i-- {
		if v.Pre[i].IsNum {
			copied := v
			copied.Pre[i].VersionNum++
			return copied, nil
		}
	}
	return v, fmt.Errorf("the version %s cannot be incremented, no numeric prerelease portions", v.String())
}

func reasonAndMessage(reason, message string) map[string]string {
	return map[string]string{
		releaseAnnotationReason:  reason,
		releaseAnnotationMessage: message,
	}
}

func updateReleaseTarget(release *Release, is *imagev1.ImageStream) {
	if release.Config.As == releaseConfigModeStable {
		release.Source = is
	}
	release.Target = is
}
