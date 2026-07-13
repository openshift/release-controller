package main

import (
	"testing"
	"time"

	imagev1 "github.com/openshift/api/image/v1"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
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

func TestCalculateSyncActions_StalePayloadPhaseDoesNotBlockOtherTags(t *testing.T) {
	// Regression test: a tag whose ReleasePayload CRD has all-Unknown conditions
	// (returning Pending from GetReleasePhase) but whose annotation says Accepted
	// should not cause unrelated Ready tags to be misclassified as Pending.
	// With PayloadPhases populated, GetTagPhase prefers the CRD value, so the
	// stale tag appears as Pending. The Ready tag should still be counted as
	// unready (not pending) so it proceeds through syncReady.
	gen := int64(1)
	stableRelease := &releasecontroller.Release{
		Config: &releasecontroller.ReleaseConfig{
			Name: "4-stable",
			As:   releasecontroller.ReleaseConfigModeStable,
		},
		Source: &imagev1.ImageStream{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "release",
				Namespace: "ocp",
			},
		},
		Target: &imagev1.ImageStream{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "release",
				Namespace: "ocp",
			},
			Spec: imagev1.ImageStreamSpec{
				Tags: []imagev1.TagReference{
					{
						Name:       "4.7.0-fc.0",
						Generation: &gen,
						Annotations: map[string]string{
							releasecontroller.ReleaseAnnotationSource: "ocp/release",
							releasecontroller.ReleaseAnnotationName:   "4-stable",
							releasecontroller.ReleaseAnnotationPhase:  releasecontroller.ReleasePhaseAccepted,
						},
					},
					{
						Name:       "4.22.5",
						Generation: &gen,
						Annotations: map[string]string{
							releasecontroller.ReleaseAnnotationSource: "ocp/release",
							releasecontroller.ReleaseAnnotationName:   "4-stable",
							releasecontroller.ReleaseAnnotationPhase:  releasecontroller.ReleasePhaseReady,
						},
					},
				},
			},
		},
		PayloadPhases: map[string]string{
			"4.7.0-fc.0": releasecontroller.ReleasePhasePending,
			"4.22.5":      releasecontroller.ReleasePhaseReady,
		},
	}

	adoptTags, pendingTags, _, _, _, _ := calculateSyncActions(stableRelease, time.Now())

	// 4.7.0-fc.0 should be in pendingTags (CRD says Pending)
	foundStale := false
	for _, tag := range pendingTags {
		if tag.Name == "4.7.0-fc.0" {
			foundStale = true
		}
	}
	if !foundStale {
		t.Errorf("expected 4.7.0-fc.0 in pendingTags, got %v", releasecontroller.TagNames(pendingTags))
	}

	// 4.22.5 should NOT be in pendingTags or adoptTags — it should fall through
	// to the default case (unreadyTagCount++) and be handled by syncReady
	for _, tag := range pendingTags {
		if tag.Name == "4.22.5" {
			t.Errorf("4.22.5 should not be in pendingTags, but it was found")
		}
	}
	for _, tag := range adoptTags {
		if tag.Name == "4.22.5" {
			t.Errorf("4.22.5 should not be in adoptTags, but it was found")
		}
	}
}
