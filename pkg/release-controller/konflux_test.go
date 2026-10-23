package releasecontroller

import (
	"encoding/json"
	"fmt"
	"strings"
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

func TestValidateKonfluxReleasePublish(t *testing.T) {
	// The annotation key is the prefix plus the step name, and the segment of it after the
	// slash is limited to 63 characters, so the prefix eats into the step name budget.
	annotationNamePrefix := strings.SplitN(ReleaseAnnotationKonfluxReleasePrefix, "/", 2)[1]
	longestValidStepName := strings.Repeat("a", 63-len(annotationNamePrefix))

	valid := func(mutate func(*PublishKonfluxRelease)) *PublishKonfluxRelease {
		publish := &PublishKonfluxRelease{
			Namespace:   "ocp-test-tenant",
			ReleasePlan: "hypershift-operator-release",
			SecretName:  "konflux-kubeconfig",
			CLIImage:    "registry.ci.openshift.org/ocp/4.21:cli",
			Data:        json.RawMessage(`{"mapping": {"components": [{"name": "hypershift-operator", "tags": ["${RELEASE_TAG}"]}]}}`),
		}
		if mutate != nil {
			mutate(publish)
		}
		return publish
	}

	testCases := []struct {
		name        string
		stepName    string
		as          string
		publish     *PublishKonfluxRelease
		expectError string
	}{
		{
			name:     "a stable stream is valid",
			stepName: "hypershift",
			as:       ReleaseConfigModeStable,
			publish:  valid(nil),
		},
		{
			name:     "a layered stream is valid",
			stepName: "hypershift",
			as:       ReleaseConfigModeLayered,
			publish:  valid(nil),
		},
		{
			name:     "empty data is valid",
			stepName: "hypershift",
			as:       ReleaseConfigModeLayered,
			publish:  valid(func(p *PublishKonfluxRelease) { p.Data = nil }),
		},
		{
			name:     "the longest step name that fits in the annotation is valid",
			stepName: longestValidStepName,
			as:       ReleaseConfigModeLayered,
			publish:  valid(nil),
		},
		{
			name:     "a step name one character too long for the annotation is rejected",
			stepName: longestValidStepName + "a",
			as:       ReleaseConfigModeLayered,
			publish:  valid(nil),
			expectError: fmt.Sprintf("konfluxRelease publish for %s has an invalid annotation name: %s must be at most 63 characters",
				longestValidStepName+"a", annotationNamePrefix+longestValidStepName+"a"),
		},
		{
			name:        "a step name that is not a DNS1123 label is rejected",
			stepName:    "Bad_Name",
			as:          ReleaseConfigModeLayered,
			publish:     valid(nil),
			expectError: "konfluxRelease publish for Bad_Name has an invalid name: a lowercase RFC 1123 label must consist of lower case alphanumeric characters or '-', and must start and end with an alphanumeric character (e.g. 'my-name',  or '123-abc', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?')",
		},
		{
			name:        "an empty step name is rejected",
			stepName:    "",
			as:          ReleaseConfigModeLayered,
			publish:     valid(nil),
			expectError: "konfluxRelease publish for  has an invalid name: a lowercase RFC 1123 label must consist of lower case alphanumeric characters or '-', and must start and end with an alphanumeric character (e.g. 'my-name',  or '123-abc', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?')",
		},
		{
			name:     "a step name longer than a DNS1123 label is reported as an invalid name",
			stepName: strings.Repeat("a", 64),
			as:       ReleaseConfigModeLayered,
			publish:  valid(nil),
			expectError: fmt.Sprintf("konfluxRelease publish for %s has an invalid name: must be no more than 63 characters",
				strings.Repeat("a", 64)),
		},
		{
			name:        "a mode other than stable or layered is rejected",
			stepName:    "hypershift",
			as:          "Nightly",
			publish:     valid(nil),
			expectError: "konfluxRelease publish for hypershift requires 'as' to be 'Stable' or 'Layered'",
		},
		{
			name:        "a missing namespace is rejected",
			stepName:    "hypershift",
			as:          ReleaseConfigModeLayered,
			publish:     valid(func(p *PublishKonfluxRelease) { p.Namespace = "" }),
			expectError: "konfluxRelease publish for hypershift has no namespace",
		},
		{
			name:        "a missing releasePlan is rejected",
			stepName:    "hypershift",
			as:          ReleaseConfigModeLayered,
			publish:     valid(func(p *PublishKonfluxRelease) { p.ReleasePlan = "" }),
			expectError: "konfluxRelease publish for hypershift has no releasePlan",
		},
		{
			name:        "a missing secretName is rejected",
			stepName:    "hypershift",
			as:          ReleaseConfigModeLayered,
			publish:     valid(func(p *PublishKonfluxRelease) { p.SecretName = "" }),
			expectError: "konfluxRelease publish for hypershift has no secretName",
		},
		{
			name:        "a missing cliImage is rejected",
			stepName:    "hypershift",
			as:          ReleaseConfigModeLayered,
			publish:     valid(func(p *PublishKonfluxRelease) { p.CLIImage = "" }),
			expectError: "konfluxRelease publish for hypershift has no cliImage",
		},
		{
			name:        "invalid data is rejected",
			stepName:    "hypershift",
			as:          ReleaseConfigModeLayered,
			publish:     valid(func(p *PublishKonfluxRelease) { p.Data = json.RawMessage(`{"tags": ["${RELEASE-TAG}"]}`) }),
			expectError: "konfluxRelease publish for hypershift has invalid data: references unknown variables ${RELEASE-TAG}, supported variables are ${RELEASE_TAG}, ${RELEASE_NAME}, ${SNAPSHOT}",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateKonfluxReleasePublish(tc.stepName, tc.as, tc.publish)
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
