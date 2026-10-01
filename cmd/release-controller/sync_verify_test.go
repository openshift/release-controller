package main

import (
	"encoding/json"
	"strings"
	"testing"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	"github.com/blang/semver"
	imagev1 "github.com/openshift/api/image/v1"
	imagelisters "github.com/openshift/client-go/image/listers/image/v1"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	payloadlisters "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
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

func TestPreviousMinorSourceOverride(t *testing.T) {
	policy := &releasecontroller.PreviousMinorOverride{TargetVersion: "5.0", Stream: "4-scos-stable", Version: "4.22"}
	armPolicy := &releasecontroller.PreviousMinorOverride{TargetVersion: "5.0", Stream: "4-scos-stable-arm64", Version: "4.22"}
	for _, tc := range []struct {
		name, target, namespace, stream, expected, errorText                       string
		policy                                                                     *releasecontroller.PreviousMinorOverride
		missing, noEligible, rejectedPayload, missingPullSpec, nonStable, periodic bool
	}{
		{name: "first 5.0 payload", target: "5.0.0-okd-scos.0", policy: policy, expected: "4.22.0-okd-scos.10"},
		{name: "later 5.0 payload", target: "5.0.0-okd-scos.1", policy: policy, expected: "4.22.0-okd-scos.10"},
		{name: "5.0 patch", target: "5.0.3-okd-scos.2", policy: policy, expected: "4.22.0-okd-scos.10"},
		{name: "5.0 prerelease", target: "5.0.0-okd-scos.ec.2", policy: policy, expected: "4.22.0-okd-scos.10"},
		{name: "5.1 normal previous minor", target: "5.1.1-okd-scos.1", policy: policy, expected: "5.0.2-okd-scos.1"},
		{name: "5.1 does not need override source", target: "5.1.0-okd-scos.0", policy: policy, missing: true, expected: "5.0.2-okd-scos.1"},
		{name: "5.1 without policy unchanged", target: "5.1.0-okd-scos.0", expected: "5.0.2-okd-scos.1"},
		{name: "unrelated 5.0 stream without opt in", target: "5.0.0", stream: "5-stable", expected: ""},
		{name: "unrelated 4.0 stream", target: "4.0.0", policy: policy, expected: ""},
		{name: "missing stream", target: "5.0.1", policy: policy, missing: true, errorText: "no accepted 4.22 source"},
		{name: "missing eligible tags", target: "5.0.1", policy: policy, noEligible: true, errorText: "no accepted 4.22 source"},
		{name: "source must be stable", target: "5.0.1", policy: policy, nonStable: true, errorText: "no accepted 4.22 source"},
		{name: "source must have a pull spec", target: "5.0.1", policy: policy, missingPullSpec: true, errorText: "no accepted 4.22 source"},
		{name: "releasepayload phases take precedence", target: "5.0.1", policy: policy, rejectedPayload: true, expected: "4.22.0-okd-scos.9"},
		{name: "invalid target is an error", target: "not-semver", policy: policy, errorText: "cannot match"},
		{name: "arm64 source stays in arm64 namespace", target: "5.0.1-okd-scos.0", namespace: "origin-arm64", policy: armPolicy, expected: "4.22.0-okd-scos.9"},
		{name: "arm64 cannot select amd64 source", target: "5.0.1", namespace: "origin-arm64", policy: policy, errorText: "no accepted 4.22 source"},
		{name: "periodic uses same override", target: "5.0.1", policy: policy, periodic: true, expected: "4.22.0-okd-scos.10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			namespace := tc.namespace
			if namespace == "" {
				namespace = "origin"
			}
			stream := tc.stream
			if stream == "" {
				stream = "5-scos-stable"
			}
			index := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
			addStream := func(namespace, name string, tags map[string]string) *imagev1.ImageStream {
				t.Helper()
				config, err := json.Marshal(map[string]string{"name": name, "as": releasecontroller.ReleaseConfigModeStable, "expires": "72h"})
				if err != nil {
					t.Fatal(err)
				}
				is := &imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Annotations: map[string]string{releasecontroller.ReleaseAnnotationConfig: string(config)}},
					Status:     imagev1.ImageStreamStatus{PublicDockerImageRepository: "quay.io/test/" + namespace, Tags: []imagev1.NamedTagEventList{{Tag: "fixture"}}},
				}
				for tag, phase := range tags {
					is.Spec.Tags = append(is.Spec.Tags, imagev1.TagReference{Name: tag, Annotations: map[string]string{
						releasecontroller.ReleaseAnnotationName:   name,
						releasecontroller.ReleaseAnnotationSource: namespace + "/" + name,
						releasecontroller.ReleaseAnnotationPhase:  phase,
					}})
				}
				if err := index.Add(is); err != nil {
					t.Fatal(err)
				}
				return is
			}
			accepted := releasecontroller.ReleasePhaseAccepted
			if !tc.missing {
				tags := map[string]string{
					"4.22.0-okd-scos.9": accepted, "4.22.0-okd-scos.10": accepted,
					"4.22.0-okd-scos.11": releasecontroller.ReleasePhaseRejected,
					"4.22.0-okd-scos.12": releasecontroller.ReleasePhaseReady,
					"4.21.9-okd-scos.99": accepted, "4.23.0-okd-scos.99": accepted,
					"invalid": accepted,
				}
				if tc.noEligible {
					delete(tags, "4.22.0-okd-scos.9")
					delete(tags, "4.22.0-okd-scos.10")
				}
				source := addStream("origin", "4-scos-stable", tags)
				if tc.missingPullSpec {
					source.Status.PublicDockerImageRepository = ""
				}
				if tc.nonStable {
					source.Annotations[releasecontroller.ReleaseAnnotationConfig] = `{"name":"4-scos-stable","as":"Integration","to":"4-scos-stable","expires":"72h"}`
				}
			}
			addStream("origin", "4-stable", map[string]string{"4.22.9": accepted})
			addStream("origin-arm64", "4-scos-stable-arm64", map[string]string{"4.22.0-okd-scos.9": accepted})
			addStream("origin", "5-scos-stable", map[string]string{"5.0.0-okd-scos.1": accepted, "5.0.2-okd-scos.1": accepted})
			targetStream := addStream(namespace, stream, map[string]string{"5.0.0-okd-scos.1": accepted, "5.0.2-okd-scos.1": accepted})
			lister := imagelisters.NewImageStreamLister(index)
			controller := &Controller{
				releaseLister: &releasecontroller.MultiImageStreamLister{Listers: map[string]imagelisters.ImageStreamNamespaceLister{
					"origin": lister.ImageStreams("origin"), "origin-arm64": lister.ImageStreams("origin-arm64"),
				}},
				eventRecorder:        record.NewFakeRecorder(20),
				releasePayloadLister: &releasecontroller.MultiReleasePayloadLister{Listers: map[string]payloadlisters.ReleasePayloadNamespaceLister{}},
			}
			if tc.rejectedPayload {
				payloadIndex := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
				if err := payloadIndex.Add(&v1alpha1.ReleasePayload{
					ObjectMeta: metav1.ObjectMeta{Name: "4.22.0-okd-scos.10", Namespace: "origin"},
					Status:     v1alpha1.ReleasePayloadStatus{Conditions: []metav1.Condition{{Type: v1alpha1.ConditionPayloadRejected, Status: metav1.ConditionTrue}}},
				}); err != nil {
					t.Fatal(err)
				}
				controller.releasePayloadLister = &releasecontroller.MultiReleasePayloadLister{Listers: map[string]payloadlisters.ReleasePayloadNamespaceLister{
					"origin": payloadlisters.NewReleasePayloadLister(payloadIndex).ReleasePayloads("origin"),
				}}
			}
			release := &releasecontroller.Release{Source: targetStream, Target: targetStream, Config: &releasecontroller.ReleaseConfig{Name: stream, As: releasecontroller.ReleaseConfigModeStable}}
			tag, pullSpec, err := controller.getUpgradeTagAndPullSpec(release, &imagev1.TagReference{Name: tc.target}, "upgrade-minor", releasecontroller.ReleaseUpgradeFromPreviousMinor, nil, tc.policy, tc.periodic)
			if tc.errorText != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errorText) {
					t.Fatalf("expected error containing %q, got %v", tc.errorText, err)
				}
				if tag != "" || pullSpec != "" {
					t.Fatalf("returned source with error: %s, %s", tag, pullSpec)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tag != tc.expected {
				t.Fatalf("tag = %q, want %q", tag, tc.expected)
			}
			wantPullSpec := ""
			if tc.expected != "" {
				wantPullSpec = "quay.io/test/" + namespace + ":" + tc.expected
			}
			if pullSpec != wantPullSpec {
				t.Fatalf("pull spec = %q, want %q", pullSpec, wantPullSpec)
			}
		})
	}
}
