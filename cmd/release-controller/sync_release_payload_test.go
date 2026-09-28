package main

import (
	"reflect"
	"testing"

	imagev1 "github.com/openshift/api/image/v1"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"github.com/openshift/release-controller/pkg/releasequalifiers"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
