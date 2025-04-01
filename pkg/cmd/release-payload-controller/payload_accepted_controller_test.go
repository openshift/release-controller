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
	"k8s.io/client-go/util/workqueue"
)

func TestPayloadAcceptedSync(t *testing.T) {
	testCases := []struct {
		name     string
		input    *v1alpha1.ReleasePayload
		expected *v1alpha1.ReleasePayload
	}{
		{
			name: "TestPayloadOverrideAccepted",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideAccepted,
						Reason:   "Manually accepted per TRT",
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideAccepted,
						Reason:   "Manually accepted per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadAccepted,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadManuallyAcceptedReason,
							Message: "Manually accepted per TRT",
						},
					},
				},
			},
		},
		{
			name: "TestPayloadOverrideAcceptedAfterRejected",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideAccepted,
						Reason:   "Manually accepted per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadRejected,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadManuallyRejectedReason,
							Message: "Manually rejected per TRT",
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideAccepted,
						Reason:   "Manually accepted per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadAccepted,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadManuallyAcceptedReason,
							Message: "Manually accepted per TRT",
						},
						{
							Type:    v1alpha1.ConditionPayloadRejected,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadManuallyRejectedReason,
							Message: "Manually rejected per TRT",
						},
					},
				},
			},
		},
		{
			name: "TestPayloadOverrideRejected",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "Manually rejected per TRT",
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "Manually rejected per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadAccepted,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadManuallyRejectedReason,
							Message: "Manually rejected per TRT",
						},
					},
				},
			},
		},
		{
			name: "TestPayloadOverrideRejectedAfterAccepted",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "Manually rejected per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadRejected,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadManuallyRejectedReason,
							Message: "Manually rejected per TRT",
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "Manually rejected per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadAccepted,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadManuallyRejectedReason,
							Message: "Manually rejected per TRT",
						},
						{
							Type:    v1alpha1.ConditionPayloadRejected,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadManuallyRejectedReason,
							Message: "Manually rejected per TRT",
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithSuccessfulBlockingJobs",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
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
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha1.ConditionPayloadAccepted,
							Status: metav1.ConditionTrue,
							Reason: ReleasePayloadAcceptedReason,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithFailedBlockingJob",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateFailure,
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
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateFailure,
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha1.ConditionPayloadAccepted,
							Status: metav1.ConditionFalse,
							Reason: ReleasePayloadAcceptedReason,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithPendingBlockingJob",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateFailure,
						},
						{
							AggregateState: v1alpha1.JobStatePending,
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
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateFailure,
						},
						{
							AggregateState: v1alpha1.JobStatePending,
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha1.ConditionPayloadAccepted,
							Status: metav1.ConditionFalse,
							Reason: ReleasePayloadAcceptedReason,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithUnknownBlockingJob",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateUnknown,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
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
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateUnknown,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha1.ConditionPayloadAccepted,
							Status: metav1.ConditionUnknown,
							Reason: ReleasePayloadAcceptedReason,
						},
					},
				},
			},
		},
		{
			name: "ReleaseCreationJobFailed",
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
						Status:  v1alpha1.ReleaseCreationJobFailed,
						Message: "BackoffLimitExceeded: Job has reached the specified backoff limit",
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadAccepted,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadCreationJobFailedReason,
							Message: "BackoffLimitExceeded: Job has reached the specified backoff limit",
						},
					},
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
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			releasePayloadClient := fake.NewSimpleClientset(testCase.input)

			releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactory(releasePayloadClient, controllerDefaultResyncDuration)
			releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()

			c := &PayloadAcceptedController{
				ReleasePayloadController: NewReleasePayloadController("Payload Accepted Controller",
					releasePayloadInformer,
					releasePayloadClient.ReleaseV1alpha1(),
					events.NewInMemoryRecorder("payload-accepted-controller-test"),
					workqueue.NewRateLimitingQueueWithConfig(workqueue.DefaultControllerRateLimiter(), workqueue.RateLimitingQueueConfig{Name: "PayloadAcceptedController"})),
			}

			if _, err := releasePayloadInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
				AddFunc: c.Enqueue,
				UpdateFunc: func(oldObj, newObj interface{}) {
					c.Enqueue(newObj)
				},
				DeleteFunc: c.Enqueue,
			}); err != nil {
				t.Errorf("Failed to add release payload event handler: %v", err)
			}

			releasePayloadInformerFactory.Start(context.Background().Done())

			if !cache.WaitForNamedCacheSync("PayloadAcceptedController", context.Background().Done(), c.cachesToSync...) {
				t.Errorf("%s: error waiting for caches to sync", testCase.name)
				return
			}

			err := c.sync(context.TODO(), fmt.Sprintf("%s/%s", testCase.input.Namespace, testCase.input.Name))
			if err != nil {
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
