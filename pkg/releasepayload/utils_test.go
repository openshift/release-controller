package releasepayload

import (
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"testing"
)

func Test_getVerificationStatusRetries(t *testing.T) {
	tests := []struct {
		name string
		job  v1alpha1.JobStatus
		want int
	}{
		{
			name: "No Results",
			job:  v1alpha1.JobStatus{},
			want: 0,
		},
		{
			name: "Single Result",
			job:  v1alpha1.JobStatus{},
			want: 0,
		},
		{
			name: "Multiple Results Without MaxRetries",
			job: v1alpha1.JobStatus{
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
				},
			},
			want: 0,
		},
		{
			name: "Multiple Result With MaxRetries - No Retries",
			job: v1alpha1.JobStatus{
				MaxRetries: 3,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateSuccess,
					},
				},
			},
			want: 0,
		},
		{
			name: "Multiple Result With MaxRetries - Single Retry",
			job: v1alpha1.JobStatus{
				MaxRetries: 3,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateSuccess,
					},
				},
			},
			want: 1,
		},
		{
			name: "Multiple Result With MaxRetries - Multiple Retries",
			job: v1alpha1.JobStatus{
				MaxRetries: 3,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateSuccess,
					},
				},
			},
			want: 2,
		},
		{
			name: "Multiple Result With MaxRetries - Max Retries",
			job: v1alpha1.JobStatus{
				MaxRetries: 3,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
				},
			},
			want: 3,
		},
		{
			name: "Multiple Result With MaxRetries - Extra Retries",
			job: v1alpha1.JobStatus{
				MaxRetries: 3,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateFailure,
					},
					{
						State: v1alpha1.JobRunStateSuccess,
					},
				},
			},
			want: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getVerificationStatusRetries(tt.job)
			if tt.want != got {
				t.Errorf("%s: Expected %v, got %v", tt.name, tt.want, got)
			}
		})
	}
}

func Test_getVerificationStatusState(t *testing.T) {
	tests := []struct {
		name  string
		state v1alpha1.JobState
		want  string
	}{
		{
			name:  "Succeeded",
			state: v1alpha1.JobStateSuccess,
			want:  releasecontroller.ReleaseVerificationStateSucceeded,
		},
		{
			name:  "Pending",
			state: v1alpha1.JobStatePending,
			want:  releasecontroller.ReleaseVerificationStatePending,
		},
		{
			name:  "Failed",
			state: v1alpha1.JobStateFailure,
			want:  releasecontroller.ReleaseVerificationStateFailed,
		},
		{
			name:  "Default",
			state: v1alpha1.JobStateUnknown,
			want:  releasecontroller.ReleaseVerificationStateSucceeded,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getVerificationStatusState(tt.state)
			if tt.want != got {
				t.Errorf("%s: Expected %v, got %v", tt.name, tt.want, got)
			}
		})
	}
}

func Test_getVerificationStatusUrl(t *testing.T) {
	tests := []struct {
		name    string
		results []v1alpha1.JobRunResult
		want    string
	}{
		{
			name:    "No Results",
			results: []v1alpha1.JobRunResult{},
			want:    "",
		},
		{
			name: "Single Result - No URL",
			results: []v1alpha1.JobRunResult{
				{
					State: v1alpha1.JobRunStateSuccess,
				},
			},
			want: "",
		},
		{
			name: "Single Successful Result",
			results: []v1alpha1.JobRunResult{
				{
					HumanProwResultsURL: "https://abc.123/",
					State:               v1alpha1.JobRunStateSuccess,
				},
			},
			want: "https://abc.123/",
		},
		{
			name: "Single Failure Result",
			results: []v1alpha1.JobRunResult{
				{
					HumanProwResultsURL: "https://abc.123/",
					State:               v1alpha1.JobRunStateFailure,
				},
			},
			want: "https://abc.123/",
		},
		{
			name: "Multiple Failure Results",
			results: []v1alpha1.JobRunResult{
				{
					HumanProwResultsURL: "https://abc.123/",
					State:               v1alpha1.JobRunStateFailure,
				},
				{
					HumanProwResultsURL: "https://def.456/",
					State:               v1alpha1.JobRunStateFailure,
				},
				{
					HumanProwResultsURL: "https://ghi.789/",
					State:               v1alpha1.JobRunStateFailure,
				},
			},
			want: "https://ghi.789/",
		},
		{
			name: "Multiple Results With Success",
			results: []v1alpha1.JobRunResult{
				{
					HumanProwResultsURL: "https://abc.123/",
					State:               v1alpha1.JobRunStateFailure,
				},
				{
					HumanProwResultsURL: "https://def.456/",
					State:               v1alpha1.JobRunStateSuccess,
				},
				{
					HumanProwResultsURL: "https://ghi.789/",
					State:               v1alpha1.JobRunStateFailure,
				},
			},
			want: "https://def.456/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getVerificationStatusUrl(tt.results)
			if tt.want != got {
				t.Errorf("%s: Expected %v, got %v", tt.name, tt.want, got)
			}
		})
	}
}
