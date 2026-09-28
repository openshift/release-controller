package main

import (
	"context"
	"fmt"
	"sort"

	imagev1 "github.com/openshift/api/image/v1"
	imagereference "github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
)

func (c *Controller) ensureReleasePayload(release *releasecontroller.Release, releaseTag *imagev1.TagReference) (*v1alpha1.ReleasePayload, error) {
	verificationJobs, err := releasecontroller.GetVerificationJobs(c.parsedReleaseConfigCache, c.eventRecorder, c.releaseLister, release, releaseTag, c.artSuffix)
	if err != nil {
		return nil, err
	}
	payloadType := v1alpha1.PayloadTypeLocal
	if releasecontroller.IsReferenceReleaseTag(release, releaseTag) {
		payloadType = v1alpha1.PayloadTypeReference
	}
	payload, err := c.releasePayloadClient.ReleasePayloads(release.Target.Namespace).Create(context.TODO(), newReleasePayload(release, releaseTag, releaseTag.Name, c.jobNamespace, c.prowNamespace, verificationJobs, release.Config.Upgrade, v1alpha1.PayloadVerificationDataSourceBuildFarm, payloadType), metav1.CreateOptions{})
	if err == nil {
		klog.V(4).Infof("ReleasePayload: %s/%s created", payload.Namespace, payload.Name)
		return payload, nil
	}
	if errors.IsAlreadyExists(err) {
		payload, err := c.releasePayloadClient.ReleasePayloads(release.Target.Namespace).Get(context.TODO(), releaseTag.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return c.reconcileLayeredReleasePayloadCoordinates(release, releaseTag, payload)
	}
	return nil, err
}

// reconcileLayeredReleasePayloadCoordinates corrects coordinates written by
// the old reference-release path for Layered payloads. Those rc_payload__ tags
// were never created for Layered releases, which use the tag's DockerImage
// directly. Other coordinates and all other spec and status fields may be
// user-managed and are left untouched.
func (c *Controller) reconcileLayeredReleasePayloadCoordinates(release *releasecontroller.Release, releaseTag *imagev1.TagReference, payload *v1alpha1.ReleasePayload) (*v1alpha1.ReleasePayload, error) {
	if !canReconcileLayeredReleasePayloadCoordinates(release, releaseTag, payload) {
		return payload, nil
	}
	legacyRepository := release.Config.ReferenceRelease.PullRepository
	desired, ok := releaseCoordinatesFromPullSpec(releasecontroller.ReleasePullSpec(release, releaseTag))
	if !ok || isSyntheticLayeredCoordinate(desired, payload.Name, legacyRepository) {
		return payload, nil
	}
	if _, changed := correctedLayeredReleaseCoordinates(payload.Spec.ReleaseCoordinates, payload.Name, legacyRepository, desired); !changed {
		return payload, nil
	}

	client := c.releasePayloadClient.ReleasePayloads(release.Target.Namespace)
	result := payload
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := client.Get(context.TODO(), payload.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if !canReconcileLayeredReleasePayloadCoordinates(release, releaseTag, current) {
			result = current
			return nil
		}
		coordinates, changed := correctedLayeredReleaseCoordinates(current.Spec.ReleaseCoordinates, current.Name, legacyRepository, desired)
		if !changed {
			result = current
			return nil
		}
		updated := current.DeepCopy()
		updated.Spec.ReleaseCoordinates = coordinates
		result, err = client.Update(context.TODO(), updated, metav1.UpdateOptions{})
		return err
	})
	return result, err
}

func canReconcileLayeredReleasePayloadCoordinates(release *releasecontroller.Release, releaseTag *imagev1.TagReference, payload *v1alpha1.ReleasePayload) bool {
	if release == nil || release.Config == nil || release.Config.As != releasecontroller.ReleaseConfigModeLayered || release.Target == nil || releaseTag == nil || payload == nil {
		return false
	}
	if !releasecontroller.IsReferenceReleaseTag(release, releaseTag) || release.Config.ReferenceRelease.PullRepository == "" {
		return false
	}
	if payload.Namespace != release.Target.Namespace || payload.Name != releaseTag.Name || payload.Spec.PayloadType != v1alpha1.PayloadTypeReference {
		return false
	}
	expectedOwner := v1alpha1.PayloadCoordinates{
		Namespace:          release.Target.Namespace,
		ImagestreamName:    release.Target.Name,
		ImagestreamTagName: releaseTag.Name,
		StreamName:         release.Config.Name,
	}
	return payload.Spec.PayloadCoordinates == expectedOwner
}

func correctedLayeredReleaseCoordinates(current []v1alpha1.ReleaseCoordinates, payloadName, legacyRepository string, desired v1alpha1.ReleaseCoordinates) ([]v1alpha1.ReleaseCoordinates, bool) {
	desiredPresent := false
	for _, coordinates := range current {
		if coordinates == desired {
			desiredPresent = true
			break
		}
	}

	corrected := make([]v1alpha1.ReleaseCoordinates, 0, len(current))
	changed := false
	desiredInserted := desiredPresent
	for _, coordinates := range current {
		if !isSyntheticLayeredCoordinate(coordinates, payloadName, legacyRepository) {
			corrected = append(corrected, coordinates)
			continue
		}
		changed = true
		if !desiredInserted {
			corrected = append(corrected, desired)
			desiredInserted = true
		}
	}
	return corrected, changed
}

func isSyntheticLayeredCoordinate(coordinates v1alpha1.ReleaseCoordinates, payloadName, legacyRepository string) bool {
	return coordinates.Digest == "" &&
		coordinates.Repository == legacyRepository &&
		coordinates.Tag == releasecontroller.ReferencePayloadTag(payloadName)
}

func newReleasePayload(release *releasecontroller.Release, tag *imagev1.TagReference, name, jobNamespace, prowNamespace string, verificationJobs map[string]releasecontroller.ReleaseVerification, upgradeJobs map[string]releasecontroller.UpgradeVerification, dataSource v1alpha1.PayloadVerificationDataSource, payloadType v1alpha1.PayloadType) *v1alpha1.ReleasePayload {
	payload := v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: release.Target.Namespace,
		},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
				ProwCoordinates: v1alpha1.ProwCoordinates{Namespace: prowNamespace},
			},
			PayloadCoordinates: v1alpha1.PayloadCoordinates{
				Namespace:          release.Target.Namespace,
				ImagestreamName:    release.Target.Name,
				ImagestreamTagName: name,
				StreamName:         release.Config.Name,
			},
			PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
				BlockingJobs:                  []v1alpha1.CIConfiguration{},
				InformingJobs:                 []v1alpha1.CIConfiguration{},
				UpgradeJobs:                   []v1alpha1.CIConfiguration{},
				PayloadVerificationDataSource: dataSource,
			},
			PayloadType: payloadType,
		},
	}

	// Only add ReleaseCreationCoordinates for releases that need payload creation jobs
	// Layered releases use pre-existing images and don't need creation jobs
	if release.Config.As != releasecontroller.ReleaseConfigModeLayered {
		payload.Spec.PayloadCreationConfig.ReleaseCreationCoordinates = v1alpha1.ReleaseCreationCoordinates{
			Namespace:              jobNamespace,
			ReleaseCreationJobName: name,
		}

		// We should only be populating the ReleaseMirrorCoordinates if/when they are actually defined...
		// And only for releases that have PayloadCreationConfig (not layered releases)
		if release.Config.AlternateImageRepository != "" && release.Config.AlternateImageRepositorySecretName != "" {
			payload.Spec.PayloadCreationConfig.ReleaseMirrorCoordinates = v1alpha1.ReleaseMirrorCoordinates{
				Namespace:            jobNamespace,
				ReleaseMirrorJobName: releaseMirrorJobName(name),
			}
		}
	}

	if release.Config.As == releasecontroller.ReleaseConfigModeLayered {
		if coordinates, ok := releaseCoordinatesFromPullSpec(releasecontroller.ReleasePullSpec(release, tag)); ok {
			payload.Spec.ReleaseCoordinates = []v1alpha1.ReleaseCoordinates{coordinates}
		}
	} else if releasecontroller.IsReferenceReleaseTag(release, tag) {
		payload.Spec.ReleaseCoordinates = []v1alpha1.ReleaseCoordinates{{
			Repository: release.Config.ReferenceRelease.PullRepository,
			Tag:        releasecontroller.ReferencePayloadTag(name),
		}}
	} else if len(release.Target.Status.PublicDockerImageRepository) > 0 {
		payload.Spec.ReleaseCoordinates = []v1alpha1.ReleaseCoordinates{{
			Repository: release.Target.Status.PublicDockerImageRepository,
			Tag:        name,
		}}
	}

	// Sort the ReleaseVerification items into a consistent order
	var sortedKeys []string
	for key := range verificationJobs {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	for _, verifyName := range sortedKeys {
		verificationJobDefinition := verificationJobs[verifyName]
		if verificationJobDefinition.Disabled {
			continue
		}

		ciConfig := v1alpha1.CIConfiguration{
			CIConfigurationName:    verifyName,
			CIConfigurationJobName: verificationJobDefinition.ProwJob.Name,
			MaxRetries:             verificationJobDefinition.MaxRetries,
			Qualifiers:             verificationJobDefinition.Qualifiers,
		}

		switch {
		case verificationJobDefinition.AggregatedProwJob != nil:
			// Every Aggregated Job will contain a Blocking "Aggregator" job and an Informing "Analysis" job
			// Adding the Blocking Job
			blockingJobName := defaultAggregateProwJobName
			if verificationJobDefinition.AggregatedProwJob.ProwJob != nil && len(verificationJobDefinition.AggregatedProwJob.ProwJob.Name) > 0 {
				blockingJobName = verificationJobDefinition.AggregatedProwJob.ProwJob.Name
			}
			ciConfig.CIConfigurationJobName = fmt.Sprintf("%s-%s", verifyName, blockingJobName)
			payload.Spec.PayloadVerificationConfig.BlockingJobs = append(payload.Spec.PayloadVerificationConfig.BlockingJobs, ciConfig)

			// Adding the Informing Job
			informingJob := v1alpha1.CIConfiguration{
				CIConfigurationName:    verifyName,
				CIConfigurationJobName: verificationJobDefinition.ProwJob.Name,
				AnalysisJobCount:       verificationJobDefinition.AggregatedProwJob.AnalysisJobCount,
				Qualifiers:             verificationJobDefinition.Qualifiers,
			}
			payload.Spec.PayloadVerificationConfig.InformingJobs = append(payload.Spec.PayloadVerificationConfig.InformingJobs, informingJob)
		default:
			if verificationJobDefinition.Optional {
				payload.Spec.PayloadVerificationConfig.InformingJobs = append(payload.Spec.PayloadVerificationConfig.InformingJobs, ciConfig)
			} else {
				payload.Spec.PayloadVerificationConfig.BlockingJobs = append(payload.Spec.PayloadVerificationConfig.BlockingJobs, ciConfig)
			}
		}
	}

	// Sort the UpgradeVerification items into a consistent order
	sortedKeys = nil
	for key := range upgradeJobs {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	for _, cloudPlatform := range sortedKeys {
		definition := upgradeJobs[cloudPlatform]
		if definition.Disabled {
			continue
		}
		ciConfig := v1alpha1.CIConfiguration{
			CIConfigurationName:    cloudPlatform,
			CIConfigurationJobName: definition.ProwJob.Name,
		}
		payload.Spec.PayloadVerificationConfig.UpgradeJobs = append(payload.Spec.PayloadVerificationConfig.UpgradeJobs, ciConfig)
	}
	return &payload
}

// releaseCoordinatesFromPullSpec converts a tagged or digest-pinned image
// reference into the structured form used by ReleasePayload consumers.
func releaseCoordinatesFromPullSpec(pullSpec string) (v1alpha1.ReleaseCoordinates, bool) {
	ref, err := imagereference.Parse(pullSpec)
	if err != nil || ref.Name == "" || (ref.Tag == "" && ref.ID == "") {
		return v1alpha1.ReleaseCoordinates{}, false
	}
	repository := ref.AsRepository().Exact()
	if repository == "" {
		return v1alpha1.ReleaseCoordinates{}, false
	}
	return v1alpha1.ReleaseCoordinates{
		Repository: repository,
		Tag:        ref.Tag,
		Digest:     ref.ID,
	}, true
}
