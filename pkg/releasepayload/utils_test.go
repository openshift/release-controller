package releasepayload

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
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

func Test_convertToVerificationStatusMapResult(t *testing.T) {
	tests := []struct {
		name      string
		job       v1alpha1.JobStatus
		wantState string
	}{
		{
			name: "Unknown state with no URL returns Pending",
			job: v1alpha1.JobStatus{
				AggregateState: v1alpha1.JobStateUnknown,
			},
			wantState: releasecontroller.ReleaseVerificationStatePending,
		},
		{
			name: "Unknown state with URL returns Succeeded",
			job: v1alpha1.JobStatus{
				AggregateState: v1alpha1.JobStateUnknown,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						HumanProwResultsURL: "https://abc.123/",
						State:               v1alpha1.JobRunStateSuccess,
					},
				},
			},
			wantState: releasecontroller.ReleaseVerificationStateSucceeded,
		},
		{
			name: "Success state with URL returns Succeeded",
			job: v1alpha1.JobStatus{
				AggregateState: v1alpha1.JobStateSuccess,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						HumanProwResultsURL: "https://abc.123/",
						State:               v1alpha1.JobRunStateSuccess,
					},
				},
			},
			wantState: releasecontroller.ReleaseVerificationStateSucceeded,
		},
		{
			name: "Success state with no URL returns Pending",
			job: v1alpha1.JobStatus{
				AggregateState: v1alpha1.JobStateSuccess,
			},
			wantState: releasecontroller.ReleaseVerificationStatePending,
		},
		{
			name: "Pending state returns Pending",
			job: v1alpha1.JobStatus{
				AggregateState: v1alpha1.JobStatePending,
			},
			wantState: releasecontroller.ReleaseVerificationStatePending,
		},
		{
			name: "Failure state returns Failed",
			job: v1alpha1.JobStatus{
				AggregateState: v1alpha1.JobStateFailure,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						HumanProwResultsURL: "https://abc.123/",
						State:               v1alpha1.JobRunStateFailure,
					},
				},
			},
			wantState: releasecontroller.ReleaseVerificationStateFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := convertToVerificationStatusMapResult(tt.job)
			if !ok {
				t.Fatalf("%s: expected ok to be true", tt.name)
			}
			if result.State != tt.wantState {
				t.Errorf("%s: Expected state %v, got %v", tt.name, tt.wantState, result.State)
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

func Test_getVerificationStatusPreviousAttemptURLs(t *testing.T) {
	tests := []struct {
		name       string
		results    []v1alpha1.JobRunResult
		primaryURL string
		want       []string
	}{
		{
			name:       "No Results",
			results:    []v1alpha1.JobRunResult{},
			primaryURL: "",
			want:       nil,
		},
		{
			name: "Single Result",
			results: []v1alpha1.JobRunResult{
				{
					HumanProwResultsURL: "https://abc.123/",
					State:               v1alpha1.JobRunStateSuccess,
				},
			},
			primaryURL: "https://abc.123/",
			want:       nil,
		},
		{
			name: "Multiple Results - Primary Is Success",
			results: []v1alpha1.JobRunResult{
				{
					HumanProwResultsURL: "https://abc.123/",
					State:               v1alpha1.JobRunStateFailure,
				},
				{
					HumanProwResultsURL: "https://def.456/",
					State:               v1alpha1.JobRunStateSuccess,
				},
			},
			primaryURL: "https://def.456/",
			want:       []string{"https://abc.123/"},
		},
		{
			name: "Multiple Results - All Failures",
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
			primaryURL: "https://ghi.789/",
			want:       []string{"https://abc.123/", "https://def.456/"},
		},
		{
			name: "Results With Empty URL Excluded",
			results: []v1alpha1.JobRunResult{
				{
					HumanProwResultsURL: "",
					State:               v1alpha1.JobRunStateFailure,
				},
				{
					HumanProwResultsURL: "https://def.456/",
					State:               v1alpha1.JobRunStateFailure,
				},
				{
					HumanProwResultsURL: "https://ghi.789/",
					State:               v1alpha1.JobRunStateSuccess,
				},
			},
			primaryURL: "https://ghi.789/",
			want:       []string{"https://def.456/"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getVerificationStatusPreviousAttemptURLs(tt.results, tt.primaryURL)
			if len(tt.want) != len(got) {
				t.Errorf("%s: Expected %v, got %v", tt.name, tt.want, got)
				return
			}
			for i := range tt.want {
				if tt.want[i] != got[i] {
					t.Errorf("%s: Expected %v at index %d, got %v", tt.name, tt.want[i], i, got[i])
				}
			}
		})
	}
}

func Test_getTransitionTime(t *testing.T) {
	t1 := metav1.NewTime(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	t2 := metav1.NewTime(time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC))
	t3 := metav1.NewTime(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

	tests := []struct {
		name    string
		results []v1alpha1.JobRunResult
		want    *metav1.Time
	}{
		{
			name:    "No results",
			results: nil,
			want:    nil,
		},
		{
			name: "Single result with CompletionTime",
			results: []v1alpha1.JobRunResult{
				{
					StartTime:      t1,
					CompletionTime: &t1,
				},
			},
			want: &t1,
		},
		{
			name: "Single result without CompletionTime",
			results: []v1alpha1.JobRunResult{
				{
					StartTime: t1,
				},
			},
			want: nil,
		},
		{
			name: "Multiple results returns latest CompletionTime",
			results: []v1alpha1.JobRunResult{
				{
					StartTime:      t1,
					CompletionTime: &t1,
				},
				{
					StartTime:      t3,
					CompletionTime: &t3,
				},
				{
					StartTime:      t2,
					CompletionTime: &t2,
				},
			},
			want: &t3,
		},
		{
			name: "Multiple results, latest has no CompletionTime",
			results: []v1alpha1.JobRunResult{
				{
					StartTime:      t1,
					CompletionTime: &t1,
				},
				{
					StartTime: t2,
				},
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getTransitionTime(tt.results)
			if tt.want == nil && got != nil {
				t.Errorf("expected nil, got %v", got)
			} else if tt.want != nil && got == nil {
				t.Errorf("expected %v, got nil", tt.want)
			} else if tt.want != nil && !tt.want.Equal(got) {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func Test_convertToVerificationStatusMapResult_TransitionTime(t *testing.T) {
	completionTime := metav1.NewTime(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

	tests := []struct {
		name               string
		job                v1alpha1.JobStatus
		wantTransitionTime *metav1.Time
	}{
		{
			name:               "No results has nil TransitionTime",
			job:                v1alpha1.JobStatus{},
			wantTransitionTime: nil,
		},
		{
			name: "Result with CompletionTime populates TransitionTime",
			job: v1alpha1.JobStatus{
				AggregateState: v1alpha1.JobStateFailure,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State:               v1alpha1.JobRunStateFailure,
						HumanProwResultsURL: "https://example.com/",
						StartTime:           metav1.NewTime(time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)),
						CompletionTime:      &completionTime,
					},
				},
			},
			wantTransitionTime: &completionTime,
		},
		{
			name: "Result without CompletionTime has nil TransitionTime",
			job: v1alpha1.JobStatus{
				AggregateState: v1alpha1.JobStatePending,
				JobRunResults: []v1alpha1.JobRunResult{
					{
						State:     v1alpha1.JobRunStatePending,
						StartTime: metav1.NewTime(time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)),
					},
				},
			},
			wantTransitionTime: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := convertToVerificationStatusMapResult(tt.job)
			if !ok {
				t.Fatalf("expected ok to be true")
			}
			if tt.wantTransitionTime == nil && result.TransitionTime != nil {
				t.Errorf("expected nil TransitionTime, got %v", result.TransitionTime)
			} else if tt.wantTransitionTime != nil && result.TransitionTime == nil {
				t.Errorf("expected TransitionTime %v, got nil", tt.wantTransitionTime)
			} else if tt.wantTransitionTime != nil && !tt.wantTransitionTime.Equal(result.TransitionTime) {
				t.Errorf("expected TransitionTime %v, got %v", tt.wantTransitionTime, result.TransitionTime)
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
