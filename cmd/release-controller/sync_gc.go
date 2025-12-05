package main

import (
	"context"
	"fmt"
	"time"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog"
)

// garbageCollectSync checks for unreferenced objects and deletes them. Because this can run
// concurrently with the main sync loop, we rely on generational markers on resources to
// know whether to delete the objects.
func (c *Controller) garbageCollectSync() error {
	defer func() {
		if err := recover(); err != nil {
			panic(err)
		}
	}()

	imageStreams, err := c.releaseLister.List(labels.Everything())
	if err != nil {
		return err
	}
	jobs, err := c.jobLister.List(labels.Everything())
	if err != nil {
		return err
	}
	mirrors, err := c.releaseLister.List(labels.Everything())
	if err != nil {
		return err
	}
	payloads, err := c.releasePayloadLister.List(labels.Everything())
	if err != nil {
		return err
	}

	// find all valid releases and targets
	active := sets.NewString()
	targets := make(map[string]int64)
	for _, imageStream := range imageStreams {
		if _, ok := imageStream.Annotations[releasecontroller.ReleaseAnnotationHasReleases]; ok {
			for _, tag := range imageStream.Spec.Tags {
				active.Insert(tag.Name)
			}
			targets[fmt.Sprintf("%s/%s", imageStream.Namespace, imageStream.Name)] = imageStream.Generation
			continue
		}

		value, ok := imageStream.Annotations[releasecontroller.ReleaseAnnotationConfig]
		if !ok {
			continue
		}
		config, err := releasecontroller.ParseReleaseConfig(value, c.parsedReleaseConfigCache)
		if err != nil {
			continue
		}
		if config.As == releasecontroller.ReleaseConfigModeStable {
			for _, tag := range imageStream.Spec.Tags {
				active.Insert(tag.Name)
			}
			targets[fmt.Sprintf("%s/%s", imageStream.Namespace, imageStream.Name)] = imageStream.Generation
		}
	}

	// all jobs created for a release that no longer exists should be deleted
	// Request the deletion of their underlying pods as well...
	policy := metav1.DeletePropagationBackground

	for _, job := range jobs {
		if active.Has(job.Annotations[releasecontroller.ReleaseAnnotationReleaseTag]) {
			continue
		}
		targetGeneration, ok := targets[job.Annotations[releasecontroller.ReleaseAnnotationTarget]]
		if !ok {
			continue
		}
		generation, ok := releasecontroller.ReleaseGenerationFromObject(job.Name, job.Annotations)
		if !ok {
			continue
		}
		if generation < targetGeneration {
			klog.V(2).Infof("Removing orphaned release job %s/%s", job.Namespace, job.Name)
			if err := c.jobClient.Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{PropagationPolicy: &policy}); err != nil && !errors.IsNotFound(err) {
				utilruntime.HandleError(fmt.Errorf("can't delete orphaned release job %s/%s: %v", job.Namespace, job.Name, err))
			}
			continue
		}
		if job.Status.CompletionTime != nil && job.Status.CompletionTime.Time.Before(time.Now().Add(-2*time.Hour)) {
			klog.V(2).Infof("Removing old completed release job %s/%s", job.Namespace, job.Name)
			if err := c.jobClient.Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{PropagationPolicy: &policy}); err != nil && !errors.IsNotFound(err) {
				utilruntime.HandleError(fmt.Errorf("can't delete old release job %s/%s: %v", job.Namespace, job.Name, err))
			}
			continue
		}
	}

	// all image mirrors created for a release that no longer exists should be deleted
	for _, mirror := range mirrors {
		if active.Has(mirror.Annotations[releasecontroller.ReleaseAnnotationReleaseTag]) {
			continue
		}
		targetGeneration, ok := targets[mirror.Annotations[releasecontroller.ReleaseAnnotationTarget]]
		if !ok {
			continue
		}
		generation, ok := releasecontroller.ReleaseGenerationFromObject(mirror.Name, mirror.Annotations)
		if !ok {
			continue
		}
		if generation < targetGeneration {
			klog.V(2).Infof("Removing orphaned release mirror %s/%s", mirror.Namespace, mirror.Name)
			if err := c.imageClient.ImageStreams(mirror.Namespace).Delete(context.TODO(), mirror.Name, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
				utilruntime.HandleError(fmt.Errorf("can't delete orphaned release mirror %s/%s: %v", mirror.Namespace, mirror.Name, err))
			}
		}
	}

	// all releasepayloads created for releases that no longer exist should be garbage collected
	for _, payload := range payloads {
		if active.Has(payload.Name) {
			continue
		}

		// Get the release config from the ImageStream to check if alternate repository is configured
		imageStream, err := c.releaseLister.ImageStreams(payload.Spec.PayloadCoordinates.Namespace).Get(payload.Spec.PayloadCoordinates.ImagestreamName)
		if err == nil {
			release, ok, err := releasecontroller.ReleaseDefinition(imageStream, c.parsedReleaseConfigCache, c.eventRecorder, *c.releaseLister)
			if err == nil && ok && len(release.Config.AlternateImageRepository) > 0 && len(release.Config.AlternateImageRepositorySecretName) > 0 {
				_, err := c.ensureRemoveTagJob(payload, release)
				if err != nil {
					klog.V(2).Infof("Failed to create remove tag job for releasepayload %s/%s: %v, proceeding with direct deletion", payload.Namespace, payload.Name, err)
				} else {
					klog.V(2).Infof("Created remove tag job for orphaned releasepayload %s/%s, pruner will handle quay.io tag deletion", payload.Namespace, payload.Name)
				}
			}
		}

		// Delete the ReleasePayload
		klog.V(2).Infof("Removing orphaned releasepayload %s/%s", payload.Namespace, payload.Name)
		if err := c.releasePayloadClient.ReleasePayloads(payload.Namespace).Delete(context.TODO(), payload.Name, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("can't delete orphaned releasepayload %s/%s: %v", payload.Namespace, payload.Name, err))
		}
	}
	return nil
}
