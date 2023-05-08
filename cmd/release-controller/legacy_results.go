package main

import (
	"context"
	"fmt"
	imagev1 "github.com/openshift/api/image/v1"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"time"
)

func (c *Controller) processLegacyResults(interval time.Duration, stopCh <-chan struct{}) {
	wait.Until(func() {
		imageStreams, err := c.releaseLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("Unable to list imagestreams: %v", err)
			return
		}
		for _, stream := range imageStreams {
			if _, ok := stream.Annotations[releasecontroller.ReleaseAnnotationConfig]; ok {
				release, err := c.loadReleaseForSync(stream.Namespace, stream.Name)
				if err != nil || release == nil {
					klog.Warningf("could not load release for sync for %s/%s: %v", stream.Namespace, stream.Name, err)
					continue
				}
				if release.Config.As != releasecontroller.ReleaseConfigModeStable {
					klog.V(4).Infof("Skipping non-stable imagestream: %s/%s", release.Source.Namespace, release.Source.Name)
					continue
				}
				klog.Infof("Adding release imagestream: %s/%s", stream.Namespace, stream.Name)
				c.addLegacyResultsQueueKey(queueKey{
					namespace: stream.Namespace,
					name:      stream.Name,
				})
			}
		}
	}, interval, stopCh)
}

func (c *Controller) syncLegacyResults(key queueKey) error {
	defer func() {
		if err := recover(); err != nil {
			panic(err)
		}
	}()

	release, err := c.loadReleaseForSync(key.namespace, key.name)
	if err != nil || release == nil {
		klog.V(6).Infof("could not load release for sync for %s/%s: %v", key.namespace, key.name, err)
		return err
	}

	if release.Config.As != releasecontroller.ReleaseConfigModeStable {
		klog.V(4).Infof("Skipping non-stable imagestream: %s/%s", release.Source.Namespace, release.Source.Name)
		return nil
	}

	imagestream := release.Source

	for j := range imagestream.Spec.Tags {
		tag := &imagestream.Spec.Tags[j]

		if tag.From != nil && tag.From.Kind == "ImageStreamTag" {
			klog.V(4).Infof("Skipping imageStreamTag: %s", tag.Name)
			continue
		}

		// Lookup the ReleasePayload resource.  If it exists, then move on
		_, err := c.releasePayloadLister.ReleasePayloads(key.namespace).Get(tag.Name)
		if err == nil {
			klog.V(4).Infof("found releasepayload: %s/%s", key.namespace, tag.Name)
			continue
		}
		// If there is any error other than NotFound, there is a problem
		if err != nil && !errors.IsNotFound(err) {
			return err
		}

		klog.V(4).Infof("processing tag: %s/%s:%s", imagestream.Namespace, imagestream.Name, tag.Name)

		// Get the Verification Job definitions
		verificationJobs, err := releasecontroller.GetVerificationJobs(c.parsedReleaseConfigCache, c.eventRecorder, c.releaseLister, release, tag, c.artSuffix)
		if err != nil {
			return err
		}

		// Ensure the existing state is preserved.  This is a big hammer, but it's the only way we have to guarantee that
		// the ReleasePayload's status matches the status of the ImageStream's Annotation.
		releasePayload := newReleasePayload(release, tag.Name, c.jobNamespace, c.prowNamespace, verificationJobs, release.Config.Upgrade, v1alpha1.PayloadVerificationDataSourceImageStream)
		setPayloadOverride(tag, releasePayload)

		// Create the payload
		payload, err := c.releasePayloadClient.ReleasePayloads(release.Target.Namespace).Create(context.TODO(), releasePayload, metav1.CreateOptions{})
		switch {
		case err == nil:
			klog.V(4).Infof("ReleasePayload: %s/%s created", payload.Namespace, payload.Name)
			continue
		case errors.IsAlreadyExists(err):
			klog.Errorf("Unable to create ReleasePayload because it already exists")
			continue
		default:
			klog.Errorf("unable to create ReleasePayload: %v", err)
			return err
		}
	}
	return nil
}

func setPayloadOverride(tag *imagev1.TagReference, releasePayload *v1alpha1.ReleasePayload) {
	var legacyOverride v1alpha1.ReleasePayloadOverrideType
	phase, ok := tag.Annotations[releasecontroller.ReleaseAnnotationPhase]
	reason, _ := tag.Annotations[releasecontroller.ReleaseAnnotationReason]
	message, _ := tag.Annotations[releasecontroller.ReleaseAnnotationMessage]
	if ok {
		switch phase {
		case releasecontroller.ReleasePhaseAccepted:
			legacyOverride = v1alpha1.ReleasePayloadOverrideAccepted
		case releasecontroller.ReleasePhaseRejected, releasecontroller.ReleasePhaseFailed:
			legacyOverride = v1alpha1.ReleasePayloadOverrideRejected
		default:
			return
		}
		releasePayload.Spec.PayloadOverride = v1alpha1.ReleasePayloadOverride{
			Override: legacyOverride,
			Reason:   fmt.Sprintf("LegacyResult(reason=%q,message=%q)", reason, message),
		}
	}
}
