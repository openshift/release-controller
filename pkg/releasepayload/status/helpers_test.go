package status

import (
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"reflect"
	"testing"
)

func TestGetJobs(t *testing.T) {
	testCases := []struct {
		name     string
		input    v1alpha1.ReleasePayloadStatus
		expected []v1alpha1.JobStatus
	}{
		{
			name: "PayloadConditions",
			input: v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "one",
						CIConfigurationJobName: "blocking-job-1",
					},
					{
						CIConfigurationName:    "two",
						CIConfigurationJobName: "blocking-job-2",
					},
					{
						CIConfigurationName:    "three",
						CIConfigurationJobName: "blocking-job-3",
					},
					{
						CIConfigurationName:    "four",
						CIConfigurationJobName: "blocking-job-4",
					},
				},
				InformingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "one",
						CIConfigurationJobName: "informing-job-1",
					},
					{
						CIConfigurationName:    "two",
						CIConfigurationJobName: "informing-job-2",
					},
					{
						CIConfigurationName:    "three",
						CIConfigurationJobName: "informing-job-3",
					},
					{
						CIConfigurationName:    "four",
						CIConfigurationJobName: "informing-job-4",
					},
				},
			},
			expected: []v1alpha1.JobStatus{
				{
					CIConfigurationName:    "one",
					CIConfigurationJobName: "blocking-job-1",
				},
				{
					CIConfigurationName:    "two",
					CIConfigurationJobName: "blocking-job-2",
				},
				{
					CIConfigurationName:    "three",
					CIConfigurationJobName: "blocking-job-3",
				},
				{
					CIConfigurationName:    "four",
					CIConfigurationJobName: "blocking-job-4",
				},
				{
					CIConfigurationName:    "one",
					CIConfigurationJobName: "informing-job-1",
				},
				{
					CIConfigurationName:    "two",
					CIConfigurationJobName: "informing-job-2",
				},
				{
					CIConfigurationName:    "three",
					CIConfigurationJobName: "informing-job-3",
				},
				{
					CIConfigurationName:    "four",
					CIConfigurationJobName: "informing-job-4",
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			jobs := GetJobs(testCase.input)
			if !reflect.DeepEqual(jobs, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, jobs)
			}
		})
	}
}
