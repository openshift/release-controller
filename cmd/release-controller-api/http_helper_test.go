package main

import (
	"reflect"
	"sort"
	"testing"

	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	"github.com/blang/semver"
	"github.com/google/go-cmp/cmp"
	imagev1 "github.com/openshift/api/image/v1"
	corev1 "k8s.io/api/core/v1"
)

func Test_calculateReleaseUpgrades(t *testing.T) {
	tests := []struct {
		name    string
		release *releasecontroller.Release
		tags    []*imagev1.TagReference
		graph   func() *releasecontroller.UpgradeGraph
		want    *ReleaseUpgrades
		wantFn  func() *ReleaseUpgrades
	}{
		{
			tags: []*imagev1.TagReference{},
			graph: func() *releasecontroller.UpgradeGraph {
				g := releasecontroller.NewUpgradeGraph("amd64")
				return g
			},
			want: &ReleaseUpgrades{
				Width: 0,
				Tags:  []ReleaseTagUpgrade{},
			},
		},
		{
			tags: []*imagev1.TagReference{
				{Name: "4.0.1"},
				{Name: "4.0.0"},
			},
			graph: func() *releasecontroller.UpgradeGraph {
				g := releasecontroller.NewUpgradeGraph("amd64")
				return g
			},
			want: &ReleaseUpgrades{
				Width: 0,
				Tags: []ReleaseTagUpgrade{
					{},
					{},
				},
			},
		},
		{
			tags: []*imagev1.TagReference{
				{Name: "4.0.1"},
				{Name: "4.0.0"},
				{Name: "4.0.0-9"},
			},
			graph: func() *releasecontroller.UpgradeGraph {
				g := releasecontroller.NewUpgradeGraph("amd64")
				g.Add("4.0.0", "4.0.1", releasecontroller.UpgradeResult{State: releasecontroller.ReleaseVerificationStateFailed, URL: "https://test.com/1"})
				return g
			},
			wantFn: func() *ReleaseUpgrades {
				internal0 := []releasecontroller.UpgradeHistory{{From: "4.0.0", To: "4.0.1", Success: 0, Failure: 1, Total: 1}}
				u := &ReleaseUpgrades{
					Width: 1,
					Tags: []ReleaseTagUpgrade{
						{
							Internal: internal0,
							Visual: []ReleaseTagUpgradeVisual{
								{Begin: &internal0[0]},
							},
						},
						{
							Visual: []ReleaseTagUpgradeVisual{
								{End: &internal0[0]},
							},
						},
						{},
					},
				}
				return u
			},
		},
		{
			tags: []*imagev1.TagReference{
				{Name: "4.0.5"},
				{Name: "4.0.4"},
				{Name: "4.0.3"},
				{Name: "4.0.2"},
				{Name: "4.0.1"},
			},
			graph: func() *releasecontroller.UpgradeGraph {
				g := releasecontroller.NewUpgradeGraph("amd64")
				g.Add("4.0.4", "4.0.5", releasecontroller.UpgradeResult{State: releasecontroller.ReleaseVerificationStateFailed, URL: "https://test.com/1"})
				g.Add("4.0.3", "4.0.5", releasecontroller.UpgradeResult{State: releasecontroller.ReleaseVerificationStateSucceeded, URL: "https://test.com/2"})
				g.Add("4.0.0", "4.0.2", releasecontroller.UpgradeResult{State: releasecontroller.ReleaseVerificationStateSucceeded, URL: "https://test.com/2"})
				return g
			},
			wantFn: func() *ReleaseUpgrades {
				internal0 := []releasecontroller.UpgradeHistory{
					{From: "4.0.4", To: "4.0.5", Success: 0, Failure: 1, Total: 1},
					{From: "4.0.3", To: "4.0.5", Success: 1, Failure: 0, Total: 1},
				}
				u := &ReleaseUpgrades{
					Width: 2,
					Tags: []ReleaseTagUpgrade{
						{
							Internal: internal0,
							Visual: []ReleaseTagUpgradeVisual{
								{Begin: &internal0[0]},
								{Begin: &internal0[1]},
							},
						},
						{
							Visual: []ReleaseTagUpgradeVisual{
								{End: &internal0[0]},
								{Current: &internal0[1]},
							},
						},
						{
							Visual: []ReleaseTagUpgradeVisual{
								{},
								{End: &internal0[1]},
							},
						},
						{
							External: []releasecontroller.UpgradeHistory{{From: "4.0.0", To: "4.0.2", Success: 1, Total: 1}},
						},
						{},
					},
				}
				return u
			},
		},

		{
			tags: []*imagev1.TagReference{
				{Name: "4.1.0-0.test-10"},
				{Name: "4.1.0-0.test-09"},
				{Name: "4.1.0-0.test-08"},
				{Name: "4.1.0-0.test-07"},
				{Name: "4.1.0-0.test-06"},
			},
			graph: func() *releasecontroller.UpgradeGraph {
				g := releasecontroller.NewUpgradeGraph("amd64")
				g.Add("4.1.0-0.test-08", "4.1.0-0.test-09", releasecontroller.UpgradeResult{State: releasecontroller.ReleaseVerificationStateFailed, URL: "https://test.com/1"})
				g.Add("4.1.0-0.test-07", "4.1.0-0.test-08", releasecontroller.UpgradeResult{State: releasecontroller.ReleaseVerificationStateSucceeded, URL: "https://test.com/2"})
				g.Add("4.1.0-rc.0", "4.1.0-0.test-08", releasecontroller.UpgradeResult{State: releasecontroller.ReleaseVerificationStateSucceeded, URL: "https://test.com/2"})
				return g
			},
			wantFn: func() *ReleaseUpgrades {
				internal0 := []releasecontroller.UpgradeHistory{
					{From: "4.1.0-0.test-08", To: "4.1.0-0.test-09", Success: 0, Failure: 1, Total: 1},
				}
				internal1 := []releasecontroller.UpgradeHistory{
					{From: "4.1.0-0.test-07", To: "4.1.0-0.test-08", Success: 1, Failure: 0, Total: 1},
				}
				u := &ReleaseUpgrades{
					Width: 2,
					Tags: []ReleaseTagUpgrade{
						{},
						{
							Internal: internal0,
							Visual: []ReleaseTagUpgradeVisual{
								{Begin: &internal0[0]},
							},
						},
						{
							Internal: internal1,
							Visual: []ReleaseTagUpgradeVisual{
								{End: &internal0[0]},
								{Begin: &internal1[0]},
							},
							External: []releasecontroller.UpgradeHistory{{From: "4.1.0-rc.0", To: "4.1.0-0.test-08", Success: 1, Total: 1}},
						},
						{
							Visual: []ReleaseTagUpgradeVisual{
								{},
								{End: &internal1[0]},
							},
						},
						{},
					},
				}
				return u
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantFn != nil {
				tt.want = tt.wantFn()
			}
			if tt.release == nil {
				tt.release = &releasecontroller.Release{
					Config: &releasecontroller.ReleaseConfig{},
				}
			}
			if got := calculateReleaseUpgrades(tt.release, tt.tags, tt.graph(), false); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s", cmp.Diff(tt.want, got))
			}
		})
	}
}

func TestSemanticVersions_Tags(t *testing.T) {
	tests := []struct {
		name string
		v    releasecontroller.SemanticVersions
		want []*imagev1.TagReference
	}{
		{
			v: releasecontroller.NewSemanticVersions([]*imagev1.TagReference{
				{Name: "4.0.0"}, {Name: "4.0.1"}, {Name: "4.0.0-2"}, {Name: "4.0.0-1-a"},
			}),
			want: []*imagev1.TagReference{
				{Name: "4.0.1"}, {Name: "4.0.0"}, {Name: "4.0.0-1-a"}, {Name: "4.0.0-2"},
			},
		},
		{
			v: releasecontroller.NewSemanticVersions([]*imagev1.TagReference{
				{Name: "4.0.0-0.9"}, {Name: "4.0.0-0.2"}, {Name: "4.0.0-0.2.a"},
			}),
			want: []*imagev1.TagReference{
				{Name: "4.0.0-0.9"}, {Name: "4.0.0-0.2.a"}, {Name: "4.0.0-0.2"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sort.Sort(tt.v)
			if got := tt.v.Tags(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SemanticVersions.Tags() = %v, want %v", releasecontroller.TagNames(got), releasecontroller.TagNames(tt.want))
			}
		})
	}
}

func TestSemVer(t *testing.T) {
	x, err := semver.Parse("4.1.0-0.nightly")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s", x.String())
}

func Test_takeUpgradesFromNames(t *testing.T) {
	tests := []struct {
		name             string
		summaries        []releasecontroller.UpgradeHistory
		names            map[string]int
		wantWithNames    []releasecontroller.UpgradeHistory
		wantWithoutNames []releasecontroller.UpgradeHistory
	}{
		{
			summaries: []releasecontroller.UpgradeHistory{
				{From: "a", To: "c"},
				{From: "b", To: "c"},
			},
			names: map[string]int{"a": 1, "c": 3},
			wantWithNames: []releasecontroller.UpgradeHistory{
				{From: "a", To: "c"},
			},
			wantWithoutNames: []releasecontroller.UpgradeHistory{
				{From: "b", To: "c"},
			},
		},
		{
			summaries: []releasecontroller.UpgradeHistory{
				{From: "a", To: "c"},
				{From: "b", To: "c"},
			},
			names: map[string]int{"a": 1, "b": 2, "c": 3},
			wantWithNames: []releasecontroller.UpgradeHistory{
				{From: "a", To: "c"},
				{From: "b", To: "c"},
			},
		},
		{
			summaries: []releasecontroller.UpgradeHistory{
				{From: "a", To: "c"},
				{From: "b", To: "c"},
			},
			names: map[string]int{"b": 2, "c": 3},
			wantWithNames: []releasecontroller.UpgradeHistory{
				{From: "b", To: "c"},
			},
			wantWithoutNames: []releasecontroller.UpgradeHistory{
				{From: "a", To: "c"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotWithNames, gotWithoutNames := takeUpgradesFromNames(tt.summaries, tt.names)
			if !reflect.DeepEqual(gotWithNames, tt.wantWithNames) {
				t.Errorf("takeUpgradesFromNames() gotWithNames = %v, want %v", gotWithNames, tt.wantWithNames)
			}
			if !reflect.DeepEqual(gotWithoutNames, tt.wantWithoutNames) {
				t.Errorf("takeUpgradesFromNames() gotWithoutNames = %v, want %v", gotWithoutNames, tt.wantWithoutNames)
			}
		})
	}
}

func TestResolveReleasePullSpec(t *testing.T) {
	tests := []struct {
		name    string
		release *releasecontroller.Release
		tag     string
		want    string
	}{
		{
			name: "layered reference tag resolves to DockerImage source",
			release: &releasecontroller.Release{
				Target: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{Tags: []imagev1.TagReference{{
						Name:      "1.2.3",
						Reference: true,
						From: &corev1.ObjectReference{
							Kind: "DockerImage",
							Name: "quay.io/example/layered@sha256:e08883ade89b50664c14f2a9434018921a012c4506dde92e8779e482e025ea4c",
						},
					}}},
					Status: imagev1.ImageStreamStatus{PublicDockerImageRepository: "quay-proxy.ci.openshift.org/openshift/ci"},
				},
				Config: &releasecontroller.ReleaseConfig{
					As: releasecontroller.ReleaseConfigModeLayered,
					ReferenceRelease: &releasecontroller.ReferenceRelease{
						PullRepository: "quay-proxy.ci.openshift.org/openshift/ci",
					},
				},
			},
			tag:  "1.2.3",
			want: "quay.io/example/layered@sha256:e08883ade89b50664c14f2a9434018921a012c4506dde92e8779e482e025ea4c",
		},
		{
			name: "layered non-reference tag resolves to DockerImage source without status",
			release: &releasecontroller.Release{
				Target: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{Tags: []imagev1.TagReference{{
						Name: "1.2.3",
						From: &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/example/layered:v1.2.3"},
					}}},
				},
				Config: &releasecontroller.ReleaseConfig{As: releasecontroller.ReleaseConfigModeLayered},
			},
			tag:  "1.2.3",
			want: "quay.io/example/layered:v1.2.3",
		},
		{
			name: "reference tag in Target resolves to external repo",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli", Reference: true},
						},
					},
				},
				Target: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "4.18.0-0.nightly-2025-01-01-000000", Reference: true},
						},
					},
					Status: imagev1.ImageStreamStatus{
						PublicDockerImageRepository: "registry.ci.openshift.org/ocp/release",
					},
				},
				Config: &releasecontroller.ReleaseConfig{
					ReferenceRelease: &releasecontroller.ReferenceRelease{
						PushRepository: "quay.io/openshift-release-dev/ocp-release",
						PullRepository: "quay.io/openshift-release-dev/ocp-release",
					},
				},
			},
			tag:  "4.18.0-0.nightly-2025-01-01-000000",
			want: "quay.io/openshift-release-dev/ocp-release:rc_payload__4.18.0-0.nightly-2025-01-01-000000",
		},
		{
			name: "reference source but tag not in Target falls back to FindPublicImagePullSpec",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli", Reference: true},
						},
					},
				},
				Target: &imagev1.ImageStream{
					Status: imagev1.ImageStreamStatus{
						PublicDockerImageRepository: "registry.ci.openshift.org/ocp/release",
						Tags: []imagev1.NamedTagEventList{
							{
								Tag:   "4.18.0-0.nightly-2025-01-01-000000",
								Items: []imagev1.TagEvent{{DockerImageReference: "sha256:abc123", Generation: 1}},
							},
						},
					},
				},
				Config: &releasecontroller.ReleaseConfig{
					ReferenceRelease: &releasecontroller.ReferenceRelease{
						PushRepository: "quay.io/openshift-release-dev/ocp-release",
						PullRepository: "quay.io/openshift-release-dev/ocp-release",
					},
				},
			},
			tag:  "4.18.0-0.nightly-2025-01-01-000000",
			want: "registry.ci.openshift.org/ocp/release:4.18.0-0.nightly-2025-01-01-000000",
		},
		{
			name: "transitional: reference source but legacy tag (Reference false) resolves to local Target",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli", Reference: true},
						},
					},
				},
				Target: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "4.18.0-0.nightly-2025-01-01-000000", Reference: false},
						},
					},
					Status: imagev1.ImageStreamStatus{
						PublicDockerImageRepository: "registry.ci.openshift.org/ocp/release",
						Tags: []imagev1.NamedTagEventList{
							{
								Tag:   "4.18.0-0.nightly-2025-01-01-000000",
								Items: []imagev1.TagEvent{{DockerImageReference: "sha256:abc123", Generation: 1}},
							},
						},
					},
				},
				Config: &releasecontroller.ReleaseConfig{
					ReferenceRelease: &releasecontroller.ReferenceRelease{
						PushRepository: "quay.io/openshift-release-dev/ocp-release",
						PullRepository: "quay.io/openshift-release-dev/ocp-release",
					},
				},
			},
			tag:  "4.18.0-0.nightly-2025-01-01-000000",
			want: "registry.ci.openshift.org/ocp/release:4.18.0-0.nightly-2025-01-01-000000",
		},
		{
			name: "local release uses FindPublicImagePullSpec",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli"},
						},
					},
				},
				Target: &imagev1.ImageStream{
					Status: imagev1.ImageStreamStatus{
						PublicDockerImageRepository: "registry.ci.openshift.org/ocp/release",
						Tags: []imagev1.NamedTagEventList{
							{
								Tag:   "4.12.0",
								Items: []imagev1.TagEvent{{DockerImageReference: "sha256:def456", Generation: 1}},
							},
						},
					},
				},
				Config: &releasecontroller.ReleaseConfig{},
			},
			tag:  "4.12.0",
			want: "registry.ci.openshift.org/ocp/release:4.12.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveReleasePullSpec(tt.release, tt.tag)
			if got != tt.want {
				t.Errorf("resolveReleasePullSpec() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPullSpecFromCoordinates(t *testing.T) {
	tests := []struct {
		name   string
		coords []v1alpha1.ReleaseCoordinates
		want   string
	}{
		{
			name:   "nil coordinates",
			coords: nil,
			want:   "",
		},
		{
			name:   "empty slice",
			coords: []v1alpha1.ReleaseCoordinates{},
			want:   "",
		},
		{
			name:   "empty repository",
			coords: []v1alpha1.ReleaseCoordinates{{Tag: "v1.0"}},
			want:   "",
		},
		{
			name:   "tag-based",
			coords: []v1alpha1.ReleaseCoordinates{{Repository: "quay.io/ocp/release", Tag: "rc_payload__4.18.0"}},
			want:   "quay.io/ocp/release:rc_payload__4.18.0",
		},
		{
			name:   "digest-based",
			coords: []v1alpha1.ReleaseCoordinates{{Repository: "quay.io/ocp/release", Digest: "sha256:abc123"}},
			want:   "quay.io/ocp/release@sha256:abc123",
		},
		{
			name:   "digest takes precedence over tag",
			coords: []v1alpha1.ReleaseCoordinates{{Repository: "quay.io/ocp/release", Tag: "v1", Digest: "sha256:abc123"}},
			want:   "quay.io/ocp/release@sha256:abc123",
		},
		{
			name:   "repository only with no tag or digest",
			coords: []v1alpha1.ReleaseCoordinates{{Repository: "quay.io/ocp/release"}},
			want:   "",
		},
		{
			name: "uses first coordinate",
			coords: []v1alpha1.ReleaseCoordinates{
				{Repository: "quay.io/ocp/release", Tag: "first"},
				{Repository: "registry.example.com/release", Tag: "second"},
			},
			want: "quay.io/ocp/release:first",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pullSpecFromCoordinates(tt.coords)
			if got != tt.want {
				t.Errorf("pullSpecFromCoordinates() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReleaseGraphPullSpec(t *testing.T) {
	const source = "quay.io/example/layered@sha256:e08883ade89b50664c14f2a9434018921a012c4506dde92e8779e482e025ea4c"
	generation := int64(1)
	tests := []struct {
		name    string
		release *releasecontroller.Release
		tag     *imagev1.TagReference
		want    string
	}{
		{
			name: "layered release uses DockerImage source even when status has an image ID",
			tag: &imagev1.TagReference{
				Name: "1.2.3",
				From: &corev1.ObjectReference{Kind: "DockerImage", Name: source},
			},
			release: &releasecontroller.Release{
				Config: &releasecontroller.ReleaseConfig{As: releasecontroller.ReleaseConfigModeLayered},
				Target: &imagev1.ImageStream{
					Status: imagev1.ImageStreamStatus{
						PublicDockerImageRepository: "quay-proxy.ci.openshift.org/openshift/ci",
						Tags: []imagev1.NamedTagEventList{{
							Tag:   "1.2.3",
							Items: []imagev1.TagEvent{{Image: "sha256:status"}},
						}},
					},
				},
			},
			want: source,
		},
		{
			name: "legacy imported layered release preserves public repository digest",
			tag:  &imagev1.TagReference{Name: "1.2.3", Generation: &generation},
			release: &releasecontroller.Release{
				Config: &releasecontroller.ReleaseConfig{As: releasecontroller.ReleaseConfigModeLayered},
				Target: &imagev1.ImageStream{
					Status: imagev1.ImageStreamStatus{
						PublicDockerImageRepository: "registry.example.com/layered",
						Tags: []imagev1.NamedTagEventList{{
							Tag:   "1.2.3",
							Items: []imagev1.TagEvent{{Image: "sha256:0123456789abcdef", Generation: generation}},
						}},
					},
				},
			},
			want: "registry.example.com/layered@sha256:0123456789abcdef",
		},
		{
			name: "layered release with wrong source kind and no status remains unresolved",
			tag: &imagev1.TagReference{
				Name: "1.2.3",
				From: &corev1.ObjectReference{Kind: "ImageStreamTag", Name: "layered:source"},
			},
			release: &releasecontroller.Release{
				Config: &releasecontroller.ReleaseConfig{As: releasecontroller.ReleaseConfigModeLayered},
				Target: &imagev1.ImageStream{},
			},
			want: "",
		},
		{
			name: "layered release with missing source and no status remains unresolved",
			tag:  &imagev1.TagReference{Name: "1.2.3"},
			release: &releasecontroller.Release{
				Config: &releasecontroller.ReleaseConfig{As: releasecontroller.ReleaseConfigModeLayered},
				Target: &imagev1.ImageStream{},
			},
			want: "",
		},
		{
			name: "local release preserves public repository digest",
			tag:  &imagev1.TagReference{Name: "4.20.0"},
			release: &releasecontroller.Release{
				Config: &releasecontroller.ReleaseConfig{},
				Target: &imagev1.ImageStream{
					Status: imagev1.ImageStreamStatus{
						PublicDockerImageRepository: "registry.ci.openshift.org/ocp/release",
						Tags: []imagev1.NamedTagEventList{{
							Tag:   "4.20.0",
							Items: []imagev1.TagEvent{{Image: "sha256:local"}},
						}},
					},
				},
			},
			want: "registry.ci.openshift.org/ocp/release@sha256:local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.release.Config.As == releasecontroller.ReleaseConfigModeLayered {
				tt.release.Target.Spec.Tags = []imagev1.TagReference{*tt.tag}
			}
			if got := releaseGraphPullSpec(tt.release, tt.tag); got != tt.want {
				t.Fatalf("releaseGraphPullSpec() = %q, want %q", got, tt.want)
			}
		})
	}
}

func Test_preferredReleases(t *testing.T) {
	type stream struct {
		name string
		as   string
		hide bool
	}
	tests := []struct {
		name    string
		streams []stream
		want    []string
	}{
		{
			name: "stable streams order by version, newest first",
			streams: []stream{
				{name: "4-stable", as: releasecontroller.ReleaseConfigModeStable},
				{name: "5-stable", as: releasecontroller.ReleaseConfigModeStable},
				{name: "4-scos-stable", as: releasecontroller.ReleaseConfigModeStable},
				{name: "5-scos-stable", as: releasecontroller.ReleaseConfigModeStable},
			},
			want: []string{"5-scos-stable", "5-stable", "4-scos-stable", "4-stable"},
		},
		{
			name: "stable and layered sort ahead of the rest, hidden streams last",
			streams: []stream{
				{name: "4.21.0-0.nightly"},
				{name: "5.0.0-0.nightly"},
				{name: "4.20.0-0.ci", hide: true},
				{name: "4-stable", as: releasecontroller.ReleaseConfigModeStable},
				{name: "5-stable", as: releasecontroller.ReleaseConfigModeStable},
				{name: "4-dev-preview", as: releasecontroller.ReleaseConfigModeLayered},
			},
			want: []string{"5-stable", "4-dev-preview", "4-stable", "5.0.0-0.nightly", "4.21.0-0.nightly", "4.20.0-0.ci"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			releases := make(preferredReleases, 0, len(tt.streams))
			for _, s := range tt.streams {
				releases = append(releases, ReleaseStream{Release: &releasecontroller.Release{
					Config: &releasecontroller.ReleaseConfig{Name: s.name, As: s.as, Hide: s.hide},
				}})
			}
			sort.Sort(releases)
			got := make([]string, 0, len(releases))
			for _, r := range releases {
				got = append(got, r.Release.Config.Name)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("unexpected order (-want +got):\n%s", diff)
			}
		})
	}
}
