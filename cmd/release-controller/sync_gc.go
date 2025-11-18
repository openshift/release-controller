package main

import (
	"context"
	"fmt"
	"time"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog"

	imagev1 "github.com/openshift/api/image/v1"
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
	// create a tag in quay.io with remove__ prefix - the pruner will handle deletion of both tags
	for _, payload := range payloads {
		if active.Has(payload.Name) {
			continue
		}
		// Get the target ImageStream from the ReleasePayload coordinates
		targetNamespace := payload.Spec.PayloadCoordinates.Namespace
		targetImageStreamName := payload.Spec.PayloadCoordinates.ImagestreamName
		if len(targetNamespace) == 0 || len(targetImageStreamName) == 0 {
			utilruntime.HandleError(fmt.Errorf("releasepayload %s/%s missing payload coordinates", payload.Namespace, payload.Name))
			continue
		}
		// Get the target ImageStream
		targetImageStream, err := c.imageClient.ImageStreams(targetNamespace).Get(context.TODO(), targetImageStreamName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			utilruntime.HandleError(fmt.Errorf("can't get target imagestream %s/%s for releasepayload %s/%s: %v", targetNamespace, targetImageStreamName, payload.Namespace, payload.Name, err))
			continue
		}
		// Create a tag with remove__ prefix pointing to the original tag
		removeTagName := fmt.Sprintf("remove__%s", payload.Name)
		// Check if the remove__ tag already exists
		if existingTag := releasecontroller.FindTagReference(targetImageStream, removeTagName); existingTag != nil {
			// Tag already exists, just delete the ReleasePayload
			klog.V(2).Infof("Removing orphaned releasepayload %s/%s (remove__ tag already exists)", payload.Namespace, payload.Name)
			if err := c.releasePayloadClient.ReleasePayloads(payload.Namespace).Delete(context.TODO(), payload.Name, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
				utilruntime.HandleError(fmt.Errorf("can't delete orphaned releasepayload %s/%s: %v", payload.Namespace, payload.Name, err))
			}
			continue
		}
		// Create the tag in the target ImageStream
		target := targetImageStream.DeepCopy()
		target.Spec.Tags = append(target.Spec.Tags, imagev1.TagReference{
			Name: removeTagName,
			From: &corev1.ObjectReference{
				Kind: "ImageStreamTag",
				Name: payload.Name,
			},
			ImportPolicy: imagev1.TagImportPolicy{ImportMode: imagev1.ImportModePreserveOriginal},
		})
		klog.V(2).Infof("Garbage collecting releasepayload %s/%s by creating tag %s/%s:%s", payload.Namespace, payload.Name, targetNamespace, targetImageStreamName, removeTagName)
		_, err = c.imageClient.ImageStreams(target.Namespace).Update(context.TODO(), target, metav1.UpdateOptions{})
		if err != nil && !errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("can't create tag %s/%s:%s for garbage collection: %v", targetNamespace, targetImageStreamName, removeTagName, err))
			continue
		}
		klog.V(2).Infof("Removing orphaned releasepayload %s/%s", payload.Namespace, payload.Name)
		if err := c.releasePayloadClient.ReleasePayloads(payload.Namespace).Delete(context.TODO(), payload.Name, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("can't delete orphaned releasepayload %s/%s: %v", payload.Namespace, payload.Name, err))
		}
	}
	return nil
}
