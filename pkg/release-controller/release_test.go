package releasecontroller

import (
	"strings"
	"testing"

	imagev1 "github.com/openshift/api/image/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestResolveCLIImage(t *testing.T) {
	testCases := []struct {
		name          string
		release       *Release
		mirror        *imagev1.ImageStream
		expected      string
		expectError   bool
		errorContains string
	}{
		{
			name: "override set returns override regardless of release type",
			release: &Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli", Reference: true},
						},
					},
				},
				Config: &ReleaseConfig{
					OverrideCLIImage: "quay.io/custom/cli:latest",
					ReferenceRelease: &ReferenceRelease{},
				},
			},
			mirror: &imagev1.ImageStream{
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "cli", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "registry.example.com/cli:v1"}},
					},
				},
			},
			expected: "quay.io/custom/cli:latest",
		},
		{
			name: "reference release with cli spec tag returns spec tag image",
			release: &Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli", Reference: true},
						},
					},
				},
				Config: &ReleaseConfig{
					ReferenceRelease: &ReferenceRelease{},
				},
			},
			mirror: &imagev1.ImageStream{
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "cli", From: &corev1.ObjectReference{Kind: "DockerImage", Name: "registry.example.com/cli:v1"}},
					},
				},
			},
			expected: "registry.example.com/cli:v1",
		},
		{
			name: "reference release with no cli spec tag returns error",
			release: &Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "other", Reference: true},
						},
					},
				},
				Config: &ReleaseConfig{
					ReferenceRelease: &ReferenceRelease{},
				},
			},
			mirror: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ocp", Name: "4.17-art-latest"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "other"},
					},
				},
			},
			expectError:   true,
			errorContains: "4.17-art-latest",
		},
		{
			name: "traditional release with DockerImageRepository returns repo:cli",
			release: &Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli"},
						},
					},
				},
				Config: &ReleaseConfig{},
			},
			mirror: &imagev1.ImageStream{
				Status: imagev1.ImageStreamStatus{
					DockerImageRepository: "image-registry.openshift-image-registry.svc:5000/ocp/release",
				},
			},
			expected: "image-registry.openshift-image-registry.svc:5000/ocp/release:cli",
		},
		{
			name: "traditional release with empty DockerImageRepository returns error",
			release: &Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli"},
						},
					},
				},
				Config: &ReleaseConfig{},
			},
			mirror: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ocp", Name: "4.17-art-latest"},
				Status:     imagev1.ImageStreamStatus{},
			},
			expectError:   true,
			errorContains: "4.17-art-latest",
		},
	}

	t.Parallel()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			actual, err := ResolveCLIImage(tc.release, tc.mirror)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errorContains != "" && !strings.Contains(err.Error(), tc.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tc.errorContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if actual != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, actual)
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
			name: "reference release with empty PullRepository returns empty string",
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
						PullRepository: "",
					},
				},
			},
			tagName:  "4.17.0-0.nightly-2025-01-01-000000",
			expected: "",
		},
		{
			name: "non-reference release with empty PublicDockerImageRepository returns empty string",
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
						PublicDockerImageRepository: "",
					},
				},
				Config: &ReleaseConfig{
					Name: "4.17.0-0.nightly",
				},
			},
			tagName:  "4.17.0-0.nightly-2025-01-01-000000",
			expected: "",
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
