package main

import (
	imagev1 "github.com/openshift/api/image/v1"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"testing"
)

func TestSetPayloadOverride(t *testing.T) {
	testCases := []struct {
		name     string
		tag      *imagev1.TagReference
		payload  *v1alpha1.ReleasePayload
		expected *v1alpha1.ReleasePayload
	}{
		{
			name: "PhaseAnnotationNotSet",
			tag: &imagev1.TagReference{
				Annotations: map[string]string{},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
		},
		{
			name: "AcceptedTag",
			tag: &imagev1.TagReference{
				Annotations: map[string]string{
					releasecontroller.ReleaseAnnotationPhase: releasecontroller.ReleasePhaseAccepted,
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideAccepted,
						Reason:   "LegacyResult(reason=\"\",message=\"\")",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
		},
		{
			name: "RejectedTag",
			tag: &imagev1.TagReference{
				Annotations: map[string]string{
					releasecontroller.ReleaseAnnotationPhase: releasecontroller.ReleasePhaseRejected,
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "LegacyResult(reason=\"\",message=\"\")",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
		},
		{
			name: "PendingTag",
			tag: &imagev1.TagReference{
				Annotations: map[string]string{
					releasecontroller.ReleaseAnnotationPhase: releasecontroller.ReleasePhasePending,
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
		},
		{
			name: "FailedTag",
			tag: &imagev1.TagReference{
				Annotations: map[string]string{
					releasecontroller.ReleaseAnnotationPhase: releasecontroller.ReleasePhaseFailed,
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "LegacyResult(reason=\"\",message=\"\")",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
		},
		{
			name: "FailedTagWithReasonAndMessageAnnotationSet",
			tag: &imagev1.TagReference{
				Annotations: map[string]string{
					releasecontroller.ReleaseAnnotationPhase:   releasecontroller.ReleasePhaseFailed,
					releasecontroller.ReleaseAnnotationReason:  "CreateReleaseFailed",
					releasecontroller.ReleaseAnnotationMessage: "Could not create the release image",
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "LegacyResult(reason=\"CreateReleaseFailed\",message=\"Could not create the release image\")",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
		},
		{
			name: "ReadyTag",
			tag: &imagev1.TagReference{
				Annotations: map[string]string{
					releasecontroller.ReleaseAnnotationPhase: releasecontroller.ReleasePhaseReady,
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
		},
		{
			name: "RejectedTagWithReasonAnnotationSet",
			tag: &imagev1.TagReference{
				Annotations: map[string]string{
					releasecontroller.ReleaseAnnotationPhase:  releasecontroller.ReleasePhaseRejected,
					releasecontroller.ReleaseAnnotationReason: "VerificationFailed",
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "LegacyResult(reason=\"VerificationFailed\",message=\"\")",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
		},
		{
			name: "RejectedTagWithMessageAnnotationSet",
			tag: &imagev1.TagReference{
				Annotations: map[string]string{
					releasecontroller.ReleaseAnnotationPhase:   releasecontroller.ReleasePhaseRejected,
					releasecontroller.ReleaseAnnotationMessage: "release verification step failed: gcp-sdn-upgrade-4.10-micro, aws-ovn-upgrade-4.10-minor, azure-sdn-upgrade-4.10-minor, upgrade-minor-aws-ovn, gcp, upgrade, upgrade-minor",
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "LegacyResult(reason=\"\",message=\"release verification step failed: gcp-sdn-upgrade-4.10-micro, aws-ovn-upgrade-4.10-minor, azure-sdn-upgrade-4.10-minor, upgrade-minor-aws-ovn, gcp, upgrade, upgrade-minor\")",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
		},
		{
			name: "RejectedTagWithReasonAndMessageAnnotationSet",
			tag: &imagev1.TagReference{
				Annotations: map[string]string{
					releasecontroller.ReleaseAnnotationPhase:   releasecontroller.ReleasePhaseRejected,
					releasecontroller.ReleaseAnnotationReason:  "VerificationFailed",
					releasecontroller.ReleaseAnnotationMessage: "release verification step failed: gcp-sdn-upgrade-4.10-micro, aws-ovn-upgrade-4.10-minor, azure-sdn-upgrade-4.10-minor, upgrade-minor-aws-ovn, gcp, upgrade, upgrade-minor",
				},
			},
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.13.0",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.13.0",
					},
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.13.0",
						},
						ProwCoordinates: v1alpha1.ProwCoordinates{
							Namespace: "ci",
						},
					},
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "LegacyResult(reason=\"VerificationFailed\",message=\"release verification step failed: gcp-sdn-upgrade-4.10-micro, aws-ovn-upgrade-4.10-minor, azure-sdn-upgrade-4.10-minor, upgrade-minor-aws-ovn, gcp, upgrade, upgrade-minor\")",
					},
					PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
						BlockingJobs:  []v1alpha1.CIConfiguration{},
						InformingJobs: []v1alpha1.CIConfiguration{},
						UpgradeJobs:   []v1alpha1.CIConfiguration{},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setPayloadOverride(tc.tag, tc.payload)
			if !reflect.DeepEqual(tc.payload, tc.expected) {
				t.Errorf("%s: Expected %v, got %v", tc.name, tc.expected, tc.payload)
			}
		})
	}
}
