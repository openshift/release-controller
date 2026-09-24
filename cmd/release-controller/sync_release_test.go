package main

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation"
)

func TestReleaseMirrorJobName(t *testing.T) {
	tests := []struct {
		name    string
		tagName string
		want    string
	}{
		{
			name:    "ShortTagNameUnchanged",
			tagName: "4.11.0-0.nightly-2022-02-09-091559",
			want:    "4.11.0-0.nightly-2022-02-09-091559-alternate-mirror",
		},
		{
			name:    "MaxLengthTagNameUnchanged",
			tagName: "4.11.0-0.nightly-art12345-2022-02-09-0915",
			want:    "4.11.0-0.nightly-art12345-2022-02-09-0915-alternate-mirror",
		},
		{
			name:    "LongTagNameTruncatedAndHashed",
			tagName: "5.0.0-0.nightly-art23398-ppc64le-2026-09-15-150326",
			want:    "5.0.0-0.nightly-art23398-ppc64le-2026-09-15-150326-alte-s6r39ib",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := releaseMirrorJobName(tc.tagName)
			if got != tc.want {
				t.Errorf("expected job name %q, got %q", tc.want, got)
			}
			if len(got) > maxJobNameLength {
				t.Errorf("expected job name of no more than %d characters, got %d: %q", maxJobNameLength, len(got), got)
			}
			if errs := validation.IsValidLabelValue(got); len(errs) > 0 {
				t.Errorf("job name %q is not a valid label value: %s", got, strings.Join(errs, ", "))
			}
		})
	}
}

func TestSafeJobNameUniqueness(t *testing.T) {
	// Tags that only differ beyond the truncation point must not collide
	first := releaseMirrorJobName("5.0.0-0.nightly-art23398-ppc64le-2026-09-15-150326")
	second := releaseMirrorJobName("5.0.0-0.nightly-art23398-ppc64le-2026-09-15-150327")
	if first == second {
		t.Errorf("expected unique job names for distinct tags, got %q for both", first)
	}
}
