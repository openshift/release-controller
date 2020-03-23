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
