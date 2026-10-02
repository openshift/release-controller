package main

import (
	"testing"
	"time"

	imagev1 "github.com/openshift/api/image/v1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
)

func Test_previousReleaseForChangelog(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	testCases := []struct {
		name     string
		tags     []imagev1.TagReference
		tag      imagev1.TagReference
		expected string
	}{{
		name:     "the first release of a stream has nothing to compare against",
		tags:     []imagev1.TagReference{adoptedTag("0.0.1-a", releasecontroller.ReleasePhaseReady, time.Minute, now)},
		tag:      adoptedTag("0.0.1-a", releasecontroller.ReleasePhaseReady, time.Minute, now),
		expected: "",
	}, {
		name: "the release itself is never the previous release",
		tags: []imagev1.TagReference{
			adoptedTag("0.0.1-b", releasecontroller.ReleasePhaseReady, time.Minute, now),
			adoptedTag("0.0.1-a", releasecontroller.ReleasePhaseAccepted, time.Hour, now),
		},
		tag:      adoptedTag("0.0.1-b", releasecontroller.ReleasePhaseReady, time.Minute, now),
		expected: "0.0.1-a",
	}, {
		name: "the most recent accepted release is preferred",
		tags: []imagev1.TagReference{
			adoptedTag("0.0.1-c", releasecontroller.ReleasePhaseReady, time.Minute, now),
			adoptedTag("0.0.1-b", releasecontroller.ReleasePhaseReady, time.Hour, now),
			adoptedTag("0.0.1-a", releasecontroller.ReleasePhaseAccepted, 2*time.Hour, now),
		},
		tag:      adoptedTag("0.0.1-c", releasecontroller.ReleasePhaseReady, time.Minute, now),
		expected: "0.0.1-a",
	}, {
		name: "the most recent release is used when none has been accepted",
		tags: []imagev1.TagReference{
			adoptedTag("0.0.1-c", releasecontroller.ReleasePhaseReady, time.Minute, now),
			adoptedTag("0.0.1-b", releasecontroller.ReleasePhaseReady, time.Hour, now),
			adoptedTag("0.0.1-a", releasecontroller.ReleasePhaseReady, 2*time.Hour, now),
		},
		tag:      adoptedTag("0.0.1-c", releasecontroller.ReleasePhaseReady, time.Minute, now),
		expected: "0.0.1-b",
	}, {
		name: "releases created after the one being compared are ignored",
		tags: []imagev1.TagReference{
			adoptedTag("0.0.1-c", releasecontroller.ReleasePhaseAccepted, time.Minute, now),
			adoptedTag("0.0.1-b", releasecontroller.ReleasePhaseReady, time.Hour, now),
			adoptedTag("0.0.1-a", releasecontroller.ReleasePhaseAccepted, 2*time.Hour, now),
		},
		tag:      adoptedTag("0.0.1-b", releasecontroller.ReleasePhaseReady, time.Hour, now),
		expected: "0.0.1-a",
	}, {
		name: "releases that never became ready are ignored",
		tags: []imagev1.TagReference{
			adoptedTag("0.0.1-c", releasecontroller.ReleasePhaseReady, time.Minute, now),
			adoptedTag("0.0.1-b", releasecontroller.ReleasePhasePending, time.Hour, now),
			adoptedTag("0.0.1-a", releasecontroller.ReleasePhaseAccepted, 2*time.Hour, now),
		},
		tag:      adoptedTag("0.0.1-c", releasecontroller.ReleasePhaseReady, time.Minute, now),
		expected: "0.0.1-a",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			release := releaseWithImports(layeredConfig(), nil, tc.tags...)

			var actual string
			if previous := previousReleaseForChangelog(release, &tc.tag); previous != nil {
				actual = previous.Name
			}
			if actual != tc.expected {
				t.Errorf("Expected previous release %q, got %q", tc.expected, actual)
			}
		})
	}
}
