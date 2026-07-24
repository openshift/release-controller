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
	"k8s.io/utils/clock"
)

func TestPayloadMirrorSync(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		payload  *v1alpha1.ReleasePayload
		expected *v1alpha1.ReleasePayload
	}{
		{
			name: "ReleasePayloadWithoutReleaseMirrorJobStatusOrConditions",
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
							Type:   v1alpha1.ConditionPayloadMirrorFailed,
							Status: metav1.ConditionUnknown,
							Reason: ReleasePayloadMirrorFailedReason,
						},
						{
							Type:   v1alpha1.ConditionPayloadMirrored,
							Status: metav1.ConditionUnknown,
							Reason: ReleasePayloadMirroredReason,
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
							Type:    v1alpha1.ConditionPayloadMirrored,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadMirroredReason,
							Message: ReleaseMirrorJobSuccessMessage,
						},
						{
							Type:    v1alpha1.ConditionPayloadMirrorFailed,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadMirrorFailedReason,
							Message: ReleaseMirrorJobSuccessMessage,
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
							Type:    v1alpha1.ConditionPayloadMirrored,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadMirroredReason,
							Message: ReleaseMirrorJobSuccessMessage,
						},
						{
							Type:    v1alpha1.ConditionPayloadMirrorFailed,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadMirrorFailedReason,
							Message: ReleaseMirrorJobSuccessMessage,
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
							Type:    v1alpha1.ConditionPayloadMirrored,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadMirroredReason,
							Message: ReleaseMirrorJobFailureMessage,
						},
						{
							Type:    v1alpha1.ConditionPayloadMirrorFailed,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadMirrorFailedReason,
							Message: ReleaseMirrorJobFailureMessage,
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
							Type:    v1alpha1.ConditionPayloadMirrored,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadMirroredReason,
							Message: ReleaseMirrorJobFailureMessage,
						},
						{
							Type:    v1alpha1.ConditionPayloadMirrorFailed,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadMirrorFailedReason,
							Message: ReleaseMirrorJobFailureMessage,
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
							Type:    v1alpha1.ConditionPayloadMirrored,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadMirroredReason,
							Message: ReleaseMirrorJobSuccessMessage,
						},
						{
							Type:    v1alpha1.ConditionPayloadMirrorFailed,
							Status:  metav1.ConditionUnknown,
							Reason:  ReleasePayloadMirrorFailedReason,
							Message: ReleaseMirrorJobFailureMessage,
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
							Type:   v1alpha1.ConditionPayloadMirrorFailed,
							Status: metav1.ConditionUnknown,
							Reason: ReleasePayloadMirrorFailedReason,
						},
						{
							Type:   v1alpha1.ConditionPayloadMirrored,
							Status: metav1.ConditionUnknown,
							Reason: ReleasePayloadMirroredReason,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithSuccessfulReleaseMirrorJob",
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseMirrorJobResult: v1alpha1.ReleaseMirrorJobResult{
						Status: v1alpha1.ReleaseMirrorJobSuccess,
					},
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha1.ConditionPayloadMirrored,
							Status: metav1.ConditionUnknown,
							Reason: ReleasePayloadMirroredReason,
						},
						{
							Type:   v1alpha1.ConditionPayloadMirrorFailed,
							Status: metav1.ConditionUnknown,
							Reason: ReleasePayloadMirrorFailedReason,
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
					ReleaseMirrorJobResult: v1alpha1.ReleaseMirrorJobResult{
						Status: v1alpha1.ReleaseMirrorJobSuccess,
					},
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadMirrorFailed,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadMirrorFailedReason,
							Message: ReleaseMirrorJobSuccessMessage,
						},
						{
							Type:    v1alpha1.ConditionPayloadMirrored,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadMirroredReason,
							Message: ReleaseMirrorJobSuccessMessage,
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

			c, err := NewPayloadMirrorController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), events.NewInMemoryRecorder("payload-mirror-controller-test", clock.RealClock{}))
			if err != nil {
				t.Fatalf("Failed to create Payload Mirror Controller: %v", err)
			}

			releasePayloadInformerFactory.Start(context.Background().Done())

			if !cache.WaitForNamedCacheSync("PayloadMirrorController", context.Background().Done(), c.cachesToSync...) {
				t.Errorf("%s: error waiting for caches to sync", testCase.name)
				return
			}

			if err := c.sync(context.TODO(), fmt.Sprintf("%s/%s", testCase.payload.Namespace, testCase.payload.Name)); err != nil {
				t.Errorf("%s: unexpected err: %v", testCase.name, err)
			}

			// Performing a live lookup instead of having to wait for the cache to sink (again)...
			output, err := c.releasePayloadClient.ReleasePayloads(testCase.payload.Namespace).Get(context.TODO(), testCase.payload.Name, metav1.GetOptions{})
			if err != nil {
				t.Errorf("%s: unexpected err: %v", testCase.name, err)
			}
			if !cmp.Equal(output, testCase.expected, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, output)
			}
		})
	}
}

func TestPayloadMirrorWithoutMirrorConfig(t *testing.T) {
	testCases := []struct {
		name                    string
		hasMirrorConfig         bool
		expectMirroredCondition metav1.ConditionStatus
		expectFailedCondition   metav1.ConditionStatus
		expectMessage           string
	}{
		{
			name:                    "Payload with mirror config follows normal job-based flow",
			hasMirrorConfig:         true,
			expectMirroredCondition: metav1.ConditionUnknown, // Waits for job completion
			expectFailedCondition:   metav1.ConditionUnknown,
		},
		{
			name:                    "Payload without mirror config marked as mirrored immediately",
			hasMirrorConfig:         false,
			expectMirroredCondition: metav1.ConditionTrue, // Immediate success
			expectFailedCondition:   metav1.ConditionFalse,
			expectMessage:           "Release payload using pre-existing image, no mirror job needed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a test payload with coordinates (like a real release payload)
			payload := &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-release",
					Namespace: "test-namespace",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "test-namespace",
						ImagestreamName:    "releases",
						ImagestreamTagName: "test-release",
					},
					PayloadType: v1alpha1.PayloadTypeLocal,
				},
			}

			if tc.hasMirrorConfig {
				payload.Spec.PayloadCreationConfig = v1alpha1.PayloadCreationConfig{
					ReleaseMirrorCoordinates: v1alpha1.ReleaseMirrorCoordinates{
						Namespace:            "test-jobs",
						ReleaseMirrorJobName: "test-release-mirror",
					},
				}
			}

			// Set up fake client and controller
			fakeClient := fake.NewSimpleClientset(payload)
			informerFactory := releasepayloadinformers.NewSharedInformerFactory(fakeClient, 0)
			payloadInformer := informerFactory.Release().V1alpha1().ReleasePayloads()

			controller, err := NewPayloadMirrorController(
				payloadInformer,
				fakeClient.ReleaseV1alpha1(),
				events.NewInMemoryRecorder("test", clock.RealClock{}),
			)
			if err != nil {
				t.Fatalf("Failed to create controller: %v", err)
			}

			// Start informer and wait for cache sync
			informerFactory.Start(make(chan struct{}))
			cache.WaitForCacheSync(make(chan struct{}), payloadInformer.Informer().HasSynced)

			// Sync the payload
			err = controller.sync(context.Background(), "test-namespace/test-release")
			if err != nil {
				t.Fatalf("Sync failed: %v", err)
			}

			// Get the updated payload
			updatedPayload, err := fakeClient.ReleaseV1alpha1().ReleasePayloads("test-namespace").Get(context.Background(), "test-release", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get updated payload: %v", err)
			}

			// Check the conditions
			var mirroredCond, failedCond *metav1.Condition
			for _, cond := range updatedPayload.Status.Conditions {
				if cond.Type == v1alpha1.ConditionPayloadMirrored {
					mirroredCond = &cond
				}
				if cond.Type == v1alpha1.ConditionPayloadMirrorFailed {
					failedCond = &cond
				}
			}

			if mirroredCond == nil {
				t.Fatalf("PayloadMirrored condition not found")
			}
			if failedCond == nil {
				t.Fatalf("PayloadMirrorFailed condition not found")
			}

			if mirroredCond.Status != tc.expectMirroredCondition {
				t.Errorf("Expected PayloadMirrored condition status %s, got %s", tc.expectMirroredCondition, mirroredCond.Status)
			}

			if failedCond.Status != tc.expectFailedCondition {
				t.Errorf("Expected PayloadMirrorFailed condition status %s, got %s", tc.expectFailedCondition, failedCond.Status)
			}

			if tc.expectMessage != "" && mirroredCond.Message != tc.expectMessage {
				t.Errorf("Expected message %q, got %q", tc.expectMessage, mirroredCond.Message)
			}
		})
	}
}
