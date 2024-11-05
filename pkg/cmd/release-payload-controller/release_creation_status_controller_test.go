package release_payload_controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/client/clientset/versioned/fake"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	fake2 "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

func TestComputeReleaseCreationJobStatus(t *testing.T) {
	testCases := []struct {
		name     string
		job      *batchv1.Job
		expected v1alpha1.ReleaseCreationJobStatus
	}{
		{
			name: "JobStatusCompletionTimeSet",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ci-release",
				},
				Status: batchv1.JobStatus{
					CompletionTime: &metav1.Time{
						Time: time.Now(),
					},
				},
			},
			expected: v1alpha1.ReleaseCreationJobSuccess,
		},
		{
			name: "JobStatusConditionsNotSet",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ci-release",
				},
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{},
				},
			},
			expected: v1alpha1.ReleaseCreationJobUnknown,
		},
		{
			name: "JobStatusConditionsSuspendedSet",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ci-release",
				},
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobSuspended,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: v1alpha1.ReleaseCreationJobUnknown,
		},
		{
			name: "JobStatusConditionsFailedSet",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ci-release",
				},
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobFailed,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: v1alpha1.ReleaseCreationJobFailed,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			releaseCreationJobStatus := computeReleaseCreationJobStatus(testCase.job)

			if !cmp.Equal(releaseCreationJobStatus, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, releaseCreationJobStatus)
			}
		})
	}
}

func TestReleaseCreationStatusSync(t *testing.T) {
	testCases := []struct {
		name        string
		job         runtime.Object
		input       *v1alpha1.ReleasePayload
		expected    *v1alpha1.ReleasePayload
		expectedErr error
	}{
		{
			name: "ReleasePayloadStatusNotSet",
			job:  &batchv1.Job{},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
			},
			expectedErr: ErrCoordinatesNotSet,
		},
		{
			name: "ReleasePayloadStatusSetWithNoJob",
			job:  &batchv1.Job{},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
						Status: v1alpha1.ReleaseCreationJobUnknown,
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
						Status:  v1alpha1.ReleaseCreationJobUnknown,
						Message: ReleaseCreationJobUnknownMessage,
					},
				},
			},
		},
		{
			name: "ReleasePayloadStatusSetWithCompleteJob",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ci-release",
				},
				Status: batchv1.JobStatus{
					CompletionTime: &metav1.Time{
						Time: time.Now(),
					},
				},
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
						Status: v1alpha1.ReleaseCreationJobUnknown,
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
						Status:  v1alpha1.ReleaseCreationJobSuccess,
						Message: ReleaseCreationJobSuccessMessage,
					},
				},
			},
		},
		{
			name: "ReleasePayloadStatusSetWithFailedJob",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ci-release",
				},
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobFailed,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
						Status: v1alpha1.ReleaseCreationJobUnknown,
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
						Status:  v1alpha1.ReleaseCreationJobFailed,
						Message: ReleaseCreationJobFailureMessage,
					},
				},
			},
		},
		{
			name: "ReleasePayloadStatusSetWithFailedJobReasonAndMessage",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ci-release",
				},
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:    batchv1.JobFailed,
							Status:  corev1.ConditionTrue,
							Reason:  "BackoffLimitExceeded",
							Message: "Job has reached the specified backoff limit",
						},
					},
				},
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
						Status: v1alpha1.ReleaseCreationJobUnknown,
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
						Status:  v1alpha1.ReleaseCreationJobFailed,
						Message: "BackoffLimitExceeded: Job has reached the specified backoff limit",
					},
				},
			},
		},
		{
			name: "ReleasePayloadStatusSetWithSuspendedJob",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ci-release",
				},
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type: batchv1.JobSuspended,
						},
					},
				},
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
						Status: v1alpha1.ReleaseCreationJobUnknown,
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
						Status:  v1alpha1.ReleaseCreationJobUnknown,
						Message: ReleaseCreationJobUnknownMessage,
					},
				},
			},
		},
		{
			name: "ReleasePayloadStatusWithDeletedStatus",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ci-release",
				},
				Status: batchv1.JobStatus{
					CompletionTime: &metav1.Time{
						Time: time.Now(),
					},
				},
			},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
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
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
						Status:  v1alpha1.ReleaseCreationJobSuccess,
						Message: ReleaseCreationJobSuccessMessage,
					},
				},
			},
		},
		{
			name: "ReleasePayloadStatusWithDeletedStatusAndNoBatchJob",
			job:  &batchv1.CronJob{},
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
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
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
						Status:  v1alpha1.ReleaseCreationJobUnknown,
						Message: ReleaseCreationJobUnknownMessage,
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			kubeClient := fake2.NewSimpleClientset(testCase.job)
			kubeFactory := informers.NewSharedInformerFactory(kubeClient, controllerDefaultResyncDuration)
			batchJobInformer := kubeFactory.Batch().V1().Jobs()

			releasePayloadClient := fake.NewSimpleClientset(testCase.input)
			releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactory(releasePayloadClient, controllerDefaultResyncDuration)
			releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()

			c := &ReleaseCreationStatusController{
				ReleasePayloadController: NewReleasePayloadController("Release Creation Status Controller",
					releasePayloadInformer,
					releasePayloadClient.ReleaseV1alpha1(),
					events.NewInMemoryRecorder("release-creation-status-controller-test"),
					workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ReleaseCreationStatusController")),
				batchJobLister: batchJobInformer.Lister(),
			}
			c.cachesToSync = append(c.cachesToSync, batchJobInformer.Informer().HasSynced)

			batchJobFilter := func(obj interface{}) bool {
				if batchJob, ok := obj.(*batchv1.Job); ok {
					if _, ok := batchJob.Annotations[releasecontroller.ReleaseAnnotationReleaseTag]; ok {
						return true
					}
				}
				return false
			}

			if _, err := batchJobInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
				FilterFunc: batchJobFilter,
				Handler: cache.ResourceEventHandlerFuncs{
					AddFunc:    c.lookupReleasePayload,
					UpdateFunc: func(old, new interface{}) { c.lookupReleasePayload(new) },
					DeleteFunc: c.lookupReleasePayload,
				},
			}); err != nil {
				t.Errorf("Failed to add batch job event handler: %v", err)
			}

			// In case someone/something deletes the ReleaseCreationJobResult.Status, try and rectify it...
			releasePayloadFilter := func(obj interface{}) bool {
				if releasePayload, ok := obj.(*v1alpha1.ReleasePayload); ok {
					switch {
					// Check that we have the necessary information to proceed
					case len(releasePayload.Status.ReleaseCreationJobResult.Coordinates.Namespace) == 0 || len(releasePayload.Status.ReleaseCreationJobResult.Coordinates.Name) == 0:
						return false
					// Check if we need to process this ReleasePayload at all
					case len(releasePayload.Status.ReleaseCreationJobResult.Status) == 0 || len(releasePayload.Status.ReleaseCreationJobResult.Message) == 0:
						return true
					}
				}
				return false
			}

			if _, err := releasePayloadInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
				FilterFunc: releasePayloadFilter,
				Handler: cache.ResourceEventHandlerFuncs{
					AddFunc:    c.Enqueue,
					UpdateFunc: func(old, new interface{}) { c.Enqueue(new) },
					DeleteFunc: c.Enqueue,
				},
			}); err != nil {
				t.Errorf("Failed to add release payload event handler: %v", err)
			}

			releasePayloadInformerFactory.Start(context.Background().Done())
			kubeFactory.Start(context.Background().Done())

			if !cache.WaitForNamedCacheSync("ReleaseCreationStatusController", context.Background().Done(), c.cachesToSync...) {
				t.Errorf("%s: error waiting for caches to sync", testCase.name)
				return
			}

			err := c.sync(context.TODO(), fmt.Sprintf("%s/%s", testCase.input.Namespace, testCase.input.Name))
			if err != nil && err != testCase.expectedErr {
				t.Errorf("%s - expected error: %v, got: %v", testCase.name, testCase.expectedErr, err)
			}

			// Performing a live lookup instead of having to wait for the cache to sink (again)...
			output, _ := c.releasePayloadClient.ReleasePayloads(testCase.input.Namespace).Get(context.TODO(), testCase.input.Name, metav1.GetOptions{})
			if !cmp.Equal(output, testCase.expected, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, output)
			}
		})
	}
}

func TestComputeReleaseCreationJobMessage(t *testing.T) {
	var value int32 = 1
	testCases := []struct {
		name     string
		job      *batchv1.Job
		expected string
	}{
		{
			name: "JobStatusCompletionTimeSet",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ci-release",
				},
				Status: batchv1.JobStatus{
					CompletionTime: &metav1.Time{
						Time: time.Now(),
					},
				},
			},
			expected: ReleaseCreationJobSuccessMessage,
		},
		{
			name: "JobStatusConditionsNotSet",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ci-release",
				},
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{},
				},
			},
			expected: ReleaseCreationJobUnknownMessage,
		},
		{
			name: "JobStatusConditionsFailedSet",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ci-release",
				},
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:    batchv1.JobFailed,
							Status:  corev1.ConditionTrue,
							Reason:  "BackoffLimitExceeded",
							Message: "Job has reached the specified backoff limit",
						},
					},
				},
			},
			expected: "BackoffLimitExceeded: Job has reached the specified backoff limit",
		},
		{
			name: "JobStatusReady",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ci-release",
				},
				Status: batchv1.JobStatus{
					Ready: &value,
				},
			},
			expected: ReleaseCreationJobPendingMessage,
		},
		{
			name: "JobStatusActive",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ci-release",
				},
				Status: batchv1.JobStatus{
					Active: 1,
				},
			},
			expected: ReleaseCreationJobPendingMessage,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			releaseCreationJobMessage := computeReleaseCreationJobMessage(testCase.job)

			if !cmp.Equal(releaseCreationJobMessage, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, releaseCreationJobMessage)
			}
		})
	}
}
