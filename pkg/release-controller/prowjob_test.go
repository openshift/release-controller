package releasecontroller

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestTruncateProwJobResultsURL(t *testing.T) {
	testCases := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "Matching Prefix",
			url:      "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-openshift-release-master-nightly-4.8-e2e-aws/1577585519322206208",
			expected: "/periodic-ci-openshift-release-master-nightly-4.8-e2e-aws/1577585519322206208",
		},
		{
			name:     "Non-Matching Prefix",
			url:      "https://prow.ci.openshift.org/view/gs/test-platform-results2/logs/periodic-ci-openshift-release-master-nightly-4.8-e2e-aws/1577585519322206208",
			expected: "https://prow.ci.openshift.org/view/gs/test-platform-results2/logs/periodic-ci-openshift-release-master-nightly-4.8-e2e-aws/1577585519322206208",
		},
		{
			name:     "Garbage",
			url:      "garbage in...garbage out",
			expected: "garbage in...garbage out",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := truncateProwJobResultsURL(testCase.url)
			if !cmp.Equal(result, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, result)
			}
		})
	}
}

func TestGenerateProwJobResultsURL(t *testing.T) {
	testCases := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "Truncated URL",
			url:      "/periodic-ci-openshift-release-master-nightly-4.8-e2e-aws/1577585519322206208",
			expected: "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-openshift-release-master-nightly-4.8-e2e-aws/1577585519322206208",
		},
		{
			name:     "Full URL",
			url:      "https://prow.ci.openshift.org/view/gs/test-platform-results2/logs/periodic-ci-openshift-release-master-nightly-4.8-e2e-aws/1577585519322206208",
			expected: "https://prow.ci.openshift.org/view/gs/test-platform-results2/logs/periodic-ci-openshift-release-master-nightly-4.8-e2e-aws/1577585519322206208",
		},
		{
			name:     "Garbage",
			url:      "garbage in...garbage out",
			expected: "garbage in...garbage out",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := GenerateProwJobResultsURL(testCase.url)
			if !cmp.Equal(result, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, result)
			}
		})
	}
}
