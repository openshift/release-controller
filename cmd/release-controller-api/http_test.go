package main

import (
	"testing"

	"github.com/blang/semver"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
)

var reference4Stable = releasecontroller.StableRelease{
	Versions: []releasecontroller.SemanticVersion{{
		Version: &semver.Version{Major: 4, Minor: 13, Patch: 0, Pre: []semver.PRVersion{{VersionStr: "rc", IsNum: false}, {VersionNum: 0, IsNum: true}}},
	}, {
		Version: &semver.Version{Major: 4, Minor: 12, Patch: 2},
	}, {
		Version: &semver.Version{Major: 4, Minor: 12, Patch: 1},
	}, {
		Version: &semver.Version{Major: 4, Minor: 12, Patch: 0},
	}, {
		Version: &semver.Version{Major: 4, Minor: 11, Patch: 2},
	}, {
		Version: &semver.Version{Major: 4, Minor: 11, Patch: 1},
	}, {
		Version: &semver.Version{Major: 4, Minor: 11, Patch: 0},
	}},
}

var reference4Preview = releasecontroller.StableRelease{
	Versions: []releasecontroller.SemanticVersion{{
		Version: &semver.Version{Major: 4, Minor: 13, Patch: 0, Pre: []semver.PRVersion{{VersionStr: "ec", IsNum: false}, {VersionNum: 0, IsNum: true}}},
	}, {
		Version: &semver.Version{Major: 4, Minor: 12, Patch: 0, Pre: []semver.PRVersion{{VersionStr: "ec", IsNum: false}, {VersionNum: 2, IsNum: true}}},
	}, {
		Version: &semver.Version{Major: 4, Minor: 12, Patch: 0, Pre: []semver.PRVersion{{VersionStr: "ec", IsNum: false}, {VersionNum: 1, IsNum: true}}},
	}, {
		Version: &semver.Version{Major: 4, Minor: 12, Patch: 0, Pre: []semver.PRVersion{{VersionStr: "ec", IsNum: false}, {VersionNum: 0, IsNum: true}}},
	}},
}

func TestNextMinor(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		tagInfo     releaseTagInfo
		expectedTag string
	}{{
		name: "References with ECs first",
		tagInfo: releaseTagInfo{
			Tag: "4.13.0-rc.0",
			Info: &ReleaseStreamTag{
				Stable: &releasecontroller.StableReferences{
					Releases: releasecontroller.StableReleases{reference4Preview, reference4Stable},
				},
			},
		},
		expectedTag: "4.12.2",
	}, {
		name: "References with stable first",
		tagInfo: releaseTagInfo{
			Tag: "4.13.0-rc.0",
			Info: &ReleaseStreamTag{
				Stable: &releasecontroller.StableReferences{
					Releases: releasecontroller.StableReleases{reference4Stable, reference4Preview},
				},
			},
		},
		expectedTag: "4.12.2",
	}, {
		name: "Previous for 4.12.3",
		tagInfo: releaseTagInfo{
			Tag: "4.12.3",
			Info: &ReleaseStreamTag{
				Stable: &releasecontroller.StableReferences{
					Releases: releasecontroller.StableReleases{reference4Preview, reference4Stable},
				},
			},
		},
		expectedTag: "4.11.2",
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualTag := previousMinor(&tc.tagInfo)
			if actualTag != tc.expectedTag {
				t.Errorf("Expected tag %s, got %s", tc.expectedTag, actualTag)
			}
		})
	}
}
