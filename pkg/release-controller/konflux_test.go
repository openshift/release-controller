package releasecontroller

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	imagev1 "github.com/openshift/api/image/v1"
)

func TestExpandKonfluxData(t *testing.T) {
	vars := map[string]string{
		KonfluxVarReleaseTag:  "4.21.0-0.nightly-2026-01-02-030405",
		KonfluxVarReleaseName: "hypershift-ci",
		KonfluxVarSnapshot:    "hypershift-abcde",
	}

	testCases := []struct {
		name     string
		data     string
		expected map[string]any
	}{
		{
			name:     "empty data expands to nothing",
			data:     "",
			expected: nil,
		},
		{
			name: "release tag is substituted into a nested mapping tag list",
			data: `{"mapping": {"components": [{"name": "hypershift-operator", "tags": ["${RELEASE_TAG}", "latest"]}]}}`,
			expected: map[string]any{
				"mapping": map[string]any{
					"components": []any{
						map[string]any{
							"name": "hypershift-operator",
							"tags": []any{"4.21.0-0.nightly-2026-01-02-030405", "latest"},
						},
					},
				},
			},
		},
		{
			name: "konflux templating is left for konflux to expand",
			data: `{"tags": ["{{ git_sha }}", "${RELEASE_TAG}-{{ timestamp }}"]}`,
			expected: map[string]any{
				"tags": []any{"{{ git_sha }}", "4.21.0-0.nightly-2026-01-02-030405-{{ timestamp }}"},
			},
		},
		{
			name: "every supported variable is substituted, including in map keys' values",
			data: `{"a": "${RELEASE_NAME}", "b": {"c": "${SNAPSHOT}"}}`,
			expected: map[string]any{
				"a": "hypershift-ci",
				"b": map[string]any{"c": "hypershift-abcde"},
			},
		},
		{
			name: "non string leaves are left alone",
			data: `{"count": 3, "enabled": true, "missing": null}`,
			expected: map[string]any{
				"count":   float64(3),
				"enabled": true,
				"missing": nil,
			},
		},
		{
			name: "a value containing JSON metacharacters cannot corrupt the document",
			data: `{"note": "${RELEASE_TAG} \"quoted\" and \\escaped"}`,
			expected: map[string]any{
				"note": `4.21.0-0.nightly-2026-01-02-030405 "quoted" and \escaped`,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := ExpandKonfluxData(json.RawMessage(tc.data), vars)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Errorf("unexpected data (-want +got):\n%s", diff)
			}
		})
	}
}

func TestValidateKonfluxData(t *testing.T) {
	testCases := []struct {
		name        string
		data        string
		expectError string
	}{
		{
			name: "empty data is valid",
			data: "",
		},
		{
			name: "supported variables are valid",
			data: `{"a": "${RELEASE_TAG}", "b": ["${RELEASE_NAME}", "${SNAPSHOT}"]}`,
		},
		{
			name: "konflux templating is not treated as a variable reference",
			data: `{"tags": ["{{ git_sha }}"]}`,
		},
		{
			name:        "a misspelled variable is rejected",
			data:        `{"tags": ["${RELEASE-TAG}"]}`,
			expectError: "references unknown variables ${RELEASE-TAG}, supported variables are ${RELEASE_TAG}, ${RELEASE_NAME}, ${SNAPSHOT}",
		},
		{
			name:        "unknown variables are reported once each, in order",
			data:        `{"a": "${FOO}", "b": "${BAR}", "c": "${FOO}"}`,
			expectError: "references unknown variables ${BAR}, ${FOO}, supported variables are ${RELEASE_TAG}, ${RELEASE_NAME}, ${SNAPSHOT}",
		},
		{
			name:        "a JSON array is rejected",
			data:        `["nope"]`,
			expectError: "must be a JSON object: json: cannot unmarshal array into Go value of type map[string]interface {}",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateKonfluxData(json.RawMessage(tc.data))
			switch {
			case err == nil && tc.expectError != "":
				t.Errorf("expected error %q, got none", tc.expectError)
			case err != nil && tc.expectError == "":
				t.Errorf("unexpected error: %v", err)
			case err != nil && err.Error() != tc.expectError:
				t.Errorf("expected error %q, got %q", tc.expectError, err.Error())
			}
		})
	}
}

func TestKonfluxSnapshotForTag(t *testing.T) {
	testCases := []struct {
		name     string
		tag      *imagev1.TagReference
		expected string
	}{
		{
			name:     "nil tag",
			tag:      nil,
			expected: "",
		},
		{
			name:     "tag without annotations",
			tag:      &imagev1.TagReference{Name: "4.21.0"},
			expected: "",
		},
		{
			name: "tag with a snapshot",
			tag: &imagev1.TagReference{Annotations: map[string]string{
				ReleaseAnnotationKonfluxSnapshot: "hypershift-abcde",
			}},
			expected: "hypershift-abcde",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if actual := KonfluxSnapshotForTag(tc.tag); actual != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, actual)
			}
		})
	}
}
