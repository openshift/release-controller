package jobstatus

import (
	"github.com/google/go-cmp/cmp"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"testing"
)

func TestComputeJobState(t *testing.T) {
	testCases := []struct {
		name     string
		input    []v1alpha1.JobStatus
		expected v1alpha1.JobState
	}{
		{
			name: "AllJobsSuccessful",
			input: []v1alpha1.JobStatus{
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
			expected: v1alpha1.JobStateSuccess,
		},
		{
			name: "PendingJob",
			input: []v1alpha1.JobStatus{
				{
					AggregateState: v1alpha1.JobStateSuccess,
				},
				{
					AggregateState: v1alpha1.JobStateSuccess,
				},
				{
					AggregateState: v1alpha1.JobStatePending,
				},
			},
			expected: v1alpha1.JobStatePending,
		},
		{
			name: "UnknownJob",
			input: []v1alpha1.JobStatus{
				{
					AggregateState: v1alpha1.JobStateSuccess,
				},
				{
					AggregateState: v1alpha1.JobStateSuccess,
				},
				{
					AggregateState: v1alpha1.JobStateUnknown,
				},
			},
			expected: v1alpha1.JobStateUnknown,
		},
		{
			name: "FailedJob",
			input: []v1alpha1.JobStatus{
				{
					AggregateState: v1alpha1.JobStateSuccess,
				},
				{
					AggregateState: v1alpha1.JobStateFailure,
				},
				{
					AggregateState: v1alpha1.JobStateSuccess,
				},
			},
			expected: v1alpha1.JobStateFailure,
		},
		{
			name: "Garbage",
			input: []v1alpha1.JobStatus{
				{
					AggregateState: v1alpha1.JobStateSuccess,
				},
				{
					AggregateState: "garbage",
				},
				{
					AggregateState: v1alpha1.JobStateSuccess,
				},
			},
			expected: v1alpha1.JobStateUnknown,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ComputeJobState(testCase.input)
			if !cmp.Equal(result, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, result)
			}
		})
	}
}
