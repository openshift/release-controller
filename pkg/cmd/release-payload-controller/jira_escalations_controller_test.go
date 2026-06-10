package release_payload_controller

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	gojira "github.com/andygrunwald/go-jira"
	"github.com/google/go-cmp/cmp"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/bigquery"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/fake"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	releasequalifierslib "github.com/openshift/release-controller/pkg/releasequalifiers"
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications"
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/jira"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/clock"
	"sigs.k8s.io/prow/pkg/jira/fakejira"
)

func completionTimeMinutesAgo(minutesAgo int) civil.DateTime {
	return civil.DateTimeOf(time.Now().Add(-time.Duration(minutesAgo) * time.Minute))
}

func TestNewJiraEscalationsController(t *testing.T) {
	t.Parallel()

	objs := []runtime.Object{}
	client := releasepayloadclient.NewSimpleClientset(objs...)
	informerFactory := releasepayloadinformers.NewSharedInformerFactory(client, 0)
	releasePayloadInformer := informerFactory.Release().V1alpha1().ReleasePayloads()

	eventRecorder := events.NewInMemoryRecorder("test", clock.RealClock{})
	configAccessor := &mockConfigAccessor{}
	bqClient := &bigquery.FakeClient{}

	controller, err := NewJiraEscalationsController(
		releasePayloadInformer,
		client.ReleaseV1alpha1(),
		eventRecorder,
		configAccessor,
		bqClient,
		nil,
	)

	if err != nil {
		t.Fatalf("Failed to create controller: %v", err)
	}

	if controller == nil {
		t.Fatal("Expected non-nil controller")
	}

	if controller.configAccessor == nil {
		t.Error("Expected configAccessor to be set")
	}

	if controller.bigQueryClient == nil {
		t.Error("Expected bigQueryClient to be set")
	}
}

func TestGetWindowSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		escalation jira.Escalation
		want       int
	}{
		{
			name: "OverLastRuns specified",
			escalation: jira.Escalation{
				OverLastRuns: intPtr(20),
				Failures:     5,
			},
			want: 20,
		},
		{
			name: "OverLastRuns not specified, use Failures",
			escalation: jira.Escalation{
				Failures: 10,
			},
			want: 10,
		},
		{
			name: "Neither specified, use default",
			escalation: jira.Escalation{
				Name: "test",
			},
			want: 10,
		},
	}

	controller := &JiraEscalationsController{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := controller.getWindowSize(tt.escalation)
			if got != tt.want {
				t.Errorf("getWindowSize() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEvaluatePassPercentage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		results   []bigquery.ReleaseQualifiersProwjobSummaryResult
		threshold int
		want      bool
	}{
		{
			name: "Pass percentage below threshold",
			results: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "test-job", State: "success", URL: "https://prow.ci/1"},
				{Release: "4.19.0-0.nightly-2", Name: "test-job", State: "failure", URL: "https://prow.ci/2"},
				{Release: "4.19.0-0.nightly-3", Name: "test-job", State: "failure", URL: "https://prow.ci/3"},
				{Release: "4.19.0-0.nightly-4", Name: "test-job", State: "success", URL: "https://prow.ci/4"},
				{Release: "4.19.0-0.nightly-5", Name: "test-job", State: "failure", URL: "https://prow.ci/5"},
				{Release: "4.19.0-0.nightly-6", Name: "test-job", State: "failure", URL: "https://prow.ci/6"},
				{Release: "4.19.0-0.nightly-7", Name: "test-job", State: "failure", URL: "https://prow.ci/7"},
				{Release: "4.19.0-0.nightly-8", Name: "test-job", State: "failure", URL: "https://prow.ci/8"},
				{Release: "4.19.0-0.nightly-9", Name: "test-job", State: "success", URL: "https://prow.ci/9"},
				{Release: "4.19.0-0.nightly-10", Name: "test-job", State: "failure", URL: "https://prow.ci/10"},
			},
			threshold: 60,
			want:      true, // 30% pass rate < 60%
		},
		{
			name: "Pass percentage above threshold",
			results: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "test-job", State: "success", URL: "https://prow.ci/1"},
				{Release: "4.19.0-0.nightly-2", Name: "test-job", State: "success", URL: "https://prow.ci/2"},
				{Release: "4.19.0-0.nightly-3", Name: "test-job", State: "success", URL: "https://prow.ci/3"},
				{Release: "4.19.0-0.nightly-4", Name: "test-job", State: "success", URL: "https://prow.ci/4"},
				{Release: "4.19.0-0.nightly-5", Name: "test-job", State: "success", URL: "https://prow.ci/5"},
				{Release: "4.19.0-0.nightly-6", Name: "test-job", State: "success", URL: "https://prow.ci/6"},
				{Release: "4.19.0-0.nightly-7", Name: "test-job", State: "success", URL: "https://prow.ci/7"},
				{Release: "4.19.0-0.nightly-8", Name: "test-job", State: "failure", URL: "https://prow.ci/8"},
				{Release: "4.19.0-0.nightly-9", Name: "test-job", State: "failure", URL: "https://prow.ci/9"},
				{Release: "4.19.0-0.nightly-10", Name: "test-job", State: "failure", URL: "https://prow.ci/10"},
			},
			threshold: 60,
			want:      false, // 70% pass rate > 60%
		},
		{
			name: "Pass percentage equal to threshold",
			results: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "test-job", State: "success", URL: "https://prow.ci/1"},
				{Release: "4.19.0-0.nightly-2", Name: "test-job", State: "success", URL: "https://prow.ci/2"},
				{Release: "4.19.0-0.nightly-3", Name: "test-job", State: "success", URL: "https://prow.ci/3"},
				{Release: "4.19.0-0.nightly-4", Name: "test-job", State: "failure", URL: "https://prow.ci/4"},
				{Release: "4.19.0-0.nightly-5", Name: "test-job", State: "failure", URL: "https://prow.ci/5"},
			},
			threshold: 60,
			want:      false, // 60% pass rate == 60% (not less than)
		},
		{
			name:      "Empty results",
			results:   []bigquery.ReleaseQualifiersProwjobSummaryResult{},
			threshold: 60,
			want:      false,
		},
	}

	controller := &JiraEscalationsController{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := controller.evaluatePassPercentage(tt.results, tt.threshold)
			if got != tt.want {
				t.Errorf("evaluatePassPercentage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluateFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		results    []bigquery.ReleaseQualifiersProwjobSummaryResult
		threshold  int
		windowSize int
		want       bool
	}{
		{
			name: "Consecutive failures - threshold met",
			results: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "test-job", State: "failure", URL: "https://prow.ci/1"},
				{Release: "4.19.0-0.nightly-2", Name: "test-job", State: "failure", URL: "https://prow.ci/2"},
				{Release: "4.19.0-0.nightly-3", Name: "test-job", State: "failure", URL: "https://prow.ci/3"},
				{Release: "4.19.0-0.nightly-4", Name: "test-job", State: "success", URL: "https://prow.ci/4"},
			},
			threshold:  3,
			windowSize: 3,
			want:       true,
		},
		{
			name: "Consecutive failures - threshold not met",
			results: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "test-job", State: "failure", URL: "https://prow.ci/1"},
				{Release: "4.19.0-0.nightly-2", Name: "test-job", State: "success", URL: "https://prow.ci/2"},
				{Release: "4.19.0-0.nightly-3", Name: "test-job", State: "failure", URL: "https://prow.ci/3"},
				{Release: "4.19.0-0.nightly-4", Name: "test-job", State: "failure", URL: "https://prow.ci/4"},
			},
			threshold:  3,
			windowSize: 3,
			want:       false,
		},
		{
			name: "Total failures in window - threshold met",
			results: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "test-job", State: "failure", URL: "https://prow.ci/1"},
				{Release: "4.19.0-0.nightly-2", Name: "test-job", State: "success", URL: "https://prow.ci/2"},
				{Release: "4.19.0-0.nightly-3", Name: "test-job", State: "failure", URL: "https://prow.ci/3"},
				{Release: "4.19.0-0.nightly-4", Name: "test-job", State: "success", URL: "https://prow.ci/4"},
				{Release: "4.19.0-0.nightly-5", Name: "test-job", State: "failure", URL: "https://prow.ci/5"},
				{Release: "4.19.0-0.nightly-6", Name: "test-job", State: "failure", URL: "https://prow.ci/6"},
				{Release: "4.19.0-0.nightly-7", Name: "test-job", State: "success", URL: "https://prow.ci/7"},
				{Release: "4.19.0-0.nightly-8", Name: "test-job", State: "failure", URL: "https://prow.ci/8"},
				{Release: "4.19.0-0.nightly-9", Name: "test-job", State: "success", URL: "https://prow.ci/9"},
				{Release: "4.19.0-0.nightly-10", Name: "test-job", State: "failure", URL: "https://prow.ci/10"},
			},
			threshold:  6,
			windowSize: 10,
			want:       true,
		},
		{
			name: "Total failures in window - threshold not met",
			results: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "test-job", State: "failure", URL: "https://prow.ci/1"},
				{Release: "4.19.0-0.nightly-2", Name: "test-job", State: "success", URL: "https://prow.ci/2"},
				{Release: "4.19.0-0.nightly-3", Name: "test-job", State: "failure", URL: "https://prow.ci/3"},
				{Release: "4.19.0-0.nightly-4", Name: "test-job", State: "success", URL: "https://prow.ci/4"},
				{Release: "4.19.0-0.nightly-5", Name: "test-job", State: "success", URL: "https://prow.ci/5"},
				{Release: "4.19.0-0.nightly-6", Name: "test-job", State: "success", URL: "https://prow.ci/6"},
				{Release: "4.19.0-0.nightly-7", Name: "test-job", State: "success", URL: "https://prow.ci/7"},
				{Release: "4.19.0-0.nightly-8", Name: "test-job", State: "success", URL: "https://prow.ci/8"},
				{Release: "4.19.0-0.nightly-9", Name: "test-job", State: "success", URL: "https://prow.ci/9"},
				{Release: "4.19.0-0.nightly-10", Name: "test-job", State: "success", URL: "https://prow.ci/10"},
			},
			threshold:  6,
			windowSize: 10,
			want:       false,
		},
		{
			name:       "Empty results",
			results:    []bigquery.ReleaseQualifiersProwjobSummaryResult{},
			threshold:  3,
			windowSize: 3,
			want:       false,
		},
	}

	controller := &JiraEscalationsController{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := controller.evaluateFailures(tt.results, tt.threshold, tt.windowSize)
			if got != tt.want {
				t.Errorf("evaluateFailures() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeJiraConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		globalConfig releasequalifierslib.ReleaseQualifier
		jobConfig    releasequalifierslib.ReleaseQualifier
		want         *jira.Notification
	}{
		{
			name: "Global config only",
			globalConfig: releasequalifierslib.ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project:   "GLOBAL",
						Component: "GlobalComponent",
						Assignee:  "global@example.com",
					},
				},
			},
			jobConfig: releasequalifierslib.ReleaseQualifier{},
			want: &jira.Notification{
				Project:   "GLOBAL",
				Component: "GlobalComponent",
				Assignee:  "global@example.com",
			},
		},
		{
			name:         "Job config only",
			globalConfig: releasequalifierslib.ReleaseQualifier{},
			jobConfig: releasequalifierslib.ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project:   "JOB",
						Component: "JobComponent",
						Thread:    "job-thread",
					},
				},
			},
			want: &jira.Notification{
				Project:     "JOB",
				Component:   "JobComponent",
				Thread:      "job-thread",
				Escalations: []jira.Escalation{},
			},
		},
		{
			name: "Job config overrides global",
			globalConfig: releasequalifierslib.ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project:     "GLOBAL",
						Component:   "GlobalComponent",
						Assignee:    "global@example.com",
						Summary:     "Global Summary",
						Description: "Global Description",
					},
				},
			},
			jobConfig: releasequalifierslib.ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project:   "JOB",
						Component: "JobComponent",
						Thread:    "job-thread",
					},
				},
			},
			want: &jira.Notification{
				Project:     "JOB",
				Component:   "JobComponent",
				Assignee:    "global@example.com",
				Summary:     "Global Summary",
				Description: "Global Description",
				Thread:      "job-thread",
				Escalations: []jira.Escalation{},
			},
		},
		{
			name:         "Neither has Jira config",
			globalConfig: releasequalifierslib.ReleaseQualifier{},
			jobConfig:    releasequalifierslib.ReleaseQualifier{},
			want:         nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := tt.globalConfig.Merge(tt.jobConfig)
			var got *jira.Notification
			if merged.Notifications != nil {
				got = merged.Notifications.Jira
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Merge() Jira config mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetJobQualifiers(t *testing.T) {
	t.Parallel()

	releasePayload := &v1alpha1.ReleasePayload{
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
			PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
				BlockingJobs: []v1alpha1.CIConfiguration{
					{
						CIConfigurationName: "blocking-job-1",
						Qualifiers: releasequalifierslib.ReleaseQualifiers{
							"rosa": releasequalifierslib.ReleaseQualifier{
								BadgeName: "ROSA",
							},
						},
					},
				},
				InformingJobs: []v1alpha1.CIConfiguration{
					{
						CIConfigurationName: "informing-job-1",
						Qualifiers: releasequalifierslib.ReleaseQualifiers{
							"osd": releasequalifierslib.ReleaseQualifier{
								BadgeName: "OSD",
							},
						},
					},
				},
				UpgradeJobs: []v1alpha1.CIConfiguration{
					{
						CIConfigurationName: "upgrade-job-1",
						Qualifiers: releasequalifierslib.ReleaseQualifiers{
							"telco": releasequalifierslib.ReleaseQualifier{
								BadgeName: "Telco",
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name                string
		ciConfigurationName string
		want                releasequalifierslib.ReleaseQualifiers
	}{
		{
			name:                "Blocking job qualifiers",
			ciConfigurationName: "blocking-job-1",
			want: releasequalifierslib.ReleaseQualifiers{
				"rosa": releasequalifierslib.ReleaseQualifier{
					BadgeName: "ROSA",
				},
			},
		},
		{
			name:                "Informing job qualifiers",
			ciConfigurationName: "informing-job-1",
			want: releasequalifierslib.ReleaseQualifiers{
				"osd": releasequalifierslib.ReleaseQualifier{
					BadgeName: "OSD",
				},
			},
		},
		{
			name:                "Upgrade job qualifiers",
			ciConfigurationName: "upgrade-job-1",
			want: releasequalifierslib.ReleaseQualifiers{
				"telco": releasequalifierslib.ReleaseQualifier{
					BadgeName: "Telco",
				},
			},
		},
		{
			name:                "Non-existent job",
			ciConfigurationName: "non-existent",
			want:                nil,
		},
	}

	controller := &JiraEscalationsController{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := controller.getJobQualifiers(releasePayload, tt.ciConfigurationName)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("getJobQualifiers() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildThreadID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		releasePayload *v1alpha1.ReleasePayload
		qualifierID    releasequalifierslib.QualifierId
		jiraConfig     *jira.Notification
		want           string
	}{
		{
			name: "With thread specified",
			releasePayload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ocp"},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
				},
			},
			qualifierID: "rosa",
			jiraConfig: &jira.Notification{
				Project:   "OCPBUGS",
				Component: "installer",
				Thread:    "rosa-thread",
			},
			want: "4.19.0-0.nightly-rosa-OCPBUGS-installer-rosa-thread",
		},
		{
			name: "Without thread specified",
			releasePayload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ocp"},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
				},
			},
			qualifierID: "osd",
			jiraConfig: &jira.Notification{
				Project:   "OCPBUGS",
				Component: "installer",
			},
			want: "4.19.0-0.nightly-osd-OCPBUGS-installer-",
		},
		{
			name: "Empty StreamName defaults to unknown",
			releasePayload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ocp"},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{},
				},
			},
			qualifierID: "rosa",
			jiraConfig: &jira.Notification{
				Project:   "OCPBUGS",
				Component: "installer",
				Thread:    "rosa-thread",
			},
			want: "unknown-rosa-OCPBUGS-installer-rosa-thread",
		},
	}

	controller := &JiraEscalationsController{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := controller.buildThreadID(tt.releasePayload, tt.qualifierID, tt.jiraConfig)
			if got != tt.want {
				t.Errorf("buildThreadID() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestShouldTriggerEscalation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		escalation jira.Escalation
		jobHistory []bigquery.ReleaseQualifiersProwjobSummaryResult
		want       bool
	}{
		{
			name: "PassPercentage - threshold breached",
			escalation: jira.Escalation{
				Name:           "quality",
				OverLastRuns:   intPtr(10),
				PassPercentage: intPtr(60),
				Priority:       "High",
			},
			jobHistory: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "test-job", State: "success", URL: "https://prow.ci/1"},
				{Release: "4.19.0-0.nightly-2", Name: "test-job", State: "failure", URL: "https://prow.ci/2"},
				{Release: "4.19.0-0.nightly-3", Name: "test-job", State: "failure", URL: "https://prow.ci/3"},
				{Release: "4.19.0-0.nightly-4", Name: "test-job", State: "failure", URL: "https://prow.ci/4"},
				{Release: "4.19.0-0.nightly-5", Name: "test-job", State: "failure", URL: "https://prow.ci/5"},
				{Release: "4.19.0-0.nightly-6", Name: "test-job", State: "failure", URL: "https://prow.ci/6"},
				{Release: "4.19.0-0.nightly-7", Name: "test-job", State: "failure", URL: "https://prow.ci/7"},
				{Release: "4.19.0-0.nightly-8", Name: "test-job", State: "failure", URL: "https://prow.ci/8"},
				{Release: "4.19.0-0.nightly-9", Name: "test-job", State: "success", URL: "https://prow.ci/9"},
				{Release: "4.19.0-0.nightly-10", Name: "test-job", State: "failure", URL: "https://prow.ci/10"},
			},
			want: true, // 20% pass < 60%
		},
		{
			name: "Consecutive failures - threshold met",
			escalation: jira.Escalation{
				Name:     "consecutive",
				Failures: 3,
				Priority: "Critical",
			},
			jobHistory: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "test-job", State: "failure", URL: "https://prow.ci/1"},
				{Release: "4.19.0-0.nightly-2", Name: "test-job", State: "failure", URL: "https://prow.ci/2"},
				{Release: "4.19.0-0.nightly-3", Name: "test-job", State: "failure", URL: "https://prow.ci/3"},
				{Release: "4.19.0-0.nightly-4", Name: "test-job", State: "success", URL: "https://prow.ci/4"},
			},
			want: true,
		},
		{
			name: "Failures in window - threshold met",
			escalation: jira.Escalation{
				Name:         "window",
				Failures:     5,
				OverLastRuns: intPtr(10),
				Priority:     "High",
			},
			jobHistory: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "test-job", State: "failure", URL: "https://prow.ci/1"},
				{Release: "4.19.0-0.nightly-2", Name: "test-job", State: "success", URL: "https://prow.ci/2"},
				{Release: "4.19.0-0.nightly-3", Name: "test-job", State: "failure", URL: "https://prow.ci/3"},
				{Release: "4.19.0-0.nightly-4", Name: "test-job", State: "success", URL: "https://prow.ci/4"},
				{Release: "4.19.0-0.nightly-5", Name: "test-job", State: "failure", URL: "https://prow.ci/5"},
				{Release: "4.19.0-0.nightly-6", Name: "test-job", State: "failure", URL: "https://prow.ci/6"},
				{Release: "4.19.0-0.nightly-7", Name: "test-job", State: "success", URL: "https://prow.ci/7"},
				{Release: "4.19.0-0.nightly-8", Name: "test-job", State: "failure", URL: "https://prow.ci/8"},
				{Release: "4.19.0-0.nightly-9", Name: "test-job", State: "success", URL: "https://prow.ci/9"},
				{Release: "4.19.0-0.nightly-10", Name: "test-job", State: "success", URL: "https://prow.ci/10"},
			},
			want: true, // 5 failures in 10 runs
		},
		{
			name: "Empty job history",
			escalation: jira.Escalation{
				Name:     "test",
				Failures: 3,
				Priority: "Low",
			},
			jobHistory: []bigquery.ReleaseQualifiersProwjobSummaryResult{},
			want:       false,
		},
	}

	controller := &JiraEscalationsController{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := controller.shouldTriggerEscalation(tt.escalation, tt.jobHistory)
			if got != tt.want {
				t.Errorf("shouldTriggerEscalation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCollectJobNamesWithEscalations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		releasePayload   *v1alpha1.ReleasePayload
		qualifiersConfig releasequalifierslib.ReleaseQualifiers
		wantDefault      []string
		wantFiltered     []bigquery.ProwjobQueryFilter
	}{
		{
			name: "Jobs with Jira escalations - no OverPeriod",
			releasePayload: &v1alpha1.ReleasePayload{
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "rosa-hcp",
								CIConfigurationJobName: "periodic-ci-rosa-hcp",
								Qualifiers: releasequalifierslib.ReleaseQualifiers{
									"rosa": releasequalifierslib.ReleaseQualifier{},
								},
							},
						},
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "osd-aws",
								CIConfigurationJobName: "periodic-ci-osd-aws",
								Qualifiers: releasequalifierslib.ReleaseQualifiers{
									"osd": releasequalifierslib.ReleaseQualifier{},
								},
							},
						},
					},
				},
			},
			qualifiersConfig: releasequalifierslib.ReleaseQualifiers{
				"rosa": releasequalifierslib.ReleaseQualifier{
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "OCPBUGS",
							Escalations: []jira.Escalation{
								{Name: "low", Failures: 2, Priority: "Low"},
							},
						},
					},
				},
				"osd": releasequalifierslib.ReleaseQualifier{
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "OCPBUGS",
							Escalations: []jira.Escalation{
								{Name: "high", Failures: 5, Priority: "High"},
							},
						},
					},
				},
			},
			wantDefault:  []string{"periodic-ci-rosa-hcp", "periodic-ci-osd-aws"},
			wantFiltered: nil,
		},
		{
			name: "Jobs without Jira escalations",
			releasePayload: &v1alpha1.ReleasePayload{
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "test-job",
								CIConfigurationJobName: "periodic-ci-test",
								Qualifiers: releasequalifierslib.ReleaseQualifiers{
									"test": releasequalifierslib.ReleaseQualifier{},
								},
							},
						},
					},
				},
			},
			qualifiersConfig: releasequalifierslib.ReleaseQualifiers{
				"test": releasequalifierslib.ReleaseQualifier{
					BadgeName: "Test",
				},
			},
			wantDefault:  nil,
			wantFiltered: nil,
		},
		{
			name: "Mixed - default and filtered jobs",
			releasePayload: &v1alpha1.ReleasePayload{
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "rosa-hcp",
								CIConfigurationJobName: "periodic-ci-rosa-hcp",
								Qualifiers: releasequalifierslib.ReleaseQualifiers{
									"rosa": releasequalifierslib.ReleaseQualifier{},
								},
							},
							{
								CIConfigurationName:    "osd-aws",
								CIConfigurationJobName: "periodic-ci-osd-aws",
								Qualifiers: releasequalifierslib.ReleaseQualifiers{
									"osd": releasequalifierslib.ReleaseQualifier{},
								},
							},
							{
								CIConfigurationName:    "test-job",
								CIConfigurationJobName: "periodic-ci-test",
								Qualifiers: releasequalifierslib.ReleaseQualifiers{
									"test": releasequalifierslib.ReleaseQualifier{},
								},
							},
						},
					},
				},
			},
			qualifiersConfig: releasequalifierslib.ReleaseQualifiers{
				"rosa": releasequalifierslib.ReleaseQualifier{
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "OCPBUGS",
							Escalations: []jira.Escalation{
								{Name: "low", Failures: 2, Priority: "Low"},
							},
						},
					},
				},
				"osd": releasequalifierslib.ReleaseQualifier{
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "OCPBUGS",
							Escalations: []jira.Escalation{
								{Name: "quality", OverLastRuns: intPtr(20), OverPeriod: "2d", PassPercentage: intPtr(80), Priority: "Critical"},
							},
						},
					},
				},
				"test": releasequalifierslib.ReleaseQualifier{
					BadgeName: "Test",
				},
			},
			wantDefault: []string{"periodic-ci-rosa-hcp"},
			wantFiltered: []bigquery.ProwjobQueryFilter{
				{Name: "periodic-ci-osd-aws", Interval: "2 DAY"},
			},
		},
		{
			name: "Job with OverPeriod in weeks",
			releasePayload: &v1alpha1.ReleasePayload{
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "rosa-hcp",
								CIConfigurationJobName: "periodic-ci-rosa-hcp",
								Qualifiers: releasequalifierslib.ReleaseQualifiers{
									"rosa": releasequalifierslib.ReleaseQualifier{},
								},
							},
						},
					},
				},
			},
			qualifiersConfig: releasequalifierslib.ReleaseQualifiers{
				"rosa": releasequalifierslib.ReleaseQualifier{
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "OCPBUGS",
							Escalations: []jira.Escalation{
								{Name: "weekly", Failures: 10, OverLastRuns: intPtr(50), OverPeriod: "1w", Priority: "Major"},
							},
						},
					},
				},
			},
			wantDefault: nil,
			wantFiltered: []bigquery.ProwjobQueryFilter{
				{Name: "periodic-ci-rosa-hcp", Interval: "7 DAY"},
			},
		},
		{
			name: "Job with multiple qualifiers uses largest interval",
			releasePayload: &v1alpha1.ReleasePayload{
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "multi-qual",
								CIConfigurationJobName: "periodic-ci-multi-qual",
								Qualifiers: releasequalifierslib.ReleaseQualifiers{
									"short": releasequalifierslib.ReleaseQualifier{},
									"long":  releasequalifierslib.ReleaseQualifier{},
								},
							},
						},
					},
				},
			},
			qualifiersConfig: releasequalifierslib.ReleaseQualifiers{
				"short": releasequalifierslib.ReleaseQualifier{
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "OCPBUGS",
							Escalations: []jira.Escalation{
								{Name: "quick", Failures: 2, OverPeriod: "2d", Priority: "Low"},
							},
						},
					},
				},
				"long": releasequalifierslib.ReleaseQualifier{
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "OCPBUGS",
							Escalations: []jira.Escalation{
								{Name: "weekly", Failures: 5, OverPeriod: "1w", Priority: "Major"},
							},
						},
					},
				},
			},
			wantDefault: nil,
			wantFiltered: []bigquery.ProwjobQueryFilter{
				{Name: "periodic-ci-multi-qual", Interval: "7 DAY"},
			},
		},
	}

	controller := &JiraEscalationsController{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDefault, gotFiltered := controller.collectJobNamesWithEscalations(tt.releasePayload, tt.qualifiersConfig)

			// Check default jobs
			defaultMap := make(map[string]bool)
			for _, name := range gotDefault {
				defaultMap[name] = true
			}
			wantDefaultMap := make(map[string]bool)
			for _, name := range tt.wantDefault {
				wantDefaultMap[name] = true
			}
			if diff := cmp.Diff(wantDefaultMap, defaultMap); diff != "" {
				t.Errorf("defaultJobs mismatch (-want +got):\n%s", diff)
			}

			// Check filtered jobs
			filteredMap := make(map[string]string)
			for _, f := range gotFiltered {
				filteredMap[f.Name] = f.Interval
			}
			wantFilteredMap := make(map[string]string)
			for _, f := range tt.wantFiltered {
				wantFilteredMap[f.Name] = f.Interval
			}
			if diff := cmp.Diff(wantFilteredMap, filteredMap); diff != "" {
				t.Errorf("filteredJobs mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGroupHistoryByJob(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		allHistory []bigquery.ReleaseQualifiersProwjobSummaryResult
		want       map[string][]bigquery.ReleaseQualifiersProwjobSummaryResult
	}{
		{
			name: "Multiple jobs with results",
			allHistory: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "job1", State: "success", URL: "https://prow.ci/j1-1"},
				{Release: "4.19.0-0.nightly-2", Name: "job1", State: "failure", URL: "https://prow.ci/j1-2"},
				{Release: "4.19.0-0.nightly-1", Name: "job2", State: "success", URL: "https://prow.ci/j2-1"},
				{Release: "4.19.0-0.nightly-2", Name: "job2", State: "success", URL: "https://prow.ci/j2-2"},
				{Release: "4.19.0-0.nightly-3", Name: "job1", State: "success", URL: "https://prow.ci/j1-3"},
			},
			want: map[string][]bigquery.ReleaseQualifiersProwjobSummaryResult{
				"job1": {
					{Release: "4.19.0-0.nightly-1", Name: "job1", State: "success", URL: "https://prow.ci/j1-1"},
					{Release: "4.19.0-0.nightly-2", Name: "job1", State: "failure", URL: "https://prow.ci/j1-2"},
					{Release: "4.19.0-0.nightly-3", Name: "job1", State: "success", URL: "https://prow.ci/j1-3"},
				},
				"job2": {
					{Release: "4.19.0-0.nightly-1", Name: "job2", State: "success", URL: "https://prow.ci/j2-1"},
					{Release: "4.19.0-0.nightly-2", Name: "job2", State: "success", URL: "https://prow.ci/j2-2"},
				},
			},
		},
		{
			name:       "Empty history",
			allHistory: []bigquery.ReleaseQualifiersProwjobSummaryResult{},
			want:       map[string][]bigquery.ReleaseQualifiersProwjobSummaryResult{},
		},
		{
			name: "Single job with single result",
			allHistory: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "job1", State: "success", URL: "https://prow.ci/j1-1"},
			},
			want: map[string][]bigquery.ReleaseQualifiersProwjobSummaryResult{
				"job1": {
					{Release: "4.19.0-0.nightly-1", Name: "job1", State: "success", URL: "https://prow.ci/j1-1"},
				},
			},
		},
	}

	controller := &JiraEscalationsController{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := controller.groupHistoryByJob(tt.allHistory)

			if len(got) != len(tt.want) {
				t.Errorf("groupHistoryByJob() got %d jobs, want %d", len(got), len(tt.want))
			}

			for jobName, wantResults := range tt.want {
				gotResults, exists := got[jobName]
				if !exists {
					t.Errorf("groupHistoryByJob() missing job %s", jobName)
					continue
				}

				if diff := cmp.Diff(wantResults, gotResults); diff != "" {
					t.Errorf("groupHistoryByJob() job %s mismatch (-want +got):\n%s", jobName, diff)
				}
			}
		})
	}
}

func intPtr(i int) *int {
	return &i
}

func TestSync(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		releasePayload     *v1alpha1.ReleasePayload
		qualifiersConfig   releasequalifierslib.ReleaseQualifiers
		bigQueryResults    []bigquery.ReleaseQualifiersProwjobSummaryResult
		expectedEventCount int
		expectedError      bool
	}{
		{
			name: "Successful escalation trigger - consecutive failures",
			releasePayload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.19.0-0.nightly-2024-04-20-123456",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "rosa-hcp",
								CIConfigurationJobName: "periodic-ci-rosa-hcp",
								Qualifiers: releasequalifierslib.ReleaseQualifiers{
									"rosa": releasequalifierslib.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "rosa-hcp",
							CIConfigurationJobName: "periodic-ci-rosa-hcp",
							AggregateState:         v1alpha1.JobStateFailure,
						},
					},
				},
			},
			qualifiersConfig: releasequalifierslib.ReleaseQualifiers{
				"rosa": releasequalifierslib.ReleaseQualifier{
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project:   "OCPBUGS",
							Component: "installer",
							Escalations: []jira.Escalation{
								{
									Name:     "critical",
									Failures: 3,
									Priority: "Critical",
								},
							},
						},
					},
				},
			},
			bigQueryResults: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "periodic-ci-rosa-hcp", State: "failure", URL: "https://prow.ci/1"},
				{Release: "4.19.0-0.nightly-2", Name: "periodic-ci-rosa-hcp", State: "failure", URL: "https://prow.ci/2"},
				{Release: "4.19.0-0.nightly-3", Name: "periodic-ci-rosa-hcp", State: "failure", URL: "https://prow.ci/3"},
			},
			expectedEventCount: 0,
			expectedError:      false,
		},
		{
			name: "No escalation - insufficient failures",
			releasePayload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.19.0-0.nightly-2024-04-20-123456",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "rosa-hcp",
								CIConfigurationJobName: "periodic-ci-rosa-hcp",
								Qualifiers: releasequalifierslib.ReleaseQualifiers{
									"rosa": releasequalifierslib.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "rosa-hcp",
							CIConfigurationJobName: "periodic-ci-rosa-hcp",
							AggregateState:         v1alpha1.JobStateFailure,
						},
					},
				},
			},
			qualifiersConfig: releasequalifierslib.ReleaseQualifiers{
				"rosa": releasequalifierslib.ReleaseQualifier{
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project:   "OCPBUGS",
							Component: "installer",
							Escalations: []jira.Escalation{
								{
									Name:     "critical",
									Failures: 3,
									Priority: "Critical",
								},
							},
						},
					},
				},
			},
			bigQueryResults: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "periodic-ci-rosa-hcp", State: "failure", URL: "https://prow.ci/1"},
				{Release: "4.19.0-0.nightly-2", Name: "periodic-ci-rosa-hcp", State: "success", URL: "https://prow.ci/2"},
				{Release: "4.19.0-0.nightly-3", Name: "periodic-ci-rosa-hcp", State: "failure", URL: "https://prow.ci/3"},
			},
			expectedEventCount: 0,
			expectedError:      false,
		},
		{
			name: "Escalation with pass percentage threshold",
			releasePayload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.19.0-0.nightly-2024-04-20-123456",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						InformingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "osd-aws",
								CIConfigurationJobName: "periodic-ci-osd-aws",
								Qualifiers: releasequalifierslib.ReleaseQualifiers{
									"osd": releasequalifierslib.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "osd-aws",
							CIConfigurationJobName: "periodic-ci-osd-aws",
							AggregateState:         v1alpha1.JobStateFailure,
						},
					},
				},
			},
			qualifiersConfig: releasequalifierslib.ReleaseQualifiers{
				"osd": releasequalifierslib.ReleaseQualifier{
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project:   "OCPBUGS",
							Component: "installer",
							Escalations: []jira.Escalation{
								{
									Name:           "quality",
									OverLastRuns:   intPtr(10),
									PassPercentage: intPtr(60),
									Priority:       "High",
								},
							},
						},
					},
				},
			},
			bigQueryResults: []bigquery.ReleaseQualifiersProwjobSummaryResult{
				{Release: "4.19.0-0.nightly-1", Name: "periodic-ci-osd-aws", State: "success", URL: "https://prow.ci/1"},
				{Release: "4.19.0-0.nightly-2", Name: "periodic-ci-osd-aws", State: "failure", URL: "https://prow.ci/2"},
				{Release: "4.19.0-0.nightly-3", Name: "periodic-ci-osd-aws", State: "failure", URL: "https://prow.ci/3"},
				{Release: "4.19.0-0.nightly-4", Name: "periodic-ci-osd-aws", State: "failure", URL: "https://prow.ci/4"},
				{Release: "4.19.0-0.nightly-5", Name: "periodic-ci-osd-aws", State: "failure", URL: "https://prow.ci/5"},
				{Release: "4.19.0-0.nightly-6", Name: "periodic-ci-osd-aws", State: "failure", URL: "https://prow.ci/6"},
				{Release: "4.19.0-0.nightly-7", Name: "periodic-ci-osd-aws", State: "failure", URL: "https://prow.ci/7"},
				{Release: "4.19.0-0.nightly-8", Name: "periodic-ci-osd-aws", State: "failure", URL: "https://prow.ci/8"},
				{Release: "4.19.0-0.nightly-9", Name: "periodic-ci-osd-aws", State: "success", URL: "https://prow.ci/9"},
				{Release: "4.19.0-0.nightly-10", Name: "periodic-ci-osd-aws", State: "failure", URL: "https://prow.ci/10"},
			},
			expectedEventCount: 0,
			expectedError:      false,
		},
		{
			name: "No jobs with escalations configured",
			releasePayload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.19.0-0.nightly-2024-04-20-123456",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs: []v1alpha1.CIConfiguration{
							{
								CIConfigurationName:    "test-job",
								CIConfigurationJobName: "periodic-ci-test",
								Qualifiers: releasequalifierslib.ReleaseQualifiers{
									"test": releasequalifierslib.ReleaseQualifier{},
								},
							},
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "test-job",
							CIConfigurationJobName: "periodic-ci-test",
							AggregateState:         v1alpha1.JobStateSuccess,
						},
					},
				},
			},
			qualifiersConfig: releasequalifierslib.ReleaseQualifiers{
				"test": releasequalifierslib.ReleaseQualifier{
					BadgeName: "Test",
				},
			},
			bigQueryResults:    []bigquery.ReleaseQualifiersProwjobSummaryResult{},
			expectedEventCount: 0,
			expectedError:      false,
		},
		{
			name: "ReleasePayload not found",
			releasePayload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-existent",
					Namespace: "ocp",
				},
			},
			qualifiersConfig:   releasequalifierslib.ReleaseQualifiers{},
			bigQueryResults:    []bigquery.ReleaseQualifiersProwjobSummaryResult{},
			expectedEventCount: 0,
			expectedError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake clientset and informer
			objs := []runtime.Object{}
			if tt.name != "ReleasePayload not found" {
				objs = append(objs, tt.releasePayload)
			}

			client := releasepayloadclient.NewSimpleClientset(objs...)
			informerFactory := releasepayloadinformers.NewSharedInformerFactory(client, 0)
			releasePayloadInformer := informerFactory.Release().V1alpha1().ReleasePayloads()

			// Create event recorder
			eventRecorder := events.NewInMemoryRecorder("test", clock.RealClock{})

			// Create config accessor
			configAccessor := &mockConfigAccessor{
				config: tt.qualifiersConfig,
			}

			// Create fake BigQuery client
			bqClient := bigquery.NewFakeClient()

			// Convert test results to interface{} slice for the fake client
			var defaultResults []any
			for _, result := range tt.bigQueryResults {
				defaultResults = append(defaultResults, result)
			}
			bqClient.DefaultResult = defaultResults

			// Create controller
			controller, err := NewJiraEscalationsController(
				releasePayloadInformer,
				client.ReleaseV1alpha1(),
				eventRecorder,
				configAccessor,
				bqClient,
				nil,
			)
			if err != nil {
				t.Fatalf("Failed to create controller: %v", err)
			}

			// Start informer and wait for cache sync
			ctx := t.Context()

			informerFactory.Start(ctx.Done())
			if !cache.WaitForCacheSync(ctx.Done(), releasePayloadInformer.Informer().HasSynced) {
				t.Fatal("Failed to sync informer cache")
			}

			// Call sync
			key := tt.releasePayload.Namespace + "/" + tt.releasePayload.Name
			err = controller.sync(ctx, key)

			// Check error expectation
			if tt.expectedError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectedError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Check event count
			recordedEvents := eventRecorder.Events()
			if len(recordedEvents) != tt.expectedEventCount {
				t.Errorf("Expected %d events, got %d", tt.expectedEventCount, len(recordedEvents))
				for i, evt := range recordedEvents {
					t.Logf("Event %d: %s", i, evt)
				}
			}
		})
	}
}

// mockJiraClient wraps fakejira.FakeClient and overrides SearchV2JqlWithContext
// since fakejira.SearchRequest has unexported fields that prevent setting up
// search expectations from outside the package. All other Jira operations
// (CreateIssue, UpdateIssue, AddComment, etc.) are handled by the embedded
// fakejira.FakeClient.
type mockJiraClient struct {
	fakejira.FakeClient
	searchResults map[string][]gojira.Issue
	searchError   error
	watchersAdded map[string][]string
}

func (m *mockJiraClient) JiraClient() *gojira.Client {
	return nil
}

func (m *mockJiraClient) SearchV2JqlWithContext(_ context.Context, jql string, _ *gojira.SearchOptionsV2) ([]gojira.Issue, *gojira.Response, error) {
	if m.searchError != nil {
		return nil, nil, m.searchError
	}
	for key, issues := range m.searchResults {
		if strings.Contains(jql, key) {
			return issues, nil, nil
		}
	}
	return nil, nil, nil
}

func TestIsHigherPriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		newP     string
		existP   string
		expected bool
	}{
		{"Blocker > Critical", "Blocker", "Critical", true},
		{"Critical > Major", "Critical", "Major", true},
		{"Major > Normal", "Major", "Normal", true},
		{"Normal > Minor", "Normal", "Minor", true},
		{"Minor > Trivial", "Minor", "Trivial", true},
		{"Major not higher than Critical", "Major", "Critical", false},
		{"Same priority", "Major", "Major", false},
		{"Unknown new priority", "Unknown", "Major", false},
		{"Unknown existing priority", "Major", "Unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isHigherPriority(tt.newP, tt.existP)
			if got != tt.expected {
				t.Errorf("isHigherPriority(%s, %s) = %v, want %v", tt.newP, tt.existP, got, tt.expected)
			}
		})
	}
}

func TestBuildEscalationSummary(t *testing.T) {
	t.Parallel()

	controller := &JiraEscalationsController{}

	t.Run("uses config summary when set", func(t *testing.T) {
		jiraConfig := &jira.Notification{Summary: "Custom Summary"}
		escalation := jira.Escalation{Name: "critical", Priority: "Critical"}
		job := v1alpha1.JobStatus{CIConfigurationJobName: "periodic-ci-test"}

		got := controller.buildEscalationSummary(jiraConfig, escalation, job)
		if got != "Custom Summary" {
			t.Errorf("expected 'Custom Summary', got '%s'", got)
		}
	})

	t.Run("generates summary when not set", func(t *testing.T) {
		jiraConfig := &jira.Notification{}
		escalation := jira.Escalation{Name: "critical", Priority: "Critical"}
		job := v1alpha1.JobStatus{CIConfigurationJobName: "periodic-ci-test"}

		got := controller.buildEscalationSummary(jiraConfig, escalation, job)
		if got != "[Critical] critical escalation for periodic-ci-test" {
			t.Errorf("unexpected summary: %s", got)
		}
	})

	t.Run("includes mentions in summary", func(t *testing.T) {
		jiraConfig := &jira.Notification{}
		escalation := jira.Escalation{
			Name:     "critical",
			Priority: "Critical",
			Mentions: []string{"user1", "user2"},
		}
		job := v1alpha1.JobStatus{CIConfigurationJobName: "periodic-ci-test"}

		got := controller.buildEscalationSummary(jiraConfig, escalation, job)
		if !strings.Contains(got, "| CC: [~user1], [~user2]") {
			t.Errorf("expected mentions in summary, got: %s", got)
		}
	})

	t.Run("includes needsInfo in summary", func(t *testing.T) {
		jiraConfig := &jira.Notification{}
		escalation := jira.Escalation{
			Name:      "critical",
			Priority:  "Critical",
			NeedsInfo: []string{"user3"},
		}
		job := v1alpha1.JobStatus{CIConfigurationJobName: "periodic-ci-test"}

		got := controller.buildEscalationSummary(jiraConfig, escalation, job)
		if !strings.Contains(got, "| Needs Info: [~user3]") {
			t.Errorf("expected needsInfo in summary, got: %s", got)
		}
	})

	t.Run("includes both mentions and needsInfo in summary", func(t *testing.T) {
		jiraConfig := &jira.Notification{}
		escalation := jira.Escalation{
			Name:      "critical",
			Priority:  "Critical",
			Mentions:  []string{"user1"},
			NeedsInfo: []string{"user2", "user3"},
		}
		job := v1alpha1.JobStatus{CIConfigurationJobName: "periodic-ci-test"}

		got := controller.buildEscalationSummary(jiraConfig, escalation, job)
		if !strings.Contains(got, "| CC: [~user1]") {
			t.Errorf("expected mentions in summary, got: %s", got)
		}
		if !strings.Contains(got, "| Needs Info: [~user2], [~user3]") {
			t.Errorf("expected needsInfo in summary, got: %s", got)
		}
	})

	t.Run("appends mentions to custom summary", func(t *testing.T) {
		jiraConfig := &jira.Notification{Summary: "Custom Summary"}
		escalation := jira.Escalation{
			Name:     "critical",
			Priority: "Critical",
			Mentions: []string{"user1"},
		}
		job := v1alpha1.JobStatus{CIConfigurationJobName: "periodic-ci-test"}

		got := controller.buildEscalationSummary(jiraConfig, escalation, job)
		if !strings.HasPrefix(got, "Custom Summary") {
			t.Errorf("expected custom summary prefix, got: %s", got)
		}
		if !strings.Contains(got, "| CC: [~user1]") {
			t.Errorf("expected mentions appended to custom summary, got: %s", got)
		}
	})
}

func TestBuildEscalationDescription(t *testing.T) {
	t.Parallel()

	controller := &JiraEscalationsController{}
	releasePayload := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{Name: "4.19.0-0.nightly-1", Namespace: "ocp"},
	}
	job := v1alpha1.JobStatus{CIConfigurationJobName: "periodic-ci-test"}

	t.Run("consecutive failures", func(t *testing.T) {
		escalation := jira.Escalation{Name: "critical", Failures: 3, Priority: "Critical"}
		got := controller.buildEscalationDescription(releasePayload, job, "rosa", &jira.Notification{}, escalation)
		if !strings.Contains(got, "3 consecutive failures") {
			t.Errorf("expected consecutive failures text, got: %s", got)
		}
	})

	t.Run("windowed failures", func(t *testing.T) {
		escalation := jira.Escalation{Name: "high", Failures: 5, OverLastRuns: intPtr(10), Priority: "High"}
		got := controller.buildEscalationDescription(releasePayload, job, "rosa", &jira.Notification{}, escalation)
		if !strings.Contains(got, "5 failures over last 10 runs") {
			t.Errorf("expected windowed failures text, got: %s", got)
		}
	})

	t.Run("pass percentage", func(t *testing.T) {
		escalation := jira.Escalation{Name: "quality", PassPercentage: intPtr(60), OverLastRuns: intPtr(10), Priority: "High"}
		got := controller.buildEscalationDescription(releasePayload, job, "rosa", &jira.Notification{}, escalation)
		if !strings.Contains(got, "Pass percentage below 60%") {
			t.Errorf("expected pass percentage text, got: %s", got)
		}
		if !strings.Contains(got, "over last 10 runs") {
			t.Errorf("expected over last runs text, got: %s", got)
		}
	})

	t.Run("with custom description", func(t *testing.T) {
		escalation := jira.Escalation{Name: "critical", Failures: 3, Priority: "Critical"}
		config := &jira.Notification{Description: "Custom preamble"}
		got := controller.buildEscalationDescription(releasePayload, job, "rosa", config, escalation)
		if got != "Custom preamble" {
			t.Errorf("expected custom description used as-is, got: %s", got)
		}
	})
}

func TestTriggerJiraEscalation_NilClient(t *testing.T) {
	t.Parallel()

	controller := &JiraEscalationsController{
		ReleasePayloadController: &ReleasePayloadController{},
		jiraClient:               nil,
	}

	releasePayload := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{Name: "test-payload", Namespace: "ocp"},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
		},
	}
	job := v1alpha1.JobStatus{CIConfigurationJobName: "periodic-ci-test"}
	jiraConfig := &jira.Notification{Project: "OCPBUGS", Component: "installer"}
	escalation := jira.Escalation{Name: "critical", Failures: 3, Priority: "Critical"}

	// Should not panic with nil jiraClient
	controller.triggerJiraEscalation(context.Background(), releasePayload, job, "rosa", jiraConfig, escalation)
}

func TestCreateNewEscalation(t *testing.T) {
	t.Parallel()

	releasePayload := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{Name: "test-payload", Namespace: "ocp"},
	}
	job := v1alpha1.JobStatus{CIConfigurationJobName: "periodic-ci-rosa-hcp"}

	t.Run("creates issue with all fields", func(t *testing.T) {
		fakeJira := &fakejira.FakeClient{}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               fakeJira,
		}

		jiraConfig := &jira.Notification{
			Project:     "OCPBUGS",
			Component:   "installer",
			Assignee:    "testuser",
			Summary:     "Custom: ROSA HCP failures",
			Description: "ROSA HCP is failing consistently.",
		}
		escalation := jira.Escalation{Name: "critical", Failures: 3, Priority: "Critical"}
		threadID := "4.19.0-0.nightly-rosa-OCPBUGS-installer-"

		controller.createNewEscalation(releasePayload, job, "rosa", jiraConfig, escalation, threadID)

		if len(fakeJira.Issues) != 1 {
			t.Fatalf("Expected 1 issue created, got %d", len(fakeJira.Issues))
		}

		created := fakeJira.Issues[0]
		if created.Fields.Project.Key != "OCPBUGS" {
			t.Errorf("Expected project OCPBUGS, got %s", created.Fields.Project.Key)
		}
		if created.Fields.Summary != "Custom: ROSA HCP failures" {
			t.Errorf("Expected custom summary, got %s", created.Fields.Summary)
		}
		if created.Fields.Priority == nil || created.Fields.Priority.Name != "Critical" {
			t.Errorf("Expected priority Critical, got %v", created.Fields.Priority)
		}
		if created.Fields.Type.Name != "Bug" {
			t.Errorf("Expected issue type Bug, got %s", created.Fields.Type.Name)
		}
		if len(created.Fields.Labels) != 1 || created.Fields.Labels[0] != threadID {
			t.Errorf("Expected label %s, got %v", threadID, created.Fields.Labels)
		}
		if len(created.Fields.Components) != 1 || created.Fields.Components[0].Name != "installer" {
			t.Errorf("Expected component installer, got %v", created.Fields.Components)
		}
		if created.Fields.Assignee == nil || created.Fields.Assignee.Name != "testuser" {
			t.Errorf("Expected assignee testuser, got %v", created.Fields.Assignee)
		}
		if created.Fields.Description != "ROSA HCP is failing consistently." {
			t.Errorf("Expected custom description used as-is, got: %s", created.Fields.Description)
		}
		if created.Key == "" {
			t.Error("Expected fakejira to assign a key")
		}
	})

	t.Run("creates issue without optional fields", func(t *testing.T) {
		fakeJira := &fakejira.FakeClient{}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               fakeJira,
		}

		jiraConfig := &jira.Notification{Project: "OCPBUGS"}
		escalation := jira.Escalation{Name: "low", Failures: 2, Priority: "Minor"}
		threadID := "4.19.0-0.nightly-rosa-OCPBUGS--"

		controller.createNewEscalation(releasePayload, job, "rosa", jiraConfig, escalation, threadID)

		if len(fakeJira.Issues) != 1 {
			t.Fatalf("Expected 1 issue created, got %d", len(fakeJira.Issues))
		}

		created := fakeJira.Issues[0]
		if created.Fields.Assignee != nil {
			t.Errorf("Expected no assignee, got %v", created.Fields.Assignee)
		}
		if len(created.Fields.Components) != 0 {
			t.Errorf("Expected no components, got %v", created.Fields.Components)
		}
		if !strings.Contains(created.Fields.Summary, "periodic-ci-rosa-hcp") {
			t.Errorf("Expected generated summary with job name, got: %s", created.Fields.Summary)
		}
	})

	t.Run("creates issue with mentions and needsInfo in summary", func(t *testing.T) {
		mock := &mockJiraClient{
			searchResults: map[string][]gojira.Issue{},
			watchersAdded: make(map[string][]string),
		}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               mock,
			addWatcherFn: func(issueKey, username string) error {
				mock.watchersAdded[issueKey] = append(mock.watchersAdded[issueKey], username)
				return nil
			},
		}

		jiraConfig := &jira.Notification{Project: "OCPBUGS", Component: "installer"}
		escalation := jira.Escalation{
			Name:      "critical",
			Failures:  3,
			Priority:  "Critical",
			Mentions:  []string{"lead1", "lead2"},
			NeedsInfo: []string{"sme1", "sme2"},
		}
		threadID := "4.19.0-0.nightly-rosa-OCPBUGS-installer-"

		issueKey := controller.createNewEscalation(releasePayload, job, "rosa", jiraConfig, escalation, threadID)

		if len(mock.Issues) != 1 {
			t.Fatalf("Expected 1 issue created, got %d", len(mock.Issues))
		}

		created := mock.Issues[0]
		if !strings.Contains(created.Fields.Summary, "| CC: [~lead1], [~lead2]") {
			t.Errorf("Expected mentions in summary, got: %s", created.Fields.Summary)
		}
		if !strings.Contains(created.Fields.Summary, "| Needs Info: [~sme1], [~sme2]") {
			t.Errorf("Expected needsInfo in summary, got: %s", created.Fields.Summary)
		}

		if issueKey == "" {
			t.Fatal("Expected non-empty issue key")
		}
		watchers := mock.watchersAdded[issueKey]
		if len(watchers) != 2 || watchers[0] != "sme1" || watchers[1] != "sme2" {
			t.Errorf("Expected watchers [sme1, sme2], got %v", watchers)
		}
	})

	t.Run("handles CreateIssue error gracefully", func(t *testing.T) {
		fakeJira := &fakejira.FakeClient{
			CreateIssueError: map[string]error{
				"": fmt.Errorf("permission denied"),
			},
		}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               fakeJira,
		}

		jiraConfig := &jira.Notification{Project: "OCPBUGS"}
		escalation := jira.Escalation{Name: "critical", Failures: 3, Priority: "Critical"}

		// Should not panic on CreateIssue error
		controller.createNewEscalation(releasePayload, job, "rosa", jiraConfig, escalation, "thread-id")
	})
}

func TestUpdateExistingEscalation(t *testing.T) {
	t.Parallel()

	releasePayload := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{Name: "test-payload", Namespace: "ocp"},
	}
	job := v1alpha1.JobStatus{CIConfigurationJobName: "periodic-ci-rosa-hcp"}
	jiraConfig := &jira.Notification{Project: "OCPBUGS", Component: "installer"}

	t.Run("upgrades priority and adds comment", func(t *testing.T) {
		existingIssue := &gojira.Issue{
			ID:  "1",
			Key: "OCPBUGS-1",
			Fields: &gojira.IssueFields{
				Priority: &gojira.Priority{Name: "Minor"},
				Project:  gojira.Project{Key: "OCPBUGS"},
			},
		}
		fakeJira := &fakejira.FakeClient{
			Issues: []*gojira.Issue{existingIssue},
		}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               fakeJira,
		}

		escalation := jira.Escalation{Name: "critical", Failures: 3, Priority: "Critical"}
		controller.updateExistingEscalation(existingIssue, releasePayload, job, "rosa", jiraConfig, escalation, "thread-id")

		updated, err := fakeJira.GetIssue("OCPBUGS-1")
		if err != nil {
			t.Fatalf("Failed to get updated issue: %v", err)
		}

		if updated.Fields.Priority.Name != "Critical" {
			t.Errorf("Expected priority upgraded to Critical, got %s", updated.Fields.Priority.Name)
		}

		if updated.Fields.Comments == nil || len(updated.Fields.Comments.Comments) != 1 {
			t.Fatalf("Expected 1 comment, got %v", updated.Fields.Comments)
		}

		comment := updated.Fields.Comments.Comments[0]
		if !strings.Contains(comment.Body, "Escalation 'critical' re-triggered") {
			t.Errorf("Expected re-triggered text in comment, got: %s", comment.Body)
		}
		if !strings.Contains(comment.Body, "periodic-ci-rosa-hcp") {
			t.Errorf("Expected job name in comment, got: %s", comment.Body)
		}
	})

	t.Run("does not downgrade priority", func(t *testing.T) {
		existingIssue := &gojira.Issue{
			ID:  "1",
			Key: "OCPBUGS-1",
			Fields: &gojira.IssueFields{
				Priority: &gojira.Priority{Name: "Critical"},
				Project:  gojira.Project{Key: "OCPBUGS"},
			},
		}
		fakeJira := &fakejira.FakeClient{
			Issues: []*gojira.Issue{existingIssue},
		}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               fakeJira,
		}

		escalation := jira.Escalation{Name: "low", Failures: 2, Priority: "Minor"}
		controller.updateExistingEscalation(existingIssue, releasePayload, job, "rosa", jiraConfig, escalation, "thread-id")

		updated, err := fakeJira.GetIssue("OCPBUGS-1")
		if err != nil {
			t.Fatalf("Failed to get issue: %v", err)
		}

		if updated.Fields.Priority.Name != "Critical" {
			t.Errorf("Expected priority to remain Critical, got %s", updated.Fields.Priority.Name)
		}

		if updated.Fields.Comments == nil || len(updated.Fields.Comments.Comments) != 1 {
			t.Error("Expected comment to still be added even when priority is not upgraded")
		}
	})

	t.Run("handles same priority", func(t *testing.T) {
		existingIssue := &gojira.Issue{
			ID:  "1",
			Key: "OCPBUGS-1",
			Fields: &gojira.IssueFields{
				Priority: &gojira.Priority{Name: "Major"},
				Project:  gojira.Project{Key: "OCPBUGS"},
			},
		}
		fakeJira := &fakejira.FakeClient{
			Issues: []*gojira.Issue{existingIssue},
		}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               fakeJira,
		}

		escalation := jira.Escalation{Name: "medium", Failures: 4, Priority: "Major"}
		controller.updateExistingEscalation(existingIssue, releasePayload, job, "rosa", jiraConfig, escalation, "thread-id")

		updated, err := fakeJira.GetIssue("OCPBUGS-1")
		if err != nil {
			t.Fatalf("Failed to get issue: %v", err)
		}

		if updated.Fields.Priority.Name != "Major" {
			t.Errorf("Expected priority to remain Major, got %s", updated.Fields.Priority.Name)
		}

		if updated.Fields.Comments == nil || len(updated.Fields.Comments.Comments) != 1 {
			t.Error("Expected comment added")
		}
	})

	t.Run("includes mentions in summary and adds watchers on re-trigger", func(t *testing.T) {
		existingIssue := &gojira.Issue{
			ID:  "1",
			Key: "OCPBUGS-1",
			Fields: &gojira.IssueFields{
				Priority: &gojira.Priority{Name: "Minor"},
				Project:  gojira.Project{Key: "OCPBUGS"},
			},
		}
		mock := &mockJiraClient{
			FakeClient:    fakejira.FakeClient{Issues: []*gojira.Issue{existingIssue}},
			searchResults: map[string][]gojira.Issue{},
			watchersAdded: make(map[string][]string),
		}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               mock,
			addWatcherFn: func(issueKey, username string) error {
				mock.watchersAdded[issueKey] = append(mock.watchersAdded[issueKey], username)
				return nil
			},
		}

		escalation := jira.Escalation{
			Name:      "critical",
			Failures:  3,
			Priority:  "Critical",
			Mentions:  []string{"lead1"},
			NeedsInfo: []string{"sme1"},
		}
		controller.updateExistingEscalation(existingIssue, releasePayload, job, "rosa", jiraConfig, escalation, "thread-id")

		updated, err := mock.GetIssue("OCPBUGS-1")
		if err != nil {
			t.Fatalf("Failed to get updated issue: %v", err)
		}

		if updated.Fields.Comments == nil || len(updated.Fields.Comments.Comments) != 1 {
			t.Fatalf("Expected 1 comment, got %v", updated.Fields.Comments)
		}

		watchers := mock.watchersAdded["OCPBUGS-1"]
		if len(watchers) != 1 || watchers[0] != "sme1" {
			t.Errorf("Expected watchers [sme1], got %v", watchers)
		}
	})

	t.Run("adds multiple comments on repeated escalations", func(t *testing.T) {
		existingIssue := &gojira.Issue{
			ID:  "1",
			Key: "OCPBUGS-1",
			Fields: &gojira.IssueFields{
				Priority: &gojira.Priority{Name: "Major"},
				Project:  gojira.Project{Key: "OCPBUGS"},
			},
		}
		fakeJira := &fakejira.FakeClient{
			Issues: []*gojira.Issue{existingIssue},
		}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               fakeJira,
		}

		escalation := jira.Escalation{Name: "medium", Failures: 4, Priority: "Major"}
		controller.updateExistingEscalation(existingIssue, releasePayload, job, "rosa", jiraConfig, escalation, "thread-id")
		controller.updateExistingEscalation(existingIssue, releasePayload, job, "rosa", jiraConfig, escalation, "thread-id")

		updated, err := fakeJira.GetIssue("OCPBUGS-1")
		if err != nil {
			t.Fatalf("Failed to get issue: %v", err)
		}

		if updated.Fields.Comments == nil || len(updated.Fields.Comments.Comments) != 2 {
			t.Errorf("Expected 2 comments after two escalations, got %d", len(updated.Fields.Comments.Comments))
		}
	})
}

func TestFindExistingEscalationIssue(t *testing.T) {
	t.Parallel()

	threadID := "4.19.0-0.nightly-rosa-OCPBUGS-installer-"

	t.Run("finds existing open issue", func(t *testing.T) {
		existingIssue := gojira.Issue{
			ID:  "1",
			Key: "OCPBUGS-1",
			Fields: &gojira.IssueFields{
				Priority: &gojira.Priority{Name: "Major"},
				Labels:   []string{threadID},
			},
		}
		mock := &mockJiraClient{
			searchResults: map[string][]gojira.Issue{
				threadID: {existingIssue},
			},
		}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               mock,
		}

		found, err := controller.findExistingEscalationIssue(context.Background(), "OCPBUGS", threadID)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if found == nil {
			t.Fatal("Expected to find existing issue")
		}
		if found.Key != "OCPBUGS-1" {
			t.Errorf("Expected key OCPBUGS-1, got %s", found.Key)
		}
	})

	t.Run("returns nil when no match", func(t *testing.T) {
		mock := &mockJiraClient{
			searchResults: map[string][]gojira.Issue{},
		}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               mock,
		}

		found, err := controller.findExistingEscalationIssue(context.Background(), "OCPBUGS", threadID)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if found != nil {
			t.Errorf("Expected nil, got %v", found)
		}
	})

	t.Run("returns error on search failure", func(t *testing.T) {
		mock := &mockJiraClient{
			searchError: fmt.Errorf("jira unavailable"),
		}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               mock,
		}

		_, err := controller.findExistingEscalationIssue(context.Background(), "OCPBUGS", threadID)
		if err == nil {
			t.Fatal("Expected error")
		}
		if !strings.Contains(err.Error(), "jira unavailable") {
			t.Errorf("Expected jira unavailable error, got: %v", err)
		}
	})
}

func TestTriggerJiraEscalation_EndToEnd(t *testing.T) {
	t.Parallel()

	releasePayload := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{Name: "test-payload", Namespace: "ocp"},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
		},
	}
	job := v1alpha1.JobStatus{CIConfigurationJobName: "periodic-ci-rosa-hcp"}
	jiraConfig := &jira.Notification{
		Project:   "OCPBUGS",
		Component: "installer",
		Assignee:  "oncall",
	}

	t.Run("creates new issue when none exists", func(t *testing.T) {
		mock := &mockJiraClient{
			FakeClient:    fakejira.FakeClient{},
			searchResults: map[string][]gojira.Issue{},
		}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               mock,
		}

		escalation := jira.Escalation{Name: "critical", Failures: 3, Priority: "Critical"}
		controller.triggerJiraEscalation(context.Background(), releasePayload, job, "rosa", jiraConfig, escalation)

		if len(mock.Issues) != 1 {
			t.Fatalf("Expected 1 issue, got %d", len(mock.Issues))
		}

		created := mock.Issues[0]
		if created.Fields.Project.Key != "OCPBUGS" {
			t.Errorf("Expected project OCPBUGS, got %s", created.Fields.Project.Key)
		}
		if created.Fields.Priority.Name != "Critical" {
			t.Errorf("Expected priority Critical, got %s", created.Fields.Priority.Name)
		}
		if created.Fields.Assignee == nil || created.Fields.Assignee.Name != "oncall" {
			t.Errorf("Expected assignee oncall, got %v", created.Fields.Assignee)
		}
	})

	t.Run("updates existing issue on re-trigger", func(t *testing.T) {
		threadID := "4.19.0-0.nightly-rosa-OCPBUGS-installer-"
		existingIssue := &gojira.Issue{
			ID:  "1",
			Key: "OCPBUGS-1",
			Fields: &gojira.IssueFields{
				Priority: &gojira.Priority{Name: "Minor"},
				Project:  gojira.Project{Key: "OCPBUGS"},
				Labels:   []string{threadID},
			},
		}
		mock := &mockJiraClient{
			FakeClient: fakejira.FakeClient{
				Issues: []*gojira.Issue{existingIssue},
			},
			searchResults: map[string][]gojira.Issue{
				threadID: {*existingIssue},
			},
		}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               mock,
		}

		escalation := jira.Escalation{Name: "critical", Failures: 3, Priority: "Blocker"}
		controller.triggerJiraEscalation(context.Background(), releasePayload, job, "rosa", jiraConfig, escalation)

		if len(mock.Issues) != 1 {
			t.Fatalf("Expected no new issues, got %d", len(mock.Issues))
		}

		updated, err := mock.GetIssue("OCPBUGS-1")
		if err != nil {
			t.Fatalf("Failed to get issue: %v", err)
		}
		if updated.Fields.Priority.Name != "Blocker" {
			t.Errorf("Expected priority upgraded to Blocker, got %s", updated.Fields.Priority.Name)
		}
		if updated.Fields.Comments == nil || len(updated.Fields.Comments.Comments) != 1 {
			t.Fatalf("Expected 1 comment, got %v", updated.Fields.Comments)
		}
		if !strings.Contains(updated.Fields.Comments.Comments[0].Body, "critical") {
			t.Errorf("Expected escalation name in comment, got: %s", updated.Fields.Comments.Comments[0].Body)
		}
	})

	t.Run("skips gracefully on search error", func(t *testing.T) {
		mock := &mockJiraClient{
			FakeClient:  fakejira.FakeClient{},
			searchError: fmt.Errorf("jira timeout"),
		}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               mock,
		}

		escalation := jira.Escalation{Name: "critical", Failures: 3, Priority: "Critical"}
		// Should not panic on search error
		controller.triggerJiraEscalation(context.Background(), releasePayload, job, "rosa", jiraConfig, escalation)

		if len(mock.Issues) != 0 {
			t.Errorf("Expected no issues created on search error, got %d", len(mock.Issues))
		}
	})
}

func TestGetSetNotificationState(t *testing.T) {
	t.Parallel()

	rp := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{Name: "test-payload", Namespace: "ocp"},
	}
	controller := &JiraEscalationsController{ReleasePayloadController: &ReleasePayloadController{}}

	t.Run("returns empty state for nonexistent qualifier", func(t *testing.T) {
		state := controller.getNotificationState(rp, "missing", "thread-1")
		if state.ActiveEscalation != "" {
			t.Errorf("Expected empty state, got %+v", state)
		}
	})

	t.Run("set and get roundtrip", func(t *testing.T) {
		state := v1alpha1.JiraNotificationState{
			IssueKey:         "OCPBUGS-42",
			ActiveEscalation: "critical",
			ActivePriority:   "Critical",
		}
		controller.setNotificationState(rp, "rosa", "thread-1", state)

		got := controller.getNotificationState(rp, "rosa", "thread-1")
		if got.IssueKey != "OCPBUGS-42" || got.ActiveEscalation != "critical" || got.ActivePriority != "Critical" {
			t.Errorf("Roundtrip failed: got %+v", got)
		}
	})

	t.Run("different threads are independent", func(t *testing.T) {
		controller.setNotificationState(rp, "rosa", "thread-2", v1alpha1.JiraNotificationState{
			ActiveEscalation: "low",
		})

		got1 := controller.getNotificationState(rp, "rosa", "thread-1")
		got2 := controller.getNotificationState(rp, "rosa", "thread-2")
		if got1.ActiveEscalation != "critical" {
			t.Errorf("Expected thread-1 to still have 'critical', got %s", got1.ActiveEscalation)
		}
		if got2.ActiveEscalation != "low" {
			t.Errorf("Expected thread-2 to have 'low', got %s", got2.ActiveEscalation)
		}
	})
}

func TestDuplicatePrevention(t *testing.T) {
	t.Parallel()

	mock := &mockJiraClient{FakeClient: fakejira.FakeClient{}}
	controller := &JiraEscalationsController{
		ReleasePayloadController: &ReleasePayloadController{},
		jiraClient:               mock,
		configAccessor: &mockConfigAccessor{
			config: releasequalifierslib.ReleaseQualifiers{
				"rosa": {
					Enabled: releasequalifierslib.BoolPtr(true),
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "OCPBUGS",
							Escalations: []jira.Escalation{
								{Name: "critical", Failures: 2, Priority: "Critical"},
							},
						},
					},
				},
			},
		},
	}

	releasePayload := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{Name: "test-payload", Namespace: "ocp"},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
			PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
				InformingJobs: []v1alpha1.CIConfiguration{
					{
						CIConfigurationName:    "rosa-hcp",
						CIConfigurationJobName: "periodic-ci-rosa-hcp",
						Qualifiers: releasequalifierslib.ReleaseQualifiers{
							"rosa": {},
						},
					},
				},
			},
		},
		Status: v1alpha1.ReleasePayloadStatus{
			InformingJobResults: []v1alpha1.JobStatus{
				{CIConfigurationName: "rosa-hcp", CIConfigurationJobName: "periodic-ci-rosa-hcp"},
			},
		},
	}

	jobHistory := map[string][]bigquery.ReleaseQualifiersProwjobSummaryResult{
		"periodic-ci-rosa-hcp": {
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
		},
	}

	// First call should create an issue
	err := controller.processJobsForEscalations(
		context.Background(), releasePayload, releasePayload.Status.InformingJobResults,
		controller.configAccessor.Get(), jobHistory,
	)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(mock.Issues) != 1 {
		t.Fatalf("Expected 1 issue after first call, got %d", len(mock.Issues))
	}

	// Verify notification state was recorded
	state := controller.getNotificationState(releasePayload, "rosa", "4.19.0-0.nightly-rosa-OCPBUGS--")
	if state.ActiveEscalation != "critical" {
		t.Errorf("Expected ActiveEscalation=critical, got %s", state.ActiveEscalation)
	}
	if state.IssueKey == "" {
		t.Error("Expected IssueKey to be set")
	}

	// Second call with same conditions should NOT create another issue or add comment
	initialIssueCount := len(mock.Issues)
	err = controller.processJobsForEscalations(
		context.Background(), releasePayload, releasePayload.Status.InformingJobResults,
		controller.configAccessor.Get(), jobHistory,
	)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(mock.Issues) != initialIssueCount {
		t.Errorf("Expected no new issues on duplicate, got %d total", len(mock.Issues))
	}
}

func TestFailedEscalationDoesNotSuppressRetry(t *testing.T) {
	t.Parallel()

	mock := &mockJiraClient{
		FakeClient:  fakejira.FakeClient{},
		searchError: fmt.Errorf("jira unavailable"),
	}
	controller := &JiraEscalationsController{
		ReleasePayloadController: &ReleasePayloadController{},
		jiraClient:               mock,
		configAccessor: &mockConfigAccessor{
			config: releasequalifierslib.ReleaseQualifiers{
				"rosa": {
					Enabled: releasequalifierslib.BoolPtr(true),
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "OCPBUGS",
							Escalations: []jira.Escalation{
								{Name: "critical", Failures: 2, Priority: "Critical"},
							},
						},
					},
				},
			},
		},
	}

	releasePayload := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{Name: "test-payload", Namespace: "ocp"},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
			PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
				InformingJobs: []v1alpha1.CIConfiguration{
					{
						CIConfigurationName:    "rosa-hcp",
						CIConfigurationJobName: "periodic-ci-rosa-hcp",
						Qualifiers: releasequalifierslib.ReleaseQualifiers{
							"rosa": {},
						},
					},
				},
			},
		},
		Status: v1alpha1.ReleasePayloadStatus{
			InformingJobResults: []v1alpha1.JobStatus{
				{CIConfigurationName: "rosa-hcp", CIConfigurationJobName: "periodic-ci-rosa-hcp"},
			},
		},
	}

	jobHistory := map[string][]bigquery.ReleaseQualifiersProwjobSummaryResult{
		"periodic-ci-rosa-hcp": {
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
		},
	}

	// First call: Jira search fails so triggerJiraEscalation returns ""
	err := controller.processJobsForEscalations(
		context.Background(), releasePayload, releasePayload.Status.InformingJobResults,
		controller.configAccessor.Get(), jobHistory,
	)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Notification state should NOT be updated on failure
	threadID := "4.19.0-0.nightly-rosa-OCPBUGS--"
	state := controller.getNotificationState(releasePayload, "rosa", threadID)
	if state.ActiveEscalation != "" {
		t.Errorf("Expected empty ActiveEscalation after failed Jira call, got %q", state.ActiveEscalation)
	}

	// Fix the mock so Jira works on retry
	mock.searchError = nil

	// Second call should succeed and create an issue
	err = controller.processJobsForEscalations(
		context.Background(), releasePayload, releasePayload.Status.InformingJobResults,
		controller.configAccessor.Get(), jobHistory,
	)
	if err != nil {
		t.Fatalf("Unexpected error on retry: %v", err)
	}
	if len(mock.Issues) != 1 {
		t.Fatalf("Expected 1 issue after retry, got %d", len(mock.Issues))
	}
	state = controller.getNotificationState(releasePayload, "rosa", threadID)
	if state.ActiveEscalation != "critical" {
		t.Errorf("Expected ActiveEscalation=critical after successful retry, got %q", state.ActiveEscalation)
	}
	if state.IssueKey == "" {
		t.Error("Expected IssueKey to be set after successful retry")
	}
}

func TestAbatement(t *testing.T) {
	t.Parallel()

	existingIssue := &gojira.Issue{
		ID:     "1",
		Key:    "OCPBUGS-1",
		Fields: &gojira.IssueFields{Priority: &gojira.Priority{Name: "Critical"}},
	}

	mock := &mockJiraClient{
		FakeClient: fakejira.FakeClient{
			Issues: []*gojira.Issue{existingIssue},
		},
		searchResults: map[string][]gojira.Issue{
			"4.19.0-0.nightly-rosa-OCPBUGS--": {*existingIssue},
		},
	}

	controller := &JiraEscalationsController{
		ReleasePayloadController: &ReleasePayloadController{},
		jiraClient:               mock,
		configAccessor: &mockConfigAccessor{
			config: releasequalifierslib.ReleaseQualifiers{
				"rosa": {
					Enabled: releasequalifierslib.BoolPtr(true),
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "OCPBUGS",
							Escalations: []jira.Escalation{
								{Name: "critical", Failures: 3, Priority: "Critical"},
							},
						},
					},
				},
			},
		},
	}

	releasePayload := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{Name: "test-payload", Namespace: "ocp"},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
			PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
				InformingJobs: []v1alpha1.CIConfiguration{
					{
						CIConfigurationName:    "rosa-hcp",
						CIConfigurationJobName: "periodic-ci-rosa-hcp",
						Qualifiers: releasequalifierslib.ReleaseQualifiers{
							"rosa": {},
						},
					},
				},
			},
		},
		Status: v1alpha1.ReleasePayloadStatus{
			InformingJobResults: []v1alpha1.JobStatus{
				{CIConfigurationName: "rosa-hcp", CIConfigurationJobName: "periodic-ci-rosa-hcp"},
			},
		},
	}

	// Pre-seed notification state as if escalation was previously triggered
	threadID := "4.19.0-0.nightly-rosa-OCPBUGS--"
	controller.setNotificationState(releasePayload, "rosa", threadID, v1alpha1.JiraNotificationState{
		IssueKey:         "OCPBUGS-1",
		ActiveEscalation: "critical",
		ActivePriority:   "Critical",
	})

	// Job history now shows passing — conditions have abated
	jobHistory := map[string][]bigquery.ReleaseQualifiersProwjobSummaryResult{
		"periodic-ci-rosa-hcp": {
			{Name: "periodic-ci-rosa-hcp", State: "success"},
			{Name: "periodic-ci-rosa-hcp", State: "success"},
			{Name: "periodic-ci-rosa-hcp", State: "success"},
		},
	}

	err := controller.processJobsForEscalations(
		context.Background(), releasePayload, releasePayload.Status.InformingJobResults,
		controller.configAccessor.Get(), jobHistory,
	)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify abatement was recorded
	state := controller.getNotificationState(releasePayload, "rosa", threadID)
	if !state.Abated {
		t.Error("Expected Abated=true after conditions improved")
	}
	if state.ActiveEscalation != "critical" {
		t.Errorf("Expected ActiveEscalation to remain 'critical', got %s", state.ActiveEscalation)
	}

	// Verify abatement comment was added
	issue, _ := mock.GetIssue("OCPBUGS-1")
	if issue.Fields == nil || issue.Fields.Comments == nil || len(issue.Fields.Comments.Comments) == 0 {
		t.Fatal("Expected abatement comment to be added")
	}
	comment := issue.Fields.Comments.Comments[0]
	if !strings.Contains(comment.Body, "Conditions have improved") {
		t.Errorf("Expected abatement comment, got: %s", comment.Body)
	}
}

func TestReEscalationAfterAbatement(t *testing.T) {
	t.Parallel()

	mock := &mockJiraClient{
		FakeClient: fakejira.FakeClient{
			Issues: []*gojira.Issue{
				{
					ID:     "1",
					Key:    "OCPBUGS-1",
					Fields: &gojira.IssueFields{Priority: &gojira.Priority{Name: "Critical"}},
				},
			},
		},
		searchResults: map[string][]gojira.Issue{
			"4.19.0-0.nightly-rosa-OCPBUGS--": {{
				ID:     "1",
				Key:    "OCPBUGS-1",
				Fields: &gojira.IssueFields{Priority: &gojira.Priority{Name: "Critical"}},
			}},
		},
	}

	controller := &JiraEscalationsController{
		ReleasePayloadController: &ReleasePayloadController{},
		jiraClient:               mock,
		configAccessor: &mockConfigAccessor{
			config: releasequalifierslib.ReleaseQualifiers{
				"rosa": {
					Enabled: releasequalifierslib.BoolPtr(true),
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "OCPBUGS",
							Escalations: []jira.Escalation{
								{Name: "critical", Failures: 3, Priority: "Critical"},
							},
						},
					},
				},
			},
		},
	}

	releasePayload := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{Name: "test-payload", Namespace: "ocp"},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
			PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
				InformingJobs: []v1alpha1.CIConfiguration{
					{
						CIConfigurationName:    "rosa-hcp",
						CIConfigurationJobName: "periodic-ci-rosa-hcp",
						Qualifiers: releasequalifierslib.ReleaseQualifiers{
							"rosa": {},
						},
					},
				},
			},
		},
		Status: v1alpha1.ReleasePayloadStatus{
			InformingJobResults: []v1alpha1.JobStatus{
				{CIConfigurationName: "rosa-hcp", CIConfigurationJobName: "periodic-ci-rosa-hcp"},
			},
		},
	}

	// Pre-seed notification state as previously abated
	threadID := "4.19.0-0.nightly-rosa-OCPBUGS--"
	controller.setNotificationState(releasePayload, "rosa", threadID, v1alpha1.JiraNotificationState{
		IssueKey:         "OCPBUGS-1",
		ActiveEscalation: "critical",
		ActivePriority:   "Critical",
		Abated:           true,
	})

	// Job history shows failures again
	jobHistory := map[string][]bigquery.ReleaseQualifiersProwjobSummaryResult{
		"periodic-ci-rosa-hcp": {
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
		},
	}

	err := controller.processJobsForEscalations(
		context.Background(), releasePayload, releasePayload.Status.InformingJobResults,
		controller.configAccessor.Get(), jobHistory,
	)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Abated should be cleared
	state := controller.getNotificationState(releasePayload, "rosa", threadID)
	if state.Abated {
		t.Error("Expected Abated=false after re-escalation")
	}
	if state.ActiveEscalation != "critical" {
		t.Errorf("Expected ActiveEscalation=critical, got %s", state.ActiveEscalation)
	}

	// Should have updated the existing issue (comment added)
	issue, _ := mock.GetIssue("OCPBUGS-1")
	if issue.Fields == nil || issue.Fields.Comments == nil || len(issue.Fields.Comments.Comments) == 0 {
		t.Fatal("Expected re-escalation comment to be added")
	}
	comment := issue.Fields.Comments.Comments[0]
	if !strings.Contains(comment.Body, "re-triggered") {
		t.Errorf("Expected re-triggered comment, got: %s", comment.Body)
	}
}

func TestEscalationUpgradeUpdatesState(t *testing.T) {
	t.Parallel()

	mock := &mockJiraClient{FakeClient: fakejira.FakeClient{}}
	controller := &JiraEscalationsController{
		ReleasePayloadController: &ReleasePayloadController{},
		jiraClient:               mock,
		configAccessor: &mockConfigAccessor{
			config: releasequalifierslib.ReleaseQualifiers{
				"rosa": {
					Enabled: releasequalifierslib.BoolPtr(true),
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "OCPBUGS",
							Escalations: []jira.Escalation{
								{Name: "normal", Failures: 2, Priority: "Normal"},
								{Name: "critical", Failures: 4, Priority: "Critical"},
							},
						},
					},
				},
			},
		},
	}

	releasePayload := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{Name: "test-payload", Namespace: "ocp"},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.19.0-0.nightly"},
			PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
				InformingJobs: []v1alpha1.CIConfiguration{
					{
						CIConfigurationName:    "rosa-hcp",
						CIConfigurationJobName: "periodic-ci-rosa-hcp",
						Qualifiers: releasequalifierslib.ReleaseQualifiers{
							"rosa": {},
						},
					},
				},
			},
		},
		Status: v1alpha1.ReleasePayloadStatus{
			InformingJobResults: []v1alpha1.JobStatus{
				{CIConfigurationName: "rosa-hcp", CIConfigurationJobName: "periodic-ci-rosa-hcp"},
			},
		},
	}

	threadID := "4.19.0-0.nightly-rosa-OCPBUGS--"

	// Phase 1: only "normal" should trigger (2 failures)
	jobHistory := map[string][]bigquery.ReleaseQualifiersProwjobSummaryResult{
		"periodic-ci-rosa-hcp": {
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
			{Name: "periodic-ci-rosa-hcp", State: "success"},
		},
	}

	err := controller.processJobsForEscalations(
		context.Background(), releasePayload, releasePayload.Status.InformingJobResults,
		controller.configAccessor.Get(), jobHistory,
	)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	state := controller.getNotificationState(releasePayload, "rosa", threadID)
	if state.ActiveEscalation != "normal" {
		t.Errorf("Phase 1: expected ActiveEscalation=normal, got %s", state.ActiveEscalation)
	}
	if state.ActivePriority != "Normal" {
		t.Errorf("Phase 1: expected ActivePriority=Normal, got %s", state.ActivePriority)
	}

	// Phase 2: both "normal" and "critical" trigger (4 failures), critical is higher
	jobHistory = map[string][]bigquery.ReleaseQualifiersProwjobSummaryResult{
		"periodic-ci-rosa-hcp": {
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
			{Name: "periodic-ci-rosa-hcp", State: "failure"},
		},
	}

	err = controller.processJobsForEscalations(
		context.Background(), releasePayload, releasePayload.Status.InformingJobResults,
		controller.configAccessor.Get(), jobHistory,
	)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	state = controller.getNotificationState(releasePayload, "rosa", threadID)
	if state.ActiveEscalation != "critical" {
		t.Errorf("Phase 2: expected ActiveEscalation=critical, got %s", state.ActiveEscalation)
	}
	if state.ActivePriority != "Critical" {
		t.Errorf("Phase 2: expected ActivePriority=Critical, got %s", state.ActivePriority)
	}
}

func TestHandleAbatement_NilClient(t *testing.T) {
	t.Parallel()

	controller := &JiraEscalationsController{
		ReleasePayloadController: &ReleasePayloadController{},
		jiraClient:               nil,
	}

	releasePayload := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{Name: "test-payload", Namespace: "ocp"},
	}
	jiraConfig := &jira.Notification{Project: "OCPBUGS"}
	state := v1alpha1.JiraNotificationState{
		ActiveEscalation: "critical",
		ActivePriority:   "Critical",
	}

	result := controller.handleAbatement(context.Background(), releasePayload, "rosa", jiraConfig, "thread-1", state)
	if !result.Abated {
		t.Error("Expected Abated=true even with nil client")
	}
}

func TestGetRelevantResults(t *testing.T) {
	t.Parallel()

	// Results ordered by completion DESC (most recent first), all within the last hour
	recentResults := []bigquery.ReleaseQualifiersProwjobSummaryResult{
		{Release: "4.19.0-0.nightly-1", Name: "test-job", State: "success", URL: "https://prow.ci/1", CompletionTime: completionTimeMinutesAgo(5)},
		{Release: "4.19.0-0.nightly-2", Name: "test-job", State: "failure", URL: "https://prow.ci/2", CompletionTime: completionTimeMinutesAgo(10)},
		{Release: "4.19.0-0.nightly-3", Name: "test-job", State: "success", URL: "https://prow.ci/3", CompletionTime: completionTimeMinutesAgo(15)},
		{Release: "4.19.0-0.nightly-4", Name: "test-job", State: "failure", URL: "https://prow.ci/4", CompletionTime: completionTimeMinutesAgo(20)},
		{Release: "4.19.0-0.nightly-5", Name: "test-job", State: "success", URL: "https://prow.ci/5", CompletionTime: completionTimeMinutesAgo(25)},
	}

	// Mixed: 3 recent (within 1 hour) and 2 old (3 days ago)
	mixedResults := []bigquery.ReleaseQualifiersProwjobSummaryResult{
		{Release: "4.19.0-0.nightly-1", Name: "test-job", State: "failure", URL: "https://prow.ci/1", CompletionTime: completionTimeMinutesAgo(10)},
		{Release: "4.19.0-0.nightly-2", Name: "test-job", State: "failure", URL: "https://prow.ci/2", CompletionTime: completionTimeMinutesAgo(20)},
		{Release: "4.19.0-0.nightly-3", Name: "test-job", State: "success", URL: "https://prow.ci/3", CompletionTime: completionTimeMinutesAgo(30)},
		{Release: "4.19.0-0.nightly-4", Name: "test-job", State: "failure", URL: "https://prow.ci/4", CompletionTime: completionTimeMinutesAgo(60 * 24 * 3)},  // 3 days ago
		{Release: "4.19.0-0.nightly-5", Name: "test-job", State: "failure", URL: "https://prow.ci/5", CompletionTime: completionTimeMinutesAgo(60*24*3 + 60)}, // 3 days + 1 hour ago
	}

	tests := []struct {
		name       string
		jobHistory []bigquery.ReleaseQualifiersProwjobSummaryResult
		windowSize int
		overPeriod string
		wantLen    int
	}{
		{
			name:       "No overPeriod - count window only",
			jobHistory: recentResults,
			windowSize: 3,
			overPeriod: "",
			wantLen:    3,
		},
		{
			name:       "No overPeriod - history shorter than window",
			jobHistory: recentResults[:3],
			windowSize: 5,
			overPeriod: "",
			wantLen:    3,
		},
		{
			name:       "OverPeriod provides more samples than OverLastRuns",
			jobHistory: recentResults,
			windowSize: 2,
			overPeriod: "7d",
			wantLen:    5, // all 5 within 7 days > windowSize 2
		},
		{
			name:       "OverLastRuns provides more samples than OverPeriod",
			jobHistory: mixedResults,
			windowSize: 5,
			overPeriod: "1h",
			wantLen:    5, // windowSize 5 > 3 within 1 hour
		},
		{
			name:       "OverPeriod and OverLastRuns tie - same count",
			jobHistory: mixedResults[:3],
			windowSize: 3,
			overPeriod: "2h",
			wantLen:    3,
		},
		{
			name:       "All results older than OverPeriod - count window wins",
			jobHistory: mixedResults[3:], // only old results
			windowSize: 2,
			overPeriod: "1h",
			wantLen:    2, // time window = 0, count window = 2
		},
		{
			name:       "Invalid OverPeriod falls back to count window",
			jobHistory: recentResults,
			windowSize: 2,
			overPeriod: "invalid",
			wantLen:    2,
		},
		{
			name:       "EmptyHistory",
			jobHistory: []bigquery.ReleaseQualifiersProwjobSummaryResult{},
			windowSize: 5,
			overPeriod: "2d",
			wantLen:    0,
		},
		{
			name:       "WindowSizeZero with overPeriod",
			jobHistory: recentResults,
			windowSize: 0,
			overPeriod: "7d",
			wantLen:    5, // time window = 5 > count window = 0
		},
	}

	controller := &JiraEscalationsController{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := controller.getRelevantResults(tt.jobHistory, tt.windowSize, tt.overPeriod)
			if len(got) != tt.wantLen {
				t.Errorf("getRelevantResults() returned %d items, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestParsePeriod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		period  string
		want    time.Duration
		wantErr bool
	}{
		{name: "hours", period: "24h", want: 24 * time.Hour},
		{name: "minutes", period: "30m", want: 30 * time.Minute},
		{name: "days", period: "2d", want: 48 * time.Hour},
		{name: "weeks", period: "1w", want: 7 * 24 * time.Hour},
		{name: "single day", period: "1d", want: 24 * time.Hour},
		{name: "two weeks", period: "2w", want: 14 * 24 * time.Hour},
		{name: "empty", period: "", wantErr: true},
		{name: "zero days", period: "0d", wantErr: true},
		{name: "negative", period: "-1d", wantErr: true},
		{name: "invalid suffix", period: "2x", wantErr: true},
		{name: "no number", period: "d", wantErr: true},
		{name: "letters only", period: "abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePeriod(tt.period)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parsePeriod(%q) expected error, got %v", tt.period, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePeriod(%q) unexpected error: %v", tt.period, err)
			}
			if got != tt.want {
				t.Errorf("parsePeriod(%q) = %v, want %v", tt.period, got, tt.want)
			}
		})
	}
}

func TestParsePeriodToSQLInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		period  string
		want    string
		wantErr bool
	}{
		{name: "hours", period: "24h", want: "24 HOUR"},
		{name: "days", period: "2d", want: "2 DAY"},
		{name: "weeks", period: "1w", want: "7 DAY"},
		{name: "two weeks", period: "2w", want: "14 DAY"},
		{name: "single hour", period: "1h", want: "1 HOUR"},
		{name: "empty", period: "", wantErr: true},
		{name: "zero", period: "0d", wantErr: true},
		{name: "negative", period: "-1d", wantErr: true},
		{name: "invalid suffix", period: "2x", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePeriodToSQLInterval(tt.period)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parsePeriodToSQLInterval(%q) expected error, got %q", tt.period, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePeriodToSQLInterval(%q) unexpected error: %v", tt.period, err)
			}
			if got != tt.want {
				t.Errorf("parsePeriodToSQLInterval(%q) = %q, want %q", tt.period, got, tt.want)
			}
		})
	}
}

// TestDebugProcessJobsForEscalations_RealData loads real BigQuery results from
// data/results/bquxjob_65df743f_19de556b6a3.json and feeds them into
// processJobsForEscalations for debugging.
func TestDebugProcessJobsForEscalations_RealData(t *testing.T) {
	// Load real BigQuery results
	dataPath := "../../../data/results/bquxjob_65df743f_19de556b6a3.json"
	raw, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("Failed to read BigQuery results: %v (run from repo root or adjust path)", err)
	}

	var rawRecords []struct {
		Release    string `json:"release_verify_tag"`
		Name       string `json:"prowjob_job_name"`
		State      string `json:"prowjob_state"`
		URL        string `json:"prowjob_url"`
		Start      string `json:"prowjob_start"`
		Completion string `json:"prowjob_completion"`
	}
	if err := json.Unmarshal(raw, &rawRecords); err != nil {
		t.Fatalf("Failed to parse BigQuery results: %v", err)
	}

	// Convert to ReleaseQualifiersProwjobSummaryResult
	var allHistory []bigquery.ReleaseQualifiersProwjobSummaryResult
	for _, r := range rawRecords {
		startTime, _ := time.Parse("2006-01-02T15:04:05", r.Start)
		completionTime, _ := time.Parse("2006-01-02T15:04:05", r.Completion)
		allHistory = append(allHistory, bigquery.ReleaseQualifiersProwjobSummaryResult{
			Release:        r.Release,
			Name:           r.Name,
			State:          r.State,
			URL:            r.URL,
			StartTime:      civil.DateTimeOf(startTime),
			CompletionTime: civil.DateTimeOf(completionTime),
		})
	}
	t.Logf("Loaded %d BigQuery results", len(allHistory))

	// Group by job name (same as the controller does)
	controller := &JiraEscalationsController{
		ReleasePayloadController: &ReleasePayloadController{},
	}
	historyByJob := controller.groupHistoryByJob(allHistory)
	for jobName, results := range historyByJob {
		successCount := 0
		for _, r := range results {
			if r.State == "success" {
				successCount++
			}
		}
		t.Logf("  %s: %d results (%d success, %d failure)",
			jobName, len(results), successCount, len(results)-successCount)
	}

	// Build ReleasePayload mirroring data/release-ocp-4.22_crt.json
	// driver-toolkit and fips-scan are blocking (crt-odd / crt-even)
	// aws-console and aws-csi are informing (crt-odd / crt-even)
	releasePayload := &v1alpha1.ReleasePayload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "4.22.0-0.nightly-2026-05-01-132406",
			Namespace: "ocp",
		},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadCoordinates: v1alpha1.PayloadCoordinates{StreamName: "4.22.0-0.nightly"},
			PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
				BlockingJobs: []v1alpha1.CIConfiguration{
					{
						CIConfigurationName:    "driver-toolkit",
						CIConfigurationJobName: "periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-driver-toolkit",
						Qualifiers: releasequalifierslib.ReleaseQualifiers{
							"crt-odd": {
								Notifications: &notifications.Notifications{
									Jira: &jira.Notification{Thread: "thread1-odd-blocking"},
								},
							},
						},
					},
					{
						CIConfigurationName:    "fips-scan",
						CIConfigurationJobName: "periodic-ci-openshift-release-main-nightly-4.22-fips-payload-scan",
						Qualifiers: releasequalifierslib.ReleaseQualifiers{
							"crt-even": {
								Notifications: &notifications.Notifications{
									Jira: &jira.Notification{Thread: "thread2-even-blocking"},
								},
							},
						},
					},
				},
				InformingJobs: []v1alpha1.CIConfiguration{
					{
						CIConfigurationName:    "aws-console",
						CIConfigurationJobName: "periodic-ci-openshift-release-main-nightly-4.22-console-aws",
						Qualifiers: releasequalifierslib.ReleaseQualifiers{
							"crt-odd": {
								Notifications: &notifications.Notifications{
									Jira: &jira.Notification{Thread: "thread1-odd-informing"},
								},
							},
						},
					},
					{
						CIConfigurationName:    "aws-csi",
						CIConfigurationJobName: "periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-csi",
						Qualifiers: releasequalifierslib.ReleaseQualifiers{
							"crt-even": {
								Notifications: &notifications.Notifications{
									Jira: &jira.Notification{Thread: "thread2-even-informing"},
								},
							},
						},
					},
				},
			},
		},
		Status: v1alpha1.ReleasePayloadStatus{
			BlockingJobResults: []v1alpha1.JobStatus{
				{CIConfigurationName: "driver-toolkit", CIConfigurationJobName: "periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-driver-toolkit", AggregateState: v1alpha1.JobStateFailure},
				{CIConfigurationName: "fips-scan", CIConfigurationJobName: "periodic-ci-openshift-release-main-nightly-4.22-fips-payload-scan", AggregateState: v1alpha1.JobStateFailure},
			},
			InformingJobResults: []v1alpha1.JobStatus{
				{CIConfigurationName: "aws-console", CIConfigurationJobName: "periodic-ci-openshift-release-main-nightly-4.22-console-aws", AggregateState: v1alpha1.JobStateFailure},
				{CIConfigurationName: "aws-csi", CIConfigurationJobName: "periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-csi", AggregateState: v1alpha1.JobStateFailure},
			},
		},
	}

	// Global qualifier config from data/crt-qualifiers.yaml
	qualifiersConfig := releasequalifierslib.ReleaseQualifiers{
		"crt-odd": releasequalifierslib.ReleaseQualifier{
			Enabled:   releasequalifierslib.BoolPtr(true),
			BadgeName: "CRT-ODD",
			Notifications: &notifications.Notifications{
				Jira: &jira.Notification{
					Project:     "CRT",
					Component:   "release-controller",
					Assignee:    "brawilli@redhat.com",
					Summary:     "Best summary ever ODD",
					Description: "Best description ever ODD",
					Escalations: []jira.Escalation{
						{Name: "low", Failures: 1, Priority: "Low"},
						{Name: "normal", Failures: 3, Priority: "Normal"},
						{Name: "high", Failures: 5, Priority: "High"},
						{Name: "critical", Failures: 7, Priority: "Critical"},
					},
				},
			},
		},
		"crt-even": releasequalifierslib.ReleaseQualifier{
			Enabled:   releasequalifierslib.BoolPtr(true),
			BadgeName: "CRT-EVEN",
			Notifications: &notifications.Notifications{
				Jira: &jira.Notification{
					Project:     "CRT",
					Component:   "release-controller",
					Assignee:    "brawilli@redhat.com",
					Summary:     "Best summary ever EVEN",
					Description: "Best description ever EVEN",
					Escalations: []jira.Escalation{
						{Name: "low", Failures: 2, Priority: "Low"},
						{Name: "normal", Failures: 4, Priority: "Normal"},
						{Name: "high", Failures: 6, Priority: "High"},
						{Name: "critical", Failures: 8, Priority: "Critical"},
					},
				},
			},
		},
	}

	// Set up fake Jira client
	mock := &mockJiraClient{FakeClient: fakejira.FakeClient{}, searchResults: map[string][]gojira.Issue{}}
	controller.jiraClient = mock
	controller.configAccessor = &mockConfigAccessor{config: qualifiersConfig}

	// Process blocking jobs
	//t.Log("=== Processing Blocking Jobs ===")
	//err = controller.processJobsForEscalations(
	//	context.Background(),
	//	releasePayload,
	//	releasePayload.Status.BlockingJobResults,
	//	qualifiersConfig,
	//	historyByJob,
	//)
	//if err != nil {
	//	t.Fatalf("processJobsForEscalations (blocking) error: %v", err)
	//}

	// Process informing jobs
	t.Log("=== Processing Informing Jobs ===")
	err = controller.processJobsForEscalations(
		context.Background(),
		releasePayload,
		releasePayload.Status.InformingJobResults,
		qualifiersConfig,
		historyByJob,
	)
	if err != nil {
		t.Fatalf("processJobsForEscalations error: %v", err)
	}

	// Dump results
	//t.Log("=== Jira Issues Created ===")
	//for i, issue := range mock.Issues {
	//	t.Logf("Issue %d: key=%s project=%s priority=%s summary=%s",
	//		i, issue.Key, issue.Fields.Project.Key,
	//		issue.Fields.Priority.Name, issue.Fields.Summary)
	//	t.Logf("  Labels: %v", issue.Fields.Labels)
	//	t.Logf("  Description: %.200s", issue.Fields.Description)
	//}
	//
	//t.Log("=== Notification State ===")
	//for qualifierID, summary := range releasePayload.Status.QualifiersSummary {
	//	for threadID, state := range summary.JiraNotifications {
	//		t.Logf("  qualifier=%s thread=%s escalation=%s priority=%s issueKey=%s abated=%v",
	//			qualifierID, threadID, state.ActiveEscalation, state.ActivePriority, state.IssueKey, state.Abated)
	//	}
	//}

	// Optionally assert specific outcomes here for debugging
	//if len(mock.Issues) == 0 {
	//	t.Log("No escalations triggered — adjust thresholds or check job history")
	//}
}

func TestAddWatchers(t *testing.T) {
	t.Parallel()

	t.Run("nil jiraClient does not panic", func(t *testing.T) {
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               nil,
		}
		controller.addWatchers("OCPBUGS-1", []string{"user1"})
	})

	t.Run("empty users does nothing", func(t *testing.T) {
		mock := &mockJiraClient{watchersAdded: make(map[string][]string)}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               mock,
			addWatcherFn: func(issueKey, username string) error {
				mock.watchersAdded[issueKey] = append(mock.watchersAdded[issueKey], username)
				return nil
			},
		}
		controller.addWatchers("OCPBUGS-1", nil)
		controller.addWatchers("OCPBUGS-1", []string{})
		if len(mock.watchersAdded) != 0 {
			t.Errorf("Expected no watchers added, got %v", mock.watchersAdded)
		}
	})

	t.Run("adds multiple watchers", func(t *testing.T) {
		mock := &mockJiraClient{watchersAdded: make(map[string][]string)}
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               mock,
			addWatcherFn: func(issueKey, username string) error {
				mock.watchersAdded[issueKey] = append(mock.watchersAdded[issueKey], username)
				return nil
			},
		}

		controller.addWatchers("OCPBUGS-1", []string{"user1", "user2", "user3"})

		watchers := mock.watchersAdded["OCPBUGS-1"]
		if len(watchers) != 3 {
			t.Fatalf("Expected 3 watchers, got %d", len(watchers))
		}
		if watchers[0] != "user1" || watchers[1] != "user2" || watchers[2] != "user3" {
			t.Errorf("Expected [user1, user2, user3], got %v", watchers)
		}
	})

	t.Run("continues on individual watcher failure", func(t *testing.T) {
		mock := &mockJiraClient{watchersAdded: make(map[string][]string)}
		callCount := 0
		controller := &JiraEscalationsController{
			ReleasePayloadController: &ReleasePayloadController{},
			jiraClient:               mock,
			addWatcherFn: func(issueKey, username string) error {
				callCount++
				if username == "baduser" {
					return fmt.Errorf("user not found")
				}
				mock.watchersAdded[issueKey] = append(mock.watchersAdded[issueKey], username)
				return nil
			},
		}

		controller.addWatchers("OCPBUGS-1", []string{"user1", "baduser", "user2"})

		if callCount != 3 {
			t.Errorf("Expected 3 calls to addWatcherFn, got %d", callCount)
		}
		watchers := mock.watchersAdded["OCPBUGS-1"]
		if len(watchers) != 2 || watchers[0] != "user1" || watchers[1] != "user2" {
			t.Errorf("Expected [user1, user2] (skipping baduser), got %v", watchers)
		}
	})

}

func TestGetJobHistory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupClient  func() *bigquery.FakeClient
		useNilClient bool
		defaultJobs  []string
		filteredJobs []bigquery.ProwjobQueryFilter
		wantLen      int
		wantErr      string
	}{
		{
			name:         "NilBigQueryClient",
			useNilClient: true,
			defaultJobs:  []string{"job-1"},
			wantLen:      0,
		},
		{
			name: "BigQueryReturnsError",
			setupClient: func() *bigquery.FakeClient {
				c := bigquery.NewFakeClient()
				c.SummaryError = fmt.Errorf("connection refused")
				return c
			},
			defaultJobs: []string{"job-1"},
			wantErr:     "failed to query BigQuery for jobs",
		},
		{
			name: "SuccessfulQuery with default jobs",
			setupClient: func() *bigquery.FakeClient {
				c := bigquery.NewFakeClient()
				c.DefaultResult = []any{
					bigquery.ReleaseQualifiersProwjobSummaryResult{
						Release: "4.19.0-0.nightly-1", Name: "job-1", State: "success", URL: "https://prow.ci/1",
					},
					bigquery.ReleaseQualifiersProwjobSummaryResult{
						Release: "4.19.0-0.nightly-2", Name: "job-1", State: "failure", URL: "https://prow.ci/2",
					},
				}
				return c
			},
			defaultJobs: []string{"job-1"},
			wantLen:     2,
		},
		{
			name: "SuccessfulQuery with filtered jobs",
			setupClient: func() *bigquery.FakeClient {
				c := bigquery.NewFakeClient()
				c.DefaultResult = []any{
					bigquery.ReleaseQualifiersProwjobSummaryResult{
						Release: "4.19.0-0.nightly-1", Name: "job-1", State: "success", URL: "https://prow.ci/1",
					},
				}
				return c
			},
			filteredJobs: []bigquery.ProwjobQueryFilter{
				{Name: "job-1", Interval: "2 DAY"},
			},
			wantLen: 1,
		},
		{
			name: "EmptyResults",
			setupClient: func() *bigquery.FakeClient {
				return bigquery.NewFakeClient()
			},
			defaultJobs: []string{},
			wantLen:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &JiraEscalationsController{
				ReleasePayloadController: &ReleasePayloadController{},
			}

			if !tt.useNilClient {
				controller.bigQueryClient = tt.setupClient()
			}

			results, err := controller.getJobHistory(context.Background(), tt.defaultJobs, tt.filteredJobs)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(results) != tt.wantLen {
				t.Errorf("Expected %d results, got %d", tt.wantLen, len(results))
			}
		})
	}
}
