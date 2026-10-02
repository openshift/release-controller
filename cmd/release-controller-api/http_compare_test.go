package main

import (
	"testing"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
)

func TestComparisonChangelogEnabled(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		comparison Comparison
		expected   bool
	}{{
		name:       "comparison without a release",
		comparison: Comparison{Type: From},
		expected:   true,
	}, {
		name: "comparison of a stable release",
		comparison: Comparison{
			Type:    From,
			Release: &releasecontroller.Release{Config: &releasecontroller.ReleaseConfig{Name: "4-stable", As: releasecontroller.ReleaseConfigModeStable}},
		},
		expected: true,
	}, {
		name: "comparison of a stable release with changelogs disabled",
		comparison: Comparison{
			Type:    To,
			Release: &releasecontroller.Release{Config: &releasecontroller.ReleaseConfig{Name: "4-stable", As: releasecontroller.ReleaseConfigModeStable, ChangelogGeneration: releasecontroller.ChangelogGenerationDisabled}},
		},
		expected: false,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if actual := tc.comparison.changelogEnabled(); actual != tc.expected {
				t.Errorf("Expected %t, got %t", tc.expected, actual)
			}
		})
	}
}
