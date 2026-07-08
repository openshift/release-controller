package main

import (
	"testing"

	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetRejectionDetails(t *testing.T) {
	tests := []struct {
		name        string
		payload     *v1alpha1.ReleasePayload
		wantReason  string
		wantMessage string
	}{
		{
			name: "PayloadRejected=True returns condition details",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadRejected,
							Status:  metav1.ConditionTrue,
							Reason:  "BlockingJobFailed",
							Message: "release verification step failed: e2e-aws",
						},
					},
				},
			},
			wantReason:  "BlockingJobFailed",
			wantMessage: "release verification step failed: e2e-aws",
		},
		{
			name: "PayloadRejected=False returns defaults",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha1.ConditionPayloadRejected,
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			wantReason:  "VerificationFailed",
			wantMessage: "release verification failed",
		},
		{
			name: "No conditions returns defaults",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{},
			},
			wantReason:  "VerificationFailed",
			wantMessage: "release verification failed",
		},
		{
			name: "Multiple conditions finds PayloadRejected",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha1.ConditionPayloadCreated,
							Status: metav1.ConditionTrue,
						},
						{
							Type:    v1alpha1.ConditionPayloadRejected,
							Status:  metav1.ConditionTrue,
							Reason:  "ManuallyRejected",
							Message: "rejected by admin",
						},
						{
							Type:   v1alpha1.ConditionPayloadAccepted,
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			wantReason:  "ManuallyRejected",
			wantMessage: "rejected by admin",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, message := getRejectionDetails(tt.payload)
			if reason != tt.wantReason {
				t.Errorf("expected reason %q, got %q", tt.wantReason, reason)
			}
			if message != tt.wantMessage {
				t.Errorf("expected message %q, got %q", tt.wantMessage, message)
			}
		})
	}
}
