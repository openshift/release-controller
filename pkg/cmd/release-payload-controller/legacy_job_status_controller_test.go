package release_payload_controller

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	imagev1 "github.com/openshift/api/image/v1"
	imagefake "github.com/openshift/client-go/image/clientset/versioned/fake"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/client/clientset/versioned/fake"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

func newLegacyJobStatus(name string, results []v1alpha1.JobRunResult) v1alpha1.JobStatus {
	return v1alpha1.JobStatus{
		CIConfigurationName: name,
		JobRunResults:       results,
	}
}

func newLegacyJobStatusPointer(name string, results []v1alpha1.JobRunResult) *v1alpha1.JobStatus {
	status := newLegacyJobStatus(name, results)
	return &status
}

func newLegacyJobRunResult(state v1alpha1.JobRunState, url string) v1alpha1.JobRunResult {
	return v1alpha1.JobRunResult{
		State:               state,
		HumanProwResultsURL: url,
	}
}

func newImageStream(name, namespace string, tags []imagev1.TagReference) *imagev1.ImageStream {
	return &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
		Spec: imagev1.ImageStreamSpec{
			Tags: tags,
		},
		Status: imagev1.ImageStreamStatus{},
	}
}

func TestSetLegacyJobStatus(t *testing.T) {
	t.Parallel()
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
			jobStatus: newLegacyJobStatus("A", []v1alpha1.JobRunResult{}),
			expected: &v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{},
			},
		},
		{
			name: "MatchingBlockingJobUpdated",
			input: &v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{
					newLegacyJobStatus("A", []v1alpha1.JobRunResult{}),
				},
			},
			jobStatus: newLegacyJobStatus("A", []v1alpha1.JobRunResult{
				newLegacyJobRunResult("X", "https://abc.123.com"),
			}),
			expected: &v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{
					newLegacyJobStatus("A", []v1alpha1.JobRunResult{
						newLegacyJobRunResult("X", "https://abc.123.com"),
					}),
				},
			},
		},
		{
			name: "MatchingInformingJobUpdated",
			input: &v1alpha1.ReleasePayloadStatus{
				InformingJobResults: []v1alpha1.JobStatus{
					newLegacyJobStatus("A", []v1alpha1.JobRunResult{}),
				},
			},
			jobStatus: newLegacyJobStatus("A", []v1alpha1.JobRunResult{
				newLegacyJobRunResult("X", "https://abc.123.com"),
			}),
			expected: &v1alpha1.ReleasePayloadStatus{
				InformingJobResults: []v1alpha1.JobStatus{
					newLegacyJobStatus("A", []v1alpha1.JobRunResult{
						newLegacyJobRunResult("X", "https://abc.123.com"),
					}),
				},
			},
		},
		{
			name: "UpgradeJobsNotUpdated",
			input: &v1alpha1.ReleasePayloadStatus{
				UpgradeJobResults: []v1alpha1.JobStatus{
					newLegacyJobStatus("A", []v1alpha1.JobRunResult{}),
				},
			},
			jobStatus: newLegacyJobStatus("A", []v1alpha1.JobRunResult{
				newLegacyJobRunResult("X", "https://abc.123.com"),
			}),
			expected: &v1alpha1.ReleasePayloadStatus{
				UpgradeJobResults: []v1alpha1.JobStatus{
					newLegacyJobStatus("A", []v1alpha1.JobRunResult{}),
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setLegacyJobStatus(testCase.input, testCase.jobStatus)
			if !reflect.DeepEqual(testCase.input, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, testCase.input)
			}
		})
	}
}

func TestFindLegacyJobStatus(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name                string
		payloadName         string
		input               *v1alpha1.ReleasePayloadStatus
		ciConfigurationName string
		expected            *v1alpha1.JobStatus
		expectedError       string
	}{
		{
			name:                "NoJobs",
			payloadName:         "4.11.0-0.nightly-2022-06-23-153912",
			input:               &v1alpha1.ReleasePayloadStatus{},
			ciConfigurationName: "A",
			expected:            nil,
			expectedError:       "unable to locate legacy job results for A",
		},
		{
			name:        "BlockingJob",
			payloadName: "4.11.0-0.nightly-2022-06-23-153912",
			input: &v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{
					newLegacyJobStatus("A", []v1alpha1.JobRunResult{
						newLegacyJobRunResult("X", "https://abc.123.com"),
					}),
				},
			},
			ciConfigurationName: "A",
			expected: newLegacyJobStatusPointer("A", []v1alpha1.JobRunResult{
				newLegacyJobRunResult("X", "https://abc.123.com"),
			}),
		},
		{
			name:        "InformingJob",
			payloadName: "4.11.0-0.nightly-2022-06-23-153912",
			input: &v1alpha1.ReleasePayloadStatus{
				InformingJobResults: []v1alpha1.JobStatus{
					newLegacyJobStatus("A", []v1alpha1.JobRunResult{
						newLegacyJobRunResult("X", "https://abc.123.com"),
					}),
				},
			},
			ciConfigurationName: "A",
			expected: newLegacyJobStatusPointer("A", []v1alpha1.JobRunResult{
				newLegacyJobRunResult("X", "https://abc.123.com"),
			}),
		},
		{
			name:        "NoMatchingJob",
			payloadName: "4.11.0-0.nightly-2022-06-23-153912",
			input: &v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName: "A",
					},
				},
				InformingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName: "C",
					},
				},
			},
			ciConfigurationName: "X",
			expected:            nil,
			expectedError:       "unable to locate legacy job results for X",
		},
		{
			name:        "AutomaticReleaseUpgradeTest",
			payloadName: "4.11.22",
			input: &v1alpha1.ReleasePayloadStatus{
				UpgradeJobResults: []v1alpha1.JobStatus{
					newLegacyJobStatus("aws", []v1alpha1.JobRunResult{
						newLegacyJobRunResult("4.11.22-upgrade-from-4.10.18-aws", "https://abc.123.com"),
					}),
				},
			},
			ciConfigurationName: "aws",
			expected:            nil,
			expectedError:       "unable to locate legacy job results for aws",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			jobStatus, err := findLegacyJobStatus(testCase.payloadName, testCase.input, testCase.ciConfigurationName)
			if err != nil && !cmp.Equal(err.Error(), testCase.expectedError) {
				t.Fatalf("%s: Expected error %v, got %v", testCase.name, testCase.expectedError, err)
			}
			if !reflect.DeepEqual(jobStatus, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, jobStatus)
			}
		})
	}
}

func TestLegacyJobStatusSync(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		imagestream []runtime.Object
		input       *v1alpha1.ReleasePayload
		expected    *v1alpha1.ReleasePayload
		expectedErr error
	}{
		{
			name:        "InvalidCacheKey",
			imagestream: []runtime.Object{},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid/4.12.9",
					Namespace: "ocp",
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid/4.12.9",
					Namespace: "ocp",
				},
			},
		},
		{
			name:        "MissingPayloadVerificationDataSource",
			imagestream: []runtime.Object{},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
		},
		{
			name:        "BuildFarmPayloadVerificationDataSource",
			imagestream: []runtime.Object{},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
		},
		{
			name:        "MissingImageStream",
			imagestream: []runtime.Object{},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			expectedErr: fmt.Errorf("imagestream.image.openshift.io \"release\" not found"),
		},
		{
			name: "MissingImageStreamTag",
			imagestream: []runtime.Object{
				newImageStream("release", "ocp", []imagev1.TagReference{}),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
		},
		{
			name: "MissingVerificationResultsAnnotation",
			imagestream: []runtime.Object{
				newImageStream("release", "ocp", []imagev1.TagReference{
					{
						Name: "4.12.9",
					},
				}),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			expectedErr: fmt.Errorf("unable to determine job status results for imagestreamtag: ocp/release:4.12.9"),
		},
		{
			name: "InvalidVerificationResultsAnnotation",
			imagestream: []runtime.Object{
				newImageStream("release", "ocp", []imagev1.TagReference{
					{
						Name: "4.12.9",
						Annotations: map[string]string{
							releasecontroller.ReleaseAnnotationVerify: `{"blah": blah,}`,
						},
					},
				}),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			expectedErr: fmt.Errorf("unable to unmarshal VerificationStatusMap for imagestreamtag: ocp/release:4.12.9"),
		},
		{
			name: "NullVerificationResultsAnnotation",
			imagestream: []runtime.Object{
				newImageStream("release", "ocp", []imagev1.TagReference{
					{
						Name: "4.12.9",
						Annotations: map[string]string{
							releasecontroller.ReleaseAnnotationVerify: "null",
						},
					},
				}),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
		},
		{
			name: "EmptyVerificationResultsAnnotation",
			imagestream: []runtime.Object{
				newImageStream("release", "ocp", []imagev1.TagReference{
					{
						Name: "4.12.9",
						Annotations: map[string]string{
							releasecontroller.ReleaseAnnotationVerify: "",
						},
					},
				}),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			expectedErr: fmt.Errorf("unable to determine job status results for imagestreamtag: ocp/release:4.12.9"),
		},
		{
			name: "NoMatchingVerificationResults",
			imagestream: []runtime.Object{
				newImageStream("release", "ocp", []imagev1.TagReference{
					{
						Name: "4.12.9",
						Annotations: map[string]string{
							releasecontroller.ReleaseAnnotationVerify: `{"aws-sdn-serial":{"state":"Succeeded","url":"https://abc123.com"}}`,
						},
					},
				}),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{},
			},
		},
		{
			name: "MatchingBlockingVerificationResults",
			imagestream: []runtime.Object{
				newImageStream("release", "ocp", []imagev1.TagReference{
					{
						Name: "4.12.9",
						Annotations: map[string]string{
							releasecontroller.ReleaseAnnotationVerify: `{"aws-sdn-serial":{"state":"Succeeded","url":"https://abc123.com"}}`,
						},
					},
				}),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName: "aws-sdn-serial",
							JobRunResults:       nil,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName: "aws-sdn-serial",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									State:               v1alpha1.JobRunStateSuccess,
									HumanProwResultsURL: "https://abc123.com",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "MatchingInformingVerificationResults",
			imagestream: []runtime.Object{
				newImageStream("release", "ocp", []imagev1.TagReference{
					{
						Name: "4.12.9",
						Annotations: map[string]string{
							releasecontroller.ReleaseAnnotationVerify: `{"aws-sdn-serial":{"state":"Succeeded","url":"https://abc123.com"}}`,
						},
					},
				}),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName: "aws-sdn-serial",
							JobRunResults:       nil,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName: "aws-sdn-serial",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									State:               v1alpha1.JobRunStateSuccess,
									HumanProwResultsURL: "https://abc123.com",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "UpdateIdenticalVerificationResults",
			imagestream: []runtime.Object{
				newImageStream("release", "ocp", []imagev1.TagReference{
					{
						Name: "4.12.9",
						Annotations: map[string]string{
							releasecontroller.ReleaseAnnotationVerify: `{"aws-sdn-serial":{"state":"Succeeded","url":"https://abc123.com"}}`,
						},
					},
				}),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName: "aws-sdn-serial",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									State:               v1alpha1.JobRunStateSuccess,
									HumanProwResultsURL: "https://abc123.com",
								},
							},
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName: "aws-sdn-serial",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									State:               v1alpha1.JobRunStateSuccess,
									HumanProwResultsURL: "https://abc123.com",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "UpdateExistingVerificationResults",
			imagestream: []runtime.Object{
				newImageStream("release", "ocp", []imagev1.TagReference{
					{
						Name: "4.12.9",
						Annotations: map[string]string{
							releasecontroller.ReleaseAnnotationVerify: `{"aws-sdn-serial":{"state":"Succeeded","url":"https://abc123.com/good"}}`,
						},
					},
				}),
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName: "aws-sdn-serial",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									State:               v1alpha1.JobRunStateFailure,
									HumanProwResultsURL: "https://abc123.com/bad",
								},
							},
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.12.9",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.12.9",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceImageStream,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName: "aws-sdn-serial",
							JobRunResults: []v1alpha1.JobRunResult{
								{
									State:               v1alpha1.JobRunStateFailure,
									HumanProwResultsURL: "https://abc123.com/bad",
								},
								{
									State:               v1alpha1.JobRunStateSuccess,
									HumanProwResultsURL: "https://abc123.com/good",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			imageStreamClient := imagefake.NewSimpleClientset(testCase.imagestream...)

			imageStreamInformerFactory := imageinformers.NewSharedInformerFactory(imageStreamClient, controllerDefaultResyncDuration)
			imageStreamInformer := imageStreamInformerFactory.Image().V1().ImageStreams()

			releasePayloadClient := fake.NewSimpleClientset(testCase.input)
			releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactory(releasePayloadClient, controllerDefaultResyncDuration)
			releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()

			c, err := NewLegacyJobStatusController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), imageStreamInformer, events.NewInMemoryRecorder("legacy-job-status-controller"))
			if err != nil {
				t.Fatalf("Failed to create Legacy Job Status Controller: %v", err)
			}

			c.cachesToSync = append(c.cachesToSync, imageStreamInformer.Informer().HasSynced, releasePayloadInformer.Informer().HasSynced)

			releasePayloadInformerFactory.Start(context.Background().Done())
			imageStreamInformerFactory.Start(context.Background().Done())

			if !cache.WaitForNamedCacheSync("LegacyJobStatusController", context.Background().Done(), c.cachesToSync...) {
				t.Fatalf("%s: error waiting for caches to sync", testCase.name)
				return
			}

			if err := c.sync(context.TODO(), fmt.Sprintf("%s/%s", testCase.input.Namespace, testCase.input.Name)); err != nil {
				if testCase.expectedErr == nil {
					t.Fatalf("%s - encountered unexpected error: %v", testCase.name, err)
				}
				if !cmp.Equal(err.Error(), testCase.expectedErr.Error()) {
					t.Errorf("%s - expected error: %v, got: %v", testCase.name, testCase.expectedErr, err)
				}
			}

			// Performing a live lookup instead of having to wait for the cache to sink (again)...
			output, _ := c.releasePayloadClient.ReleasePayloads(testCase.input.Namespace).Get(context.TODO(), testCase.input.Name, metav1.GetOptions{})
			if !cmp.Equal(output, testCase.expected, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, output)
			}
		})
	}
}
