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
	input := "* Red Hat Enterprise Linux CoreOS 10 10.0 upgraded from 10.0.20260101-0 to 10.0.20260201-0\n"
	m := reMdRHCoS10Diff.FindStringSubmatch(input)
	if m == nil {
		t.Fatal("expected match for RHEL 10 upgrade line")
	}
	if m[1] != "10.0.20260101-0" || m[3] != "10.0.20260201-0" {
		t.Fatalf("unexpected submatches: %v", m)
	}
}

func TestTransformMarkDownOutputDualRHCOSLines(t *testing.T) {
	input := `## Changes from 4.20.0
* Red Hat Enterprise Linux CoreOS 9.8 upgraded from 9.8.20260101-0 to 9.8.20260201-0
* Red Hat Enterprise Linux CoreOS 10 10.0 upgraded from 10.0.20260101-0 to 10.0.20260201-0
`
	out, err := TransformMarkDownOutput(input, "4.20.0", "4.21.0", "x86_64", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "coreos-base-alert") < 2 {
		t.Fatalf("expected two CoreOS base layer infoboxes, got:\n%s", out)
	}
}

func TestTransformMarkDownOutputHeaders(t *testing.T) {
	testCases := []struct {
		name            string
		markdown        string
		fromTag         string
		toTag           string
		expected        string
		expectedMissing []string
	}{{
		// A release payload names itself after the version in its metadata,
		// which is the tag the release controller knows it by.
		name: "release payload",
		markdown: `# 4.21.0

Created: 2026-09-30 00:00:00 +0000 UTC

Image Digest: ` + "`sha256:abc`" + `

## Changes from 4.20.0

### Components
`,
		fromTag:         "4.20.0",
		toTag:           "4.21.0",
		expected:        "## Changes from [4.20.0](/releasetag/4.20.0)\n",
		expectedMissing: []string{"# 4.21.0"},
	}, {
		// An image that is not a release payload has no metadata to name
		// itself from, so oc falls back to the name of the image stream in its
		// image-references file, here the name of the component.
		name: "layered release named after its image references",
		markdown: `# hypershift

Created: 2026-09-30 00:00:00 +0000 UTC

Image Digest: ` + "`sha256:abc`" + `

## Changes from hypershift

### Components
`,
		fromTag:         "0.0.1-a",
		toTag:           "0.0.1-b",
		expected:        "## Changes from [0.0.1-a](/releasetag/0.0.1-a)\n",
		expectedMissing: []string{"# hypershift", "Changes from hypershift"},
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := TransformMarkDownOutput(tc.markdown, tc.fromTag, tc.toTag, "x86_64", "")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(out, tc.expected) {
				t.Errorf("Expected the changelog to start with %q, got:\n%s", tc.expected, out)
			}
			for _, missing := range tc.expectedMissing {
				if strings.Contains(out, missing) {
					t.Errorf("Expected the changelog not to contain %q, got:\n%s", missing, out)
				}
			}
		})
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
