package releasecontroller

import (
	"testing"

	imagev1 "github.com/openshift/api/image/v1"
)

func TestIsReferenceRelease(t *testing.T) {
	testCases := []struct {
		name     string
		release  *Release
		expected bool
	}{
		{
			name: "nil source returns false",
			release: &Release{
				Source: nil,
				Config: &ReleaseConfig{
					ReferenceRelease: &ReferenceRelease{
						PullRepository: "quay.io/openshift-release-dev/ocp-release",
					},
				},
			},
			expected: false,
		},
		{
			name: "nil config returns false",
			release: &Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli", Reference: true},
						},
					},
				},
				Config: nil,
			},
			expected: false,
		},
		{
			name: "nil ReferenceRelease returns false",
			release: &Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli", Reference: true},
						},
					},
				},
				Config: &ReleaseConfig{
					Name: "4.17.0-0.nightly",
				},
			},
			expected: false,
		},
		{
			name: "non-reference tags returns false",
			release: &Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli"},
						},
					},
				},
				Config: &ReleaseConfig{
					Name: "4.17.0-0.nightly",
					ReferenceRelease: &ReferenceRelease{
						PullRepository: "quay.io/openshift-release-dev/ocp-release",
					},
				},
			},
			expected: false,
		},
		{
			name: "fully configured reference release returns true",
			release: &Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli", Reference: true},
						},
					},
				},
				Config: &ReleaseConfig{
					Name: "4.17.0-0.nightly",
					ReferenceRelease: &ReferenceRelease{
						PushRepository: "quay.io/openshift-release-dev/ocp-release",
						PullRepository: "quay.io/openshift-release-dev/ocp-release",
					},
				},
			},
			expected: true,
		},
	}

	t.Parallel()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := IsReferenceRelease(tc.release)
			if actual != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, actual)
			}
		})
	}
}

func TestReleasePullSpec(t *testing.T) {
	testCases := []struct {
		name     string
		release  *Release
		tagName  string
		expected string
	}{
		{
			name: "non-reference release uses Target pull spec",
			release: &Release{
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
					},
				},
				Config: &ReleaseConfig{
					Name: "4.17.0-0.nightly",
				},
			},
			tagName:  "4.17.0-0.nightly-2025-01-01-000000",
			expected: "registry.ci.openshift.org/ocp/release:4.17.0-0.nightly-2025-01-01-000000",
		},
		{
			name: "reference release uses ReferenceRelease with payload tag prefix",
			release: &Release{
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
					},
				},
				Config: &ReleaseConfig{
					Name: "4.17.0-0.nightly",
					ReferenceRelease: &ReferenceRelease{
						PushRepository: "quay.io/openshift-release-dev/ocp-release",
						PullRepository: "quay.io/openshift-release-dev/ocp-release",
					},
				},
			},
			tagName:  "4.17.0-0.nightly-2025-01-01-000000",
			expected: "quay.io/openshift-release-dev/ocp-release:rc_payload__4.17.0-0.nightly-2025-01-01-000000",
		},
		{
			name: "reference tags but nil ReferenceRelease falls back to Target",
			release: &Release{
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
					},
				},
				Config: &ReleaseConfig{
					Name: "4.17.0-0.nightly",
				},
			},
			tagName:  "4.17.0-0.nightly-2025-01-01-000000",
			expected: "registry.ci.openshift.org/ocp/release:4.17.0-0.nightly-2025-01-01-000000",
		},
		{
			name: "no spec tags means non-reference",
			release: &Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{},
				},
				Target: &imagev1.ImageStream{
					Status: imagev1.ImageStreamStatus{
						PublicDockerImageRepository: "registry.ci.openshift.org/ocp/release",
					},
				},
				Config: &ReleaseConfig{
					Name: "4.17.0-0.nightly",
					ReferenceRelease: &ReferenceRelease{
						PushRepository: "quay.io/openshift-release-dev/ocp-release",
						PullRepository: "quay.io/openshift-release-dev/ocp-release",
					},
				},
			},
			tagName:  "4.17.0-0.nightly-2025-01-01-000000",
			expected: "registry.ci.openshift.org/ocp/release:4.17.0-0.nightly-2025-01-01-000000",
		},
	}

	t.Parallel()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := ReleasePullSpec(tc.release, tc.tagName)
			if actual != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, actual)
			}
		})
	}
}
