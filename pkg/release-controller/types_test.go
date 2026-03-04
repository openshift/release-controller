package releasecontroller

import (
	"testing"
)

func TestIncomplete(t *testing.T) {
	testCases := []struct {
		name               string
		status             VerificationStatusMap
		required           map[string]ReleaseVerification
		expectIncomplete   bool
		expectIncludesName string
		expectExcludesName string
	}{
		{
			name: "async job not pending is not counted as incomplete",
			status: VerificationStatusMap{
				"blocking-job": {State: ReleaseVerificationStateSucceeded},
			},
			required: map[string]ReleaseVerification{
				"blocking-job": {},
				"async-job":    {Optional: true, Async: true},
			},
			expectIncomplete: false,
		},
		{
			name:   "async job with no status is not counted as incomplete",
			status: VerificationStatusMap{},
			required: map[string]ReleaseVerification{
				"async-job": {Optional: true, Async: true},
			},
			expectIncomplete: false,
		},
		{
			name:   "non-async optional job still counts as incomplete",
			status: VerificationStatusMap{},
			required: map[string]ReleaseVerification{
				"optional-job": {Optional: true},
			},
			expectIncomplete:   true,
			expectIncludesName: "optional-job",
		},
		{
			name: "disabled job is not counted as incomplete",
			status: VerificationStatusMap{},
			required: map[string]ReleaseVerification{
				"disabled-job": {Disabled: true},
			},
			expectIncomplete: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			names, incomplete := tc.status.Incomplete(tc.required)
			if incomplete != tc.expectIncomplete {
				t.Errorf("expected incomplete=%v, got %v (names: %v)", tc.expectIncomplete, incomplete, names)
			}
			if tc.expectIncludesName != "" {
				found := false
				for _, n := range names {
					if n == tc.expectIncludesName {
						found = true
					}
				}
				if !found {
					t.Errorf("expected names to include %q, got %v", tc.expectIncludesName, names)
				}
			}
			if tc.expectExcludesName != "" {
				for _, n := range names {
					if n == tc.expectExcludesName {
						t.Errorf("expected names to exclude %q, but it was present", tc.expectExcludesName)
					}
				}
			}
		})
	}
}

func TestVerificationJobsWithRetries_Async(t *testing.T) {
	testCases := []struct {
		name                    string
		jobs                    map[string]ReleaseVerification
		result                  VerificationStatusMap
		expectRetryNames        []string
		expectBlockingJobFailed bool
	}{
		{
			name: "async job failure does not cause blocking failure",
			jobs: map[string]ReleaseVerification{
				"async-job": {Optional: true, Async: true},
			},
			result: VerificationStatusMap{
				"async-job": {State: ReleaseVerificationStateFailed},
			},
			expectRetryNames:        nil,
			expectBlockingJobFailed: false,
		},
		{
			name: "async job not started is not added to retry list",
			jobs: map[string]ReleaseVerification{
				"async-job": {Optional: true, Async: true},
			},
			result:                  VerificationStatusMap{},
			expectRetryNames:        nil,
			expectBlockingJobFailed: false,
		},
		{
			name: "non-async blocking job failure is still detected",
			jobs: map[string]ReleaseVerification{
				"blocking-job": {MaxRetries: 0},
				"async-job":    {Optional: true, Async: true},
			},
			result: VerificationStatusMap{
				"blocking-job": {State: ReleaseVerificationStateFailed, Retries: 0},
				"async-job":    {State: ReleaseVerificationStateFailed},
			},
			expectBlockingJobFailed: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			names, blockingFailed := VerificationJobsWithRetries(tc.jobs, tc.result)
			if blockingFailed != tc.expectBlockingJobFailed {
				t.Errorf("expected blockingJobFailed=%v, got %v", tc.expectBlockingJobFailed, blockingFailed)
			}
			if len(tc.expectRetryNames) == 0 && len(names) > 0 {
				t.Errorf("expected no retry names, got %v", names)
			}
		})
	}
}
