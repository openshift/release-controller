package qualifiers

import (
	"reflect"
	"sort"
	"testing"

	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/releasequalifiers"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mockConfigAccessor implements ConfigAccessor for testing
type mockConfigAccessor struct {
	config releasequalifiers.ReleaseQualifiers
}

func (m *mockConfigAccessor) Get() releasequalifiers.ReleaseQualifiers {
	return m.config
}

func boolPtr(b bool) *bool {
	return &b
}

func TestComputeBadgeEarned(t *testing.T) {
	tests := []struct {
		name           string
		enabled        *bool
		aggregateState v1alpha1.JobState
		want           bool
	}{
		{
			name:           "Enabled=true + AggregateState=Success → earned",
			enabled:        boolPtr(true),
			aggregateState: v1alpha1.JobStateSuccess,
			want:           true,
		},
		{
			name:           "Enabled=true + AggregateState=Failure → not earned",
			enabled:        boolPtr(true),
			aggregateState: v1alpha1.JobStateFailure,
			want:           false,
		},
		{
			name:           "Enabled=true + AggregateState=Pending → not earned",
			enabled:        boolPtr(true),
			aggregateState: v1alpha1.JobStatePending,
			want:           false,
		},
		{
			name:           "Enabled=false + AggregateState=Success → not earned",
			enabled:        boolPtr(false),
			aggregateState: v1alpha1.JobStateSuccess,
			want:           false,
		},
		{
			name:           "Enabled=nil + AggregateState=Success → not earned (nil treated as disabled)",
			enabled:        nil,
			aggregateState: v1alpha1.JobStateSuccess,
			want:           false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComputeBadgeEarned(tt.enabled, tt.aggregateState); got != tt.want {
				t.Errorf("ComputeBadgeEarned() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeBadgePropagated(t *testing.T) {
	tests := []struct {
		name           string
		enabled        *bool
		badgeStatus    releasequalifiers.BadgeStatus
		aggregateState v1alpha1.JobState
		want           bool
	}{
		{
			name:           "Enabled=false → never propagated (regardless of BadgeStatus)",
			enabled:        boolPtr(false),
			badgeStatus:    releasequalifiers.BadgeStatusYes,
			aggregateState: v1alpha1.JobStateSuccess,
			want:           false,
		},
		{
			name:           "Enabled=true + BadgeStatusYes → always propagated",
			enabled:        boolPtr(true),
			badgeStatus:    releasequalifiers.BadgeStatusYes,
			aggregateState: v1alpha1.JobStateFailure,
			want:           true,
		},
		{
			name:           "Enabled=true + BadgeStatusNo → never propagated",
			enabled:        boolPtr(true),
			badgeStatus:    releasequalifiers.BadgeStatusNo,
			aggregateState: v1alpha1.JobStateSuccess,
			want:           false,
		},
		{
			name:           "Enabled=true + BadgeStatusOnSuccess + AggregateState=Success → propagated",
			enabled:        boolPtr(true),
			badgeStatus:    releasequalifiers.BadgeStatusOnSuccess,
			aggregateState: v1alpha1.JobStateSuccess,
			want:           true,
		},
		{
			name:           "Enabled=true + BadgeStatusOnSuccess + AggregateState=Failure → not propagated",
			enabled:        boolPtr(true),
			badgeStatus:    releasequalifiers.BadgeStatusOnSuccess,
			aggregateState: v1alpha1.JobStateFailure,
			want:           false,
		},
		{
			name:           "Enabled=true + BadgeStatusOnFailure + AggregateState=Failure → propagated",
			enabled:        boolPtr(true),
			badgeStatus:    releasequalifiers.BadgeStatusOnFailure,
			aggregateState: v1alpha1.JobStateFailure,
			want:           true,
		},
		{
			name:           "Enabled=true + BadgeStatusOnFailure + AggregateState=Success → not propagated",
			enabled:        boolPtr(true),
			badgeStatus:    releasequalifiers.BadgeStatusOnFailure,
			aggregateState: v1alpha1.JobStateSuccess,
			want:           false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComputeBadgePropagated(tt.enabled, tt.badgeStatus, tt.aggregateState); got != tt.want {
				t.Errorf("ComputeBadgePropagated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeQualifierState(t *testing.T) {
	tests := []struct {
		name        string
		jobStatuses []v1alpha1.JobStatus
		want        v1alpha1.JobState
	}{
		{
			name: "All jobs successful → Success",
			jobStatuses: []v1alpha1.JobStatus{
				{AggregateState: v1alpha1.JobStateSuccess},
				{AggregateState: v1alpha1.JobStateSuccess},
			},
			want: v1alpha1.JobStateSuccess,
		},
		{
			name: "Any job failed → Failure",
			jobStatuses: []v1alpha1.JobStatus{
				{AggregateState: v1alpha1.JobStateSuccess},
				{AggregateState: v1alpha1.JobStateFailure},
			},
			want: v1alpha1.JobStateFailure,
		},
		{
			name: "Some jobs pending → Pending",
			jobStatuses: []v1alpha1.JobStatus{
				{AggregateState: v1alpha1.JobStateSuccess},
				{AggregateState: v1alpha1.JobStatePending},
			},
			want: v1alpha1.JobStatePending,
		},
		{
			name:        "No jobs → Unknown",
			jobStatuses: []v1alpha1.JobStatus{},
			want:        v1alpha1.JobStateUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComputeQualifierState(tt.jobStatuses); got != tt.want {
				t.Errorf("ComputeQualifierState() = %v, want %v", got, tt.want)
			}
		})
	}
}

func newJobRunResult(state v1alpha1.JobRunState) v1alpha1.JobRunResult {
	return v1alpha1.JobRunResult{
		State: state,
		Coordinates: v1alpha1.JobRunCoordinates{
			Name:      "test-job",
			Namespace: "ci",
			Cluster:   "build01",
		},
		StartTime:           v1.Time{Time: v1.Now().Time},
		HumanProwResultsURL: "https://prow.ci.openshift.org/view/test",
	}
}

func newJobStatus(ciName, jobName string, maxRetries int, aggState v1alpha1.JobState, results []v1alpha1.JobRunResult) v1alpha1.JobStatus {
	return v1alpha1.JobStatus{
		CIConfigurationName:    ciName,
		CIConfigurationJobName: jobName,
		MaxRetries:             maxRetries,
		AggregateState:         aggState,
		JobRunResults:          results,
	}
}

func createBasePayloadSpec() v1alpha1.ReleasePayloadSpec {
	return v1alpha1.ReleasePayloadSpec{
		PayloadCoordinates: v1alpha1.PayloadCoordinates{
			Namespace:          "ocp",
			ImagestreamName:    "release",
			ImagestreamTagName: "4.20.8",
		},
		PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
			ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
				Namespace: "ci-release",
			},
			ProwCoordinates: v1alpha1.ProwCoordinates{
				Namespace: "ci",
			},
			ReleaseMirrorCoordinates: v1alpha1.ReleaseMirrorCoordinates{
				Namespace:            "ci-release",
				ReleaseMirrorJobName: "4.20.8-alternate-mirror",
			},
		},
		PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
			BlockingJobs: []v1alpha1.CIConfiguration{
				{
					CIConfigurationName:    "upgrade",
					CIConfigurationJobName: "periodic-ci-openshift-release-master-stable-4.y-e2e-aws-ovn-upgrade",
					MaxRetries:             2,
				},
			},
			InformingJobs: []v1alpha1.CIConfiguration{
				{
					CIConfigurationName:    "aws-ovn-techpreview",
					CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview",
					MaxRetries:             1,
					Qualifiers: releasequalifiers.ReleaseQualifiers{
						"techpreview": releasequalifiers.ReleaseQualifier{},
					},
				},
				{
					CIConfigurationName:    "aws-ovn-techpreview-serial-1of3",
					CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-1of3",
					MaxRetries:             1,
					Qualifiers: releasequalifiers.ReleaseQualifiers{
						"techpreview": releasequalifiers.ReleaseQualifier{},
					},
				},
				{
					CIConfigurationName:    "aws-ovn-techpreview-serial-2of3",
					CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-2of3",
					MaxRetries:             1,
					Qualifiers: releasequalifiers.ReleaseQualifiers{
						"techpreview": releasequalifiers.ReleaseQualifier{},
					},
				},
				{
					CIConfigurationName:    "aws-ovn-techpreview-serial-3of3",
					CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-3of3",
					MaxRetries:             1,
					Qualifiers: releasequalifiers.ReleaseQualifiers{
						"techpreview": releasequalifiers.ReleaseQualifier{},
					},
				},
				{
					CIConfigurationName:    "aws-ovn-upgrade-4.20-micro-fips",
					CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-e2e-aws-ovn-upgrade-fips",
					MaxRetries:             1,
					Qualifiers: releasequalifiers.ReleaseQualifiers{
						"fips": releasequalifiers.ReleaseQualifier{},
					},
				},
				{
					CIConfigurationName:    "fips-scan",
					CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan",
					MaxRetries:             1,
					Qualifiers: releasequalifiers.ReleaseQualifiers{
						"fips": releasequalifiers.ReleaseQualifier{},
					},
				},
				{
					CIConfigurationName:    "hypershift-ovn-conformance-4.20",
					CIConfigurationJobName: "periodic-ci-openshift-hypershift-release-4.20-periodics-e2e-aws-ovn-conformance",
					MaxRetries:             2,
					Qualifiers: releasequalifiers.ReleaseQualifiers{
						"hypershift": releasequalifiers.ReleaseQualifier{},
					},
				},
				{
					CIConfigurationName:    "metal-ipi-ovn-bm",
					CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-e2e-metal-ipi-ovn-bm",
					Qualifiers: releasequalifiers.ReleaseQualifiers{
						"metal": releasequalifiers.ReleaseQualifier{},
					},
				},
				{
					CIConfigurationName:    "metal-ipi-ovn-ipv6",
					CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-e2e-metal-ipi-ovn-ipv6",
					Qualifiers: releasequalifiers.ReleaseQualifiers{
						"metal": releasequalifiers.ReleaseQualifier{},
					},
				},
				{
					CIConfigurationName:    "microshift-ovn-conformance-parallel",
					CIConfigurationJobName: "periodic-ci-openshift-microshift-release-4.20-periodics-e2e-aws-ovn-ocp-conformance",
					Qualifiers: releasequalifiers.ReleaseQualifiers{
						"microshift": releasequalifiers.ReleaseQualifier{},
					},
				},
				{
					CIConfigurationName:    "microshift-ovn-conformance-serial",
					CIConfigurationJobName: "periodic-ci-openshift-microshift-release-4.20-periodics-e2e-aws-ovn-ocp-conformance-serial",
					Qualifiers: releasequalifiers.ReleaseQualifiers{
						"microshift": releasequalifiers.ReleaseQualifier{},
					},
				},
				{
					CIConfigurationName:    "rosa-classic-sts-conformance",
					CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-e2e-rosa-sts-ovn",
					Qualifiers: releasequalifiers.ReleaseQualifiers{
						"rosa": releasequalifiers.ReleaseQualifier{},
					},
				},
			},
			PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
		},
	}
}

func createBaseConfig() releasequalifiers.ReleaseQualifiers {
	return releasequalifiers.ReleaseQualifiers{
		"techpreview": {
			Enabled:            boolPtr(true),
			BadgeName:          "Tech Preview",
			PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
			Summary:            "Tech Preview Features",
			Description:        "Tests for technology preview features",
		},
		"fips": {
			Enabled:            boolPtr(true),
			BadgeName:          "FIPS",
			PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
			Summary:            "FIPS Compliance",
			Description:        "Tests for FIPS compliance",
		},
		"hypershift": {
			Enabled:            boolPtr(true),
			BadgeName:          "HyperShift",
			PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
			Summary:            "HyperShift",
			Description:        "Tests for HyperShift",
		},
		"metal": {
			Enabled:            boolPtr(true),
			BadgeName:          "Metal",
			PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
			Summary:            "Bare Metal",
			Description:        "Tests for bare metal",
		},
		"microshift": {
			Enabled:            boolPtr(true),
			BadgeName:          "MicroShift",
			PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
			Summary:            "MicroShift",
			Description:        "Tests for MicroShift",
		},
		"rosa": {
			Enabled:            boolPtr(true),
			BadgeName:          "ROSA",
			PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
			Summary:            "ROSA",
			Description:        "Tests for ROSA",
		},
	}
}

func TestQualifierStateLabelKey(t *testing.T) {
	tests := []struct {
		qualifierID releasequalifiers.QualifierId
		want        string
	}{
		{"fips", "release.openshift.io/fips_state"},
		{"techpreview", "release.openshift.io/techpreview_state"},
		{"sdn-migration", "release.openshift.io/sdn-migration_state"},
	}
	for _, tt := range tests {
		if got := QualifierStateLabelKey(tt.qualifierID); got != tt.want {
			t.Errorf("QualifierStateLabelKey(%q) = %q, want %q", tt.qualifierID, got, tt.want)
		}
	}
}

func TestGenerateQualifiersSummary(t *testing.T) {
	tests := []struct {
		name           string
		payload        *v1alpha1.ReleasePayload
		configAccessor releasequalifiers.ConfigAccessor
		want           *v1alpha1.QualifiersSummary
	}{
		{
			name: "AllJobsSuccessful_AllBadgesEarnedAndPropagated",
			configAccessor: &mockConfigAccessor{
				config: createBaseConfig(),
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: createBasePayloadSpec(),
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						// techpreview qualifier jobs (4 jobs - all success)
						newJobStatus("aws-ovn-techpreview", "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview", 1, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						newJobStatus("aws-ovn-techpreview-serial-1of3", "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-1of3", 1, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						newJobStatus("aws-ovn-techpreview-serial-2of3", "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-2of3", 1, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						newJobStatus("aws-ovn-techpreview-serial-3of3", "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-3of3", 1, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						// fips qualifier jobs (2 jobs - all success)
						newJobStatus("aws-ovn-upgrade-4.20-micro-fips", "periodic-ci-openshift-release-master-nightly-4.20-e2e-aws-ovn-upgrade-fips", 1, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						newJobStatus("fips-scan", "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan", 1, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						// hypershift qualifier jobs (1 job - success)
						newJobStatus("hypershift-ovn-conformance-4.20", "periodic-ci-openshift-hypershift-release-4.20-periodics-e2e-aws-ovn-conformance", 2, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						// metal qualifier jobs (2 jobs - all success)
						newJobStatus("metal-ipi-ovn-bm", "periodic-ci-openshift-release-master-nightly-4.20-e2e-metal-ipi-ovn-bm", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						newJobStatus("metal-ipi-ovn-ipv6", "periodic-ci-openshift-release-master-nightly-4.20-e2e-metal-ipi-ovn-ipv6", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						// microshift qualifier jobs (2 jobs - all success)
						newJobStatus("microshift-ovn-conformance-parallel", "periodic-ci-openshift-microshift-release-4.20-periodics-e2e-aws-ovn-ocp-conformance", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						newJobStatus("microshift-ovn-conformance-serial", "periodic-ci-openshift-microshift-release-4.20-periodics-e2e-aws-ovn-ocp-conformance-serial", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						// rosa qualifier jobs (1 job - success)
						newJobStatus("rosa-classic-sts-conformance", "periodic-ci-openshift-release-master-nightly-4.20-e2e-rosa-sts-ovn", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
					},
				},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"techpreview": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "aws-ovn-techpreview", CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview"},
							{CIConfigurationName: "aws-ovn-techpreview-serial-1of3", CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-1of3"},
							{CIConfigurationName: "aws-ovn-techpreview-serial-2of3", CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-2of3"},
							{CIConfigurationName: "aws-ovn-techpreview-serial-3of3", CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-3of3"},
						},
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "Tech Preview",
						BadgeEarned:     true,
						BadgePropagated: true,
					},
					"fips": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "aws-ovn-upgrade-4.20-micro-fips", CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-e2e-aws-ovn-upgrade-fips"},
							{CIConfigurationName: "fips-scan", CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan"},
						},
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "FIPS",
						BadgeEarned:     true,
						BadgePropagated: true,
					},
					"hypershift": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "hypershift-ovn-conformance-4.20", CIConfigurationJobName: "periodic-ci-openshift-hypershift-release-4.20-periodics-e2e-aws-ovn-conformance"},
						},
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "HyperShift",
						BadgeEarned:     true,
						BadgePropagated: true,
					},
					"metal": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "metal-ipi-ovn-bm", CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-e2e-metal-ipi-ovn-bm"},
							{CIConfigurationName: "metal-ipi-ovn-ipv6", CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-e2e-metal-ipi-ovn-ipv6"},
						},
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "Metal",
						BadgeEarned:     true,
						BadgePropagated: true,
					},
					"microshift": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "microshift-ovn-conformance-parallel", CIConfigurationJobName: "periodic-ci-openshift-microshift-release-4.20-periodics-e2e-aws-ovn-ocp-conformance"},
							{CIConfigurationName: "microshift-ovn-conformance-serial", CIConfigurationJobName: "periodic-ci-openshift-microshift-release-4.20-periodics-e2e-aws-ovn-ocp-conformance-serial"},
						},
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "MicroShift",
						BadgeEarned:     true,
						BadgePropagated: true,
					},
					"rosa": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "rosa-classic-sts-conformance", CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-e2e-rosa-sts-ovn"},
						},
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "ROSA",
						BadgeEarned:     true,
						BadgePropagated: true,
					},
				},
			},
		},
		{
			name: "MixedStates_PerQualifierIndependence",
			configAccessor: &mockConfigAccessor{
				config: createBaseConfig(),
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: createBasePayloadSpec(),
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						// techpreview: 2 Success, 1 Failure, 1 Pending → Failure
						newJobStatus("aws-ovn-techpreview", "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview", 1, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						newJobStatus("aws-ovn-techpreview-serial-1of3", "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-1of3", 1, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						newJobStatus("aws-ovn-techpreview-serial-2of3", "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-2of3", 1, v1alpha1.JobStateFailure, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateFailure),
						}),
						newJobStatus("aws-ovn-techpreview-serial-3of3", "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-3of3", 1, v1alpha1.JobStatePending, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStatePending),
						}),
						// fips: All Success
						newJobStatus("aws-ovn-upgrade-4.20-micro-fips", "periodic-ci-openshift-release-master-nightly-4.20-e2e-aws-ovn-upgrade-fips", 1, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						newJobStatus("fips-scan", "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan", 1, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						// hypershift: Pending
						newJobStatus("hypershift-ovn-conformance-4.20", "periodic-ci-openshift-hypershift-release-4.20-periodics-e2e-aws-ovn-conformance", 2, v1alpha1.JobStatePending, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStatePending),
						}),
						// metal: 1 Success, 1 Failure → Failure
						newJobStatus("metal-ipi-ovn-bm", "periodic-ci-openshift-release-master-nightly-4.20-e2e-metal-ipi-ovn-bm", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						newJobStatus("metal-ipi-ovn-ipv6", "periodic-ci-openshift-release-master-nightly-4.20-e2e-metal-ipi-ovn-ipv6", 0, v1alpha1.JobStateFailure, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateFailure),
						}),
						// microshift: 1 Success, 1 Pending → Pending
						newJobStatus("microshift-ovn-conformance-parallel", "periodic-ci-openshift-microshift-release-4.20-periodics-e2e-aws-ovn-ocp-conformance", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						newJobStatus("microshift-ovn-conformance-serial", "periodic-ci-openshift-microshift-release-4.20-periodics-e2e-aws-ovn-ocp-conformance-serial", 0, v1alpha1.JobStatePending, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStatePending),
						}),
						// rosa: Success
						newJobStatus("rosa-classic-sts-conformance", "periodic-ci-openshift-release-master-nightly-4.20-e2e-rosa-sts-ovn", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
					},
				},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"techpreview": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "aws-ovn-techpreview", CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview"},
							{CIConfigurationName: "aws-ovn-techpreview-serial-1of3", CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-1of3"},
							{CIConfigurationName: "aws-ovn-techpreview-serial-2of3", CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-2of3"},
							{CIConfigurationName: "aws-ovn-techpreview-serial-3of3", CIConfigurationJobName: "periodic-ci-openshift-release-master-ci-4.20-e2e-aws-ovn-techpreview-serial-3of3"},
						},
						AggregateState:  v1alpha1.JobStateFailure, // One failed job causes entire qualifier to fail
						BadgeName:       "Tech Preview",
						BadgeEarned:     false, // Not earned because not all jobs succeeded
						BadgePropagated: false, // Not propagated because badge not earned
					},
					"fips": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "aws-ovn-upgrade-4.20-micro-fips", CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-e2e-aws-ovn-upgrade-fips"},
							{CIConfigurationName: "fips-scan", CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-fips-payload-scan"},
						},
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "FIPS",
						BadgeEarned:     true,
						BadgePropagated: true,
					},
					"hypershift": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "hypershift-ovn-conformance-4.20", CIConfigurationJobName: "periodic-ci-openshift-hypershift-release-4.20-periodics-e2e-aws-ovn-conformance"},
						},
						AggregateState:  v1alpha1.JobStatePending,
						BadgeName:       "HyperShift",
						BadgeEarned:     false,
						BadgePropagated: false,
					},
					"metal": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "metal-ipi-ovn-bm", CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-e2e-metal-ipi-ovn-bm"},
							{CIConfigurationName: "metal-ipi-ovn-ipv6", CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-e2e-metal-ipi-ovn-ipv6"},
						},
						AggregateState:  v1alpha1.JobStateFailure,
						BadgeName:       "Metal",
						BadgeEarned:     false,
						BadgePropagated: false,
					},
					"microshift": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "microshift-ovn-conformance-parallel", CIConfigurationJobName: "periodic-ci-openshift-microshift-release-4.20-periodics-e2e-aws-ovn-ocp-conformance"},
							{CIConfigurationName: "microshift-ovn-conformance-serial", CIConfigurationJobName: "periodic-ci-openshift-microshift-release-4.20-periodics-e2e-aws-ovn-ocp-conformance-serial"},
						},
						AggregateState:  v1alpha1.JobStatePending,
						BadgeName:       "MicroShift",
						BadgeEarned:     false,
						BadgePropagated: false,
					},
					"rosa": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "rosa-classic-sts-conformance", CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.20-e2e-rosa-sts-ovn"},
						},
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "ROSA",
						BadgeEarned:     true,
						BadgePropagated: true,
					},
				},
			},
		},
		{
			name: "EmptyPayload_NoQualifiers",
			configAccessor: &mockConfigAccessor{
				config: createBaseConfig(),
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{},
			},
		},
		{
			name: "JobQualifier_SkippedWhenNoGlobalConfigEntry",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"fips": {
						Enabled:            boolPtr(true),
						BadgeName:          "FIPS",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-fips-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
							{
								CIConfigurationName:    "orphan-job",
								CIConfigurationJobName: "periodic-ci-orphan-job",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"no-global-match": releasequalifiers.ReleaseQualifier{
										BadgeName: "Orphan",
									},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						newJobStatus("fips-scan", "periodic-ci-fips-scan", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
						newJobStatus("orphan-job", "periodic-ci-orphan-job", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
					},
				},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"fips": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "fips-scan", CIConfigurationJobName: "periodic-ci-fips-scan"},
						},
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "FIPS",
						BadgeEarned:     true,
						BadgePropagated: true,
					},
				},
			},
		},
		{
			name: "MultipleJobTypes_BlockingAndUpgradeJobs",
			configAccessor: &mockConfigAccessor{
				config: createBaseConfig(),
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
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
						newJobStatus("fips-blocking", "periodic-ci-openshift-release-master-blocking-fips", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
					},
					InformingJobResults: []v1alpha1.JobStatus{
						newJobStatus("fips-informing", "periodic-ci-openshift-release-master-informing-fips", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
					},
					UpgradeJobResults: []v1alpha1.JobStatus{
						newJobStatus("fips-upgrade", "periodic-ci-openshift-release-master-upgrade-fips", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
					},
				},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"fips": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "fips-blocking", CIConfigurationJobName: "periodic-ci-openshift-release-master-blocking-fips"},
							{CIConfigurationName: "fips-informing", CIConfigurationJobName: "periodic-ci-openshift-release-master-informing-fips"},
							{CIConfigurationName: "fips-upgrade", CIConfigurationJobName: "periodic-ci-openshift-release-master-upgrade-fips"},
						},
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "FIPS",
						BadgeEarned:     true,
						BadgePropagated: true,
					},
				},
			},
		},
		{
			name: "ApprovalQualifier_LabelPresent_BadgeEarned",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"sdn-migration": {
						Enabled:            boolPtr(true),
						Approval:           boolPtr(true),
						BadgeName:          "SDN Migration",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
					Labels: map[string]string{
						"release.openshift.io/sdn-migration_state": "Accepted",
					},
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"sdn-migration": {
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "SDN Migration",
						BadgeEarned:     true,
						BadgePropagated: true,
						Approval:        true,
					},
				},
			},
		},
		{
			name: "ApprovalQualifier_NoLabel_NotIncluded",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"sdn-migration": {
						Enabled:            boolPtr(true),
						Approval:           boolPtr(true),
						BadgeName:          "SDN Migration",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{},
			},
		},
		{
			name: "ApprovalQualifier_RejectedLabel_FailureState",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"sdn-migration": {
						Enabled:            boolPtr(true),
						Approval:           boolPtr(true),
						BadgeName:          "SDN Migration",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
					Labels: map[string]string{
						"release.openshift.io/sdn-migration_state": "Rejected",
					},
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"sdn-migration": {
						AggregateState:  v1alpha1.JobStateFailure,
						BadgeName:       "SDN Migration",
						BadgeEarned:     false,
						BadgePropagated: false,
						Approval:        true,
					},
				},
			},
		},
		{
			name: "ApprovalQualifier_UnknownLabelValue_NotIncluded",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"sdn-migration": {
						Enabled:            boolPtr(true),
						Approval:           boolPtr(true),
						BadgeName:          "SDN Migration",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
					Labels: map[string]string{
						"release.openshift.io/sdn-migration_state": "SomethingElse",
					},
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{},
			},
		},
		{
			name: "ApprovalQualifier_ApprovalFalse_NotProcessed",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"sdn-migration": {
						Enabled:            boolPtr(true),
						Approval:           boolPtr(false),
						BadgeName:          "SDN Migration",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
					Labels: map[string]string{
						"release.openshift.io/sdn-migration_state": "Accepted",
					},
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{},
			},
		},
		{
			name: "ApprovalQualifier_NoJobs_SummaryHasNoJobReferences",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"sdn-migration": {
						Enabled:            boolPtr(true),
						Approval:           boolPtr(true),
						BadgeName:          "SDN Migration",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusYes,
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
					Labels: map[string]string{
						"release.openshift.io/sdn-migration_state": "Accepted",
					},
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"sdn-migration": {
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "SDN Migration",
						BadgeEarned:     true,
						BadgePropagated: true,
						Approval:        true,
					},
				},
			},
		},
		{
			name: "ApprovalQualifier_SkippedWhenJobBasedEntryExists",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"fips": {
						Enabled:            boolPtr(true),
						Approval:           boolPtr(true),
						BadgeName:          "FIPS Approval",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
					Labels: map[string]string{
						"release.openshift.io/fips_state": "Accepted",
					},
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-fips-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						newJobStatus("fips-scan", "periodic-ci-fips-scan", 0, v1alpha1.JobStateFailure, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateFailure),
						}),
					},
				},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"fips": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "fips-scan", CIConfigurationJobName: "periodic-ci-fips-scan"},
						},
						AggregateState:  v1alpha1.JobStateFailure,
						BadgeName:       "FIPS Approval",
						BadgeEarned:     false,
						BadgePropagated: false,
					},
				},
			},
		},
		{
			name: "ApprovalQualifier_MixedWithJobQualifiers",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"fips": {
						Enabled:            boolPtr(true),
						BadgeName:          "FIPS",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
					"sdn-migration": {
						Enabled:            boolPtr(true),
						Approval:           boolPtr(true),
						BadgeName:          "SDN Migration",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
					Labels: map[string]string{
						"release.openshift.io/sdn-migration_state": "Accepted",
					},
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-fips-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						newJobStatus("fips-scan", "periodic-ci-fips-scan", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
					},
				},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"fips": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "fips-scan", CIConfigurationJobName: "periodic-ci-fips-scan"},
						},
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "FIPS",
						BadgeEarned:     true,
						BadgePropagated: true,
					},
					"sdn-migration": {
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "SDN Migration",
						BadgeEarned:     true,
						BadgePropagated: true,
						Approval:        true,
					},
				},
			},
		},
		{
			name: "FailureLabels_NoneConfigured_OmittedFromSummary",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"fips": {
						Enabled:            boolPtr(true),
						BadgeName:          "FIPS",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-fips-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						newJobStatus("fips-scan", "periodic-ci-fips-scan", 0, v1alpha1.JobStateFailure, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateFailure),
						}),
					},
				},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"fips": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "fips-scan", CIConfigurationJobName: "periodic-ci-fips-scan"},
						},
						AggregateState:  v1alpha1.JobStateFailure,
						BadgeName:       "FIPS",
						BadgeEarned:     false,
						BadgePropagated: false,
					},
				},
			},
		},
		{
			name: "FailureLabels_PopulatedOnFailure_OmittedOnSuccess",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"fips": {
						Enabled:            boolPtr(true),
						BadgeName:          "FIPS",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
						FailureLabels:      []string{"fips-failed", "needs-attention"},
					},
					"metal": {
						Enabled:            boolPtr(true),
						BadgeName:          "Metal",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
						FailureLabels:      []string{"metal-failed"},
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-fips-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
							{
								CIConfigurationName:    "metal-ipi",
								CIConfigurationJobName: "periodic-ci-metal-ipi",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"metal": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						newJobStatus("fips-scan", "periodic-ci-fips-scan", 0, v1alpha1.JobStateFailure, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateFailure),
						}),
						newJobStatus("metal-ipi", "periodic-ci-metal-ipi", 0, v1alpha1.JobStateSuccess, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateSuccess),
						}),
					},
				},
			},
			want: &v1alpha1.QualifiersSummary{
				FailureLabels: []string{"fips-failed", "needs-attention"},
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"fips": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "fips-scan", CIConfigurationJobName: "periodic-ci-fips-scan"},
						},
						AggregateState:  v1alpha1.JobStateFailure,
						BadgeName:       "FIPS",
						BadgeEarned:     false,
						BadgePropagated: false,
						FailureLabels:   []string{"fips-failed", "needs-attention"},
					},
					"metal": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "metal-ipi", CIConfigurationJobName: "periodic-ci-metal-ipi"},
						},
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "Metal",
						BadgeEarned:     true,
						BadgePropagated: true,
						FailureLabels:   []string{"metal-failed"},
					},
				},
			},
		},
		{
			name: "FailureLabels_OmittedOnPending",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"fips": {
						Enabled:            boolPtr(true),
						BadgeName:          "FIPS",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
						FailureLabels:      []string{"fips-failed"},
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-fips-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						newJobStatus("fips-scan", "periodic-ci-fips-scan", 0, v1alpha1.JobStatePending, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStatePending),
						}),
					},
				},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"fips": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "fips-scan", CIConfigurationJobName: "periodic-ci-fips-scan"},
						},
						AggregateState: v1alpha1.JobStatePending,
						BadgeName:      "FIPS",
						BadgeEarned:    false,
						FailureLabels:  []string{"fips-failed"},
					},
				},
			},
		},
		{
			name: "ApprovalQualifier_FailureLabels_PopulatedOnRejected_OmittedOnAccepted",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"sdn-migration": {
						Enabled:            boolPtr(true),
						Approval:           boolPtr(true),
						BadgeName:          "SDN Migration",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
						FailureLabels:      []string{"sdn-migration-rejected"},
					},
					"other-approval": {
						Enabled:            boolPtr(true),
						Approval:           boolPtr(true),
						BadgeName:          "Other Approval",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
						FailureLabels:      []string{"other-rejected"},
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
					Labels: map[string]string{
						"release.openshift.io/sdn-migration_state": "Rejected",
						"release.openshift.io/other-approval_state": "Accepted",
					},
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			want: &v1alpha1.QualifiersSummary{
				FailureLabels: []string{"sdn-migration-rejected"},
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"sdn-migration": {
						AggregateState:  v1alpha1.JobStateFailure,
						BadgeName:       "SDN Migration",
						BadgeEarned:     false,
						BadgePropagated: false,
						Approval:        true,
						FailureLabels:   []string{"sdn-migration-rejected"},
					},
					"other-approval": {
						AggregateState:  v1alpha1.JobStateSuccess,
						BadgeName:       "Other Approval",
						BadgeEarned:     true,
						BadgePropagated: true,
						Approval:        true,
						FailureLabels:   []string{"other-rejected"},
					},
				},
			},
		},
		{
			name: "ApprovalQualifier_Rejected_OnFailureBadgePropagated",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"sdn-migration": {
						Enabled:            boolPtr(true),
						Approval:           boolPtr(true),
						BadgeName:          "SDN Migration",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnFailure,
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
					Labels: map[string]string{
						"release.openshift.io/sdn-migration_state": "Rejected",
					},
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			want: &v1alpha1.QualifiersSummary{
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"sdn-migration": {
						AggregateState:  v1alpha1.JobStateFailure,
						BadgeName:       "SDN Migration",
						BadgeEarned:     false,
						BadgePropagated: true,
						Approval:        true,
					},
				},
			},
		},
		{
			name: "FailureLabels_MultipleQualifiersFailing_AggregatedAndSorted",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"fips": {
						Enabled:            boolPtr(true),
						BadgeName:          "FIPS",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
						FailureLabels:      []string{"needs-attention", "fips-failed"},
					},
					"metal": {
						Enabled:            boolPtr(true),
						BadgeName:          "Metal",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
						FailureLabels:      []string{"metal-failed", "hardware-issue"},
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-fips-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
							{
								CIConfigurationName:    "metal-ipi",
								CIConfigurationJobName: "periodic-ci-metal-ipi",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"metal": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						newJobStatus("fips-scan", "periodic-ci-fips-scan", 0, v1alpha1.JobStateFailure, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateFailure),
						}),
						newJobStatus("metal-ipi", "periodic-ci-metal-ipi", 0, v1alpha1.JobStateFailure, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateFailure),
						}),
					},
				},
			},
			want: &v1alpha1.QualifiersSummary{
				FailureLabels: []string{"fips-failed", "hardware-issue", "metal-failed", "needs-attention"},
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"fips": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "fips-scan", CIConfigurationJobName: "periodic-ci-fips-scan"},
						},
						AggregateState:  v1alpha1.JobStateFailure,
						BadgeName:       "FIPS",
						BadgeEarned:     false,
						BadgePropagated: false,
						FailureLabels:   []string{"needs-attention", "fips-failed"},
					},
					"metal": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "metal-ipi", CIConfigurationJobName: "periodic-ci-metal-ipi"},
						},
						AggregateState:  v1alpha1.JobStateFailure,
						BadgeName:       "Metal",
						BadgeEarned:     false,
						BadgePropagated: false,
						FailureLabels:   []string{"metal-failed", "hardware-issue"},
					},
				},
			},
		},
		{
			name: "FailureLabels_DuplicatesAcrossQualifiers_Deduplicated",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"fips": {
						Enabled:            boolPtr(true),
						BadgeName:          "FIPS",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
						FailureLabels:      []string{"prevent_rc", "needs-attention"},
					},
					"metal": {
						Enabled:            boolPtr(true),
						BadgeName:          "Metal",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
						FailureLabels:      []string{"prevent_rc", "metal-failed"},
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-fips-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
							{
								CIConfigurationName:    "metal-ipi",
								CIConfigurationJobName: "periodic-ci-metal-ipi",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"metal": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						newJobStatus("fips-scan", "periodic-ci-fips-scan", 0, v1alpha1.JobStateFailure, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateFailure),
						}),
						newJobStatus("metal-ipi", "periodic-ci-metal-ipi", 0, v1alpha1.JobStateFailure, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateFailure),
						}),
					},
				},
			},
			want: &v1alpha1.QualifiersSummary{
				FailureLabels: []string{"metal-failed", "needs-attention", "prevent_rc"},
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"fips": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "fips-scan", CIConfigurationJobName: "periodic-ci-fips-scan"},
						},
						AggregateState:  v1alpha1.JobStateFailure,
						BadgeName:       "FIPS",
						BadgeEarned:     false,
						BadgePropagated: false,
						FailureLabels:   []string{"prevent_rc", "needs-attention"},
					},
					"metal": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "metal-ipi", CIConfigurationJobName: "periodic-ci-metal-ipi"},
						},
						AggregateState:  v1alpha1.JobStateFailure,
						BadgeName:       "Metal",
						BadgeEarned:     false,
						BadgePropagated: false,
						FailureLabels:   []string{"prevent_rc", "metal-failed"},
					},
				},
			},
		},
		{
			name: "FailureLabels_MixedJobAndApproval_BothContribute",
			configAccessor: &mockConfigAccessor{
				config: releasequalifiers.ReleaseQualifiers{
					"fips": {
						Enabled:            boolPtr(true),
						BadgeName:          "FIPS",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
						FailureLabels:      []string{"fips-failed"},
					},
					"sdn-migration": {
						Enabled:            boolPtr(true),
						Approval:           boolPtr(true),
						BadgeName:          "SDN Migration",
						PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
						FailureLabels:      []string{"sdn-rejected"},
					},
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "4.20.8",
					Namespace: "ocp",
					Labels: map[string]string{
						"release.openshift.io/sdn-migration_state": "Rejected",
					},
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "fips-scan",
								CIConfigurationJobName: "periodic-ci-fips-scan",
								Qualifiers: releasequalifiers.ReleaseQualifiers{
									"fips": releasequalifiers.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						newJobStatus("fips-scan", "periodic-ci-fips-scan", 0, v1alpha1.JobStateFailure, []v1alpha1.JobRunResult{
							newJobRunResult(v1alpha1.JobRunStateFailure),
						}),
					},
				},
			},
			want: &v1alpha1.QualifiersSummary{
				FailureLabels: []string{"fips-failed", "sdn-rejected"},
				Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
					"fips": {
						Jobs: []v1alpha1.ReleaseQualifierJobReference{
							{CIConfigurationName: "fips-scan", CIConfigurationJobName: "periodic-ci-fips-scan"},
						},
						AggregateState:  v1alpha1.JobStateFailure,
						BadgeName:       "FIPS",
						BadgeEarned:     false,
						BadgePropagated: false,
						FailureLabels:   []string{"fips-failed"},
					},
					"sdn-migration": {
						AggregateState:  v1alpha1.JobStateFailure,
						BadgeName:       "SDN Migration",
						BadgeEarned:     false,
						BadgePropagated: false,
						Approval:        true,
						FailureLabels:   []string{"sdn-rejected"},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateQualifiersSummary(tt.payload, tt.configAccessor)
			// Sort job references for deterministic comparison (map iteration order is non-deterministic)
			for id, s := range got.Qualifiers {
				sort.Sort(ByQualifierJobReferenceName(s.Jobs))
				got.Qualifiers[id] = s
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GenerateQualifiersSummary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateQualifiersSummary_PreservesJiraNotifications(t *testing.T) {
	config := &mockConfigAccessor{
		config: releasequalifiers.ReleaseQualifiers{
			"rosa": {
				Enabled:            boolPtr(true),
				BadgeName:          "ROSA",
				PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
			},
		},
	}

	payload := &v1alpha1.ReleasePayload{
		ObjectMeta: v1.ObjectMeta{Name: "test", Namespace: "ocp"},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
				InformingJobs: []v1alpha1.CIConfiguration{
					{
						CIConfigurationName:    "rosa-hcp",
						CIConfigurationJobName: "periodic-ci-rosa-hcp",
						Qualifiers: releasequalifiers.ReleaseQualifiers{
							"rosa": {},
						},
					},
				},
			},
		},
		Status: v1alpha1.ReleasePayloadStatus{
			InformingJobResults: []v1alpha1.JobStatus{
				newJobStatus("rosa-hcp", "periodic-ci-rosa-hcp", 0, v1alpha1.JobStateSuccess, nil),
			},
		},
	}

	payload.Status.QualifiersSummary = &v1alpha1.QualifiersSummary{
		Qualifiers: map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary{
			"rosa": {
				JiraNotifications: map[string]v1alpha1.JiraNotificationState{
					"ocp-rosa-OCPBUGS--default": {
						IssueKey:         "OCPBUGS-42",
						ActiveEscalation: "critical",
						ActivePriority:   "Critical",
					},
				},
			},
		},
	}

	result := GenerateQualifiersSummary(payload, config)

	rosaSummary, ok := result.Qualifiers["rosa"]
	if !ok {
		t.Fatal("Expected 'rosa' qualifier in result")
	}

	if len(rosaSummary.JiraNotifications) == 0 {
		t.Fatal("Expected JiraNotifications to be preserved from existing summary")
	}

	state, ok := rosaSummary.JiraNotifications["ocp-rosa-OCPBUGS--default"]
	if !ok {
		t.Fatal("Expected notification state for thread 'ocp-rosa-OCPBUGS--default' to be preserved")
	}

	if state.IssueKey != "OCPBUGS-42" {
		t.Errorf("Expected IssueKey=OCPBUGS-42, got %s", state.IssueKey)
	}
	if state.ActiveEscalation != "critical" {
		t.Errorf("Expected ActiveEscalation=critical, got %s", state.ActiveEscalation)
	}
}

func TestGenerateQualifiersSummary_NilExistingSummary(t *testing.T) {
	config := &mockConfigAccessor{
		config: releasequalifiers.ReleaseQualifiers{
			"rosa": {
				Enabled:            boolPtr(true),
				BadgeName:          "ROSA",
				PayloadBadgeStatus: releasequalifiers.BadgeStatusOnSuccess,
			},
		},
	}

	payload := &v1alpha1.ReleasePayload{
		ObjectMeta: v1.ObjectMeta{Name: "test", Namespace: "ocp"},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
				InformingJobs: []v1alpha1.CIConfiguration{
					{
						CIConfigurationName:    "rosa-hcp",
						CIConfigurationJobName: "periodic-ci-rosa-hcp",
						Qualifiers: releasequalifiers.ReleaseQualifiers{
							"rosa": {},
						},
					},
				},
			},
		},
		Status: v1alpha1.ReleasePayloadStatus{
			InformingJobResults: []v1alpha1.JobStatus{
				newJobStatus("rosa-hcp", "periodic-ci-rosa-hcp", 0, v1alpha1.JobStateSuccess, nil),
			},
		},
	}

	result := GenerateQualifiersSummary(payload, config)

	rosaSummary, ok := result.Qualifiers["rosa"]
	if !ok {
		t.Fatal("Expected 'rosa' qualifier in result")
	}

	if len(rosaSummary.JiraNotifications) != 0 {
		t.Errorf("Expected empty JiraNotifications for new qualifier, got %v", rosaSummary.JiraNotifications)
	}
}
