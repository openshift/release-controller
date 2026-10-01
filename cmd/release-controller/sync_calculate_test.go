package main

import (
	"fmt"
	"slices"
	"testing"
	"time"

	imagev1 "github.com/openshift/api/image/v1"
	imagefake "github.com/openshift/client-go/image/clientset/versioned/fake"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const syncTestStream = "hypershift"

// adoptedTag is a tag the controller has already taken ownership of, in the
// given phase and created the given duration before "now".
func adoptedTag(name, phase string, age time.Duration, now time.Time) imagev1.TagReference {
	return imagev1.TagReference{
		Name: name,
		Annotations: map[string]string{
			releasecontroller.ReleaseAnnotationName:              syncTestStream,
			releasecontroller.ReleaseAnnotationSource:            "ocp/" + syncTestStream,
			releasecontroller.ReleaseAnnotationPhase:             phase,
			releasecontroller.ReleaseAnnotationCreationTimestamp: now.Add(-age).Format(time.RFC3339),
		},
		Reference: true,
		From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/example/payload@sha256:" + name},
	}
}

// unadoptedTag is a tag as the build system pushes it, before the controller
// has annotated it.
func unadoptedTag(name string) imagev1.TagReference {
	return imagev1.TagReference{
		Name:      name,
		Reference: true,
		From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/example/payload@sha256:" + name},
	}
}

// skippedTag is a tag the controller has already recorded as superseded.
func skippedTag(name, supersededBy string) imagev1.TagReference {
	tag := unadoptedTag(name)
	tag.Annotations = map[string]string{releasecontroller.ReleaseAnnotationSkipped: supersededBy}
	return tag
}

// releaseWithImports builds a release whose stream reports every tag as
// imported, oldest name first, one minute apart. importedAt overrides the
// import time of individual tags, and a tag named there with the zero time is
// treated as not yet imported. Source and Target are the same stream, as they
// are for both the stable and the layered modes.
func releaseWithImports(config *releasecontroller.ReleaseConfig, importedAt map[string]time.Time, tags ...imagev1.TagReference) *releasecontroller.Release {
	base := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		names = append(names, tag.Name)
	}
	slices.Sort(names)

	var status []imagev1.NamedTagEventList
	for i, name := range names {
		created := base.Add(time.Duration(i) * time.Minute)
		if override, ok := importedAt[name]; ok {
			if override.IsZero() {
				// No items at all: the tag exists in the spec but has not imported.
				status = append(status, imagev1.NamedTagEventList{Tag: name})
				continue
			}
			created = override
		}
		status = append(status, imagev1.NamedTagEventList{
			Tag:   name,
			Items: []imagev1.TagEvent{{Created: metav1.NewTime(created), Image: "sha256:" + name}},
		})
	}

	is := &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ocp", Name: syncTestStream},
		Spec:       imagev1.ImageStreamSpec{Tags: tags},
		Status:     imagev1.ImageStreamStatus{Tags: status},
	}
	return &releasecontroller.Release{Source: is, Target: is, Config: config}
}

// layeredConfig is a layered release with neither gate configured, so it never
// restricts how many cycles run at once.
func layeredConfig() *releasecontroller.ReleaseConfig {
	return &releasecontroller.ReleaseConfig{
		Name: syncTestStream,
		As:   releasecontroller.ReleaseConfigModeLayered,
	}
}

// maxUnready configures the unready gate, which is one of the two settings that
// turn on single-cycle adoption.
func maxUnready(config *releasecontroller.ReleaseConfig, n int) *releasecontroller.ReleaseConfig {
	config.MaxUnreadyReleases = n
	return config
}

// minInterval configures the creation interval gate, the other setting that
// turns on single-cycle adoption.
func minInterval(config *releasecontroller.ReleaseConfig, seconds int) *releasecontroller.ReleaseConfig {
	config.MinCreationIntervalSeconds = seconds
	return config
}

func TestCalculateSyncActionsLayered(t *testing.T) {
	now := time.Now()

	testCases := []struct {
		name string
		// config is mutated from the layered default by the test case.
		config           *releasecontroller.ReleaseConfig
		tags             []imagev1.TagReference
		importedAt       map[string]time.Time
		expectedAdopt    []string
		expectedSkip     []string
		expectedPending  []string
		expectedRemove   []string
		expectQueueAfter bool
	}{
		// Whether single-cycle adoption applies at all is itself gated: it is the
		// two limits that make a layered release one-cycle-at-a-time, so with
		// neither configured there is nothing to serialise and every tag is taken.
		{
			name:          "every tag is adopted when neither gate is configured",
			config:        layeredConfig(),
			tags:          []imagev1.TagReference{unadoptedTag("0.0.1-b"), unadoptedTag("0.0.1-d"), unadoptedTag("0.0.1-c")},
			expectedAdopt: []string{"0.0.1-b", "0.0.1-c", "0.0.1-d"},
			expectedSkip:  nil,
		},
		{
			name:          "a single unannotated tag is adopted when nothing gates it",
			config:        layeredConfig(),
			tags:          []imagev1.TagReference{unadoptedTag("0.0.1-b")},
			expectedAdopt: []string{"0.0.1-b"},
		},
		{
			name:          "the unready gate alone turns on single-cycle adoption",
			config:        maxUnready(layeredConfig(), 2),
			tags:          []imagev1.TagReference{unadoptedTag("0.0.1-b"), unadoptedTag("0.0.1-d"), unadoptedTag("0.0.1-c")},
			expectedAdopt: []string{"0.0.1-d"},
			expectedSkip:  []string{"0.0.1-b", "0.0.1-c"},
		},
		{
			name:   "the creation interval gate alone turns on single-cycle adoption",
			config: minInterval(layeredConfig(), 3600),
			// No adopted tag, so the interval itself is not currently delaying
			// anything: the setting's mere presence is what enables selection.
			tags:          []imagev1.TagReference{unadoptedTag("0.0.1-b"), unadoptedTag("0.0.1-d"), unadoptedTag("0.0.1-c")},
			expectedAdopt: []string{"0.0.1-d"},
			expectedSkip:  []string{"0.0.1-b", "0.0.1-c"},
		},
		{
			name:   "import time wins over tag name when picking the most recent tag",
			config: maxUnready(layeredConfig(), 2),
			tags:   []imagev1.TagReference{unadoptedTag("0.0.1-b"), unadoptedTag("0.0.1-c"), unadoptedTag("0.0.1-d")},
			// 0.0.1-c imported last despite sorting before 0.0.1-d by name.
			importedAt:    map[string]time.Time{"0.0.1-c": time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC)},
			expectedAdopt: []string{"0.0.1-c"},
			expectedSkip:  []string{"0.0.1-b", "0.0.1-d"},
		},
		{
			name: "tags already marked skipped are not offered for adoption again",
			// The annotation filter runs before selection, so it holds whether or
			// not a gate is configured.
			config:        layeredConfig(),
			tags:          []imagev1.TagReference{skippedTag("0.0.1-b", "0.0.1-d"), skippedTag("0.0.1-c", "0.0.1-d"), unadoptedTag("0.0.1-d")},
			expectedAdopt: []string{"0.0.1-d"},
			expectedSkip:  nil,
		},
		{
			name:   "a tag that has not imported yet is neither adopted nor skipped",
			config: maxUnready(layeredConfig(), 2),
			tags:   []imagev1.TagReference{unadoptedTag("0.0.1-b"), unadoptedTag("0.0.1-d")},
			// 0.0.1-d is the newest by name, but has no imported image, so its age is
			// unknown and it must not be used to supersede 0.0.1-b.
			importedAt:    map[string]time.Time{"0.0.1-d": {}},
			expectedAdopt: []string{"0.0.1-b"},
			expectedSkip:  nil,
		},
		{
			name:          "nothing is adopted when no candidate has imported",
			config:        maxUnready(layeredConfig(), 2),
			tags:          []imagev1.TagReference{unadoptedTag("0.0.1-b"), unadoptedTag("0.0.1-d")},
			importedAt:    map[string]time.Time{"0.0.1-b": {}, "0.0.1-d": {}},
			expectedAdopt: nil,
			expectedSkip:  nil,
		},
		{
			name:            "an adopted pending tag is reported as pending",
			config:          layeredConfig(),
			tags:            []imagev1.TagReference{adoptedTag("0.0.1-b", releasecontroller.ReleasePhasePending, time.Hour, now)},
			expectedPending: []string{"0.0.1-b"},
		},

		// Gating of adoption itself: with a gate tripped, no new cycle may start.
		{
			name:   "adoption is held back at the max unready releases limit",
			config: maxUnready(layeredConfig(), 1),
			tags: []imagev1.TagReference{
				adoptedTag("0.0.1-b", releasecontroller.ReleasePhasePending, time.Hour, now),
				unadoptedTag("0.0.1-c"),
			},
			expectedAdopt:   nil,
			expectedPending: []string{"0.0.1-b"},
		},
		{
			name:   "adoption proceeds when below the max unready releases limit",
			config: maxUnready(layeredConfig(), 2),
			tags: []imagev1.TagReference{
				adoptedTag("0.0.1-b", releasecontroller.ReleasePhasePending, time.Hour, now),
				unadoptedTag("0.0.1-c"),
			},
			expectedAdopt:   []string{"0.0.1-c"},
			expectedPending: []string{"0.0.1-b"},
		},
		{
			name:   "accepted tags do not count towards the unready limit",
			config: maxUnready(layeredConfig(), 1),
			tags: []imagev1.TagReference{
				adoptedTag("0.0.1-a", releasecontroller.ReleasePhaseAccepted, 2*time.Hour, now),
				unadoptedTag("0.0.1-c"),
			},
			expectedAdopt: []string{"0.0.1-c"},
		},
		{
			name:   "adoption is held back inside the minimum creation interval",
			config: minInterval(layeredConfig(), 3600),
			tags: []imagev1.TagReference{
				adoptedTag("0.0.1-a", releasecontroller.ReleasePhaseAccepted, time.Minute, now),
				unadoptedTag("0.0.1-c"),
			},
			expectedAdopt:    nil,
			expectQueueAfter: true,
		},
		{
			name:   "adoption proceeds once the minimum creation interval has elapsed",
			config: minInterval(layeredConfig(), 3600),
			tags: []imagev1.TagReference{
				adoptedTag("0.0.1-a", releasecontroller.ReleasePhaseAccepted, 2*time.Hour, now),
				unadoptedTag("0.0.1-c"),
			},
			expectedAdopt: []string{"0.0.1-c"},
		},
		{
			name:   "both gates together hold adoption back",
			config: minInterval(maxUnready(layeredConfig(), 1), 3600),
			tags: []imagev1.TagReference{
				adoptedTag("0.0.1-a", releasecontroller.ReleasePhasePending, time.Minute, now),
				unadoptedTag("0.0.1-c"),
			},
			expectedAdopt:    nil,
			expectedPending:  []string{"0.0.1-a"},
			expectQueueAfter: true,
		},

		// Selection runs before the gates, so by the time a gate trips it has
		// already nominated a tag to adopt and declared the others superseded.
		// Both gates have to discard that nomination together: a tag may only be
		// marked skipped in favour of a release that actually starts. Each of
		// these cases carries two unadopted tags, so selection does produce a
		// skip list that the gate then has to clear -- with one tag the list
		// would be empty whether the gate cleared it or not, and the regression
		// would go unnoticed.
		{
			name:   "a backlog is not marked skipped at the max unready releases limit",
			config: maxUnready(layeredConfig(), 1),
			tags: []imagev1.TagReference{
				adoptedTag("0.0.1-a", releasecontroller.ReleasePhasePending, time.Hour, now),
				unadoptedTag("0.0.1-c"),
				unadoptedTag("0.0.1-d"),
			},
			expectedAdopt:   nil,
			expectedSkip:    nil,
			expectedPending: []string{"0.0.1-a"},
		},
		{
			name:   "a backlog is not marked skipped inside the minimum creation interval",
			config: minInterval(layeredConfig(), 3600),
			tags: []imagev1.TagReference{
				adoptedTag("0.0.1-a", releasecontroller.ReleasePhaseAccepted, time.Minute, now),
				unadoptedTag("0.0.1-c"),
				unadoptedTag("0.0.1-d"),
			},
			expectedAdopt:    nil,
			expectedSkip:     nil,
			expectQueueAfter: true,
		},
		{
			name:   "excess rejected tags are garbage collected",
			config: layeredConfig(),
			tags: func() []imagev1.TagReference {
				var tags []imagev1.TagReference
				for i := range 8 {
					tags = append(tags, adoptedTag(fmt.Sprintf("0.0.1-r%d", i), releasecontroller.ReleasePhaseRejected, time.Duration(i)*time.Hour, now))
				}
				return tags
			}(),
			// Five of each phase are kept, oldest beyond that are removed.
			expectedRemove: []string{"0.0.1-r5", "0.0.1-r6", "0.0.1-r7"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			release := releaseWithImports(tc.config, tc.importedAt, tc.tags...)

			adopt, skip, pending, remove, hasNewImages, inputImageHash, queueAfter := calculateSyncActions(release, now)

			assertTagNames(t, "adoptTags", adopt, tc.expectedAdopt)
			assertTagNames(t, "skipTags", skip, tc.expectedSkip)
			assertTagNames(t, "pendingTags", pending, tc.expectedPending)
			assertTagNames(t, "removeTags", remove, tc.expectedRemove)

			// The controller must never synthesise a tag for a layered release: the
			// build system owns the contents of the stream. sync only calls
			// createReleaseTag when hasNewImages is set and nothing is pending.
			if hasNewImages {
				t.Error("hasNewImages is set, which would make sync create a release tag with no image behind it")
			}
			if len(inputImageHash) > 0 {
				t.Errorf("inputImageHash: expected empty for a layered release, got %q", inputImageHash)
			}
			if tc.expectQueueAfter != (queueAfter > 0) {
				t.Errorf("queueAfter: expected >0 to be %v, got %s", tc.expectQueueAfter, queueAfter)
			}
		})
	}
}

// TestCalculateSyncActionsStableAdoptsEveryTag guards the blast radius of the
// layered "adopt only the newest tag" rule. Stable adoption is not a release
// cycle competing for the same slot: each tag is a distinct published version,
// and every one of them has to be taken over. Superseding all but the newest
// would silently drop released versions on the floor.
func TestCalculateSyncActionsStableAdoptsEveryTag(t *testing.T) {
	config := &releasecontroller.ReleaseConfig{
		Name: syncTestStream,
		As:   releasecontroller.ReleaseConfigModeStable,
		// Gating that holds a layered release back must not reach stable adoption.
		MaxUnreadyReleases:         1,
		MinCreationIntervalSeconds: 3600,
	}
	release := releaseWithImports(config, nil,
		unadoptedTag("4.17.3"), unadoptedTag("4.17.4"), unadoptedTag("4.17.5"))

	adopt, skip, _, _, _, _, _ := calculateSyncActions(release, time.Now())

	assertTagNames(t, "adoptTags", adopt, []string{"4.17.3", "4.17.4", "4.17.5"})
	assertTagNames(t, "skipTags", skip, nil)
}

// TestCalculateSyncActionsStableDoesNotGarbageCollect pins down the one place
// the stable and layered arms deliberately differ. A layered release prunes its
// old failures and rejections like an integration stream does; a stable stream
// holds published versions, which are never pruned.
func TestCalculateSyncActionsStableDoesNotGarbageCollect(t *testing.T) {
	now := time.Now()
	config := &releasecontroller.ReleaseConfig{
		Name: syncTestStream,
		As:   releasecontroller.ReleaseConfigModeStable,
	}
	var tags []imagev1.TagReference
	for i := range 8 {
		tags = append(tags, adoptedTag(fmt.Sprintf("4.17.%d", i), releasecontroller.ReleasePhaseRejected, time.Duration(i)*time.Hour, now))
	}
	release := releaseWithImports(config, nil, tags...)

	_, _, _, remove, hasNewImages, inputImageHash, _ := calculateSyncActions(release, now)

	assertTagNames(t, "removeTags", remove, nil)
	if hasNewImages {
		t.Error("hasNewImages is set, which would make sync create a release tag in a stable stream")
	}
	if len(inputImageHash) > 0 {
		t.Errorf("inputImageHash: expected empty for a stable release, got %q", inputImageHash)
	}
}

func TestSyncSkipped(t *testing.T) {
	testCases := []struct {
		name          string
		tags          []imagev1.TagReference
		skip          []string
		supersededBy  string
		expectChanged bool
		// expectedSkipAnnotations maps a tag name to the value its skipped
		// annotation must hold afterwards. Tags absent from the map must carry
		// no skipped annotation at all.
		expectedSkipAnnotations map[string]string
	}{
		{
			name:                    "annotates every superseded tag with the release that took its place",
			tags:                    []imagev1.TagReference{unadoptedTag("0.0.1-b"), unadoptedTag("0.0.1-c"), unadoptedTag("0.0.1-d")},
			skip:                    []string{"0.0.1-b", "0.0.1-c"},
			supersededBy:            "0.0.1-d",
			expectChanged:           true,
			expectedSkipAnnotations: map[string]string{"0.0.1-b": "0.0.1-d", "0.0.1-c": "0.0.1-d"},
		},
		{
			name:                    "reports no change when every tag is already marked",
			tags:                    []imagev1.TagReference{skippedTag("0.0.1-b", "0.0.1-d"), unadoptedTag("0.0.1-d")},
			skip:                    []string{"0.0.1-b"},
			supersededBy:            "0.0.1-d",
			expectChanged:           false,
			expectedSkipAnnotations: map[string]string{"0.0.1-b": "0.0.1-d"},
		},
		{
			name:                    "keeps the original supersedor when a tag is already marked",
			tags:                    []imagev1.TagReference{skippedTag("0.0.1-b", "0.0.1-c"), unadoptedTag("0.0.1-d")},
			skip:                    []string{"0.0.1-b"},
			supersededBy:            "0.0.1-d",
			expectChanged:           false,
			expectedSkipAnnotations: map[string]string{"0.0.1-b": "0.0.1-c"},
		},
		{
			name:          "tolerates a tag that has been deleted from the stream",
			tags:          []imagev1.TagReference{unadoptedTag("0.0.1-d")},
			skip:          []string{"0.0.1-b"},
			supersededBy:  "0.0.1-d",
			expectChanged: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			release := releaseWithImports(layeredConfig(), nil, tc.tags...)
			c := &Controller{imageClient: imagefake.NewSimpleClientset(release.Target).ImageV1()}

			var skipTags []*imagev1.TagReference
			for _, name := range tc.skip {
				// The tag may legitimately be absent, which is the point of one case.
				if tag := releasecontroller.FindTagReference(release.Target, name); tag != nil {
					skipTags = append(skipTags, tag)
				} else {
					skipTags = append(skipTags, &imagev1.TagReference{Name: name})
				}
			}

			changed, err := c.syncSkipped(release, skipTags, tc.supersededBy)
			if err != nil {
				t.Fatalf("syncSkipped: unexpected error: %v", err)
			}
			if changed != tc.expectChanged {
				t.Errorf("changed: expected %v, got %v", tc.expectChanged, changed)
			}

			for i := range release.Target.Spec.Tags {
				tag := &release.Target.Spec.Tags[i]
				expected, wantSkipped := tc.expectedSkipAnnotations[tag.Name]
				actual := tag.Annotations[releasecontroller.ReleaseAnnotationSkipped]
				if wantSkipped && actual != expected {
					t.Errorf("tag %s: expected skipped annotation %q, got %q", tag.Name, expected, actual)
				}
				if !wantSkipped && len(actual) > 0 {
					t.Errorf("tag %s: expected no skipped annotation, got %q", tag.Name, actual)
				}
				// A skipped tag stays outside the release lifecycle entirely.
				if wantSkipped {
					if phase := tag.Annotations[releasecontroller.ReleaseAnnotationPhase]; len(phase) > 0 {
						t.Errorf("tag %s: skipped tag was given phase %q", tag.Name, phase)
					}
					if source := tag.Annotations[releasecontroller.ReleaseAnnotationSource]; len(source) > 0 {
						t.Errorf("tag %s: skipped tag was adopted (source %q)", tag.Name, source)
					}
				}
			}

			// A layered release reads and writes the same stream, so an update has to
			// move the source along with the target.
			if release.Source != release.Target {
				t.Error("Source and Target diverged, later syncs would read a stale stream")
			}
		})
	}
}

func assertTagNames(t *testing.T, field string, actual []*imagev1.TagReference, expected []string) {
	t.Helper()
	names := releasecontroller.TagNames(actual)
	slices.Sort(names)
	want := slices.Clone(expected)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Errorf("%s: expected %v, got %v", field, expected, names)
	}
}
