package release_payload_controller

import (
	"context"
	"fmt"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/client/clientset/versioned/fake"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	v1 "k8s.io/test-infra/prow/apis/prowjobs/v1"
	prowfake "k8s.io/test-infra/prow/client/clientset/versioned/fake"
	prowjobinformers "k8s.io/test-infra/prow/client/informers/externalversions"
	"k8s.io/test-infra/prow/kube"
	"reflect"
	"testing"
)

func newJobStatus(name, jobName string, maxRetries, analysisJobCount int, aggregateState v1alpha1.JobState, results []v1alpha1.JobRunResult) v1alpha1.JobStatus {
	return v1alpha1.JobStatus{
		CIConfigurationName:    name,
		CIConfigurationJobName: jobName,
		MaxRetries:             maxRetries,
		AnalysisJobCount:       analysisJobCount,
		AggregateState:         aggregateState,
		JobRunResults:          results,
	}
}

func newJobStatusPointer(name, jobName string, maxRetries, analysisJobCount int, aggregateState v1alpha1.JobState, results []v1alpha1.JobRunResult) *v1alpha1.JobStatus {
	status := newJobStatus(name, jobName, maxRetries, analysisJobCount, aggregateState, results)
	return &status
}

func newJobRunResult(name, namespace, cluster string, state v1alpha1.JobRunState, url string) v1alpha1.JobRunResult {
	return v1alpha1.JobRunResult{
		Coordinates: v1alpha1.JobRunCoordinates{
			Name:      name,
			Namespace: namespace,
			Cluster:   cluster,
		},
		State:               state,
		HumanProwResultsURL: url,
	}
}

func newProwJob(name, namespace, release, jobName, source, cluster string, state v1.ProwJobState, url string) *v1.ProwJob {
	return &v1.ProwJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				releasecontroller.ReleaseAnnotationVerify: "true",
				releasecontroller.ReleaseLabelPayload:     release,
			},
			Annotations: map[string]string{
				kube.ProwJobAnnotation:                    jobName,
				releasecontroller.ReleaseAnnotationSource: source,
				releasecontroller.ReleaseAnnotationToTag:  release,
			},
		},
		Spec: v1.ProwJobSpec{
			Cluster: cluster,
		},
		Status: v1.ProwJobStatus{
			State: state,
			URL:   url,
		},
	}
}

func TestSetJobStatus(t *testing.T) {
	testCases := []struct {
		name      string
		input     *v1alpha1.ReleasePayloadStatus
		jobStatus v1alpha1.JobStatus
		expected  *v1alpha1.ReleasePayloadStatus
	}{
		{
			name: "NoMatchingJob",
			input: &v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{},
			},
			jobStatus: newJobStatus("A", "B", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{}),
			expected: &v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{},
			},
		},
		{
			name: "MatchingBlockingJobUpdated",
			input: &v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{
					newJobStatus("A", "B", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{}),
				},
			},
			jobStatus: newJobStatus("A", "B", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
				newJobRunResult("X", "Y", "Z", v1alpha1.JobRunStatePending, "https://abc.123.com"),
			}),
			expected: &v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{
					newJobStatus("A", "B", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
						newJobRunResult("X", "Y", "Z", v1alpha1.JobRunStatePending, "https://abc.123.com"),
					}),
				},
			},
		},
		{
			name: "MatchingInformingJobUpdated",
			input: &v1alpha1.ReleasePayloadStatus{
				InformingJobResults: []v1alpha1.JobStatus{
					newJobStatus("A", "B", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{}),
				},
			},
			jobStatus: newJobStatus("A", "B", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
				newJobRunResult("X", "Y", "Z", v1alpha1.JobRunStatePending, "https://abc.123.com"),
			}),
			expected: &v1alpha1.ReleasePayloadStatus{
				InformingJobResults: []v1alpha1.JobStatus{
					newJobStatus("A", "B", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
						newJobRunResult("X", "Y", "Z", v1alpha1.JobRunStatePending, "https://abc.123.com"),
					}),
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setJobStatus(testCase.input, testCase.jobStatus)
			if !reflect.DeepEqual(testCase.input, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, testCase.input)
			}
		})
	}
}

func TestFindJobStatus(t *testing.T) {
	testCases := []struct {
		name                   string
		payloadName            string
		input                  *v1alpha1.ReleasePayloadStatus
		ciConfigurationName    string
		ciConfigurationJobName string
		expected               *v1alpha1.JobStatus
		expectedError          string
	}{
		{
			name:                   "NoJobs",
			payloadName:            "4.11.0-0.nightly-2022-06-23-153912",
			input:                  &v1alpha1.ReleasePayloadStatus{},
			ciConfigurationName:    "A",
			ciConfigurationJobName: "B",
			expected:               nil,
			expectedError:          "unable to locate job results for A (B)",
		},
		{
			name:        "BlockingJob",
			payloadName: "4.11.0-0.nightly-2022-06-23-153912",
			input: &v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{
					newJobStatus("A", "B", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
						newJobRunResult("X", "Y", "Z", v1alpha1.JobRunStatePending, "https://abc.123.com"),
					}),
				},
			},
			ciConfigurationName:    "A",
			ciConfigurationJobName: "B",
			expected: newJobStatusPointer("A", "B", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
				newJobRunResult("X", "Y", "Z", v1alpha1.JobRunStatePending, "https://abc.123.com"),
			}),
		},
		{
			name:        "InformingJob",
			payloadName: "4.11.0-0.nightly-2022-06-23-153912",
			input: &v1alpha1.ReleasePayloadStatus{
				InformingJobResults: []v1alpha1.JobStatus{
					newJobStatus("A", "B", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
						newJobRunResult("X", "Y", "Z", v1alpha1.JobRunStatePending, "https://abc.123.com"),
					}),
				},
			},
			ciConfigurationName:    "A",
			ciConfigurationJobName: "B",
			expected: newJobStatusPointer("A", "B", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
				newJobRunResult("X", "Y", "Z", v1alpha1.JobRunStatePending, "https://abc.123.com"),
			}),
		},
		{
			name:        "NoMatchingJob",
			payloadName: "4.11.0-0.nightly-2022-06-23-153912",
			input: &v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "A",
						CIConfigurationJobName: "B",
					},
				},
				InformingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "C",
						CIConfigurationJobName: "D",
					},
				},
			},
			ciConfigurationName:    "X",
			ciConfigurationJobName: "Y",
			expected:               nil,
			expectedError:          "unable to locate job results for X (Y)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			jobStatus, err := findJobStatus(testCase.payloadName, testCase.input, testCase.ciConfigurationName, testCase.ciConfigurationJobName)
			if err != nil && !cmp.Equal(err.Error(), testCase.expectedError) {
				t.Fatalf("%s: Expected error %v, got %v", testCase.name, testCase.expectedError, err)
			}
			if !reflect.DeepEqual(jobStatus, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, jobStatus)
			}
		})
	}
}

func TestProwJobStatusSync(t *testing.T) {
	testCases := []struct {
		name        string
		prowjob     []runtime.Object
		input       *v1alpha1.ReleasePayload
		expected    *v1alpha1.ReleasePayload
		expectedErr error
	}{
		{
			name: "MissingJobStatus",
			prowjob: []runtime.Object{
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aws-serial", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-serial", "ocp/4.11-art-latest", "build01", v1.SuccessState, "https://abc.123.com"),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{},
				},
			},
		},
		{
			name: "SingleProwJob",
			prowjob: []runtime.Object{
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aws-serial", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-serial", "ocp/4.11-art-latest", "build01", v1.SuccessState, "https://abc.123.com"),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						newJobStatus("aws-serial", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-serial", 0, 0, v1alpha1.JobStateUnknown, nil),
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						newJobStatus("aws-serial", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-serial", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aws-serial", "ci", "build01", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
						}),
					},
				},
			},
		},
		{
			name: "MultipleProwJobs",
			prowjob: []runtime.Object{
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aws-serial", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-serial", "ocp/4.11-art-latest", "build01", v1.SuccessState, "https://abc.123.com"),
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aws-single-node", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-single-node", "ocp/4.11-art-latest", "build02", v1.SuccessState, "https://abc.123.com"),
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aws-techpreview", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-techpreview", "ocp/4.11-art-latest", "build01", v1.SuccessState, "https://abc.123.com"),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						newJobStatus("aws-serial", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-serial", 0, 0, v1alpha1.JobStateUnknown, nil),
						newJobStatus("aws-single-node", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-single-node", 0, 0, v1alpha1.JobStateUnknown, nil),
						newJobStatus("aws-techpreview", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-techpreview", 0, 0, v1alpha1.JobStateUnknown, nil),
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						newJobStatus("aws-serial", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-serial", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aws-serial", "ci", "build01", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
						}),
						newJobStatus("aws-single-node", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-single-node", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aws-single-node", "ci", "build02", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
						}),
						newJobStatus("aws-techpreview", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-techpreview", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aws-techpreview", "ci", "build01", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
						}),
					},
				},
			},
		},
		{
			name: "RetriedProwJob",
			prowjob: []runtime.Object{
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aws-serial", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-serial", "ocp/4.11-art-latest", "build01", v1.FailureState, "https://abc.123.com"),
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aws-serial-1", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-serial", "ocp/4.11-art-latest", "build02", v1.FailureState, "https://abc.123.com"),
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aws-serial-2", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-serial", "ocp/4.11-art-latest", "build01", v1.PendingState, "https://abc.123.com"),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						newJobStatus("aws-serial", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-serial", 3, 0, v1alpha1.JobStateUnknown, nil),
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						newJobStatus("aws-serial", "periodic-ci-openshift-release-master-nightly-4.10-e2e-aws-serial", 3, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aws-serial", "ci", "build01", v1alpha1.JobRunStateFailure, "https://abc.123.com"),
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aws-serial-1", "ci", "build02", v1alpha1.JobRunStateFailure, "https://abc.123.com"),
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aws-serial-2", "ci", "build01", v1alpha1.JobRunStatePending, "https://abc.123.com"),
						}),
					},
				},
			},
		},
		{
			name: "AnalysisProwJobs",
			prowjob: []runtime.Object{
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-aggregator", "ci", "4.11.0-0.nightly-2022-02-09-091559", "aggregated-azure-ovn-upgrade-4.11-micro-release-openshift-release-analysis-aggregator", "ocp/4.11-art-latest", "build01", v1.SuccessState, "https://abc.123.com"),
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-0", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-ci-4.11-e2e-azure-ovn-upgrade", "ocp/4.11-art-latest", "build01", v1.SuccessState, "https://abc.123.com"),
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-1", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-ci-4.11-e2e-azure-ovn-upgrade", "ocp/4.11-art-latest", "build02", v1.SuccessState, "https://abc.123.com"),
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-2", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-ci-4.11-e2e-azure-ovn-upgrade", "ocp/4.11-art-latest", "build01", v1.SuccessState, "https://abc.123.com"),
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-3", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-ci-4.11-e2e-azure-ovn-upgrade", "ocp/4.11-art-latest", "build03", v1.SuccessState, "https://abc.123.com"),
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-4", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-ci-4.11-e2e-azure-ovn-upgrade", "ocp/4.11-art-latest", "build02", v1.SuccessState, "https://abc.123.com"),
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-5", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-ci-4.11-e2e-azure-ovn-upgrade", "ocp/4.11-art-latest", "build05", v1.SuccessState, "https://abc.123.com"),
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-6", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-ci-4.11-e2e-azure-ovn-upgrade", "ocp/4.11-art-latest", "build01", v1.SuccessState, "https://abc.123.com"),
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-7", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-ci-4.11-e2e-azure-ovn-upgrade", "ocp/4.11-art-latest", "build04", v1.SuccessState, "https://abc.123.com"),
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-8", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-ci-4.11-e2e-azure-ovn-upgrade", "ocp/4.11-art-latest", "build02", v1.SuccessState, "https://abc.123.com"),
				newProwJob("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-9", "ci", "4.11.0-0.nightly-2022-02-09-091559", "periodic-ci-openshift-release-master-ci-4.11-e2e-azure-ovn-upgrade", "ocp/4.11-art-latest", "build01", v1.SuccessState, "https://abc.123.com"),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						newJobStatus("aggregated-azure-ovn-upgrade-4.11-micro", "aggregated-azure-ovn-upgrade-4.11-micro-release-openshift-release-analysis-aggregator", 0, 0, v1alpha1.JobStateUnknown, nil),
					},
					InformingJobResults: []v1alpha1.JobStatus{
						newJobStatus("aggregated-azure-ovn-upgrade-4.11-micro", "periodic-ci-openshift-release-master-ci-4.11-e2e-azure-ovn-upgrade", 0, 10, v1alpha1.JobStateUnknown, nil),
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						newJobStatus("aggregated-azure-ovn-upgrade-4.11-micro", "aggregated-azure-ovn-upgrade-4.11-micro-release-openshift-release-analysis-aggregator", 0, 0, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-aggregator", "ci", "build01", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
						}),
					},
					InformingJobResults: []v1alpha1.JobStatus{
						newJobStatus("aggregated-azure-ovn-upgrade-4.11-micro", "periodic-ci-openshift-release-master-ci-4.11-e2e-azure-ovn-upgrade", 0, 10, v1alpha1.JobStateUnknown, []v1alpha1.JobRunResult{
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-0", "ci", "build01", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-1", "ci", "build02", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-2", "ci", "build01", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-3", "ci", "build03", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-4", "ci", "build02", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-5", "ci", "build05", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-6", "ci", "build01", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-7", "ci", "build04", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-8", "ci", "build02", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
							newJobRunResult("4.11.0-0.nightly-2022-02-09-091559-aggregate-b99pz22-analysis-9", "ci", "build01", v1alpha1.JobRunStateSuccess, "https://abc.123.com"),
						}),
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			prowJobClient := prowfake.NewSimpleClientset(testCase.prowjob...)

			prowJobInformerFactory := prowjobinformers.NewSharedInformerFactory(prowJobClient, controllerDefaultResyncDuration)
			prowJobInformer := prowJobInformerFactory.Prow().V1().ProwJobs()

			releasePayloadClient := fake.NewSimpleClientset(testCase.input)
			releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactory(releasePayloadClient, controllerDefaultResyncDuration)
			releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()

			c := &ProwJobStatusController{
				ReleasePayloadController: NewReleasePayloadController("ProwJob Status Controller",
					releasePayloadInformer,
					releasePayloadClient.ReleaseV1alpha1(),
					events.NewInMemoryRecorder("prowjob-status-controller-test"),
					workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ProwJobStatusController")),
				prowJobLister: prowJobInformer.Lister(),
			}
			c.cachesToSync = append(c.cachesToSync, prowJobInformer.Informer().HasSynced)

			prowJobFilter := func(obj interface{}) bool {
				if prowJob, ok := obj.(*v1.ProwJob); ok {
					if _, ok := prowJob.Labels[releasecontroller.ReleaseLabelVerify]; ok {
						if _, ok := prowJob.Labels[releasecontroller.ReleaseLabelPayload]; ok {
							return true
						}
					}
				}
				return false
			}

			prowJobInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
				FilterFunc: prowJobFilter,
				Handler: cache.ResourceEventHandlerFuncs{
					AddFunc:    c.lookupReleasePayload,
					UpdateFunc: func(old, new interface{}) { c.lookupReleasePayload(new) },
					DeleteFunc: c.lookupReleasePayload,
				},
			})

			releasePayloadInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
				AddFunc:    c.Enqueue,
				UpdateFunc: func(old, new interface{}) { c.Enqueue(new) },
				DeleteFunc: c.Enqueue,
			})

			releasePayloadInformerFactory.Start(context.Background().Done())
			prowJobInformerFactory.Start(context.Background().Done())

			if !cache.WaitForNamedCacheSync("ProwJobStatusController", context.Background().Done(), c.cachesToSync...) {
				t.Errorf("%s: error waiting for caches to sync", testCase.name)
				return
			}

			err := c.sync(context.TODO(), fmt.Sprintf("%s/%s", testCase.input.Namespace, testCase.input.Name))
			if err != nil && err != testCase.expectedErr {
				t.Errorf("%s - expected error: %v, got: %v", testCase.name, testCase.expectedErr, err)
			}

			// Performing a live lookup instead of having to wait for the cache to sink (again)...
			output, err := c.releasePayloadClient.ReleasePayloads(testCase.input.Namespace).Get(context.TODO(), testCase.input.Name, metav1.GetOptions{})
			if !cmp.Equal(output, testCase.expected, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, output)
			}
		})
	}
}
