package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	imagev1 "github.com/openshift/api/image/v1"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasefake "github.com/openshift/release-controller/pkg/client/clientset/versioned/fake"
	releaselisters "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"github.com/openshift/release-controller/pkg/releasequalifiers"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
)

var (
	release = &releasecontroller.Release{
		Config: &releasecontroller.ReleaseConfig{
			Name: "InboundImageStreamName",
		},
		Target: &imagev1.ImageStream{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "release",
				Namespace: "ocp",
			},
		},
	}
)

func TestNewReleasePayload(t *testing.T) {
	testCases := []struct {
		name             string
		release          *releasecontroller.Release
		releaseTag       *imagev1.TagReference
		payloadName      string
		jobNamespace     string
		prowNamespace    string
		verificationJobs map[string]releasecontroller.ReleaseVerification
		upgradeJobs      map[string]releasecontroller.UpgradeVerification
		dataSource       v1alpha1.PayloadVerificationDataSource
		payloadType      v1alpha1.PayloadType
		expected         *v1alpha1.ReleasePayload
	}{
		{
			name:          "DisabledJob",
			release:       release,
			payloadName:   "4.11.0-0.nightly-2022-03-11-113341",
			jobNamespace:  "ci-release",
			prowNamespace: "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{
				"disabled-job": {
					Disabled: true,
				},
			},
			upgradeJobs: map[string]releasecontroller.UpgradeVerification{},
			dataSource:  v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType: v1alpha1.PayloadTypeLocal,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-03-11-113341",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.11.0-0.nightly-2022-03-11-113341",
						StreamName:         "InboundImageStreamName",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-03-11-113341",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:                  []v1alpha1.CIConfiguration{},
						InformingJobs:                 []v1alpha1.CIConfiguration{},
						UpgradeJobs:                   []v1alpha1.CIConfiguration{},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			},
		},
		{
			name:          "BlockingJob",
			release:       release,
			payloadName:   "4.11.0-0.nightly-2022-03-11-113341",
			jobNamespace:  "ci-release",
			prowNamespace: "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{
				"blocking-job": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
					},
				},
			},
			upgradeJobs: map[string]releasecontroller.UpgradeVerification{},
			dataSource:  v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType: v1alpha1.PayloadTypeLocal,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-03-11-113341",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.11.0-0.nightly-2022-03-11-113341",
						StreamName:         "InboundImageStreamName",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-03-11-113341",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "blocking-job",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
							},
						},
						InformingJobs:                 []v1alpha1.CIConfiguration{},
						UpgradeJobs:                   []v1alpha1.CIConfiguration{},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			},
		},
		{
			name:          "LegacyBlockingJob",
			release:       release,
			payloadName:   "4.11.0-0.nightly-2022-03-11-113341",
			jobNamespace:  "ci-release",
			prowNamespace: "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{
				"blocking-job": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
					},
				},
			},
			upgradeJobs: map[string]releasecontroller.UpgradeVerification{},
			dataSource:  v1alpha1.PayloadVerificationDataSourceImageStream,
			payloadType: v1alpha1.PayloadTypeLocal,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-03-11-113341",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.11.0-0.nightly-2022-03-11-113341",
						StreamName:         "InboundImageStreamName",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-03-11-113341",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "blocking-job",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
							},
						},
						InformingJobs:                 []v1alpha1.CIConfiguration{},
						UpgradeJobs:                   []v1alpha1.CIConfiguration{},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			},
		},
		{
			name:          "BlockingJobWithRetries",
			release:       release,
			payloadName:   "4.11.0-0.nightly-2022-03-11-113341",
			jobNamespace:  "ci-release",
			prowNamespace: "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{
				"blocking-job": {
					MaxRetries: 3,
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
					},
				},
			},
			upgradeJobs: map[string]releasecontroller.UpgradeVerification{},
			dataSource:  v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType: v1alpha1.PayloadTypeLocal,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-03-11-113341",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.11.0-0.nightly-2022-03-11-113341",
						StreamName:         "InboundImageStreamName",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-03-11-113341",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "blocking-job",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
								MaxRetries:             3,
							},
						},
						InformingJobs:                 []v1alpha1.CIConfiguration{},
						UpgradeJobs:                   []v1alpha1.CIConfiguration{},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			},
		},
		{
			name:          "InformingJob",
			release:       release,
			payloadName:   "4.11.0-0.nightly-2022-03-11-113341",
			jobNamespace:  "ci-release",
			prowNamespace: "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{
				"informing-job": {
					Optional: true,
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
					},
				},
			},
			upgradeJobs: map[string]releasecontroller.UpgradeVerification{},
			dataSource:  v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType: v1alpha1.PayloadTypeLocal,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-03-11-113341",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.11.0-0.nightly-2022-03-11-113341",
						StreamName:         "InboundImageStreamName",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-03-11-113341",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "informing-job",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
							},
						},
						UpgradeJobs:                   []v1alpha1.CIConfiguration{},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			},
		},
		{
			name:          "InformingJobWithRetries",
			release:       release,
			payloadName:   "4.11.0-0.nightly-2022-03-11-113341",
			jobNamespace:  "ci-release",
			prowNamespace: "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{
				"informing-job": {
					Optional:   true,
					MaxRetries: 3,
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
					},
				},
			},
			upgradeJobs: map[string]releasecontroller.UpgradeVerification{},
			dataSource:  v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType: v1alpha1.PayloadTypeLocal,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-03-11-113341",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.11.0-0.nightly-2022-03-11-113341",
						StreamName:         "InboundImageStreamName",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-03-11-113341",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "informing-job",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
								MaxRetries:             3,
							},
						},
						UpgradeJobs:                   []v1alpha1.CIConfiguration{},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			},
		},
		{
			name:             "UpgradeJob",
			release:          release,
			payloadName:      "4.12.11",
			jobNamespace:     "ci-release",
			prowNamespace:    "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{},
			upgradeJobs: map[string]releasecontroller.UpgradeVerification{
				"azure": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-azure-upgrade",
					},
				},
				"gcp": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-gcp-upgrade",
					},
				},
				"aws": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade",
					},
				},
			},
			dataSource:  v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType: v1alpha1.PayloadTypeLocal,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.11",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.11",
						StreamName:         "InboundImageStreamName",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.12.11",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aws",
								CIConfigurationJobName: "release-openshift-origin-installer-e2e-aws-upgrade",
							},
							{
								CIConfigurationName:    "azure",
								CIConfigurationJobName: "release-openshift-origin-installer-e2e-azure-upgrade",
							},
							{
								CIConfigurationName:    "gcp",
								CIConfigurationJobName: "release-openshift-origin-installer-e2e-gcp-upgrade",
							},
						},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			},
		},
		{
			name:             "DisabledUpgradeJob",
			release:          release,
			payloadName:      "4.12.11",
			jobNamespace:     "ci-release",
			prowNamespace:    "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{},
			upgradeJobs: map[string]releasecontroller.UpgradeVerification{
				"azure": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-azure-upgrade",
					},
				},
				"gcp": {
					Disabled: true,
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-gcp-upgrade",
					},
				},
				"aws": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade",
					},
				},
			},
			dataSource:  v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType: v1alpha1.PayloadTypeLocal,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.11",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.11",
						StreamName:         "InboundImageStreamName",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.12.11",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aws",
								CIConfigurationJobName: "release-openshift-origin-installer-e2e-aws-upgrade",
							},
							{
								CIConfigurationName:    "azure",
								CIConfigurationJobName: "release-openshift-origin-installer-e2e-azure-upgrade",
							},
						},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			},
		},
		{
			name:          "AggregatedJob",
			release:       release,
			payloadName:   "4.11.0-0.nightly-2022-03-11-113341",
			jobNamespace:  "ci-release",
			prowNamespace: "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{
				"aggregated-job": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-upgrade",
					},
					Upgrade: true,
					AggregatedProwJob: &releasecontroller.AggregatedProwJobVerification{
						AnalysisJobCount: 10,
					},
				},
			},
			upgradeJobs: map[string]releasecontroller.UpgradeVerification{},
			dataSource:  v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType: v1alpha1.PayloadTypeLocal,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-03-11-113341",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.11.0-0.nightly-2022-03-11-113341",
						StreamName:         "InboundImageStreamName",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-03-11-113341",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aggregated-job",
								CIConfigurationJobName: "aggregated-job-release-openshift-release-analysis-aggregator",
							},
						},
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aggregated-job",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-upgrade",
								AnalysisJobCount:       10,
							},
						},
						UpgradeJobs:                   []v1alpha1.CIConfiguration{},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			},
		},
		{
			name:          "AggregatedJobWithOverwrittenAggregatorJob",
			release:       release,
			payloadName:   "4.11.0-0.nightly-2022-03-11-113341",
			jobNamespace:  "ci-release",
			prowNamespace: "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{
				"aggregated-job": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-upgrade",
					},
					Upgrade: true,
					AggregatedProwJob: &releasecontroller.AggregatedProwJobVerification{
						ProwJob: &releasecontroller.ProwJobVerification{
							Name: "overwritten-prowjob-definition",
						},
						AnalysisJobCount: 10,
					},
				},
			},
			upgradeJobs: map[string]releasecontroller.UpgradeVerification{},
			dataSource:  v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType: v1alpha1.PayloadTypeLocal,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-03-11-113341",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.11.0-0.nightly-2022-03-11-113341",
						StreamName:         "InboundImageStreamName",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-03-11-113341",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aggregated-job",
								CIConfigurationJobName: "aggregated-job-overwritten-prowjob-definition",
							},
						},
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aggregated-job",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-upgrade",
								AnalysisJobCount:       10,
							},
						},
						UpgradeJobs:                   []v1alpha1.CIConfiguration{},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			},
		},
		{
			name:          "BlockingJobWithQualifiers",
			release:       release,
			payloadName:   "4.11.0-0.nightly-2022-03-11-113341",
			jobNamespace:  "ci-release",
			prowNamespace: "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{
				"blocking-job-with-qualifiers": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
					},
					Qualifiers: releasequalifiers.ReleaseQualifiers{
						"qual-a": releasequalifiers.ReleaseQualifier{
							Enabled:   new(true),
							BadgeName: "QA",
							Summary:   "Qualifier A",
						},
					},
				},
			},
			upgradeJobs: map[string]releasecontroller.UpgradeVerification{},
			dataSource:  v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType: v1alpha1.PayloadTypeLocal,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-03-11-113341",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.11.0-0.nightly-2022-03-11-113341",
						StreamName:         "InboundImageStreamName",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-03-11-113341",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "blocking-job-with-qualifiers",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"qual-a": releasequalifiers.ReleaseQualifier{
										Enabled:   new(true),
										BadgeName: "QA",
										Summary:   "Qualifier A",
									},
								},
							},
						},
						InformingJobs:                 []v1alpha1.CIConfiguration{},
						UpgradeJobs:                   []v1alpha1.CIConfiguration{},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			},
		},
		{
			name:          "AggregatedJobWithQualifiers",
			release:       release,
			payloadName:   "4.11.0-0.nightly-2022-03-11-113341",
			jobNamespace:  "ci-release",
			prowNamespace: "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{
				"aggregated-job-with-qualifiers": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-upgrade",
					},
					Upgrade: true,
					AggregatedProwJob: &releasecontroller.AggregatedProwJobVerification{
						AnalysisJobCount: 10,
					},
					Qualifiers: releasequalifiers.ReleaseQualifiers{
						"qual-b": releasequalifiers.ReleaseQualifier{
							Enabled:   new(true),
							BadgeName: "QB",
							Summary:   "Qualifier B",
						},
					},
				},
			},
			upgradeJobs: map[string]releasecontroller.UpgradeVerification{},
			dataSource:  v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType: v1alpha1.PayloadTypeLocal,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-03-11-113341",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.11.0-0.nightly-2022-03-11-113341",
						StreamName:         "InboundImageStreamName",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-03-11-113341",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aggregated-job-with-qualifiers",
								CIConfigurationJobName: "aggregated-job-with-qualifiers-release-openshift-release-analysis-aggregator",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"qual-b": releasequalifiers.ReleaseQualifier{
										Enabled:   new(true),
										BadgeName: "QB",
										Summary:   "Qualifier B",
									},
								},
							},
						},
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aggregated-job-with-qualifiers",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-upgrade",
								AnalysisJobCount:       10,
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"qual-b": releasequalifiers.ReleaseQualifier{
										Enabled:   new(true),
										BadgeName: "QB",
										Summary:   "Qualifier B",
									},
								},
							},
						},
						UpgradeJobs:                   []v1alpha1.CIConfiguration{},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			},
		},
		{
			name: "RealWorldExample",
			release: &releasecontroller.Release{
				Config: &releasecontroller.ReleaseConfig{
					Name: "4.11-art-latest",
				},
				Target: &imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "release",
						Namespace: "ocp",
					},
				},
			},
			payloadName:   "4.11.0-0.nightly-2022-03-11-113341",
			jobNamespace:  "ci-release",
			prowNamespace: "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{
				"aggregated-azure-ovn-upgrade-4.12-micro": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-ci-4.12-e2e-azure-ovn-upgrade",
					},
					Upgrade: true,
					AggregatedProwJob: &releasecontroller.AggregatedProwJobVerification{
						AnalysisJobCount: 10,
					},
				},
				"aggregated-gcp-ovn-upgrade-4.12-minor": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-ci-4.12-upgrade-from-stable-4.11-e2e-gcp-ovn-upgrade",
					},
					Upgrade:     true,
					UpgradeFrom: releasecontroller.ReleaseUpgradeFromPreviousMinor,
					AggregatedProwJob: &releasecontroller.AggregatedProwJobVerification{
						AnalysisJobCount: 10,
					},
				},
				"alibaba": {
					Optional: true,
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-alibaba",
					},
				},
				"aws-sdn": {
					Optional:   true,
					MaxRetries: 3,
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn",
					},
				},
				"aws-single-node": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-single-node",
					},
				},
				"aws-sdn-serial": {
					MaxRetries: 3,
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
					},
				},
				"metal-ipi-upgrade": {
					Optional: true,
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-metal-ipi-upgrade",
					},
					Upgrade: true,
				},
				"metal-ipi-upgrade-minor": {
					Optional: true,
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-upgrade-from-stable-4.11-e2e-metal-ipi-upgrade",
					},
					Upgrade:     true,
					UpgradeFrom: releasecontroller.ReleaseUpgradeFromPreviousMinor,
				},
			},
			upgradeJobs: map[string]releasecontroller.UpgradeVerification{
				"azure": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-azure-upgrade",
					},
				},
				"gcp": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-gcp-upgrade",
					},
				},
				"aws": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade",
					},
				},
			},
			dataSource:  v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType: v1alpha1.PayloadTypeLocal,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-03-11-113341",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.11.0-0.nightly-2022-03-11-113341",
						StreamName:         "4.11-art-latest",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-03-11-113341",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aggregated-azure-ovn-upgrade-4.12-micro",
								CIConfigurationJobName: "aggregated-azure-ovn-upgrade-4.12-micro-release-openshift-release-analysis-aggregator",
							},
							{
								CIConfigurationName:    "aggregated-gcp-ovn-upgrade-4.12-minor",
								CIConfigurationJobName: "aggregated-gcp-ovn-upgrade-4.12-minor-release-openshift-release-analysis-aggregator",
							},
							{
								CIConfigurationName:    "aws-sdn-serial",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
								MaxRetries:             3,
							},
							{
								CIConfigurationName:    "aws-single-node",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-single-node",
							},
						},
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aggregated-azure-ovn-upgrade-4.12-micro",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.12-e2e-azure-ovn-upgrade",
								AnalysisJobCount:       10,
							},
							{
								CIConfigurationName:    "aggregated-gcp-ovn-upgrade-4.12-minor",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.12-upgrade-from-stable-4.11-e2e-gcp-ovn-upgrade",
								AnalysisJobCount:       10,
							},
							{
								CIConfigurationName:    "alibaba",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-alibaba",
							},
							{
								CIConfigurationName:    "aws-sdn",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn",
								MaxRetries:             3,
							},
							{
								CIConfigurationName:    "metal-ipi-upgrade",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-metal-ipi-upgrade",
							},
							{
								CIConfigurationName:    "metal-ipi-upgrade-minor",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-upgrade-from-stable-4.11-e2e-metal-ipi-upgrade",
							},
						},
						UpgradeJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aws",
								CIConfigurationJobName: "release-openshift-origin-installer-e2e-aws-upgrade",
							},
							{
								CIConfigurationName:    "azure",
								CIConfigurationJobName: "release-openshift-origin-installer-e2e-azure-upgrade",
							},
							{
								CIConfigurationName:    "gcp",
								CIConfigurationJobName: "release-openshift-origin-installer-e2e-gcp-upgrade",
							},
						},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			},
		},
		{
			name:          "ReferencePayloadType",
			release:       release,
			payloadName:   "4.11.0-0.nightly-2022-03-11-113341",
			jobNamespace:  "ci-release",
			prowNamespace: "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{
				"blocking-job": {
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
					},
				},
			},
			upgradeJobs: map[string]releasecontroller.UpgradeVerification{},
			dataSource:  v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType: v1alpha1.PayloadTypeReference,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-03-11-113341",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.11.0-0.nightly-2022-03-11-113341",
						StreamName:         "InboundImageStreamName",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-03-11-113341",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "blocking-job",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.12-e2e-aws-sdn-serial",
							},
						},
						InformingJobs:                 []v1alpha1.CIConfiguration{},
						UpgradeJobs:                   []v1alpha1.CIConfiguration{},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					PayloadType: v1alpha1.PayloadTypeReference,
				},
			},
		},
		{
			name: "ReleaseCoordinates populated for reference release",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli", Reference: true},
						},
					},
				},
				Target: &imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "release",
						Namespace: "ocp",
					},
					Status: imagev1.ImageStreamStatus{
						PublicDockerImageRepository: "registry.ci.openshift.org/ocp/release",
					},
				},
				Config: &releasecontroller.ReleaseConfig{
					ReferenceRelease: &releasecontroller.ReferenceRelease{
						PushRepository: "quay.io/openshift-release-dev/ocp-release",
						PullRepository: "quay.io/openshift-release-dev/ocp-release",
					},
				},
			},
			releaseTag:       &imagev1.TagReference{Name: "4.18.0-0.nightly-2025-01-01-000000", Reference: true},
			payloadName:      "4.18.0-0.nightly-2025-01-01-000000",
			jobNamespace:     "ci-release",
			prowNamespace:    "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{},
			upgradeJobs:      map[string]releasecontroller.UpgradeVerification{},
			dataSource:       v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType:      v1alpha1.PayloadTypeReference,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.18.0-0.nightly-2025-01-01-000000",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.18.0-0.nightly-2025-01-01-000000",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.18.0-0.nightly-2025-01-01-000000",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:                  []v1alpha1.CIConfiguration{},
						InformingJobs:                 []v1alpha1.CIConfiguration{},
						UpgradeJobs:                   []v1alpha1.CIConfiguration{},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					ReleaseCoordinates: []v1alpha1.ReleaseCoordinates{{
						Repository: "quay.io/openshift-release-dev/ocp-release",
						Tag:        "rc_payload__4.18.0-0.nightly-2025-01-01-000000",
					}},
					PayloadType: v1alpha1.PayloadTypeReference,
				},
			},
		},
		{
			name: "ReleaseCoordinates populated for local release",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli"},
						},
					},
				},
				Target: &imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "release",
						Namespace: "ocp",
					},
					Status: imagev1.ImageStreamStatus{
						PublicDockerImageRepository: "registry.ci.openshift.org/ocp/release",
					},
				},
				Config: &releasecontroller.ReleaseConfig{},
			},
			payloadName:      "4.12.0",
			jobNamespace:     "ci-release",
			prowNamespace:    "ci",
			verificationJobs: map[string]releasecontroller.ReleaseVerification{},
			upgradeJobs:      map[string]releasecontroller.UpgradeVerification{},
			dataSource:       v1alpha1.PayloadVerificationDataSourceBuildFarm,
			payloadType:      v1alpha1.PayloadTypeLocal,
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.12.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:                  []v1alpha1.CIConfiguration{},
						InformingJobs:                 []v1alpha1.CIConfiguration{},
						UpgradeJobs:                   []v1alpha1.CIConfiguration{},
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
					ReleaseCoordinates: []v1alpha1.ReleaseCoordinates{{
						Repository: "registry.ci.openshift.org/ocp/release",
						Tag:        "4.12.0",
					}},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payload := newReleasePayload(tc.release, tc.releaseTag, tc.payloadName, tc.jobNamespace, tc.prowNamespace, tc.verificationJobs, tc.upgradeJobs, tc.dataSource, tc.payloadType)
			if !reflect.DeepEqual(payload, tc.expected) {
				t.Errorf("%s: Expected %v, got %v", tc.name, tc.expected, payload)
			}
		})
	}
}

func TestNewLayeredReleasePayloadCoordinates(t *testing.T) {
	const digest = "sha256:e08883ade89b50664c14f2a9434018921a012c4506dde92e8779e482e025ea4c"
	tests := []struct {
		name      string
		tag       *imagev1.TagReference
		status    imagev1.ImageStreamStatus
		expected  v1alpha1.ReleaseCoordinates
		wantCoord bool
	}{
		{
			name: "reference digest source",
			tag: &imagev1.TagReference{
				Name:      "1.2.3",
				Reference: true,
				From: &corev1.ObjectReference{
					Kind: "DockerImage",
					Name: "quay.io/redhat-user-workloads/example/layered@" + digest,
				},
			},
			expected: v1alpha1.ReleaseCoordinates{
				Repository: "quay.io/redhat-user-workloads/example/layered",
				Digest:     digest,
			},
			wantCoord: true,
		},
		{
			name: "non-reference tagged source",
			tag: &imagev1.TagReference{
				Name: "1.2.3",
				From: &corev1.ObjectReference{
					Kind: "DockerImage",
					Name: "quay.io/example/layered:v1.2.3",
				},
			},
			expected: v1alpha1.ReleaseCoordinates{
				Repository: "quay.io/example/layered",
				Tag:        "v1.2.3",
			},
			wantCoord: true,
		},
		{
			name: "missing source",
			tag:  &imagev1.TagReference{Name: "1.2.3", Reference: true},
		},
		{
			name: "legacy imported source",
			tag:  &imagev1.TagReference{Name: "1.2.3"},
			status: imagev1.ImageStreamStatus{
				PublicDockerImageRepository: "registry.example.com/layered",
				Tags: []imagev1.NamedTagEventList{{
					Tag:   "1.2.3",
					Items: []imagev1.TagEvent{{DockerImageReference: "registry.internal/layered@sha256:legacy"}},
				}},
			},
			expected:  v1alpha1.ReleaseCoordinates{Repository: "registry.example.com/layered", Tag: "1.2.3"},
			wantCoord: true,
		},
		{
			name: "wrong source kind",
			tag: &imagev1.TagReference{
				Name:      "1.2.3",
				Reference: true,
				From:      &corev1.ObjectReference{Kind: "ImageStreamTag", Name: "layered:source"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := &releasecontroller.Release{
				Target: &imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Name: "layered", Namespace: "ocp"},
					Status:     tt.status,
				},
				Config: &releasecontroller.ReleaseConfig{
					Name: "layered",
					As:   releasecontroller.ReleaseConfigModeLayered,
					ReferenceRelease: &releasecontroller.ReferenceRelease{
						PullRepository: "quay-proxy.ci.openshift.org/openshift/ci",
					},
				},
			}
			payload := newReleasePayload(
				release,
				tt.tag,
				tt.tag.Name,
				"ci-release",
				"ci",
				map[string]releasecontroller.ReleaseVerification{},
				map[string]releasecontroller.UpgradeVerification{},
				v1alpha1.PayloadVerificationDataSourceBuildFarm,
				v1alpha1.PayloadTypeReference,
			)
			if !tt.wantCoord {
				if len(payload.Spec.ReleaseCoordinates) != 0 {
					t.Fatalf("expected no release coordinates, got %#v", payload.Spec.ReleaseCoordinates)
				}
				return
			}
			if len(payload.Spec.ReleaseCoordinates) != 1 || payload.Spec.ReleaseCoordinates[0] != tt.expected {
				t.Fatalf("expected coordinates %#v, got %#v", tt.expected, payload.Spec.ReleaseCoordinates)
			}
		})
	}
}

func TestReleaseCoordinatesFromPullSpec(t *testing.T) {
	const digest = "sha256:e08883ade89b50664c14f2a9434018921a012c4506dde92e8779e482e025ea4c"
	tests := []struct {
		name     string
		pullSpec string
		expected v1alpha1.ReleaseCoordinates
		ok       bool
	}{
		{
			name:     "digest",
			pullSpec: "quay.io/example/layered@" + digest,
			expected: v1alpha1.ReleaseCoordinates{Repository: "quay.io/example/layered", Digest: digest},
			ok:       true,
		},
		{
			name:     "tag",
			pullSpec: "quay.io/example/layered:v1.2.3",
			expected: v1alpha1.ReleaseCoordinates{Repository: "quay.io/example/layered", Tag: "v1.2.3"},
			ok:       true,
		},
		{name: "empty"},
		{name: "invalid", pullSpec: "not a pull spec"},
		{name: "unqualified repository", pullSpec: "quay.io/example/layered"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, ok := releaseCoordinatesFromPullSpec(tt.pullSpec)
			if ok != tt.ok || actual != tt.expected {
				t.Fatalf("releaseCoordinatesFromPullSpec(%q) = (%#v, %t), want (%#v, %t)", tt.pullSpec, actual, ok, tt.expected, tt.ok)
			}
		})
	}
}

func TestEnsureReleasePayloadReconcilesExistingLayeredCoordinates(t *testing.T) {
	const (
		payloadName = "4.20.0-0.layered-2026-09-28-120000"
		digest      = "sha256:e08883ade89b50664c14f2a9434018921a012c4506dde92e8779e482e025ea4c"
	)
	desired := v1alpha1.ReleaseCoordinates{Repository: "quay.io/example/layered", Digest: digest}
	owner := v1alpha1.PayloadCoordinates{
		Namespace:          "ocp",
		ImagestreamName:    "layered",
		ImagestreamTagName: payloadName,
		StreamName:         "layered",
	}
	synthetic := v1alpha1.ReleaseCoordinates{
		Repository: "quay-proxy.ci.openshift.org/openshift/ci",
		Tag:        releasecontroller.ReferencePayloadTag(payloadName),
	}
	humanCoordinate := v1alpha1.ReleaseCoordinates{Repository: "registry.example.com/archive", Tag: "approved"}

	tests := []struct {
		name            string
		mode            string
		referenceTag    bool
		owner           v1alpha1.PayloadCoordinates
		payloadType     v1alpha1.PayloadType
		desiredPullSpec string
		coordinates     []v1alpha1.ReleaseCoordinates
		wantCoordinates []v1alpha1.ReleaseCoordinates
		wantUpdates     int
	}{
		{
			name:            "stale synthetic coordinate is replaced without touching human coordinate",
			mode:            releasecontroller.ReleaseConfigModeLayered,
			referenceTag:    true,
			owner:           owner,
			payloadType:     v1alpha1.PayloadTypeReference,
			desiredPullSpec: desired.Repository + "@" + digest,
			coordinates:     []v1alpha1.ReleaseCoordinates{synthetic, humanCoordinate},
			wantCoordinates: []v1alpha1.ReleaseCoordinates{desired, humanCoordinate},
			wantUpdates:     1,
		},
		{
			name:            "stale synthetic coordinate is replaced in place when desired coordinate exists later",
			mode:            releasecontroller.ReleaseConfigModeLayered,
			referenceTag:    true,
			owner:           owner,
			payloadType:     v1alpha1.PayloadTypeReference,
			desiredPullSpec: desired.Repository + "@" + digest,
			coordinates:     []v1alpha1.ReleaseCoordinates{synthetic, humanCoordinate, desired},
			wantCoordinates: []v1alpha1.ReleaseCoordinates{desired, humanCoordinate},
			wantUpdates:     1,
		},
		{
			name:            "correct layered coordinate is unchanged",
			mode:            releasecontroller.ReleaseConfigModeLayered,
			referenceTag:    true,
			owner:           owner,
			payloadType:     v1alpha1.PayloadTypeReference,
			desiredPullSpec: desired.Repository + "@" + digest,
			coordinates:     []v1alpha1.ReleaseCoordinates{desired},
			wantCoordinates: []v1alpha1.ReleaseCoordinates{desired},
		},
		{
			name:            "non-layered synthetic coordinate is unchanged",
			mode:            releasecontroller.ReleaseConfigModeStable,
			referenceTag:    true,
			owner:           owner,
			payloadType:     v1alpha1.PayloadTypeReference,
			desiredPullSpec: desired.Repository + "@" + digest,
			coordinates:     []v1alpha1.ReleaseCoordinates{synthetic},
			wantCoordinates: []v1alpha1.ReleaseCoordinates{synthetic},
		},
		{
			name:            "synthetic-looking coordinate in an unrelated repository is unchanged",
			mode:            releasecontroller.ReleaseConfigModeLayered,
			referenceTag:    true,
			owner:           owner,
			payloadType:     v1alpha1.PayloadTypeReference,
			desiredPullSpec: desired.Repository + "@" + digest,
			coordinates: []v1alpha1.ReleaseCoordinates{{
				Repository: humanCoordinate.Repository,
				Tag:        releasecontroller.ReferencePayloadTag(payloadName),
			}},
			wantCoordinates: []v1alpha1.ReleaseCoordinates{{
				Repository: humanCoordinate.Repository,
				Tag:        releasecontroller.ReferencePayloadTag(payloadName),
			}},
		},
		{
			name:            "non-reference layered tag is unchanged",
			mode:            releasecontroller.ReleaseConfigModeLayered,
			owner:           owner,
			payloadType:     v1alpha1.PayloadTypeReference,
			desiredPullSpec: desired.Repository + "@" + digest,
			coordinates:     []v1alpha1.ReleaseCoordinates{synthetic},
			wantCoordinates: []v1alpha1.ReleaseCoordinates{synthetic},
		},
		{
			name:         "same-name payload owned by another stream is unchanged",
			mode:         releasecontroller.ReleaseConfigModeLayered,
			referenceTag: true,
			owner: v1alpha1.PayloadCoordinates{
				Namespace:          "ocp",
				ImagestreamName:    "another-layered-stream",
				ImagestreamTagName: payloadName,
				StreamName:         "another-layered-stream",
			},
			payloadType:     v1alpha1.PayloadTypeReference,
			desiredPullSpec: desired.Repository + "@" + digest,
			coordinates:     []v1alpha1.ReleaseCoordinates{synthetic},
			wantCoordinates: []v1alpha1.ReleaseCoordinates{synthetic},
		},
		{
			name:            "payload without reference provenance is unchanged",
			mode:            releasecontroller.ReleaseConfigModeLayered,
			referenceTag:    true,
			owner:           owner,
			payloadType:     v1alpha1.PayloadTypeLocal,
			desiredPullSpec: desired.Repository + "@" + digest,
			coordinates:     []v1alpha1.ReleaseCoordinates{synthetic},
			wantCoordinates: []v1alpha1.ReleaseCoordinates{synthetic},
		},
		{
			name:            "desired rc payload tag in another repository replaces only the legacy coordinate",
			mode:            releasecontroller.ReleaseConfigModeLayered,
			referenceTag:    true,
			owner:           owner,
			payloadType:     v1alpha1.PayloadTypeReference,
			desiredPullSpec: "quay.io/example/layered:" + releasecontroller.ReferencePayloadTag(payloadName),
			coordinates:     []v1alpha1.ReleaseCoordinates{synthetic, humanCoordinate},
			wantCoordinates: []v1alpha1.ReleaseCoordinates{{
				Repository: "quay.io/example/layered",
				Tag:        releasecontroller.ReferencePayloadTag(payloadName),
			}, humanCoordinate},
			wantUpdates: 1,
		},
		{
			name:            "desired source identical to legacy signature is unchanged",
			mode:            releasecontroller.ReleaseConfigModeLayered,
			referenceTag:    true,
			owner:           owner,
			payloadType:     v1alpha1.PayloadTypeReference,
			desiredPullSpec: synthetic.Repository + ":" + synthetic.Tag,
			coordinates:     []v1alpha1.ReleaseCoordinates{synthetic, humanCoordinate},
			wantCoordinates: []v1alpha1.ReleaseCoordinates{synthetic, humanCoordinate},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:        payloadName,
					Namespace:   "ocp",
					Labels:      map[string]string{"history": "preserve"},
					Annotations: map[string]string{"release.openshift.io/note": "preserve"},
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: tt.owner,
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideAccepted,
						Reason:   "manually approved",
					},
					ReleaseCoordinates: append([]v1alpha1.ReleaseCoordinates(nil), tt.coordinates...),
					PayloadType:        tt.payloadType,
				},
				Status: v1alpha1.ReleasePayloadStatus{Conditions: []metav1.Condition{{
					Type:   v1alpha1.ConditionPayloadAccepted,
					Status: metav1.ConditionTrue,
					Reason: "TestsPassed",
				}}},
			}
			client := releasefake.NewSimpleClientset(existing)
			controller := &Controller{releasePayloadClient: client.ReleaseV1alpha1()}
			release := &releasecontroller.Release{
				Config: &releasecontroller.ReleaseConfig{
					Name: "layered",
					As:   tt.mode,
					ReferenceRelease: &releasecontroller.ReferenceRelease{
						PullRepository: synthetic.Repository,
					},
				},
				Target: &imagev1.ImageStream{ObjectMeta: metav1.ObjectMeta{Name: "layered", Namespace: "ocp"}},
			}
			tag := &imagev1.TagReference{
				Name:      payloadName,
				Reference: tt.referenceTag,
				From:      &corev1.ObjectReference{Kind: "DockerImage", Name: tt.desiredPullSpec},
			}

			got, err := controller.ensureReleasePayload(release, tag)
			if err != nil {
				t.Fatalf("ensureReleasePayload returned an error: %v", err)
			}
			if !reflect.DeepEqual(got.Spec.ReleaseCoordinates, tt.wantCoordinates) {
				t.Fatalf("coordinates = %#v, want %#v", got.Spec.ReleaseCoordinates, tt.wantCoordinates)
			}
			if got.Spec.PayloadOverride != existing.Spec.PayloadOverride {
				t.Fatalf("payload override changed: got %#v, want %#v", got.Spec.PayloadOverride, existing.Spec.PayloadOverride)
			}
			if !reflect.DeepEqual(got.Status, existing.Status) {
				t.Fatalf("payload status changed: got %#v, want %#v", got.Status, existing.Status)
			}
			if !reflect.DeepEqual(got.Labels, existing.Labels) || !reflect.DeepEqual(got.Annotations, existing.Annotations) {
				t.Fatalf("payload metadata changed: got labels/annotations %#v/%#v, want %#v/%#v", got.Labels, got.Annotations, existing.Labels, existing.Annotations)
			}
			updates := 0
			for _, action := range client.Actions() {
				if action.GetVerb() == "update" {
					updates++
				}
			}
			if updates != tt.wantUpdates {
				t.Fatalf("update actions = %d, want %d; actions: %#v", updates, tt.wantUpdates, client.Actions())
			}
			stored, err := client.ReleaseV1alpha1().ReleasePayloads("ocp").Get(context.Background(), payloadName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get reconciled payload: %v", err)
			}
			if !reflect.DeepEqual(stored.Spec.ReleaseCoordinates, tt.wantCoordinates) {
				t.Fatalf("stored coordinates = %#v, want %#v", stored.Spec.ReleaseCoordinates, tt.wantCoordinates)
			}
		})
	}
}

func TestEnsureReleasePayloadRetriesCoordinateUpdateConflicts(t *testing.T) {
	const (
		payloadName = "4.20.0-0.layered-2026-09-28-120000"
		digest      = "sha256:e08883ade89b50664c14f2a9434018921a012c4506dde92e8779e482e025ea4c"
	)
	synthetic := v1alpha1.ReleaseCoordinates{
		Repository: "quay-proxy.ci.openshift.org/openshift/ci",
		Tag:        releasecontroller.ReferencePayloadTag(payloadName),
	}
	existing := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{Name: payloadName, Namespace: "ocp", ResourceVersion: "1"},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadCoordinates: v1alpha1.PayloadCoordinates{
				Namespace:          "ocp",
				ImagestreamName:    "layered",
				ImagestreamTagName: payloadName,
				StreamName:         "layered",
			},
			ReleaseCoordinates: []v1alpha1.ReleaseCoordinates{
				synthetic,
				{Repository: "registry.example.com/history", Tag: "approved"},
			},
			PayloadType: v1alpha1.PayloadTypeReference,
		},
	}
	client := releasefake.NewSimpleClientset(existing)
	updateAttempts := 0
	client.PrependReactor("update", "releasepayloads", func(action clienttesting.Action) (bool, runtime.Object, error) {
		updateAttempts++
		if updateAttempts == 1 {
			concurrent := existing.DeepCopy()
			concurrent.ResourceVersion = "2"
			concurrent.Labels = map[string]string{"concurrent": "label"}
			concurrent.Annotations = map[string]string{"concurrent": "annotation"}
			concurrent.Spec.PayloadOverride = v1alpha1.ReleasePayloadOverride{
				Override: v1alpha1.ReleasePayloadOverrideAccepted,
				Reason:   "concurrent manual approval",
			}
			concurrent.Status.Conditions = []metav1.Condition{{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionTrue,
				Reason: "ConcurrentStatusUpdate",
			}}
			if err := client.Tracker().Update(v1alpha1.SchemeGroupVersion.WithResource("releasepayloads"), concurrent, "ocp"); err != nil {
				t.Fatalf("update tracker with concurrent mutation: %v", err)
			}
			return true, nil, apierrors.NewConflict(
				schema.GroupResource{Group: v1alpha1.GroupName, Resource: "releasepayloads"},
				payloadName,
				errors.New("concurrent update"),
			)
		}
		return false, nil, nil
	})
	controller := &Controller{releasePayloadClient: client.ReleaseV1alpha1()}
	release := &releasecontroller.Release{
		Config: &releasecontroller.ReleaseConfig{
			Name: "layered",
			As:   releasecontroller.ReleaseConfigModeLayered,
			ReferenceRelease: &releasecontroller.ReferenceRelease{
				PullRepository: synthetic.Repository,
			},
		},
		Target: &imagev1.ImageStream{ObjectMeta: metav1.ObjectMeta{Name: "layered", Namespace: "ocp"}},
	}
	tag := &imagev1.TagReference{
		Name:      payloadName,
		Reference: true,
		From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/example/layered@" + digest},
	}

	got, err := controller.ensureReleasePayload(release, tag)
	if err != nil {
		t.Fatalf("ensureReleasePayload returned an error after a conflict: %v", err)
	}
	if updateAttempts != 2 {
		t.Fatalf("update attempts = %d, want 2", updateAttempts)
	}
	want := []v1alpha1.ReleaseCoordinates{
		{Repository: "quay.io/example/layered", Digest: digest},
		{Repository: "registry.example.com/history", Tag: "approved"},
	}
	if !reflect.DeepEqual(got.Spec.ReleaseCoordinates, want) {
		t.Fatalf("coordinates = %#v, want %#v", got.Spec.ReleaseCoordinates, want)
	}
	if got.Labels["concurrent"] != "label" || got.Annotations["concurrent"] != "annotation" {
		t.Fatalf("concurrent metadata was not preserved: labels=%#v annotations=%#v", got.Labels, got.Annotations)
	}
	if got.Spec.PayloadOverride.Reason != "concurrent manual approval" {
		t.Fatalf("concurrent override was not preserved: %#v", got.Spec.PayloadOverride)
	}
	if len(got.Status.Conditions) != 1 || got.Status.Conditions[0].Reason != "ConcurrentStatusUpdate" {
		t.Fatalf("concurrent status was not preserved: %#v", got.Status)
	}
}

func TestLayeredReleasePayloadCoordinateReconciliationFromExistingPhases(t *testing.T) {
	const (
		payloadName      = "4.20.0-0.layered-2026-09-28-120000"
		legacyRepository = "quay-proxy.ci.openshift.org/openshift/ci"
		digest           = "sha256:e08883ade89b50664c14f2a9434018921a012c4506dde92e8779e482e025ea4c"
	)

	for _, phase := range []string{releasecontroller.ReleasePhaseReady, releasecontroller.ReleasePhaseAccepted} {
		t.Run(phase, func(t *testing.T) {
			conditionType := v1alpha1.ConditionPayloadCreated
			if phase == releasecontroller.ReleasePhaseAccepted {
				conditionType = v1alpha1.ConditionPayloadAccepted
			}
			existing := &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{Name: payloadName, Namespace: "ocp"},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "layered",
						ImagestreamTagName: payloadName,
						StreamName:         "layered",
					},
					ReleaseCoordinates: []v1alpha1.ReleaseCoordinates{{
						Repository: legacyRepository,
						Tag:        releasecontroller.ReferencePayloadTag(payloadName),
					}},
					PayloadType: v1alpha1.PayloadTypeReference,
				},
				Status: v1alpha1.ReleasePayloadStatus{Conditions: []metav1.Condition{{
					Type:   conditionType,
					Status: metav1.ConditionTrue,
				}}},
			}
			client := releasefake.NewSimpleClientset(existing)
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
			if err := indexer.Add(existing); err != nil {
				t.Fatalf("add payload to lister indexer: %v", err)
			}
			payloadLister := releaselisters.NewReleasePayloadLister(indexer)
			controller := &Controller{
				releasePayloadClient: client.ReleaseV1alpha1(),
				releasePayloadLister: &releasecontroller.MultiReleasePayloadLister{
					Listers: map[string]releaselisters.ReleasePayloadNamespaceLister{
						"ocp": payloadLister.ReleasePayloads("ocp"),
					},
				},
			}
			tag := imagev1.TagReference{
				Name:      payloadName,
				Reference: true,
				From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/example/layered@" + digest},
				Annotations: map[string]string{
					releasecontroller.ReleaseAnnotationName:   "layered",
					releasecontroller.ReleaseAnnotationSource: "ocp/source",
					releasecontroller.ReleaseAnnotationPhase:  phase,
				},
			}
			release := &releasecontroller.Release{
				Source: &imagev1.ImageStream{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "ocp"}},
				Target: &imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Name: "layered", Namespace: "ocp"},
					Spec:       imagev1.ImageStreamSpec{Tags: []imagev1.TagReference{tag}},
				},
				Config: &releasecontroller.ReleaseConfig{
					Name: "layered",
					As:   releasecontroller.ReleaseConfigModeLayered,
					ReferenceRelease: &releasecontroller.ReferenceRelease{
						PullRepository: legacyRepository,
					},
				},
				PayloadPhases: map[string]string{payloadName: phase},
			}

			var syncPhase func(*releasecontroller.Release) error
			if phase == releasecontroller.ReleasePhaseReady {
				syncPhase = controller.syncReady
			} else {
				syncPhase = controller.syncAccepted
			}
			if err := syncPhase(release); err != nil {
				t.Fatalf("first %s sync failed: %v", phase, err)
			}
			if err := syncPhase(release); err != nil {
				t.Fatalf("idempotent %s sync failed: %v", phase, err)
			}

			stored, err := client.ReleaseV1alpha1().ReleasePayloads("ocp").Get(context.Background(), payloadName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get reconciled payload: %v", err)
			}
			want := []v1alpha1.ReleaseCoordinates{{Repository: "quay.io/example/layered", Digest: digest}}
			if !reflect.DeepEqual(stored.Spec.ReleaseCoordinates, want) {
				t.Fatalf("coordinates after %s sync = %#v, want %#v", phase, stored.Spec.ReleaseCoordinates, want)
			}
			updates := 0
			for _, action := range client.Actions() {
				if action.GetVerb() == "update" {
					updates++
				}
			}
			if updates != 1 {
				t.Fatalf("update actions after two %s syncs = %d, want 1", phase, updates)
			}
		})
	}
}
