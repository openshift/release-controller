package release_payload_controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/client/clientset/versioned/fake"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	"github.com/openshift/release-controller/pkg/releasequalifiers"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/utils/clock"
)

// mockConfigAccessor implements releasequalifiers.ConfigAccessor for testing
type mockConfigAccessor struct {
	config releasequalifiers.ReleaseQualifiers
}

func (m *mockConfigAccessor) Get() releasequalifiers.ReleaseQualifiers {
	return m.config
}

func boolPtr(b bool) *bool {
	return &b
}

func TestQualifiersSummarySync(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		input          *v1alpha1.ReleasePayload
		configAccessor releasequalifiers.ConfigAccessor
		expected       *v1alpha1.ReleasePayload
	}{
		{
			name: "AllJobsSuccessful_AllBadgesEarned",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"techpreview": {
						Enabled:            boolPtr(true),
						BadgeName:          "Tech Preview",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
					"fips": {
						Enabled:            boolPtr(true),
						BadgeName:          "FIPS",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
				},
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aws-ovn-techpreview",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"techpreview": releasequalifiers.ReleaseQualifier{},
								},
							},
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aws-ovn-techpreview",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
						{
							CIConfigurationName:    "fips-scan",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aws-ovn-techpreview",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"techpreview": releasequalifiers.ReleaseQualifier{},
								},
							},
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aws-ovn-techpreview",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
						{
							CIConfigurationName:    "fips-scan",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
					QualifiersSummary: &v1alpha1.QualifiersSummary{
						Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
							"techpreview": {
								Jobs: []v1alpha1.ReleaseQualifierJobReference{
									{
										CIConfigurationName:    "aws-ovn-techpreview",
										CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview",
									},
								},
								AggregateState:  v1alpha1.JobStateSuccess,
								BadgeName:       "Tech Preview",
								BadgeEarned:     true,
								BadgePropagated: true,
							},
							"fips": {
								Jobs: []v1alpha1.ReleaseQualifierJobReference{
									{
										CIConfigurationName:    "fips-scan",
										CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
									},
								},
								AggregateState:  v1alpha1.JobStateSuccess,
								BadgeName:       "FIPS",
								BadgeEarned:     true,
								BadgePropagated: true,
							},
						},
					},
				},
			},
		},
		{
			name: "MixedJobStates_PartialBadgesEarned",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"techpreview": {
						Enabled:            boolPtr(true),
						BadgeName:          "Tech Preview",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
					"fips": {
						Enabled:            boolPtr(true),
						BadgeName:          "FIPS",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
				},
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aws-ovn-techpreview-1",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-1",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"techpreview": releasequalifiers.ReleaseQualifier{},
								},
							},
							{
								CIConfigurationName:    "aws-ovn-techpreview-2",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-2",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"techpreview": releasequalifiers.ReleaseQualifier{},
								},
							},
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aws-ovn-techpreview-1",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-1",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
						{
							CIConfigurationName:    "aws-ovn-techpreview-2",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-2",
							AggregateState:         v1alpha1.JobStateFailure,
						},
						{
							CIConfigurationName:    "fips-scan",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "aws-ovn-techpreview-1",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-1",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"techpreview": releasequalifiers.ReleaseQualifier{},
								},
							},
							{
								CIConfigurationName:    "aws-ovn-techpreview-2",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-2",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"techpreview": releasequalifiers.ReleaseQualifier{},
								},
							},
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aws-ovn-techpreview-1",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-1",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
						{
							CIConfigurationName:    "aws-ovn-techpreview-2",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-2",
							AggregateState:         v1alpha1.JobStateFailure,
						},
						{
							CIConfigurationName:    "fips-scan",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
					QualifiersSummary: &v1alpha1.QualifiersSummary{
						Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
							"techpreview": {
								Jobs: []v1alpha1.ReleaseQualifierJobReference{
									{
										CIConfigurationName:    "aws-ovn-techpreview-1",
										CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-1",
									},
									{
										CIConfigurationName:    "aws-ovn-techpreview-2",
										CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-2",
									},
								},
								AggregateState:  v1alpha1.JobStateFailure,
								BadgeName:       "Tech Preview",
								BadgeEarned:     false,
								BadgePropagated: false,
							},
							"fips": {
								Jobs: []v1alpha1.ReleaseQualifierJobReference{
									{
										CIConfigurationName:    "fips-scan",
										CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
									},
								},
								AggregateState:  v1alpha1.JobStateSuccess,
								BadgeName:       "FIPS",
								BadgeEarned:     true,
								BadgePropagated: true,
							},
						},
					},
				},
			},
		},
		{
			name: "NoQualifiers_EmptySummary",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{},
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "upgrade",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-stable-4.y-e2e-aws-ovn-upgrade",
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "upgrade",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-stable-4.y-e2e-aws-ovn-upgrade",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "upgrade",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-stable-4.y-e2e-aws-ovn-upgrade",
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "upgrade",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-stable-4.y-e2e-aws-ovn-upgrade",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
					QualifiersSummary: &v1alpha1.QualifiersSummary{
						Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{},
					},
				},
			},
		},
		{
			name: "UpgradeJobs_ExcludedFromQualifierSummary",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"fips": {
						Enabled:            boolPtr(true),
						BadgeName:          "FIPS",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
				},
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-blocking",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-blocking-fips",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-informing",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-informing-fips",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
						UpgradeJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-upgrade",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-upgrade-fips",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "fips-blocking",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-blocking-fips",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "fips-informing",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-informing-fips",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
					UpgradeJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "fips-upgrade",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-upgrade-fips",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-blocking",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-blocking-fips",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-informing",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-informing-fips",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
						UpgradeJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-upgrade",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-upgrade-fips",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "fips-blocking",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-blocking-fips",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "fips-informing",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-informing-fips",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
					UpgradeJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "fips-upgrade",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-upgrade-fips",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
					QualifiersSummary: &v1alpha1.QualifiersSummary{
						Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
							"fips": {
								Jobs: []v1alpha1.ReleaseQualifierJobReference{
									{
										CIConfigurationName:    "fips-blocking",
										CIConfigurationJobName: "periodic-ci-openshift-release-master-blocking-fips",
									},
									{
										CIConfigurationName:    "fips-informing",
										CIConfigurationJobName: "periodic-ci-openshift-release-master-informing-fips",
									},
								},
								AggregateState:  v1alpha1.JobStateSuccess,
								BadgeName:       "FIPS",
								BadgeEarned:     true,
								BadgePropagated: true,
							},
						},
					},
				},
			},
		},
		{
			name: "PendingJobs_BadgeNotEarned",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"hypershift": {
						Enabled:            boolPtr(true),
						BadgeName:          "HyperShift",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
				},
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "hypershift-ovn-conformance",
								CIConfigurationJobName: "periodic-ci-openshift-hypershift-release-4.20-periodics-e2e-aws-ovn-conformance",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"hypershift": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "hypershift-ovn-conformance",
							CIConfigurationJobName: "periodic-ci-openshift-hypershift-release-4.20-periodics-e2e-aws-ovn-conformance",
							AggregateState:         v1alpha1.JobStatePending,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "hypershift-ovn-conformance",
								CIConfigurationJobName: "periodic-ci-openshift-hypershift-release-4.20-periodics-e2e-aws-ovn-conformance",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"hypershift": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "hypershift-ovn-conformance",
							CIConfigurationJobName: "periodic-ci-openshift-hypershift-release-4.20-periodics-e2e-aws-ovn-conformance",
							AggregateState:         v1alpha1.JobStatePending,
						},
					},
					QualifiersSummary: &v1alpha1.QualifiersSummary{
						Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
							"hypershift": {
								Jobs: []v1alpha1.ReleaseQualifierJobReference{
									{
										CIConfigurationName:    "hypershift-ovn-conformance",
										CIConfigurationJobName: "periodic-ci-openshift-hypershift-release-4.20-periodics-e2e-aws-ovn-conformance",
									},
								},
								AggregateState:  v1alpha1.JobStatePending,
								BadgeName:       "HyperShift",
								BadgeEarned:     false,
								BadgePropagated: false,
							},
						},
					},
				},
			},
		},
		{
			name: "FailureLabels_PropagatedOnFailure",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"fips": {
						Enabled:            boolPtr(true),
						BadgeName:          "FIPS",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
						FailureLabels:      []string{"prevent_rc", "needs-attention"},
					},
				},
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "fips-scan",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
							AggregateState:         v1alpha1.JobStateFailure,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "fips-scan",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
							AggregateState:         v1alpha1.JobStateFailure,
						},
					},
					QualifiersSummary: &v1alpha1.QualifiersSummary{
						FailureLabels: []string{"needs-attention", "prevent_rc"},
						Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
							"fips": {
								Jobs: []v1alpha1.ReleaseQualifierJobReference{
									{
										CIConfigurationName:    "fips-scan",
										CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
									},
								},
								AggregateState:  v1alpha1.JobStateFailure,
								BadgeName:       "FIPS",
								BadgeEarned:     false,
								BadgePropagated: false,
								FailureLabels:   []string{"prevent_rc", "needs-attention"},
							},
						},
					},
				},
			},
		},
		{
			name: "FailureLabels_JobLevelOverride",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"hcm": {
						Enabled:            boolPtr(true),
						BadgeName:          "HCM",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
						FailureLabels:      []string{"prevent_ec"},
					},
				},
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "rosa-hcp",
								CIConfigurationJobName: "periodic-ci-rosa-hcp",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"hcm": {FailureLabels: []string{"prevent_rc"}},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "rosa-hcp",
							CIConfigurationJobName: "periodic-ci-rosa-hcp",
							AggregateState:         v1alpha1.JobStateFailure,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "rosa-hcp",
								CIConfigurationJobName: "periodic-ci-rosa-hcp",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"hcm": {FailureLabels: []string{"prevent_rc"}},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "rosa-hcp",
							CIConfigurationJobName: "periodic-ci-rosa-hcp",
							AggregateState:         v1alpha1.JobStateFailure,
						},
					},
					QualifiersSummary: &v1alpha1.QualifiersSummary{
						FailureLabels: []string{"prevent_rc"},
						Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
							"hcm": {
								Jobs: []v1alpha1.ReleaseQualifierJobReference{
									{
										CIConfigurationName:    "rosa-hcp",
										CIConfigurationJobName: "periodic-ci-rosa-hcp",
									},
								},
								AggregateState:  v1alpha1.JobStateFailure,
								BadgeName:       "HCM",
								BadgeEarned:     false,
								BadgePropagated: false,
								FailureLabels:   []string{"prevent_rc"},
							},
						},
					},
				},
			},
		},
		{
			name: "NoChanges_SummaryAlreadyUpToDate",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"fips": {
						Enabled:            boolPtr(true),
						BadgeName:          "FIPS",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
				},
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "fips-scan",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
					QualifiersSummary: &v1alpha1.QualifiersSummary{
						Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
							"fips": {
								Jobs: []v1alpha1.ReleaseQualifierJobReference{
									{
										CIConfigurationName:    "fips-scan",
										CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
									},
								},
								AggregateState:  v1alpha1.JobStateSuccess,
								BadgeName:       "FIPS",
								BadgeEarned:     true,
								BadgePropagated: true,
							},
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "fips-scan",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
					QualifiersSummary: &v1alpha1.QualifiersSummary{
						Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
							"fips": {
								Jobs: []v1alpha1.ReleaseQualifierJobReference{
									{
										CIConfigurationName:    "fips-scan",
										CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
									},
								},
								AggregateState:  v1alpha1.JobStateSuccess,
								BadgeName:       "FIPS",
								BadgeEarned:     true,
								BadgePropagated: true,
							},
						},
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			releasePayloadClient := fake.NewSimpleClientset(testCase.input)

			releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactory(releasePayloadClient, controllerDefaultResyncDuration)
			releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()

			c, err := NewJobQualifiersSummaryController(
				releasePayloadInformer,
				releasePayloadClient.ReleaseV1alpha1(),
				events.NewInMemoryRecorder("qualifiers-summary-controller-test", clock.RealClock{}),
				testCase.configAccessor,
			)
			if err != nil {
				t.Fatalf("Failed to create Qualifiers Summary Controller: %v", err)
			}

			releasePayloadInformerFactory.Start(context.Background().Done())

			if !cache.WaitForNamedCacheSync("QualifiersSummaryController", context.Background().Done(), c.cachesToSync...) {
				t.Errorf("%s: error waiting for caches to sync", testCase.name)
				return
			}

			if err := c.sync(context.TODO(), fmt.Sprintf("%s/%s", testCase.input.Namespace, testCase.input.Name)); err != nil {
				t.Errorf("%s: unexpected err: %v", testCase.name, err)
			}

			// Performing a live lookup instead of having to wait for the cache to sync (again)...
			output, _ := c.releasePayloadClient.ReleasePayloads(testCase.input.Namespace).Get(context.TODO(), testCase.input.Name, metav1.GetOptions{})
			if diff := cmp.Diff(testCase.expected, output); diff != "" {
				t.Errorf("%s: mismatch (-want +got):\n%s", testCase.name, diff)
			}
		})
	}
}

func TestQualifiersSummarySyncErrorCases(t *testing.T) {
	t.Parallel()

	t.Run("ReleasePayloadNotFound", func(t *testing.T) {
		releasePayload := &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.20.8",
				Namespace: "ocp",
			},
		}

		releasePayloadClient := fake.NewSimpleClientset(releasePayload)
		releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactory(releasePayloadClient, controllerDefaultResyncDuration)
		releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()

		c, err := NewJobQualifiersSummaryController(
			releasePayloadInformer,
			releasePayloadClient.ReleaseV1alpha1(),
			events.NewInMemoryRecorder("qualifiers-summary-controller-test", clock.RealClock{}),
			&mockConfigAccessor{config: releasequalifiers.ReleaseQualifiers{}},
		)
		if err != nil {
			t.Fatalf("Failed to create Qualifiers Summary Controller: %v", err)
		}

		releasePayloadInformerFactory.Start(context.Background().Done())

		if !cache.WaitForNamedCacheSync("QualifiersSummaryController", context.Background().Done(), c.cachesToSync...) {
			t.Errorf("error waiting for caches to sync")
			return
		}

		// Sync with a ReleasePayload that doesn't exist
		err = c.sync(context.TODO(), "ocp/nonexistent")
		if err != nil {
			t.Errorf("Expected no error when ReleasePayload not found, got: %v", err)
		}
	})

	t.Run("InvalidResourceKey", func(t *testing.T) {
		releasePayload := &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.20.8",
				Namespace: "ocp",
			},
		}

		releasePayloadClient := fake.NewSimpleClientset(releasePayload)
		releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactory(releasePayloadClient, controllerDefaultResyncDuration)
		releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()

		c, err := NewJobQualifiersSummaryController(
			releasePayloadInformer,
			releasePayloadClient.ReleaseV1alpha1(),
			events.NewInMemoryRecorder("qualifiers-summary-controller-test", clock.RealClock{}),
			&mockConfigAccessor{config: releasequalifiers.ReleaseQualifiers{}},
		)
		if err != nil {
			t.Fatalf("Failed to create Qualifiers Summary Controller: %v", err)
		}

		releasePayloadInformerFactory.Start(context.Background().Done())

		if !cache.WaitForNamedCacheSync("QualifiersSummaryController", context.Background().Done(), c.cachesToSync...) {
			t.Errorf("error waiting for caches to sync")
			return
		}

		// Sync with an invalid key format
		err = c.sync(context.TODO(), "invalid/key/format/with/too/many/parts")
		if err != nil {
			t.Errorf("Expected no error for invalid resource key, got: %v", err)
		}
	})
}

func TestQualifiersSummarySyncUpdateStatusError(t *testing.T) {
	t.Parallel()

	// Build a ReleasePayload where GenerateQualifiersSummary will produce a changed status,
	// which triggers the UpdateStatus call.
	releasePayload := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "4.20.8",
			Namespace: "ocp",
		},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
				InformingJobs: []v1alpha1.CIConfiguration{
					{
						CIConfigurationName:    "aws-ovn-techpreview",
						CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview",
						Qualifiers: releasequalifiers.ReleaseQualifiers{
							"techpreview": releasequalifiers.ReleaseQualifier{},
						},
					},
				},
			},
		},
		Status: v1alpha1.ReleasePayloadStatus{
			InformingJobResults: []v1alpha1.JobStatus{
				{
					CIConfigurationName:    "aws-ovn-techpreview",
					CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview",
					AggregateState:         v1alpha1.JobStateSuccess,
				},
			},
		},
	}

	configAccessor := &mockConfigAccessor{
		config: releasequalifiers.ReleaseQualifiers{
			"techpreview": {
				Enabled:            boolPtr(true),
				BadgeName:          "Tech Preview",
				PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
			},
		},
	}

	t.Run("UpdateStatusReturnsError", func(t *testing.T) {
		releasePayloadClient := fake.NewSimpleClientset(releasePayload.DeepCopy())
		releasePayloadClient.PrependReactor("update", "releasepayloads", func(action ktesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("update failed")
		})

		releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactory(releasePayloadClient, controllerDefaultResyncDuration)
		releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()

		c, err := NewJobQualifiersSummaryController(
			releasePayloadInformer,
			releasePayloadClient.ReleaseV1alpha1(),
			events.NewInMemoryRecorder("qualifiers-summary-controller-test", clock.RealClock{}),
			configAccessor,
		)
		if err != nil {
			t.Fatalf("Failed to create controller: %v", err)
		}

		releasePayloadInformerFactory.Start(context.Background().Done())
		if !cache.WaitForNamedCacheSync("QualifiersSummaryController", context.Background().Done(), c.cachesToSync...) {
			t.Fatal("error waiting for caches to sync")
		}

		err = c.sync(context.TODO(), "ocp/4.20.8")
		if err == nil {
			t.Error("Expected error from sync when UpdateStatus fails, got nil")
		}
	})

	t.Run("UpdateStatusReturnsNotFound", func(t *testing.T) {
		releasePayloadClient := fake.NewSimpleClientset(releasePayload.DeepCopy())
		releasePayloadClient.PrependReactor("update", "releasepayloads", func(action ktesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.NewNotFound(schema.GroupResource{Group: "release.openshift.io", Resource: "releasepayloads"}, "4.20.8")
		})

		releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactory(releasePayloadClient, controllerDefaultResyncDuration)
		releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()

		c, err := NewJobQualifiersSummaryController(
			releasePayloadInformer,
			releasePayloadClient.ReleaseV1alpha1(),
			events.NewInMemoryRecorder("qualifiers-summary-controller-test", clock.RealClock{}),
			configAccessor,
		)
		if err != nil {
			t.Fatalf("Failed to create controller: %v", err)
		}

		releasePayloadInformerFactory.Start(context.Background().Done())
		if !cache.WaitForNamedCacheSync("QualifiersSummaryController", context.Background().Done(), c.cachesToSync...) {
			t.Fatal("error waiting for caches to sync")
		}

		err = c.sync(context.TODO(), "ocp/4.20.8")
		if err != nil {
			t.Errorf("Expected no error when UpdateStatus returns NotFound, got: %v", err)
		}
	})
}
