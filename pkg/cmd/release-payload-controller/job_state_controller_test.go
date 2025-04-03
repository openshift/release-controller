package release_payload_controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/client/clientset/versioned/fake"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func TestJobStateSync(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    *v1alpha1.ReleasePayload
		expected *v1alpha1.ReleasePayload
	}{
		{
			name: "ReleasePayloadWithNoJobs",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults:  []v1alpha1.JobStatus{},
					InformingJobResults: []v1alpha1.JobStatus{},
					UpgradeJobResults:   []v1alpha1.JobStatus{},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults:  []v1alpha1.JobStatus{},
					InformingJobResults: []v1alpha1.JobStatus{},
					UpgradeJobResults:   []v1alpha1.JobStatus{},
				},
			},
		},
		{
			name: "ReleasePayloadWithNoJobRunResults",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aws-serial",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws-serial",
							JobRunResults:          []v1alpha1.JobRunResult{},
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aws-serial",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws-serial",
							JobRunResults:          []v1alpha1.JobRunResult{},
							AggregateState:         v1alpha1.JobStateUnknown,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithSuccessfulJobRunResults",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aws-serial",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws-serial",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									Coordinates: v1alpha1.JobRunCoordinates{
										Name:      "4.11.0-0.nightly-2022-02-09-091559-aws-serial",
										Namespace: "ci",
										Cluster:   "build04",
									},
									State: v1alpha1.JobRunStateSuccess,
								},
							},
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aws-serial",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws-serial",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									Coordinates: v1alpha1.JobRunCoordinates{
										Name:      "4.11.0-0.nightly-2022-02-09-091559-aws-serial",
										Namespace: "ci",
										Cluster:   "build04",
									},
									State: v1alpha1.JobRunStateSuccess,
								},
							},
							AggregateState: v1alpha1.JobStateSuccess,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithFailedJobRunResults",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aws-serial",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws-serial",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									Coordinates: v1alpha1.JobRunCoordinates{
										Name:      "4.11.0-0.nightly-2022-02-09-091559-aws-serial",
										Namespace: "ci",
										Cluster:   "build04",
									},
									State: v1alpha1.JobRunStateFailure,
								},
							},
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aws-serial",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws-serial",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									Coordinates: v1alpha1.JobRunCoordinates{
										Name:      "4.11.0-0.nightly-2022-02-09-091559-aws-serial",
										Namespace: "ci",
										Cluster:   "build04",
									},
									State: v1alpha1.JobRunStateFailure,
								},
							},
							AggregateState: v1alpha1.JobStateFailure,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithPendingJobRunResults",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aws-serial",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws-serial",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									Coordinates: v1alpha1.JobRunCoordinates{
										Name:      "4.11.0-0.nightly-2022-02-09-091559-aws-serial",
										Namespace: "ci",
										Cluster:   "build04",
									},
									State: v1alpha1.JobRunStateFailure,
								},
							},
							MaxRetries: 3,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aws-serial",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws-serial",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									Coordinates: v1alpha1.JobRunCoordinates{
										Name:      "4.11.0-0.nightly-2022-02-09-091559-aws-serial",
										Namespace: "ci",
										Cluster:   "build04",
									},
									State: v1alpha1.JobRunStateFailure,
								},
							},
							MaxRetries:     3,
							AggregateState: v1alpha1.JobStatePending,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithPendingAnalysisJobRunResults",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aggregated-aws-sdn-upgrade-4.11-micro",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws-upgrade",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									Coordinates: v1alpha1.JobRunCoordinates{
										Name:      "4.11.0-0.nightly-2022-02-09-091559-aggregate-4jpbx8b-aggregator",
										Namespace: "ci",
										Cluster:   "build04",
									},
									State: v1alpha1.JobRunStatePending,
								},
							},
						},
					},
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aggregated-aws-sdn-upgrade-4.11-micro",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws-upgrade",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									Coordinates: v1alpha1.JobRunCoordinates{
										Name:      "4.11.0-0.nightly-2022-02-09-091559-aggregate-4jpbx8b-analysis-0",
										Namespace: "ci",
										Cluster:   "build04",
									},
									State: v1alpha1.JobRunStateSuccess,
								},
								{
									Coordinates: v1alpha1.JobRunCoordinates{
										Name:      "4.11.0-0.nightly-2022-02-09-091559-aggregate-4jpbx8b-analysis-1",
										Namespace: "ci",
										Cluster:   "build04",
									},
									State: v1alpha1.JobRunStatePending,
								},
								{
									Coordinates: v1alpha1.JobRunCoordinates{
										Name:      "4.11.0-0.nightly-2022-02-09-091559-aggregate-4jpbx8b-analysis-2",
										Namespace: "ci",
										Cluster:   "build04",
									},
									State: v1alpha1.JobRunStateFailure,
								},
							},
							AnalysisJobCount: 3,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aggregated-aws-sdn-upgrade-4.11-micro",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws-upgrade",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									Coordinates: v1alpha1.JobRunCoordinates{
										Name:      "4.11.0-0.nightly-2022-02-09-091559-aggregate-4jpbx8b-aggregator",
										Namespace: "ci",
										Cluster:   "build04",
									},
									State: v1alpha1.JobRunStatePending,
								},
							},
							AggregateState: v1alpha1.JobStatePending,
						},
					},
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "aggregated-aws-sdn-upgrade-4.11-micro",
							CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws-upgrade",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									Coordinates: v1alpha1.JobRunCoordinates{
										Name:      "4.11.0-0.nightly-2022-02-09-091559-aggregate-4jpbx8b-analysis-0",
										Namespace: "ci",
										Cluster:   "build04",
									},
									State: v1alpha1.JobRunStateSuccess,
								},
								{
									Coordinates: v1alpha1.JobRunCoordinates{
										Name:      "4.11.0-0.nightly-2022-02-09-091559-aggregate-4jpbx8b-analysis-1",
										Namespace: "ci",
										Cluster:   "build04",
									},
									State: v1alpha1.JobRunStatePending,
								},
								{
									Coordinates: v1alpha1.JobRunCoordinates{
										Name:      "4.11.0-0.nightly-2022-02-09-091559-aggregate-4jpbx8b-analysis-2",
										Namespace: "ci",
										Cluster:   "build04",
									},
									State: v1alpha1.JobRunStateFailure,
								},
							},
							AnalysisJobCount: 3,
							AggregateState:   v1alpha1.JobStatePending,
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

			c, err := NewJobStateController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), events.NewInMemoryRecorder("job-state-controller-test"))
			if err != nil {
				t.Fatalf("Failed to create Job State Controller: %v", err)
			}

			releasePayloadInformerFactory.Start(context.Background().Done())

			if !cache.WaitForNamedCacheSync("JobStateController", context.Background().Done(), c.cachesToSync...) {
				t.Errorf("%s: error waiting for caches to sync", testCase.name)
				return
			}

			if err := c.sync(context.TODO(), fmt.Sprintf("%s/%s", testCase.input.Namespace, testCase.input.Name)); err != nil {
				t.Errorf("%s: unexpected err: %v", testCase.name, err)
			}

			// Performing a live lookup instead of having to wait for the cache to sink (again)...
			output, _ := c.releasePayloadClient.ReleasePayloads(testCase.input.Namespace).Get(context.TODO(), testCase.input.Name, metav1.GetOptions{})
			if !cmp.Equal(output, testCase.expected, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, output)
			}
		})
	}
}

func TestComputeJobState(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    v1alpha1.JobStatus
		expected v1alpha1.JobState
	}{
		{
			name: "NoJobRunResults",
			input: v1alpha1.JobStatus{
				CIConfigurationName:    "aws",
				CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws",
				JobRunResults:          []v1alpha1.JobRunResult{},
			},
			expected: v1alpha1.JobStateUnknown,
		},
		{
			name: "SuccessfulJobRunResult",
			input: v1alpha1.JobStatus{
				CIConfigurationName:    "aws",
				CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws",
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateSuccess,
					},
				},
			},
			expected: v1alpha1.JobStateSuccess,
		},
		{
			name: "FailedJobRunResult",
			input: v1alpha1.JobStatus{
				CIConfigurationName:    "aws",
				CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws",
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateFailure,
					},
				},
			},
			expected: v1alpha1.JobStateFailure,
		},
		{
			name: "SuccessfulJobRunResultWithRetries",
			input: v1alpha1.JobStatus{
				CIConfigurationName:    "aws",
				CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws",
				MaxRetries:             3,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateSuccess,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
				},
			},
			expected: v1alpha1.JobStateSuccess,
		},
		{
			name: "FailedJobRunResultWithRetries",
			input: v1alpha1.JobStatus{
				CIConfigurationName:    "aws",
				CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws",
				MaxRetries:             3,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
				},
			},
			expected: v1alpha1.JobStateFailure,
		},
		{
			name: "PendingJobsRunResultWithRetries",
			input: v1alpha1.JobStatus{
				CIConfigurationName:    "aws",
				CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws",
				MaxRetries:             3,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
				},
			},
			expected: v1alpha1.JobStatePending,
		},
		{
			name: "SuccessfulJobsRunResultWithAnalysisJobCount",
			input: v1alpha1.JobStatus{
				CIConfigurationName:    "aws",
				CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws",
				AnalysisJobCount:       5,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateSuccess,
					},
					{
						State: v1alpha1.JobRunStateSuccess,
					},
					{
						State: v1alpha1.JobRunStateSuccess,
					},
					{
						State: v1alpha1.JobRunStateSuccess,
					},
					{
						State: v1alpha1.JobRunStateSuccess,
					},
				},
			},
			expected: v1alpha1.JobStateSuccess,
		},
		{
			name: "FailedJobsRunResultWithAnalysisJobCount",
			input: v1alpha1.JobStatus{
				CIConfigurationName:    "aws",
				CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws",
				AnalysisJobCount:       5,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
				},
			},
			expected: v1alpha1.JobStateFailure,
		},
		{
			name: "SuccessfulJobsRunResultWithAnalysisJobCount",
			input: v1alpha1.JobStatus{
				CIConfigurationName:    "aws",
				CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws",
				AnalysisJobCount:       5,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateSuccess,
					},
					{
						State: v1alpha1.JobRunStateSuccess,
					},
					{
						State: v1alpha1.JobRunStateSuccess,
					},
					{
						State: v1alpha1.JobRunStateSuccess,
					},
					{
						State: v1alpha1.JobRunStateSuccess,
					},
				},
			},
			expected: v1alpha1.JobStateSuccess,
		},
		{
			name: "PendingJobsRunResultWithAnalysisJobCount",
			input: v1alpha1.JobStatus{
				CIConfigurationName:    "aws",
				CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws",
				AnalysisJobCount:       5,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateSuccess,
					},
					{
						State: v1alpha1.JobRunStatePending,
					},
					{
						State: v1alpha1.JobRunStatePending,
					},
					{
						State: v1alpha1.JobRunStatePending,
					},
					{
						State: v1alpha1.JobRunStatePending,
					},
				},
			},
			expected: v1alpha1.JobStatePending,
		},
		{
			name: "FailedJobsRunResultWithAnalysisJobCount",
			input: v1alpha1.JobStatus{
				CIConfigurationName:    "aws",
				CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws",
				AnalysisJobCount:       5,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
				},
			},
			expected: v1alpha1.JobStateFailure,
		},
		{
			name: "UnknownJobsRunResultWithAnalysisJobCount",
			input: v1alpha1.JobStatus{
				CIConfigurationName:    "aws",
				CIConfigurationJobName: "periodic-ci-openshift-release-master-nightly-4.11-e2e-aws",
				AnalysisJobCount:       5,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateSuccess,
					},
					{
						State: v1alpha1.JobRunStateSuccess,
					},
					{
						State: v1alpha1.JobRunStateSuccess,
					},
				},
			},
			expected: v1alpha1.JobStateUnknown,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := computeJobState(testCase.input)
			if !cmp.Equal(result, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, result)
			}
		})
	}
}

func TestInterrogateJobResults(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name                      string
		results                   []v1alpha1.JobRunResult
		expectedPendingJobs       []v1alpha1.JobRunResult
		expectedSuccessfulJobs    []v1alpha1.JobRunResult
		expectedFailedJobs        []v1alpha1.JobRunResult
		expectedUnknownFailedJobs []v1alpha1.JobRunResult
	}{
		{
			name: "SliceAndDice",
			results: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateSuccess,
				},
				{
					State: v1alpha1.JobRunStateScheduling,
				},
				{
					State: v1alpha1.JobRunStateTriggered,
				},
				{
					State: v1alpha1.JobRunStatePending,
				},
				{
					State: v1alpha1.JobRunStateFailure,
				},
				{
					State: v1alpha1.JobRunStateAborted,
				},
				{
					State: v1alpha1.JobRunStateError,
				},
				{
					State: "foobar",
				},
			},
			expectedPendingJobs: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateScheduling,
				},
				{
					State: v1alpha1.JobRunStateTriggered,
				},
				{
					State: v1alpha1.JobRunStatePending,
				},
			},
			expectedSuccessfulJobs: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateSuccess,
				},
			},
			expectedFailedJobs: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateFailure,
				},
				{
					State: v1alpha1.JobRunStateAborted,
				},
				{
					State: v1alpha1.JobRunStateError,
				},
			},
			expectedUnknownFailedJobs: []v1alpha1.JobRunResult{
				{
					State: "foobar",
				},
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var pendingJobs, successfulJobs, failedJobs, unknownJobs = interrogateJobResults(testCase.results)

			if !cmp.Equal(pendingJobs, testCase.expectedPendingJobs) {
				t.Errorf("%s: Expected pending jobs %v, got %v", testCase.name, testCase.expectedPendingJobs, pendingJobs)
			}
			if !cmp.Equal(successfulJobs, testCase.expectedSuccessfulJobs) {
				t.Errorf("%s: Expected successful jobs %v, got %v", testCase.name, testCase.expectedSuccessfulJobs, successfulJobs)
			}
			if !cmp.Equal(failedJobs, testCase.expectedFailedJobs) {
				t.Errorf("%s: Expected failed jobs %v, got %v", testCase.name, testCase.expectedFailedJobs, failedJobs)
			}
			if !cmp.Equal(unknownJobs, testCase.expectedUnknownFailedJobs) {
				t.Errorf("%s: Expected unknown jobs %v, got %v", testCase.name, testCase.expectedUnknownFailedJobs, unknownJobs)
			}
		})
	}
}

func TestProcessJobResults(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		results       []v1alpha1.JobRunResult
		expectedState v1alpha1.JobState
	}{
		{
			name: "SuccessfulJobState",
			results: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateSuccess,
				},
				{
					State: v1alpha1.JobRunStateScheduling,
				},
				{
					State: v1alpha1.JobRunStateTriggered,
				},
				{
					State: v1alpha1.JobRunStatePending,
				},
				{
					State: v1alpha1.JobRunStateFailure,
				},
				{
					State: v1alpha1.JobRunStateAborted,
				},
				{
					State: v1alpha1.JobRunStateError,
				},
				{
					State: "foobar",
				},
			},
			expectedState: v1alpha1.JobStateSuccess,
		},
		{
			name: "PendingJobState",
			results: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateScheduling,
				},
				{
					State: v1alpha1.JobRunStateTriggered,
				},
				{
					State: v1alpha1.JobRunStatePending,
				},
				{
					State: v1alpha1.JobRunStateFailure,
				},
				{
					State: v1alpha1.JobRunStateAborted,
				},
				{
					State: v1alpha1.JobRunStateError,
				},
				{
					State: "foobar",
				},
			},
			expectedState: v1alpha1.JobStatePending,
		},
		{
			name: "FailureJobState",
			results: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateFailure,
				},
				{
					State: v1alpha1.JobRunStateAborted,
				},
				{
					State: v1alpha1.JobRunStateError,
				},
				{
					State: "foobar",
				},
			},
			expectedState: v1alpha1.JobStateFailure,
		},
		{
			name: "UnknownJobState",
			results: []v1alpha1.JobRunResult{
				{
					State: "foobar",
				},
			},
			expectedState: v1alpha1.JobStateUnknown,
		},
		{
			name:          "NoResults",
			results:       []v1alpha1.JobRunResult{},
			expectedState: v1alpha1.JobStateUnknown,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var state = processJobResults(testCase.results)

			if !cmp.Equal(state, testCase.expectedState) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expectedState, state)
			}
		})
	}
}

func TestProcessAnalysisJobResults(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		results       []v1alpha1.JobRunResult
		count         int
		expectedState v1alpha1.JobState
	}{
		{
			name: "IncompleteJobRunResults",
			results: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateSuccess,
				},
				{
					State: v1alpha1.JobRunStatePending,
				},
			},
			count:         3,
			expectedState: v1alpha1.JobStateUnknown,
		},
		{
			name: "PendingJobState",
			results: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateSuccess,
				},
				{
					State: v1alpha1.JobRunStatePending,
				},
				{
					State: v1alpha1.JobRunStatePending,
				},
			},
			count:         3,
			expectedState: v1alpha1.JobStatePending,
		},
		{
			name: "FailedJobState",
			results: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateSuccess,
				},
				{
					State: v1alpha1.JobRunStateFailure,
				},
				{
					State: v1alpha1.JobRunStateFailure,
				},
			},
			count:         3,
			expectedState: v1alpha1.JobStateFailure,
		},
		{
			name: "SuccessJobState",
			results: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateSuccess,
				},
				{
					State: v1alpha1.JobRunStateSuccess,
				},
				{
					State: v1alpha1.JobRunStateSuccess,
				},
			},
			count:         3,
			expectedState: v1alpha1.JobStateSuccess,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var state = processAnalysisJobResults(testCase.results, testCase.count)

			if !cmp.Equal(state, testCase.expectedState) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expectedState, state)
			}
		})
	}
}

func TestProcessRetryJobResults(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		results       []v1alpha1.JobRunResult
		count         int
		expectedState v1alpha1.JobState
	}{
		{
			name: "SuccessfulJobRunResults",
			results: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateSuccess,
				},
				{
					State: v1alpha1.JobRunStatePending,
				},
			},
			count:         3,
			expectedState: v1alpha1.JobStateSuccess,
		},
		{
			name: "PendingJobRunResults",
			results: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStatePending,
				},
			},
			count:         3,
			expectedState: v1alpha1.JobStatePending,
		},
		{
			name: "FailureJobRunResultsWithRetries",
			results: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateFailure,
				},
				{
					State: v1alpha1.JobRunStateFailure,
				},
			},
			count:         3,
			expectedState: v1alpha1.JobStatePending,
		},
		{
			name: "FailureJobRunResults",
			results: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateFailure,
				},
				{
					State: v1alpha1.JobRunStateFailure,
				},
				{
					State: v1alpha1.JobRunStateFailure,
				},
				{
					State: v1alpha1.JobRunStateFailure,
				},
			},
			count:         3,
			expectedState: v1alpha1.JobStateFailure,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var state = processRetryJobResults(testCase.results, testCase.count)

			if !cmp.Equal(state, testCase.expectedState) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expectedState, state)
			}
		})
	}
}
