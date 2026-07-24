package release_payload_controller

import (
	"context"
	"testing"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/client/clientset/versioned/fake"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/clock"
)

func TestPayloadCreationWithoutCreationConfig(t *testing.T) {
	testCases := []struct {
		name                   string
		hasCreationConfig     bool
		expectCreatedCondition metav1.ConditionStatus
		expectFailedCondition  metav1.ConditionStatus
		expectMessage         string
	}{
		{
			name:                   "Payload with creation config follows normal job-based flow",
			hasCreationConfig:     true,
			expectCreatedCondition: metav1.ConditionUnknown, // Waits for job completion
			expectFailedCondition:  metav1.ConditionUnknown,
		},
		{
			name:                   "Payload without creation config marked as created immediately",
			hasCreationConfig:     false,
			expectCreatedCondition: metav1.ConditionTrue, // Immediate success
			expectFailedCondition:  metav1.ConditionFalse,
			expectMessage:         "Release payload using pre-existing image, no creation job needed",
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

			if tc.hasCreationConfig {
				payload.Spec.PayloadCreationConfig = v1alpha1.PayloadCreationConfig{
					ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
						Namespace:              "test-jobs",
						ReleaseCreationJobName: "test-release",
					},
				}
			}

			// Set up fake client and controller
			fakeClient := fake.NewSimpleClientset(payload)
			informerFactory := releasepayloadinformers.NewSharedInformerFactory(fakeClient, 0)
			payloadInformer := informerFactory.Release().V1alpha1().ReleasePayloads()

			controller, err := NewPayloadCreationController(
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
			createdCond := v1helpers.FindCondition(updatedPayload.Status.Conditions, v1alpha1.ConditionPayloadCreated)
			failedCond := v1helpers.FindCondition(updatedPayload.Status.Conditions, v1alpha1.ConditionPayloadFailed)

			if createdCond == nil {
				t.Fatalf("PayloadCreated condition not found")
			}
			if failedCond == nil {
				t.Fatalf("PayloadFailed condition not found")
			}

			if createdCond.Status != tc.expectCreatedCondition {
				t.Errorf("Expected PayloadCreated condition status %s, got %s", tc.expectCreatedCondition, createdCond.Status)
			}

			if failedCond.Status != tc.expectFailedCondition {
				t.Errorf("Expected PayloadFailed condition status %s, got %s", tc.expectFailedCondition, failedCond.Status)
			}

			if tc.expectMessage != "" && createdCond.Message != tc.expectMessage {
				t.Errorf("Expected message %q, got %q", tc.expectMessage, createdCond.Message)
			}
		})
	}
}