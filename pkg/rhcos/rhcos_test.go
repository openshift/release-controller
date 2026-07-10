package rhcos

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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

func TestRHCoSDiffRegex(t *testing.T) {
	testCases := []struct {
		name        string
		input       string
		shouldMatch bool
		fromVersion string
		toVersion   string
	}{
		{
			name:        "Old format without RHEL version",
			input:       "* Red Hat Enterprise Linux CoreOS upgraded from 9.8.20260312-0 to 9.8.20260227-0\n",
			shouldMatch: true,
			fromVersion: "9.8.20260312-0",
			toVersion:   "9.8.20260227-0",
		},
		{
			name:        "New format with RHEL version",
			input:       "* Red Hat Enterprise Linux CoreOS 9.8 upgraded from 9.8.20260305-0 to 9.8.20260312-0\n",
			shouldMatch: true,
			fromVersion: "9.8.20260305-0",
			toVersion:   "9.8.20260312-0",
		},
		{
			name:        "Old format with 4.x style versions",
			input:       "* Red Hat Enterprise Linux CoreOS upgraded from 418.94.202410090804-0 to 418.94.202410150804-0\n",
			shouldMatch: true,
			fromVersion: "418.94.202410090804-0",
			toVersion:   "418.94.202410150804-0",
		},
		{
			name:        "Old format without RHEL version and without 4.x style",
			input:       "* Red Hat Enterprise Linux CoreOS upgraded from 9.6.20260225-1 to 9.6.20260303-1\n",
			shouldMatch: true,
			fromVersion: "9.6.20260225-1",
			toVersion:   "9.6.20260303-1",
		},
		{
			name:        "CentOS Stream CoreOS does not match RHEL CoreOS regex",
			input:       "* CentOS Stream CoreOS upgraded from 9.6.20260225-1 to 9.6.20260303-1\n",
			shouldMatch: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			matches := reMdRHCoSDiff.FindStringSubmatch(tc.input)
			if tc.shouldMatch {
				if matches == nil {
					t.Errorf("Expected match but got none for input: %s", tc.input)
					return
				}
				if matches[1] != tc.fromVersion {
					t.Errorf("Expected from version %q, got %q", tc.fromVersion, matches[1])
				}
				if matches[3] != tc.toVersion {
					t.Errorf("Expected to version %q, got %q", tc.toVersion, matches[3])
				}
			} else {
				if matches != nil {
					t.Errorf("Expected no match but got: %v", matches)
				}
			}
		})
	}
}

func TestRHCoSVersionRegex(t *testing.T) {
	testCases := []struct {
		name        string
		input       string
		shouldMatch bool
		version     string
	}{
		{
			name:        "Old format without RHEL version",
			input:       "* Red Hat Enterprise Linux CoreOS 9.8.20260312-0\n",
			shouldMatch: true,
			version:     "9.8.20260312-0",
		},
		{
			name:        "New format with RHEL version",
			input:       "* Red Hat Enterprise Linux CoreOS 9.8 9.8.20260305-0\n",
			shouldMatch: true,
			version:     "9.8.20260305-0",
		},
		{
			name:        "Old format with 4.x style versions",
			input:       "* Red Hat Enterprise Linux CoreOS 418.94.202410090804-0\n",
			shouldMatch: true,
			version:     "418.94.202410090804-0",
		},
		{
			name:        "CentOS Stream CoreOS does not match RHEL CoreOS regex",
			input:       "* CentOS Stream CoreOS 9.8.20260312-0\n",
			shouldMatch: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			matches := reMdRHCoSVersion.FindStringSubmatch(tc.input)
			if tc.shouldMatch {
				if matches == nil {
					t.Errorf("Expected match but got none for input: %s", tc.input)
					return
				}
				if matches[1] != tc.version {
					t.Errorf("Expected version %q, got %q", tc.version, matches[1])
				}
			} else {
				if matches != nil {
					t.Errorf("Expected no match but got: %v", matches)
				}
			}
		})
	}
}

func TestRHCoS10DiffRegex(t *testing.T) {
	testCases := []struct {
		name        string
		input       string
		shouldMatch bool
		fromVersion string
		toVersion   string
	}{
		{
			name:        "Space-separated RHEL version (original format)",
			input:       "* Red Hat Enterprise Linux CoreOS 10 10.0 upgraded from 10.0.20260101-0 to 10.0.20260201-0\n",
			shouldMatch: true,
			fromVersion: "10.0.20260101-0",
			toVersion:   "10.0.20260201-0",
		},
		{
			name:        "Period-separated RHEL minor (CoreOS 10.2)",
			input:       "* Red Hat Enterprise Linux CoreOS 10.2 upgraded from 10.2.20260328-0 to 10.2.20260321-0\n",
			shouldMatch: true,
			fromVersion: "10.2.20260328-0",
			toVersion:   "10.2.20260321-0",
		},
		{
			name:        "No RHEL minor version",
			input:       "* Red Hat Enterprise Linux CoreOS 10 upgraded from 10.2.20260328-0 to 10.2.20260321-0\n",
			shouldMatch: true,
			fromVersion: "10.2.20260328-0",
			toVersion:   "10.2.20260321-0",
		},
		{
			name:        "Two-digit RHEL minor",
			input:       "* Red Hat Enterprise Linux CoreOS 10.20 upgraded from 10.20.20270101-0 to 10.20.20270201-0\n",
			shouldMatch: true,
			fromVersion: "10.20.20270101-0",
			toVersion:   "10.20.20270201-0",
		},
		{
			name:        "RHCOS 9 line should NOT match RHCOS 10 regex",
			input:       "* Red Hat Enterprise Linux CoreOS 9.8 upgraded from 9.8.20260305-0 to 9.8.20260312-0\n",
			shouldMatch: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m := reMdRHCoS10Diff.FindStringSubmatch(tc.input)
			if tc.shouldMatch {
				if m == nil {
					t.Fatalf("expected match but got none for input: %s", tc.input)
				}
				if m[1] != tc.fromVersion {
					t.Errorf("Expected from version %q, got %q", tc.fromVersion, m[1])
				}
				if m[3] != tc.toVersion {
					t.Errorf("Expected to version %q, got %q", tc.toVersion, m[3])
				}
			} else {
				if m != nil {
					t.Errorf("Expected no match but got: %v", m)
				}
			}
		})
	}
}

func TestTransformMarkDownOutputDualRHCOSLines(t *testing.T) {
	input := `## Changes from 4.20.0
* Red Hat Enterprise Linux CoreOS 9.8 upgraded from 9.8.20260101-0 to 9.8.20260201-0
* Red Hat Enterprise Linux CoreOS 10 10.0 upgraded from 10.0.20260101-0 to 10.0.20260201-0
`
	out, err := TransformMarkDownOutput(input, "4.20.0", "4.21.0", "x86_64", "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "coreos-base-alert") < 2 {
		t.Fatalf("expected two CoreOS base layer infoboxes, got:\n%s", out)
	}
}

func TestTransformJsonOutputDualCoreOS(t *testing.T) {
	j := `{
  "components": [
    {"name": "Red Hat Enterprise Linux CoreOS", "version": "9.8.20260201-0", "from": "9.8.20260101-0"},
    {"name": "Red Hat Enterprise Linux CoreOS 10", "version": "10.0.20260201-0", "from": "10.0.20260101-0"}
  ]
}`
	out, err := TransformJsonOutput(j, "x86_64", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"versionUrl"`) {
		t.Fatalf("expected versionUrl in output: %s", out)
	}
	if strings.Count(out, `"versionUrl"`) < 2 {
		t.Fatalf("expected two versionUrl fields: %s", out)
	}
}

func TestTransformJsonOutputRHCOS10WithMinor(t *testing.T) {
	j := `{
  "components": [
    {"name": "Red Hat Enterprise Linux CoreOS 10.2", "version": "10.2.20260328-0", "from": "10.2.20260321-0"}
  ]
}`
	out, err := TransformJsonOutput(j, "x86_64", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"versionUrl"`) {
		t.Fatalf("expected versionUrl in output for RHCOS 10.2 component name: %s", out)
	}
	if !strings.Contains(out, "rhel-10.2") {
		t.Fatalf("expected rhel-10.2 stream in URL: %s", out)
	}
}

func TestTransformMarkDownOutputRHCOS10Fallback(t *testing.T) {
	input := `## Changes from 5.0.0-0.nightly-2026-03-01-000000
* Red Hat Enterprise Linux CoreOS upgraded from 10.2.20260301-0 to 10.2.20260315-0
`
	out, err := TransformMarkDownOutput(input, "5.0.0-0.nightly-2026-03-01-000000", "5.0.0-0.nightly-2026-03-15-000000", "x86_64", "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Red Hat Enterprise Linux CoreOS 10") {
		t.Fatalf("expected RHCOS 10 label when version starts with 10.x, got:\n%s", out)
	}
}

func TestTransformMarkDownOutputRHCOS10PeriodFormat(t *testing.T) {
	input := `## Changes from 5.0.0
* Red Hat Enterprise Linux CoreOS 10.2 upgraded from 10.2.20260328-0 to 10.2.20260321-0
`
	out, err := TransformMarkDownOutput(input, "5.0.0", "5.0.1", "x86_64", "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Red Hat Enterprise Linux CoreOS 10") {
		t.Fatalf("expected RHCOS 10 label for CoreOS 10.2 format, got:\n%s", out)
	}
	if !strings.Contains(out, "rhel-10.2") {
		t.Fatalf("expected rhel-10.2 stream in URL, got:\n%s", out)
	}
}

func TestGetRHCoSReleaseStreamRHCOS10(t *testing.T) {
	testCases := []struct {
		name     string
		version  string
		ok       bool
		expected string
	}{
		{
			name:     "RHCOS 10.2",
			version:  "10.2.20260328-0",
			ok:       true,
			expected: "prod/streams/rhel-10.2",
		},
		{
			name:     "RHCOS 10.0",
			version:  "10.0.20260101-0",
			ok:       true,
			expected: "prod/streams/rhel-10.0",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, ok := getRHCoSReleaseStream(tc.version, "")
			if ok != tc.ok {
				t.Errorf("expected ok=%v, got %v", tc.ok, ok)
			}
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}
