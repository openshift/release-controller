package main

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog"

	imagev1 "github.com/openshift/api/image/v1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
)

func (c *Controller) ensureTagPointsToRelease(release *releasecontroller.Release, to, from string) error {
	if to == from {
		return nil
	}
	fromTag := releasecontroller.FindTagReference(release.Target, from)
	toTag := releasecontroller.FindTagReference(release.Target, to)
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
	toTag = releasecontroller.FindTagReference(target, to)
	if toTag == nil {
		target.Spec.Tags = append(target.Spec.Tags, imagev1.TagReference{
			Name: to,
		})
		toTag = &target.Spec.Tags[len(target.Spec.Tags)-1]
	}
	toTag.From = &corev1.ObjectReference{Kind: "ImageStreamTag", Name: from}
	toTag.ImportPolicy = imagev1.TagImportPolicy{ImportMode: imagev1.ImportModePreserveOriginal}

	is, err := c.imageClient.ImageStreams(target.Namespace).Update(context.TODO(), target, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	klog.V(2).Infof("Updated image stream tag %s/%s:%s to point to %s", release.Target.Namespace, release.Target.Name, to, from)
	updateReleaseTarget(release, is)
	return nil
}

func (c *Controller) ensureImageStreamMatchesRelease(release *releasecontroller.Release, toNamespace, toName, from string, tags, excludeTags []string) error {
	if len(tags) == 0 {
		klog.V(4).Infof("Ensure image stream %s/%s has contents of %s", toNamespace, toName, from)
	} else {
		klog.V(4).Infof("Ensure image stream %s/%s has tags from %s: %s", toNamespace, toName, from, strings.Join(tags, ", "))
	}
	if toNamespace == release.Source.Namespace && toName == release.Source.Name {
		return nil
	}
	fromTag := releasecontroller.FindTagReference(release.Target, from)
	if fromTag == nil {
		// tag was deleted
		return nil
	}

	var mirror *imagev1.ImageStream
	var err error

	// For layered releases, use the release target imagestream directly since there's no separate mirror
	if release.Config.As == releasecontroller.ReleaseConfigModeLayered {
		mirror = release.Target
		klog.V(4).Infof("Using release target imagestream directly for layered release publishing: %s/%s", release.Target.Namespace, release.Target.Name)
	} else {
		mirror, err = releasecontroller.GetMirror(release, from, c.releaseLister)
		if err != nil {
			klog.V(2).Infof("Error getting release mirror image stream: %v", err)
			return nil
		}
	}

	lister := c.publishLister.ImageStreams(toNamespace)
	if lister == nil {
		return fmt.Errorf("cannot publish to namespace %s, namespace was not registered for either release or publish", toNamespace)
	}
	target, err := lister.Get(toName)
	if errors.IsNotFound(err) {
		// TODO: create it?
		klog.V(2).Infof("Target image stream doesn't exist yet: %v", err)
		return nil
	}
	if err != nil {
		// TODO
		klog.V(2).Infof("Error getting publish image stream: %v", err)
		return nil
	}

	if len(tags) == 0 {
		set := fmt.Sprintf("release.openshift.io/source-%s", release.Config.Name)
		if value, ok := target.Annotations[set]; ok && value == from {
			klog.V(2).Infof("Published image stream %s/%s is up to date", toNamespace, toName)
			return nil
		}

		excluded := sets.NewString(excludeTags...)
		processed := sets.NewString()
		finalRefs := make([]imagev1.TagReference, 0, len(mirror.Spec.Tags))
		for _, tag := range mirror.Spec.Tags {
			if processed.Has(tag.Name) || excluded.Has(tag.Name) {
				continue
			}
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

	} else {
		var copied *imagev1.ImageStream
		processed := sets.NewString(excludeTags...)
		for _, tag := range tags {
			if processed.Has(tag) {
				continue
			}
			processed.Insert(tag)

			sourceTag := releasecontroller.FindTagReference(mirror, tag)
			if sourceTag == nil {
				klog.Warningf("The tag %s should be mirrored from %s to %s, but is not in the source tags", tag, release.Config.Name, toName)
				continue
			}
			targetTag := releasecontroller.FindTagReference(target, tag)
			if targetTag != nil && reflect.DeepEqual(targetTag.From, sourceTag.From) {
				// tag is identical
				continue
			}
			if copied == nil {
				copied = target.DeepCopy()
			}
			if targetTag == nil {
				copied.Spec.Tags = append(copied.Spec.Tags, *sourceTag)
			} else {
				targetTag = releasecontroller.FindTagReference(copied, tag)
				*targetTag = *sourceTag
			}
		}
		if copied == nil {
			return nil
		}
		target = copied
	}

	_, err = c.imageClient.ImageStreams(target.Namespace).Update(context.TODO(), target, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(tags) == 0 {
		klog.V(2).Infof("Updated image stream %s/%s to point to contents of %s", toNamespace, toName, from)
	} else {
		klog.V(2).Infof("Updated image stream %s/%s with tags from %s: %s", toNamespace, toName, from, strings.Join(tags, ", "))
	}
	return nil
}

// ensureExternalRegistryMirror handles mirroring of a release to an external registry
func (c *Controller) ensureExternalRegistryMirror(release *releasecontroller.Release, config *releasecontroller.PublishExternalRegistry, releaseTagName string) error {
	// Validation
	if len(config.Registry) == 0 {
		return fmt.Errorf("external registry config has no registry specified")
	}
	if len(config.SecretName) == 0 {
		return fmt.Errorf("external registry config has no secretName specified")
	}

	// Determine tags to mirror (default: release tag only)
	tagsToMirror := config.Tags
	if len(tagsToMirror) == 0 {
		tagsToMirror = []string{releaseTagName}
	}

	// Apply exclusions
	excludeSet := sets.NewString(config.ExcludeTags...)
	var finalTags []string
	for _, tag := range tagsToMirror {
		if !excludeSet.Has(tag) {
			finalTags = append(finalTags, tag)
		}
	}

	klog.V(2).Infof("Creating external registry mirror jobs for %s to %s (tags: %v)", releaseTagName, config.Registry, finalTags)

	// Create mirror jobs for each tag
	var errs []error
	for _, tag := range finalTags {
		if err := c.ensureExternalRegistryMirrorJob(release, config, tag); err != nil {
			klog.Errorf("Failed to create external registry mirror job for %s: %v", tag, err)
			errs = append(errs, fmt.Errorf("failed to create mirror job for tag %s: %v", tag, err))
		}
	}

	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}

	klog.V(2).Infof("External registry mirror jobs created successfully for %s", releaseTagName)
	return nil
}

// ensureExternalRegistryMirrorJob creates a single mirror job for a specific tag
func (c *Controller) ensureExternalRegistryMirrorJob(release *releasecontroller.Release, config *releasecontroller.PublishExternalRegistry, tagName string) error {
	// Create unique job name for this registry and tag combination
	jobName := fmt.Sprintf("%s-external-mirror-%s", tagName, sanitizeRegistryForJobName(config.Registry))

	// Kubernetes limits job names to 63 characters
	if len(jobName) > 63 {
		jobName = jobName[:63]
	}

	_, err := c.ensureJob(jobName, nil, func() (*batchv1.Job, error) {
		// Get tag reference and validate it exists
		tag := releasecontroller.FindTagReference(release.Target, tagName)
		if tag == nil {
			return nil, fmt.Errorf("tag %q not found in target imagestream %s/%s", tagName, release.Target.Namespace, release.Target.Name)
		}

		// Construct image references
		fromImage := releasecontroller.ReleasePullSpec(release, tag)
		toImage := fmt.Sprintf("%s:%s", config.Registry, tagName)

		var cliImage string
		if len(config.OverrideCLIImage) > 0 {
			cliImage = config.OverrideCLIImage
			klog.V(2).Infof("Using override CLI image for external registry mirror: %s", cliImage)
		} else {
			mirror, err := releasecontroller.GetMirror(release, tagName, c.releaseLister)
			if err != nil {
				return nil, fmt.Errorf("failed to get mirror for %s: %v", tagName, err)
			}

			cliImage, err = releasecontroller.ResolveCLIImage(release, mirror)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve CLI image: %v", err)
			}
		}

		// Create job using existing patterns
		job, prefix := newReleaseJobBase(jobName, cliImage, config.SecretName)

		// Configure mirror command (reuse manifest list logic)
		manifestListMode := "false"
		if c.manifestListMode && !release.Config.DisableManifestListMode {
			manifestListMode = "true"
		}

		job.Spec.Template.Spec.Containers[0].Command = []string{
			"/bin/bash", "-c",
			prefix + `
			oc image mirror --keep-manifest-list=$1 $2 $3
			`,
			"",
			manifestListMode, fromImage, toImage,
		}

		// Add standard annotations using release object directly (consistent with other job creation patterns)
		job.Annotations[releasecontroller.ReleaseAnnotationSource] = fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name)
		job.Annotations[releasecontroller.ReleaseAnnotationTarget] = fmt.Sprintf("%s/%s", release.Target.Namespace, release.Target.Name)
		job.Annotations[releasecontroller.ReleaseAnnotationGeneration] = strconv.FormatInt(release.Target.Generation, 10)
		job.Annotations[releasecontroller.ReleaseAnnotationReleaseTag] = tagName

		klog.V(2).Infof("Creating external registry mirror job %s/%s for %s to %s", c.jobNamespace, job.Name, tagName, toImage)
		return job, nil
	})
	return err
}

// sanitizeRegistryForJobName converts a registry URL to a job-name-safe string
func sanitizeRegistryForJobName(registry string) string {
	// Replace invalid characters with dashes and truncate if needed
	result := strings.ReplaceAll(registry, ".", "-")
	result = strings.ReplaceAll(result, "/", "-")
	result = strings.ReplaceAll(result, ":", "-")

	return result
}
