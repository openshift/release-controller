package releasecontroller

import (
	"testing"

	imagev1 "github.com/openshift/api/image/v1"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetReleasePhase(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		expected   string
	}{
		{
			name:       "no conditions returns Pending",
			conditions: nil,
			expected:   ReleasePhasePending,
		},
		{
			name: "PayloadAccepted true returns Accepted",
			conditions: []metav1.Condition{
				{Type: v1alpha1.ConditionPayloadCreated, Status: metav1.ConditionTrue},
				{Type: v1alpha1.ConditionPayloadAccepted, Status: metav1.ConditionTrue},
			},
			expected: ReleasePhaseAccepted,
		},
		{
			name: "PayloadRejected true returns Rejected",
			conditions: []metav1.Condition{
				{Type: v1alpha1.ConditionPayloadCreated, Status: metav1.ConditionTrue},
				{Type: v1alpha1.ConditionPayloadRejected, Status: metav1.ConditionTrue},
			},
			expected: ReleasePhaseRejected,
		},
		{
			name: "PayloadFailed true returns Failed",
			conditions: []metav1.Condition{
				{Type: v1alpha1.ConditionPayloadFailed, Status: metav1.ConditionTrue},
			},
			expected: ReleasePhaseFailed,
		},
		{
			name: "PayloadCreated true with no terminal returns Ready",
			conditions: []metav1.Condition{
				{Type: v1alpha1.ConditionPayloadCreated, Status: metav1.ConditionTrue},
				{Type: v1alpha1.ConditionPayloadAccepted, Status: metav1.ConditionUnknown},
				{Type: v1alpha1.ConditionPayloadRejected, Status: metav1.ConditionUnknown},
			},
			expected: ReleasePhaseReady,
		},
		{
			name: "PayloadCreated false returns Pending",
			conditions: []metav1.Condition{
				{Type: v1alpha1.ConditionPayloadCreated, Status: metav1.ConditionFalse},
			},
			expected: ReleasePhasePending,
		},
		{
			name: "terminal takes priority over created",
			conditions: []metav1.Condition{
				{Type: v1alpha1.ConditionPayloadCreated, Status: metav1.ConditionTrue},
				{Type: v1alpha1.ConditionPayloadAccepted, Status: metav1.ConditionTrue},
				{Type: v1alpha1.ConditionPayloadRejected, Status: metav1.ConditionFalse},
			},
			expected: ReleasePhaseAccepted,
		},
		{
			name: "all conditions unknown returns Pending",
			conditions: []metav1.Condition{
				{Type: v1alpha1.ConditionPayloadCreated, Status: metav1.ConditionUnknown},
				{Type: v1alpha1.ConditionPayloadAccepted, Status: metav1.ConditionUnknown},
				{Type: v1alpha1.ConditionPayloadRejected, Status: metav1.ConditionUnknown},
			},
			expected: ReleasePhasePending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: tt.conditions,
				},
			}
			got := GetReleasePhase(payload)
			if got != tt.expected {
				t.Errorf("GetReleasePhase() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestGetTagPhase(t *testing.T) {
	tests := []struct {
		name          string
		payloadPhases map[string]string
		tagName       string
		tagAnnotation string
		want          string
	}{
		{
			name:          "PayloadPhases hit returns map value",
			payloadPhases: map[string]string{"4.17.0": ReleasePhaseAccepted},
			tagName:       "4.17.0",
			tagAnnotation: ReleasePhaseRejected,
			want:          ReleasePhaseAccepted,
		},
		{
			name:          "PayloadPhases miss falls back to annotation",
			payloadPhases: map[string]string{"4.17.0": ReleasePhaseAccepted},
			tagName:       "4.18.0",
			tagAnnotation: ReleasePhaseReady,
			want:          ReleasePhaseReady,
		},
		{
			name:          "nil PayloadPhases falls back to annotation",
			payloadPhases: nil,
			tagName:       "4.17.0",
			tagAnnotation: ReleasePhasePending,
			want:          ReleasePhasePending,
		},
		{
			name:          "empty PayloadPhases falls back to annotation",
			payloadPhases: map[string]string{},
			tagName:       "4.17.0",
			tagAnnotation: ReleasePhaseFailed,
			want:          ReleasePhaseFailed,
		},
		{
			name:          "no annotation and no PayloadPhases returns empty string",
			payloadPhases: nil,
			tagName:       "4.17.0",
			tagAnnotation: "",
			want:          "",
		},
		{
			name:          "PayloadPhases overrides empty annotation",
			payloadPhases: map[string]string{"4.17.0": ReleasePhaseReady},
			tagName:       "4.17.0",
			tagAnnotation: "",
			want:          ReleasePhaseReady,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := &Release{
				Target: &imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Namespace: "test-ns", Name: "release"},
				},
				PayloadPhases: tt.payloadPhases,
			}
			tag := &imagev1.TagReference{
				Name: tt.tagName,
				Annotations: map[string]string{
					ReleaseAnnotationPhase: tt.tagAnnotation,
				},
			}
			got := GetTagPhase(release, tag)
			if got != tt.want {
				t.Errorf("GetTagPhase() = %q, want %q", got, tt.want)
			}
		})
	}
}
