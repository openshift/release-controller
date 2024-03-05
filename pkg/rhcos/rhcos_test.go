package rhcos

import (
	"github.com/google/go-cmp/cmp"
	"testing"
)

func TestComputeJobState(t *testing.T) {
	testCases := []struct {
		name         string
		version      string
		architecture string
		ok           bool
		expected     string
	}{
		{
			name:     "NoMatch",
			version:  "412.86.202211091602",
			expected: "",
		},
		{
			name:         "LessThan",
			version:      "412.86.202211091602-0",
			architecture: "",
			ok:           true,
			expected:     "releases/rhcos-4.12",
		},
		{
			name:         "EqualTo",
			version:      "412.86.202212000000-0",
			architecture: "",
			ok:           true,
			expected:     "releases/rhcos-4.12",
		},
		{
			name:         "GreaterThan",
			version:      "412.86.202302091419-0",
			architecture: "",
			ok:           true,
			expected:     "prod/streams/4.12",
		},
		{
			name:         "4.9 After Changeover",
			version:      "49.84.202302111038-0",
			architecture: "",
			ok:           true,
			expected:     "prod/streams/4.9",
		},
		{
			name:         "4.9 After Changeover",
			version:      "49.94.202302111038-0",
			architecture: "",
			ok:           true,
			expected:     "prod/streams/4.9-9.4",
		},
		{
			name:         "4.9 Before Changeover",
			version:      "49.84.202210201521-0",
			architecture: "",
			ok:           true,
			expected:     "releases/rhcos-4.9",
		},
		{
			name:         "Multi-Arch 4.9 After Changeover",
			version:      "49.84.202302111038-0",
			architecture: "-s309x",
			ok:           true,
			expected:     "prod/streams/4.9",
		},
		{
			name:         "Multi-Arch 4.9 Before Changeover",
			version:      "49.84.202210201521-0",
			architecture: "-s390x",
			ok:           true,
			expected:     "releases/rhcos-4.9-s390x",
		},
		{
			name:         "4.8 After Changeover",
			version:      "48.84.202301181057-0",
			architecture: "",
			ok:           true,
			expected:     "releases/rhcos-4.8",
		},
		{
			name:         "4.8 Before Changeover",
			version:      "48.84.202211030947-0",
			architecture: "",
			ok:           true,
			expected:     "releases/rhcos-4.8",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, ok := getRHCoSReleaseStream(testCase.version, testCase.architecture)
			if !cmp.Equal(ok, testCase.ok) {
				t.Errorf("%s: Expected ok %v, got %v", testCase.name, testCase.ok, ok)
			}
			if !cmp.Equal(result, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, result)
			}
		})
	}
}
