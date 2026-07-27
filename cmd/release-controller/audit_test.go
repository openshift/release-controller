package main

import (
	"testing"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	imagev1 "github.com/openshift/api/image/v1"
	imagefake "github.com/openshift/client-go/image/clientset/versioned/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestResolveOverrideCLIImage(t *testing.T) {
	tests := []struct {
		name     string
		override string
		objects  []runtime.Object
		want     string
	}{
		{
			name:     "empty override returns empty",
			override: "",
			want:     "",
		},
		{
			name:     "unparseable reference returns override as-is",
			override: "@@invalid@@",
			want:     "@@invalid@@",
		},
		{
			name:     "imagestream not found returns override as-is",
			override: "image-registry.openshift-image-registry.svc:5000/ocp/4.23:cli",
			want:     "image-registry.openshift-image-registry.svc:5000/ocp/4.23:cli",
		},
		{
			name:     "non-reference tag returns override as-is",
			override: "image-registry.openshift-image-registry.svc:5000/ocp/4.23:cli",
			objects: []runtime.Object{
				&imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Name: "4.23", Namespace: "ocp"},
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{
								Name:      "cli",
								Reference: false,
								From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/openshift/cli:latest"},
							},
						},
					},
				},
			},
			want: "image-registry.openshift-image-registry.svc:5000/ocp/4.23:cli",
		},
		{
			name:     "reference tag resolves to dockerImageReference from status",
			override: "image-registry.openshift-image-registry.svc:5000/ocp/4.23:cli",
			objects: []runtime.Object{
				&imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Name: "4.23", Namespace: "ocp"},
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{
								Name:      "cli",
								Reference: true,
								From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/openshift-release-dev/ocp-v4.0-art-dev:cli"},
							},
						},
					},
					Status: imagev1.ImageStreamStatus{
						Tags: []imagev1.NamedTagEventList{
							{
								Tag: "cli",
								Items: []imagev1.TagEvent{
									{DockerImageReference: "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:abcdef1234567890"},
								},
							},
						},
					},
				},
			},
			want: "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:abcdef1234567890",
		},
		{
			name:     "reference tag with no status falls back to spec From",
			override: "image-registry.openshift-image-registry.svc:5000/ocp/4.23:cli",
			objects: []runtime.Object{
				&imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Name: "4.23", Namespace: "ocp"},
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{
								Name:      "cli",
								Reference: true,
								From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/openshift-release-dev/ocp-v4.0-art-dev:cli"},
							},
						},
					},
				},
			},
			want: "quay.io/openshift-release-dev/ocp-v4.0-art-dev:cli",
		},
		{
			name:     "reference tag with no status and no spec From returns override",
			override: "image-registry.openshift-image-registry.svc:5000/ocp/4.23:cli",
			objects: []runtime.Object{
				&imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Name: "4.23", Namespace: "ocp"},
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{
								Name:      "cli",
								Reference: true,
							},
						},
					},
				},
			},
			want: "image-registry.openshift-image-registry.svc:5000/ocp/4.23:cli",
		},
		{
			name:     "tag not found in imagestream returns override as-is",
			override: "image-registry.openshift-image-registry.svc:5000/ocp/4.23:cli",
			objects: []runtime.Object{
				&imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Name: "4.23", Namespace: "ocp"},
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{
								Name:      "tools",
								Reference: true,
								From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/openshift/tools:latest"},
							},
						},
					},
				},
			},
			want: "image-registry.openshift-image-registry.svc:5000/ocp/4.23:cli",
		},
		{
			name:     "external image reference without tag returns override as-is",
			override: "quay.io/openshift-release-dev/ocp-v4.0-art-dev",
			want:     "quay.io/openshift-release-dev/ocp-v4.0-art-dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := imagefake.NewSimpleClientset(tt.objects...)
			c := &Controller{
				imageClient: fakeClient.ImageV1(),
			}
			release := &releasecontroller.Release{
				Config: &releasecontroller.ReleaseConfig{
					OverrideCLIImage: tt.override,
				},
			}
			got := c.resolveOverrideCLIImage(release)
			if got != tt.want {
				t.Errorf("resolveOverrideCLIImage() = %q, want %q", got, tt.want)
			}
		})
	}
}
