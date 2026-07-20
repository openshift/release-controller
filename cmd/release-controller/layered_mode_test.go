package main

import (
	"testing"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
)

func TestLayeredModeConfiguration(t *testing.T) {
	testCases := []struct {
		name        string
		configJSON  string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Valid layered mode without 'to' field",
			configJSON:  `{"name": "test-layered", "as": "Layered"}`,
			expectError: false,
		},
		{
			name:        "Layered mode with optional 'to' field is allowed",
			configJSON:  `{"name": "test-layered", "as": "Layered", "to": "releases"}`,
			expectError: false,
		},
		{
			name:        "Stable mode without 'to' field is valid",
			configJSON:  `{"name": "test-stable", "as": "Stable"}`,
			expectError: false,
		},
		{
			name:        "Integration mode without 'to' field should error",
			configJSON:  `{"name": "test-integration"}`,
			expectError: true,
			errorMsg:    "release must specify 'to' unless 'as' is 'Stable' or 'Layered'",
		},
		{
			name:        "Integration mode with 'to' field is valid",
			configJSON:  `{"name": "test-integration", "to": "releases"}`,
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config, err := releasecontroller.ParseReleaseConfig(tc.configJSON, nil)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if tc.errorMsg != "" && err.Error() != tc.errorMsg {
					t.Errorf("Expected error message %q, got %q", tc.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Expected no error but got: %v", err)
				return
			}

			if config == nil {
				t.Errorf("Expected valid config but got nil")
				return
			}
		})
	}
}
