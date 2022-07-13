package main

import (
	"context"
	"fmt"
	"reflect"
	"time"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	"github.com/blang/semver"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	imagev1 "github.com/openshift/api/image/v1"
)

func (c *Controller) createReleaseTag(release *releasecontroller.Release, now time.Time, inputImageHash string) (*imagev1.TagReference, error) {
	target := release.Target.DeepCopy()
	now = now.UTC().Truncate(time.Second)
	t := now.Format("2006-01-02-150405")
	tag := imagev1.TagReference{
		Name: fmt.Sprintf("%s-%s", release.Config.Name, t),
		Annotations: map[string]string{
			releasecontroller.ReleaseAnnotationName:              release.Config.Name,
			releasecontroller.ReleaseAnnotationSource:            fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
			releasecontroller.ReleaseAnnotationCreationTimestamp: now.Format(time.RFC3339),
			releasecontroller.ReleaseAnnotationPhase:             releasecontroller.ReleasePhasePending,
		},
	}
	target.Spec.Tags = append(target.Spec.Tags, tag)

	klog.V(2).Infof("Starting new release %s", tag.Name)

	is, err := c.imageClient.ImageStreams(target.Namespace).Update(context.TODO(), target, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	updateReleaseTarget(release, is)

	// Create the corresponding ReleasePayload object...
	_, err = c.ensureReleasePayload(release, tag.Name)
	if err != nil {
		return nil, err
	}

	return &is.Spec.Tags[len(is.Spec.Tags)-1], nil
}

func (c *Controller) replaceReleaseTagWithNext(release *releasecontroller.Release, tag *imagev1.TagReference) error {
	var semanticTags []semver.Version
	for _, tag := range releasecontroller.SortedReleaseTags(release) {
		version, err := semver.Parse(tag.Name)
		if err != nil {
			continue
		}
		semanticTags = append(semanticTags, version)
	}

	// we don't want to leave the tag pending, because there is no tag to increment from and
	// a future update shouldn't change it
	if len(semanticTags) == 0 {
		return c.transitionReleasePhaseFailure(release, []string{"", releasecontroller.ReleasePhasePending}, releasecontroller.ReleasePhaseFailed, reasonAndMessage("NoExistingTags", "The next tag requires at least one existing semantic version tag in this image stream."), tag.Name)
	}

	// find the next tag
	semver.Sort(semanticTags)
	newest := semanticTags[len(semanticTags)-1]
	next, err := releasecontroller.IncrementSemanticVersion(newest)
	if err != nil {
		return err
	}

	// reset the nextTag with a new name
	target := release.Target.DeepCopy()
	origin := releasecontroller.FindTagReference(target, tag.Name)
	origin.Name = next.String()
	origin.Annotations = map[string]string{
		releasecontroller.ReleaseAnnotationName:              release.Config.Name,
		releasecontroller.ReleaseAnnotationCreationTimestamp: time.Now().Format(time.RFC3339),
		releasecontroller.ReleaseAnnotationPhase:             releasecontroller.ReleasePhasePending,
		releasecontroller.ReleaseAnnotationSource:            fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
		releasecontroller.ReleaseAnnotationRewrite:           "true",
	}
	if value, ok := tag.Annotations[releasecontroller.ReleaseAnnotationMirrorImages]; ok {
		origin.Annotations[releasecontroller.ReleaseAnnotationMirrorImages] = value
	}
	origin.From = tag.From
	origin.ImportPolicy = tag.ImportPolicy

	klog.V(2).Infof("Updating next tag for %s to be %s", release.Config.Name, next)
	is, err := c.imageClient.ImageStreams(target.Namespace).Update(context.TODO(), target, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	updateReleaseTarget(release, is)
	return nil
}

func (c *Controller) removeReleaseTags(release *releasecontroller.Release, removeTags []*imagev1.TagReference) error {
	for _, tag := range removeTags {
		ctx := context.TODO()
		name := fmt.Sprintf("%s:%s", release.Target.Name, tag.Name)
		if c.softDeleteReleaseTags {
			// indicate by annotation that the isTag should be deleted
			klog.V(2).Infof("Soft-removing (annotating) release tag %s", tag.Name)
			isTag, err := c.imageClient.ImageStreamTags(release.Target.Namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}
			if isTag.Annotations == nil {
				isTag.Annotations = map[string]string{}
			}
			if _, ok := isTag.Annotations[releasecontroller.ReleaseAnnotationSoftDelete]; !ok {
				isTag.Annotations[releasecontroller.ReleaseAnnotationSoftDelete] = time.Now().Format(time.RFC3339)
				if _, err := c.imageClient.ImageStreamTags(release.Target.Namespace).Update(context.TODO(), isTag, metav1.UpdateOptions{}); err != nil && !errors.IsNotFound(err) {
					return err
				}
			}
		} else {
			// use delete imagestreamtag so that status tags are removed as well
			klog.V(2).Infof("Removing release tag %s", tag.Name)
			if err := c.imageClient.ImageStreamTags(release.Target.Namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
				if !errors.IsNotFound(err) {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Controller) setReleaseAnnotation(release *releasecontroller.Release, phase string, annotations map[string]string, names ...string) error {
	is := release.Target

	if len(names) == 0 {
		return nil
	}

	changes := 0
	target := is.DeepCopy()
	for _, name := range names {
		tag := releasecontroller.FindTagReference(target, name)
		if tag == nil {
			return fmt.Errorf("release %s no longer exists, cannot be put into %s", name, phase)
		}
		if tag.Annotations == nil {
			tag.Annotations = make(map[string]string)
		}
		if current := tag.Annotations[releasecontroller.ReleaseAnnotationPhase]; current != phase {
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
		if oldTag := releasecontroller.FindTagReference(release.Target, name); oldTag != nil {
			if reflect.DeepEqual(oldTag.Annotations, tag.Annotations) {
				continue
			}
		}
		changes++
		klog.V(2).Infof("Adding annotations to release %s", name)
	}

	if changes == 0 {
		return nil
	}

	is, err := c.imageClient.ImageStreams(target.Namespace).Update(context.TODO(), target, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	updateReleaseTarget(release, is)
	return nil
}

func (c *Controller) markReleaseReady(release *releasecontroller.Release, annotations map[string]string, names ...string) error {
	return c.ensureReleaseTagPhase(release, []string{releasecontroller.ReleasePhasePending, releasecontroller.ReleasePhaseReady, ""}, releasecontroller.ReleasePhaseReady, annotations, names...)
}

func (c *Controller) markReleaseAccepted(release *releasecontroller.Release, annotations map[string]string, names ...string) error {
	return c.ensureReleaseTagPhase(release, []string{releasecontroller.ReleasePhaseReady}, releasecontroller.ReleasePhaseAccepted, annotations, names...)
}

func (c *Controller) ensureReleaseTagPhase(release *releasecontroller.Release, preconditionPhases []string, phase string, annotations map[string]string, names ...string) error {
	if len(names) == 0 {
		return nil
	}

	changes := 0
	target := release.Target.DeepCopy()
	for _, name := range names {
		tag := releasecontroller.FindTagReference(target, name)
		if tag == nil {
			return fmt.Errorf("release %s no longer exists, cannot be put into %s", name, phase)
		}

		if current := tag.Annotations[releasecontroller.ReleaseAnnotationPhase]; !releasecontroller.ContainsString(preconditionPhases, current) {
			return fmt.Errorf("release %s is not in phase %v (%s), unable to mark %s", name, preconditionPhases, current, phase)
		}
		if tag.Annotations == nil {
			tag.Annotations = make(map[string]string)
		}
		tag.Annotations[releasecontroller.ReleaseAnnotationPhase] = phase
		delete(tag.Annotations, releasecontroller.ReleaseAnnotationReason)
		delete(tag.Annotations, releasecontroller.ReleaseAnnotationMessage)
		for k, v := range annotations {
			if len(v) == 0 {
				delete(tag.Annotations, k)
			} else {
				tag.Annotations[k] = v
			}
		}

		// if there is no change, avoid updating anything
		if oldTag := releasecontroller.FindTagReference(release.Target, name); oldTag != nil {
			if reflect.DeepEqual(oldTag.Annotations, tag.Annotations) {
				continue
			}
		}
		changes++
		klog.V(2).Infof("Marking release %s %s", name, phase)
	}

	if changes == 0 {
		return nil
	}

	is, err := c.imageClient.ImageStreams(target.Namespace).Update(context.TODO(), target, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	updateReleaseTarget(release, is)
	return nil
}

func (c *Controller) transitionReleasePhaseFailure(release *releasecontroller.Release, preconditionPhases []string, phase string, annotations map[string]string, names ...string) error {
	target := release.Target.DeepCopy()
	changed := 0
	for _, name := range names {
		if tag := releasecontroller.FindTagReference(target, name); tag != nil {
			if current := tag.Annotations[releasecontroller.ReleaseAnnotationPhase]; !releasecontroller.ContainsString(preconditionPhases, current) {
				return fmt.Errorf("release %s is not in phase %v (%s), unable to mark %s", name, preconditionPhases, current, phase)
			}
			if tag.Annotations == nil {
				tag.Annotations = make(map[string]string)
			}
			tag.Annotations[releasecontroller.ReleaseAnnotationPhase] = phase
			for k, v := range annotations {
				tag.Annotations[k] = v
			}
			klog.V(2).Infof("Marking release %s failed: %v", name, annotations)
			changed++
		}
	}
	if changed == 0 {
		// release tags have all been deleted
		return nil
	}

	is, err := c.imageClient.ImageStreams(target.Namespace).Update(context.TODO(), target, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	updateReleaseTarget(release, is)
	return nil
}

func reasonAndMessage(reason, message string) map[string]string {
	return map[string]string{
		releasecontroller.ReleaseAnnotationReason:  reason,
		releasecontroller.ReleaseAnnotationMessage: message,
	}
}

func withLog(annotations map[string]string, log string) map[string]string {
	if len(log) > 0 {
		annotations[releasecontroller.ReleaseAnnotationLog] = log
	}
	return annotations
}

func updateReleaseTarget(release *releasecontroller.Release, is *imagev1.ImageStream) {
	if release.Config.As == releasecontroller.ReleaseConfigModeStable {
		release.Source = is
	}
	release.Target = is
}
