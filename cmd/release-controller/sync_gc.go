package main

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
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
		err := recover()
		panic(err)
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

	// find all valid releases and targets
	active := sets.NewString()
	targets := make(map[string]int64)
	for _, imageStream := range imageStreams {
		if _, ok := imageStream.Annotations[releaseAnnotationHasReleases]; ok {
			for _, tag := range imageStream.Spec.Tags {
				active.Insert(tag.Name)
			}
			targets[fmt.Sprintf("%s/%s", imageStream.Namespace, imageStream.Name)] = imageStream.Generation
			continue
		}

		value, ok := imageStream.Annotations[releaseAnnotationConfig]
		if !ok {
			continue
		}
		config, err := parseReleaseConfig(value, c.parsedReleaseConfigCache)
		if err != nil {
			continue
		}
		if config.As == releaseConfigModeStable {
			for _, tag := range imageStream.Spec.Tags {
				active.Insert(tag.Name)
			}
			targets[fmt.Sprintf("%s/%s", imageStream.Namespace, imageStream.Name)] = imageStream.Generation
		}
	}

	// all jobs created for a release that no longer exists should be deleted
	for _, job := range jobs {
		if active.Has(job.Annotations[releaseAnnotationReleaseTag]) {
			continue
		}
		targetGeneration, ok := targets[job.Annotations[releaseAnnotationTarget]]
		if !ok {
			continue
		}
		generation, ok := releaseGenerationFromObject(job.Name, job.Annotations)
		if !ok {
			continue
		}
		if generation < targetGeneration {
			klog.V(2).Infof("Removing orphaned release job %s", job.Name)
			if err := c.jobClient.Jobs(job.Namespace).Delete(job.Name, nil); err != nil && !errors.IsNotFound(err) {
				utilruntime.HandleError(fmt.Errorf("can't delete orphaned release job %s: %v", job.Name, err))
			}
			continue
		}
		if job.Status.CompletionTime != nil && job.Status.CompletionTime.Time.Before(time.Now().Add(-2*time.Hour)) {
			klog.V(2).Infof("Removing old completed release job %s", job.Name)
			if err := c.jobClient.Jobs(job.Namespace).Delete(job.Name, nil); err != nil && !errors.IsNotFound(err) {
				utilruntime.HandleError(fmt.Errorf("can't delete old release job %s: %v", job.Name, err))
			}
			continue
		}
	}

	// all image mirrors created for a release that no longer exists should be deleted
	for _, mirror := range mirrors {
		if active.Has(mirror.Annotations[releaseAnnotationReleaseTag]) {
			continue
		}
		targetGeneration, ok := targets[mirror.Annotations[releaseAnnotationTarget]]
		if !ok {
			continue
		}
		generation, ok := releaseGenerationFromObject(mirror.Name, mirror.Annotations)
		if !ok {
			continue
		}
		if generation < targetGeneration {
			klog.V(2).Infof("Removing orphaned release mirror %s", mirror.Name)
			if err := c.imageClient.ImageStreams(mirror.Namespace).Delete(mirror.Name, nil); err != nil && !errors.IsNotFound(err) {
				utilruntime.HandleError(fmt.Errorf("can't delete orphaned release mirror %s: %v", mirror.Name, err))
			}
		}
	}
	return nil
}
