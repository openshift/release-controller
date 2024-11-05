package main

import (
	"reflect"
	"testing"

	imagev1 "github.com/openshift/api/image/v1"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	release = &releasecontroller.Release{
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
		payloadName      string
		jobNamespace     string
		prowNamespace    string
		verificationJobs map[string]releasecontroller.ReleaseVerification
		upgradeJobs      map[string]releasecontroller.UpgradeVerification
		dataSource       v1alpha1.PayloadVerificationDataSource
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
			dataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
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
			dataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
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
				},
			},
		},
		{
			name:          "RealWorldExample",
			release:       release,
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
			dataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
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
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payload := newReleasePayload(tc.release, tc.payloadName, tc.jobNamespace, tc.prowNamespace, tc.verificationJobs, tc.upgradeJobs, tc.dataSource)
			if !reflect.DeepEqual(payload, tc.expected) {
				t.Errorf("%s: Expected %v, got %v", tc.name, tc.expected, payload)
			}
		})
	}
}
