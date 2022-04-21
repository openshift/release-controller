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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"testing"
)

func TestPayloadCreationSync(t *testing.T) {
	testCases := []struct {
		name     string
		payload  *v1alpha1.ReleasePayload
		expected *v1alpha1.ReleasePayload
	}{
		{
			name: "ReleasePayloadWithoutReleaseCreationJobStatusOrConditions",
			payload: &v1alpha1.ReleasePayload{
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
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha1.ConditionPayloadCreated,
							Status: metav1.ConditionUnknown,
							Reason: ReleasePayloadCreatedReason,
						},
						{
							Type:   v1alpha1.ConditionPayloadFailed,
							Status: metav1.ConditionUnknown,
							Reason: ReleasePayloadFailedReason,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithSuccessfulConditions",
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadCreated,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadCreatedReason,
							Message: ReleaseCreationJobSuccessMessage,
						},
						{
							Type:    v1alpha1.ConditionPayloadFailed,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadFailedReason,
							Message: ReleaseCreationJobSuccessMessage,
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
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadCreated,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadCreatedReason,
							Message: ReleaseCreationJobSuccessMessage,
						},
						{
							Type:    v1alpha1.ConditionPayloadFailed,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadFailedReason,
							Message: ReleaseCreationJobSuccessMessage,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithFailureConditions",
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadCreated,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadCreatedReason,
							Message: ReleaseCreationJobFailureMessage,
						},
						{
							Type:    v1alpha1.ConditionPayloadFailed,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadFailedReason,
							Message: ReleaseCreationJobFailureMessage,
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
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadCreated,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadCreatedReason,
							Message: ReleaseCreationJobFailureMessage,
						},
						{
							Type:    v1alpha1.ConditionPayloadFailed,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadFailedReason,
							Message: ReleaseCreationJobFailureMessage,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithMixedConditions",
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadCreated,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadCreatedReason,
							Message: ReleaseCreationJobSuccessMessage,
						},
						{
							Type:    v1alpha1.ConditionPayloadFailed,
							Status:  metav1.ConditionUnknown,
							Reason:  ReleasePayloadFailedReason,
							Message: ReleaseCreationJobFailureMessage,
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
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha1.ConditionPayloadCreated,
							Status: metav1.ConditionUnknown,
							Reason: ReleasePayloadCreatedReason,
						},
						{
							Type:   v1alpha1.ConditionPayloadFailed,
							Status: metav1.ConditionUnknown,
							Reason: ReleasePayloadFailedReason,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithSuccessfulReleaseCreationJob",
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Status: v1alpha1.ReleaseCreationJobSuccess,
					},
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha1.ConditionPayloadCreated,
							Status: metav1.ConditionUnknown,
							Reason: ReleasePayloadCreatedReason,
						},
						{
							Type:   v1alpha1.ConditionPayloadFailed,
							Status: metav1.ConditionUnknown,
							Reason: ReleasePayloadFailedReason,
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
						Status: v1alpha1.ReleaseCreationJobSuccess,
					},
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadCreated,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadCreatedReason,
							Message: ReleaseCreationJobSuccessMessage,
						},
						{
							Type:    v1alpha1.ConditionPayloadFailed,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadFailedReason,
							Message: ReleaseCreationJobSuccessMessage,
						},
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			releasePayloadClient := fake.NewSimpleClientset(testCase.payload)
			releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactory(releasePayloadClient, controllerDefaultResyncDuration)
			releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()

			c := &PayloadCreationController{
				releasePayloadNamespace: testCase.payload.Namespace,
				releasePayloadLister:    releasePayloadInformer.Lister(),
				releasePayloadClient:    releasePayloadClient.ReleaseV1alpha1(),
				eventRecorder:           events.NewInMemoryRecorder("release-creation-job-controller-test"),
				queue:                   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ReleaseCreationJobController"),
			}
			c.cachesToSync = append(c.cachesToSync, releasePayloadInformer.Informer().HasSynced)

			releasePayloadInformerFactory.Start(context.Background().Done())

			if !cache.WaitForNamedCacheSync("ReleaseCreationJobController", context.Background().Done(), c.cachesToSync...) {
				t.Errorf("%s: error waiting for caches to sync", testCase.name)
				return
			}

			err := c.sync(context.TODO(), fmt.Sprintf("%s/%s", testCase.payload.Namespace, testCase.payload.Name))
			if err != nil {
				t.Errorf("%s: unexpected err: %v", testCase.name, err)
			}

			// Performing a live lookup instead of having to wait for the cache to sink (again)...
			output, err := c.releasePayloadClient.ReleasePayloads(testCase.payload.Namespace).Get(context.TODO(), testCase.payload.Name, metav1.GetOptions{})
			if !cmp.Equal(output, testCase.expected, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, output)
			}
		})
	}
}
