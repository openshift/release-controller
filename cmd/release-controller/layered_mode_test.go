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

func TestExternalRegistryPublishValidation(t *testing.T) {
	testCases := []struct {
		name        string
		configJSON  string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Valid external registry publish configuration",
			configJSON:  `{"name": "test", "to": "releases", "publish": {"ext-mirror": {"externalRegistry": {"registry": "quay.io/test/repo", "secretName": "test-secret"}}}}`,
			expectError: false,
		},
		{
			name:        "External registry publish missing registry",
			configJSON:  `{"name": "test", "to": "releases", "publish": {"ext-mirror": {"externalRegistry": {"secretName": "test-secret"}}}}`,
			expectError: true,
			errorMsg:    "externalRegistry publish for ext-mirror has no registry",
		},
		{
			name:        "External registry publish missing secretName",
			configJSON:  `{"name": "test", "to": "releases", "publish": {"ext-mirror": {"externalRegistry": {"registry": "quay.io/test/repo"}}}}`,
			expectError: true,
			errorMsg:    "externalRegistry publish for ext-mirror has no secretName",
		},
		{
			name:        "External registry publish with empty registry",
			configJSON:  `{"name": "test", "to": "releases", "publish": {"ext-mirror": {"externalRegistry": {"registry": "", "secretName": "test-secret"}}}}`,
			expectError: true,
			errorMsg:    "externalRegistry publish for ext-mirror has no registry",
		},
		{
			name:        "External registry publish with empty secretName",
			configJSON:  `{"name": "test", "to": "releases", "publish": {"ext-mirror": {"externalRegistry": {"registry": "quay.io/test/repo", "secretName": ""}}}}`,
			expectError: true,
			errorMsg:    "externalRegistry publish for ext-mirror has no secretName",
		},
		{
			name:        "External registry publish with optional fields",
			configJSON:  `{"name": "test", "to": "releases", "publish": {"ext-mirror": {"externalRegistry": {"registry": "quay.io/test/repo", "secretName": "test-secret", "tags": ["latest", "v1.0"], "excludeTags": ["dev"]}}}}`,
			expectError: false,
		},
		{
			name:        "External registry publish with override CLI image",
			configJSON:  `{"name": "test", "to": "releases", "publish": {"ext-mirror": {"externalRegistry": {"registry": "quay.io/test/repo", "secretName": "test-secret", "overrideCLIImage": "quay.io/openshift/cli:latest"}}}}`,
			expectError: false,
		},
		{
			name:        "Layered mode with external registry and override CLI image",
			configJSON:  `{"name": "test-layered", "as": "Layered", "publish": {"ext-mirror": {"externalRegistry": {"registry": "quay.io/test/layered", "secretName": "test-secret", "overrideCLIImage": "registry.ci.openshift.org/ocp/4.17:cli"}}}}`,
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
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}

				if config == nil {
					t.Errorf("Expected valid config but got nil")
				}
			}
		})
	}
}
