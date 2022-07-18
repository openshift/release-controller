package main

import (
	"context"
	"fmt"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sort"
)

func (c *Controller) ensureReleasePayload(release *releasecontroller.Release, tagName string) (*v1alpha1.ReleasePayload, error) {
	payload, err := c.releasePayloadClient.ReleasePayloads(release.Target.Namespace).Create(context.TODO(), newReleasePayload(release, tagName, c.jobNamespace, c.prowNamespace), metav1.CreateOptions{})
	if err == nil {
		klog.V(4).Infof("ReleasePayload: %s/%s created", payload.Namespace, payload.Name)
		return payload, nil
	}
	if errors.IsAlreadyExists(err) {
		return c.releasePayloadClient.ReleasePayloads(release.Target.Namespace).Get(context.TODO(), tagName, metav1.GetOptions{})
	}
	return nil, err
}

func newReleasePayload(release *releasecontroller.Release, name, jobNamespace, prowNamespace string) *v1alpha1.ReleasePayload {
	payload := v1alpha1.ReleasePayload{
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
				BlockingJobs:  []v1alpha1.CIConfiguration{},
				InformingJobs: []v1alpha1.CIConfiguration{},
			},
		},
	}

	// Sort the ReleaseVerification items into a consistent order
	keys := make([]string, 0, len(release.Config.Verify))
	for k := range release.Config.Verify {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, verifyName := range keys {
		definition := release.Config.Verify[verifyName]
		if definition.Disabled {
			continue
		}
		ciConfig := v1alpha1.CIConfiguration{
			CIConfigurationName:    verifyName,
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
			ciConfig.CIConfigurationJobName = fmt.Sprintf("%s-%s", verifyName, blockingJobName)
			payload.Spec.PayloadVerificationConfig.BlockingJobs = append(payload.Spec.PayloadVerificationConfig.BlockingJobs, ciConfig)

			// Adding the Informing Job
			informingJob := v1alpha1.CIConfiguration{
				CIConfigurationName:    verifyName,
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
	return &payload
}
