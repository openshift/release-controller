package v1alpha1helpers

import (
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"testing"
)

func TestCanonicalize(t *testing.T) {
	testCases := []struct {
		name     string
		input    *v1alpha1.ReleasePayload
		expected *v1alpha1.ReleasePayload
	}{
		{
			name: "PayloadConditions",
			input: &v1alpha1.ReleasePayload{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ReleasePayload",
					APIVersion: "release.openshift.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
					Labels: map[string]string{
						"release.openshift.io/imagestream":         "release",
						"release.openshift.io/imagestreamtag-name": "4.11.0-0.nightly-2022-02-09-091559",
					},
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.11.0-0.nightly-2022-02-09-091559",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type: v1alpha1.ConditionPayloadFailed,
						},
						{
							Type: v1alpha1.ConditionPayloadRejected,
						},
						{
							Type: v1alpha1.ConditionPayloadCreated,
						},
						{
							Type: v1alpha1.ConditionPayloadAccepted,
						},
					},
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "Zulu",
							CIConfigurationJobName: "job-1",
						},
						{
							CIConfigurationName:    "Romeo",
							CIConfigurationJobName: "job-2",
						},
						{
							CIConfigurationName:    "Delta",
							CIConfigurationJobName: "job-3",
						},
						{
							CIConfigurationName:    "Foxtrot",
							CIConfigurationJobName: "job-4",
						},
					},
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "10",
							CIConfigurationJobName: "job-1",
						},
						{
							CIConfigurationName:    "5",
							CIConfigurationJobName: "job-2",
						},
						{
							CIConfigurationName:    "25",
							CIConfigurationJobName: "job-3",
						},
						{
							CIConfigurationName:    "15",
							CIConfigurationJobName: "job-4",
						},
					},
					UpgradeJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "Whiskey",
							CIConfigurationJobName: "job-1",
						},
						{
							CIConfigurationName:    "Tango",
							CIConfigurationJobName: "job-2",
						},
						{
							CIConfigurationName:    "Foxtrot",
							CIConfigurationJobName: "job-3",
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ReleasePayload",
					APIVersion: "release.openshift.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
					Labels: map[string]string{
						"release.openshift.io/imagestream":         "release",
						"release.openshift.io/imagestreamtag-name": "4.11.0-0.nightly-2022-02-09-091559",
					},
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release",
						ImagestreamTagName: "4.11.0-0.nightly-2022-02-09-091559",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type: v1alpha1.ConditionPayloadAccepted,
						},
						{
							Type: v1alpha1.ConditionPayloadCreated,
						},
						{
							Type: v1alpha1.ConditionPayloadFailed,
						},
						{
							Type: v1alpha1.ConditionPayloadRejected,
						},
					},
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "Delta",
							CIConfigurationJobName: "job-3",
						},
						{
							CIConfigurationName:    "Foxtrot",
							CIConfigurationJobName: "job-4",
						},
						{
							CIConfigurationName:    "Romeo",
							CIConfigurationJobName: "job-2",
						},
						{
							CIConfigurationName:    "Zulu",
							CIConfigurationJobName: "job-1",
						},
					},
					InformingJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "10",
							CIConfigurationJobName: "job-1",
						},
						{
							CIConfigurationName:    "15",
							CIConfigurationJobName: "job-4",
						},
						{
							CIConfigurationName:    "25",
							CIConfigurationJobName: "job-3",
						},
						{
							CIConfigurationName:    "5",
							CIConfigurationJobName: "job-2",
						},
					},
					UpgradeJobResults: []v1alpha1.JobStatus{
						{
							CIConfigurationName:    "Foxtrot",
							CIConfigurationJobName: "job-3",
						},
						{
							CIConfigurationName:    "Tango",
							CIConfigurationJobName: "job-2",
						},
						{
							CIConfigurationName:    "Whiskey",
							CIConfigurationJobName: "job-1",
						},
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			copied := testCase.input.DeepCopy()
			CanonicalizeReleasePayloadStatus(copied)
			if !reflect.DeepEqual(copied, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, copied)
			}
		})
	}
}
