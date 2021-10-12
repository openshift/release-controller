package main

import (
	"fmt"
	"testing"

	prowconfig "k8s.io/test-infra/prow/config"
)

func TestValidateProwJob(t *testing.T) {
	testCases := []struct {
		name        string
		pj          *prowconfig.Periodic
		expectedErr error
	}{
		{
			name:        "No cluster yields error",
			pj:          &prowconfig.Periodic{},
			expectedErr: fmt.Errorf(`the jobs cluster must be set to a value that is not default, was ""`),
		},
		{
			name:        "Default cluster yields error",
			pj:          &prowconfig.Periodic{JobBase: prowconfig.JobBase{Cluster: "default"}},
			expectedErr: fmt.Errorf(`the jobs cluster must be set to a value that is not default, was "default"`),
		},
		{
			name: "No default cluster, no error",
			pj:   &prowconfig.Periodic{JobBase: prowconfig.JobBase{Cluster: "api.ci"}},
		},
	}

	t.Parallel()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualErr := validateProwJob(tc.pj)
			var actualErrMsg, expectedErrMsg string
			if actualErr != nil {
				actualErrMsg = actualErr.Error()
			}
			if tc.expectedErr != nil {
				expectedErrMsg = tc.expectedErr.Error()
			}
			if actualErrMsg != expectedErrMsg {
				t.Errorf("Expected err %q, got err %q", expectedErrMsg, actualErrMsg)
			}
		})
	}
}

func TestGenerateSafeProwJobName(t *testing.T) {
	testCases := []struct {
		name     string
		jobName  string
		suffix   string
		expected string
	}{
		{
			name:     "JobNameWithoutSuffixWithNoTruncation",
			jobName:  "4.9.0-0.ci-2021-08-30-130010-job-name-fake",
			suffix:   "",
			expected: "4.9.0-0.ci-2021-08-30-130010-job-name-fake",
		},
		{
			name:     "MaxSizeJobNameWithoutSuffixWithNoTruncation",
			jobName:  "4.9.0-0.ci-2021-08-30-130010-this-is-a-really-long-job-name-foo",
			suffix:   "",
			expected: "4.9.0-0.ci-2021-08-30-130010-this-is-a-really-long-job-name-foo",
		},
		{
			name:     "JobNameWithoutSuffixWithTruncation",
			jobName:  "4.9.0-0.ci-2021-08-30-130010-this-is-a-really-long-job-name-fake",
			suffix:   "",
			expected: "4.9.0-0.ci-2021-08-30-130010-this-is-a-really-long-job-name-...",
		},
		{
			name:     "JobNameWithSuffixWithNoTruncation",
			jobName:  "4.9.0-0.ci-2021-08-30-130010-job-name",
			suffix:   "analysis-1",
			expected: "4.9.0-0.ci-2021-08-30-130010-job-name-analysis-1",
		},
		{
			name:     "JobNameWithSuffixWithTruncation",
			jobName:  "4.9.0-0.ci-2021-08-30-133010-this-is-a-really-long-job-name",
			suffix:   "analysis-1",
			expected: "4.9.0-0.ci-2021-08-30-133010-this-is-a-really-lon...-analysis-1",
		},
		{
			name:     "MaxSizeJobNameWithSuffixWithNoTruncation",
			jobName:  "4.9.0-0.ci-2021-08-30-133010-fake-job-name-for-test1",
			suffix:   "analysis-1",
			expected: "4.9.0-0.ci-2021-08-30-133010-fake-job-name-for-test1-analysis-1",
		},
		{
			name:     "ExtremelyLongJobNameWithSuffixWithTruncation",
			jobName:  "4.9.0-0.ci-2021-08-30-133010-this-is-a-really-really-really-really-really-really-long-job-name",
			suffix:   "analysis-1",
			expected: "4.9.0-0.ci-2021-08-30-133010-this-is-a-really-rea...-analysis-1",
		},
	}

	t.Parallel()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := generateSafeProwJobName(tc.jobName, tc.suffix)
			if result != tc.expected {
				t.Errorf("Expected truncated string %q, got %q", tc.expected, result)
			}
			if len(result) > maxProwJobNameLength {
				t.Errorf("Expected string of length less than %d, got string of length %d", maxProwJobNameLength, len(result))
			}
		})
	}
}
