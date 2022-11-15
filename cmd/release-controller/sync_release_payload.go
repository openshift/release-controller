package main

import (
	"context"
	"fmt"
	imagev1 "github.com/openshift/api/image/v1"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"github.com/openshift/release-controller/pkg/releasepayload/v1alpha1helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	"sort"
)

func (c *Controller) ensureReleasePayload(release *releasecontroller.Release, releaseTag *imagev1.TagReference) (*v1alpha1.ReleasePayload, error) {
	verificationJobs, err := releasecontroller.GetVerificationJobs(c.parsedReleaseConfigCache, c.eventRecorder, c.releaseLister, release, releaseTag, c.artSuffix)
	if err != nil {
		return nil, err
	}
	payload, err := c.releasePayloadClient.ReleasePayloads(release.Target.Namespace).Create(context.TODO(), newReleasePayload(release, releaseTag.Name, c.jobNamespace, c.prowNamespace, verificationJobs, release.Config.Upgrade, v1alpha1.PayloadVerificationDataSourceBuildFarm), metav1.CreateOptions{})
	if err == nil {
		klog.V(4).Infof("ReleasePayload: %s/%s created", payload.Namespace, payload.Name)
		return payload, nil
	}
	if errors.IsAlreadyExists(err) {
		return c.releasePayloadClient.ReleasePayloads(release.Target.Namespace).Get(context.TODO(), releaseTag.Name, metav1.GetOptions{})
	}
	return nil, err
}

func newReleasePayload(release *releasecontroller.Release, name, jobNamespace, prowNamespace string, verificationJobs map[string]releasecontroller.ReleaseVerification, upgradeJobs map[string]releasecontroller.UpgradeVerification, dataSource v1alpha1.PayloadVerificationDataSource) *v1alpha1.ReleasePayload {
	payload := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: release.Target.Namespace,
		},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadCoordinates: v1alpha1.PayloadCoordinates{
				Namespace:          release.Target.Namespace,
				ImagestreamName:    release.Target.Name,
				ImagestreamTagName: name,
			},
			PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
				ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
					Namespace:              jobNamespace,
					ReleaseCreationJobName: name,
				},
				ProwCoordinates: v1alpha1.ProwCoordinates{Namespace: prowNamespace},
			},
			PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
				BlockingJobs:                  []v1alpha1.CIConfiguration{},
				InformingJobs:                 []v1alpha1.CIConfiguration{},
				UpgradeJobs:                   []v1alpha1.CIConfiguration{},
				PayloadVerificationDataSource: dataSource,
			},
		},
	}

	AddVerificationJobs(payload, verificationJobs)
	AddUpgradeJobs(payload, upgradeJobs)

	return payload
}

func AddVerificationJobs(payload *v1alpha1.ReleasePayload, verificationJobs map[string]releasecontroller.ReleaseVerification) {
	// Sort the ReleaseVerification items into a consistent order
	var sortedKeys []string
	for key := range verificationJobs {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	for _, verifyName := range sortedKeys {
		definition := verificationJobs[verifyName]
		addPayloadVerificationConfig(payload, verifyName, definition)
	}

	// Ensure consistent ordering
	sort.Sort(v1alpha1helpers.ByCIConfigurationCIConfigurationName(payload.Spec.PayloadVerificationConfig.BlockingJobs))
	sort.Sort(v1alpha1helpers.ByCIConfigurationCIConfigurationName(payload.Spec.PayloadVerificationConfig.InformingJobs))
}

func addPayloadVerificationConfig(payload *v1alpha1.ReleasePayload, name string, definition releasecontroller.ReleaseVerification) {
	if definition.Disabled {
		return
	}
	ciConfig := v1alpha1.CIConfiguration{
		CIConfigurationName:    name,
		CIConfigurationJobName: definition.ProwJob.Name,
		MaxRetries:             definition.MaxRetries,
	}

	switch {
	case definition.AggregatedProwJob != nil:
		// Every Aggregated Job will contain a Blocking "Aggregator" job and an Informing "Analysis" job
		// Adding the Blocking Job
		blockingJobName := defaultAggregateProwJobName
		if definition.AggregatedProwJob.ProwJob != nil && len(definition.AggregatedProwJob.ProwJob.Name) > 0 {
			blockingJobName = definition.AggregatedProwJob.ProwJob.Name
		}
		ciConfig.CIConfigurationJobName = fmt.Sprintf("%s-%s", name, blockingJobName)
		payload.Spec.PayloadVerificationConfig.BlockingJobs = append(payload.Spec.PayloadVerificationConfig.BlockingJobs, ciConfig)

		// Adding the Informing Job
		informingJob := v1alpha1.CIConfiguration{
			CIConfigurationName:    name,
			CIConfigurationJobName: definition.ProwJob.Name,
			AnalysisJobCount:       definition.AggregatedProwJob.AnalysisJobCount,
		}
		payload.Spec.PayloadVerificationConfig.InformingJobs = append(payload.Spec.PayloadVerificationConfig.InformingJobs, informingJob)
	default:
		if definition.Optional {
			payload.Spec.PayloadVerificationConfig.InformingJobs = append(payload.Spec.PayloadVerificationConfig.InformingJobs, ciConfig)
		} else {
			payload.Spec.PayloadVerificationConfig.BlockingJobs = append(payload.Spec.PayloadVerificationConfig.BlockingJobs, ciConfig)
		}
	}
}

func AddUpgradeJobs(payload *v1alpha1.ReleasePayload, upgradeJobs map[string]releasecontroller.UpgradeVerification) {
	// Sort the ReleaseVerification items into a consistent order
	var sortedKeys []string
	for key, _ := range upgradeJobs {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	for _, cloudPlatform := range sortedKeys {
		definition := upgradeJobs[cloudPlatform]
		addPayloadUpgradeConfig(payload, cloudPlatform, definition)
	}

	// Ensure consistent ordering
	sort.Sort(v1alpha1helpers.ByCIConfigurationCIConfigurationName(payload.Spec.PayloadVerificationConfig.UpgradeJobs))
}

func addPayloadUpgradeConfig(payload *v1alpha1.ReleasePayload, cloudPlatform string, definition releasecontroller.UpgradeVerification) {
	if definition.Disabled {
		return
	}
	ciConfig := v1alpha1.CIConfiguration{
		CIConfigurationName:    cloudPlatform,
		CIConfigurationJobName: definition.ProwJob.Name,
	}
	payload.Spec.PayloadVerificationConfig.UpgradeJobs = append(payload.Spec.PayloadVerificationConfig.UpgradeJobs, ciConfig)
}
