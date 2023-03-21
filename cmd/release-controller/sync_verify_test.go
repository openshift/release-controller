package main

import (
	"testing"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	"github.com/blang/semver"
	imagev1 "github.com/openshift/api/image/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var reference4Stable = releasecontroller.StableRelease{
	Release: &releasecontroller.Release{
		Config: &releasecontroller.ReleaseConfig{Name: "stableTestConfig"},
		Target: &imagev1.ImageStream{
			Status: imagev1.ImageStreamStatus{PublicDockerImageRepository: "dockerRepo"},
			Spec: imagev1.ImageStreamSpec{
				Tags: []imagev1.TagReference{{
					Name: "4.13.0-rc.0",
					Annotations: map[string]string{
						releasecontroller.ReleaseAnnotationSource: "testNamespace/stableTestSourceName",
						releasecontroller.ReleaseAnnotationName:   "stableTestConfig",
						releasecontroller.ReleaseAnnotationPhase:  releasecontroller.ReleasePhaseAccepted,
					},
				}, {
					Name: "4.12.2",
					Annotations: map[string]string{
						releasecontroller.ReleaseAnnotationSource: "testNamespace/stableTestSourceName",
						releasecontroller.ReleaseAnnotationName:   "stableTestConfig",
						releasecontroller.ReleaseAnnotationPhase:  releasecontroller.ReleasePhaseAccepted,
					},
				}, {
					Name: "4.12.1",
					Annotations: map[string]string{
						releasecontroller.ReleaseAnnotationSource: "testNamespace/stableTestSourceName",
						releasecontroller.ReleaseAnnotationName:   "stableTestConfig",
						releasecontroller.ReleaseAnnotationPhase:  releasecontroller.ReleasePhaseAccepted,
					},
				}, {
					Name: "4.12.0",
					Annotations: map[string]string{
						releasecontroller.ReleaseAnnotationSource: "testNamespace/stableTestSourceName",
						releasecontroller.ReleaseAnnotationName:   "stableTestConfig",
						releasecontroller.ReleaseAnnotationPhase:  releasecontroller.ReleasePhaseAccepted,
					},
				}},
			},
		},
		Source: &imagev1.ImageStream{ObjectMeta: metav1.ObjectMeta{
			Namespace: "testNamespace",
			Name:      "stableTestSourceName",
		}},
	},
	Versions: []releasecontroller.SemanticVersion{{
		Version: &semver.Version{Major: 4, Minor: 13, Patch: 0, Pre: []semver.PRVersion{{VersionStr: "rc", IsNum: false}, {VersionNum: 0, IsNum: true}}},
	}, {
		Version: &semver.Version{Major: 4, Minor: 12, Patch: 2},
	}, {
		Version: &semver.Version{Major: 4, Minor: 12, Patch: 1},
	}, {
		Version: &semver.Version{Major: 4, Minor: 12, Patch: 0},
	}},
}

var reference4Preview = releasecontroller.StableRelease{
	Release: &releasecontroller.Release{
		Config: &releasecontroller.ReleaseConfig{Name: "previewTestConfig"},
		Target: &imagev1.ImageStream{
			Status: imagev1.ImageStreamStatus{PublicDockerImageRepository: "dockerRepo"},
			Spec: imagev1.ImageStreamSpec{
				Tags: []imagev1.TagReference{{
					Name: "4.13.0-ec.0",
					Annotations: map[string]string{
						releasecontroller.ReleaseAnnotationSource: "testNamespace/previewTestSourceName",
						releasecontroller.ReleaseAnnotationName:   "previewTestConfig",
						releasecontroller.ReleaseAnnotationPhase:  releasecontroller.ReleasePhaseAccepted,
					},
				}, {
					Name: "4.12.0-ec.2",
					Annotations: map[string]string{
						releasecontroller.ReleaseAnnotationSource: "testNamespace/previewTestSourceName",
						releasecontroller.ReleaseAnnotationName:   "previewTestConfig",
						releasecontroller.ReleaseAnnotationPhase:  releasecontroller.ReleasePhaseAccepted,
					},
				}, {
					Name: "4.12.0-ec.1",
					Annotations: map[string]string{
						releasecontroller.ReleaseAnnotationSource: "testNamespace/previewTestSourceName",
						releasecontroller.ReleaseAnnotationName:   "previewTestConfig",
						releasecontroller.ReleaseAnnotationPhase:  releasecontroller.ReleasePhaseAccepted,
					},
				}, {
					Name: "4.12.0-ec.0",
					Annotations: map[string]string{
						releasecontroller.ReleaseAnnotationSource: "testNamespace/previewTestSourceName",
						releasecontroller.ReleaseAnnotationName:   "previewTestConfig",
						releasecontroller.ReleaseAnnotationPhase:  releasecontroller.ReleasePhaseAccepted,
					},
				}},
			},
		},
		Source: &imagev1.ImageStream{ObjectMeta: metav1.ObjectMeta{
			Namespace: "testNamespace",
			Name:      "previewTestSourceName",
		}},
	},
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

func TestFindLatestStableForVersion(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name             string
		references       *releasecontroller.StableReferences
		version          semver.Version
		expectedTag      string
		expectedPullSpec string
	}{{
		name:             "References with ECs first",
		references:       &releasecontroller.StableReferences{Releases: releasecontroller.StableReleases{reference4Preview, reference4Stable}},
		version:          semver.Version{Major: 4, Minor: 12},
		expectedTag:      "4.12.2",
		expectedPullSpec: "dockerRepo:4.12.2",
	}, {
		name:             "References with stable first",
		references:       &releasecontroller.StableReferences{Releases: releasecontroller.StableReleases{reference4Stable, reference4Preview}},
		version:          semver.Version{Major: 4, Minor: 12},
		expectedTag:      "4.12.2",
		expectedPullSpec: "dockerRepo:4.12.2",
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualTag, actualPullSpec := findLatestStableForVersion(tc.references, tc.version)
			if actualTag != tc.expectedTag {
				t.Errorf("Expected tag %s, got %s", tc.expectedTag, actualTag)
			}
			if actualPullSpec != tc.expectedPullSpec {
				t.Errorf("Expected pullspec %s, got %s", tc.expectedPullSpec, actualPullSpec)
			}
		})
	}
}
