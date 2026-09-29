package main

import (
	"fmt"
	"slices"
	"testing"

	imagev1 "github.com/openshift/api/image/v1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	corev1 "k8s.io/api/core/v1"
	prowjobv1 "sigs.k8s.io/prow/pkg/apis/prowjobs/v1"
	prowconfig "sigs.k8s.io/prow/pkg/config"
)

func TestValidateProwJob(t *testing.T) {
	testCases := []struct {
		name        string
		pj          *prowconfig.Periodic
		expectedErr error
	}{
		{
			name:        "No cluster yields error",
			pj:          &prowconfig.Periodic{},
			expectedErr: fmt.Errorf(`the jobs cluster must be set to a value that is not default, was ""`),
		},
		{
			name:        "Default cluster yields error",
			pj:          &prowconfig.Periodic{JobBase: prowconfig.JobBase{Cluster: "default"}},
			expectedErr: fmt.Errorf(`the jobs cluster must be set to a value that is not default, was "default"`),
		},
		{
			name: "No default cluster, no error",
			pj:   &prowconfig.Periodic{JobBase: prowconfig.JobBase{Cluster: "api.ci"}},
		},
	}

	t.Parallel()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualErr := validateProwJob(tc.pj)
			var actualErrMsg, expectedErrMsg string
			if actualErr != nil {
				actualErrMsg = actualErr.Error()
			}
			if tc.expectedErr != nil {
				expectedErrMsg = tc.expectedErr.Error()
			}
			if actualErrMsg != expectedErrMsg {
				t.Errorf("Expected err %q, got err %q", expectedErrMsg, actualErrMsg)
			}
		})
	}
}

func newRelease(reference bool, refRepo string) *releasecontroller.Release {
	tags := []imagev1.TagReference{{Name: "cli"}}
	if reference {
		tags[0].Reference = true
	}
	config := &releasecontroller.ReleaseConfig{
		Name: "4.17.0-0.nightly",
	}
	if len(refRepo) > 0 {
		config.ReferenceRelease = &releasecontroller.ReferenceRelease{
			PushRepository: refRepo,
			PullRepository: refRepo,
		}
	}
	return &releasecontroller.Release{
		Source: &imagev1.ImageStream{
			Spec: imagev1.ImageStreamSpec{Tags: tags},
		},
		Target: &imagev1.ImageStream{
			Status: imagev1.ImageStreamStatus{
				PublicDockerImageRepository: "registry.ci.openshift.org/ocp/release",
			},
		},
		Config: config,
	}
}

func findEnv(envs []corev1.EnvVar, name string) (string, bool) {
	for _, e := range envs {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

func TestAddReleaseEnvToProwJobSpec_NonReference(t *testing.T) {
	release := newRelease(false, "")
	tag := &imagev1.TagReference{Name: "4.17.0-0.nightly-2025-01-01-000000"}
	spec := prowjobv1.ProwJobSpec{
		PodSpec: &corev1.PodSpec{
			Containers: []corev1.Container{{Name: "test"}},
		},
	}

	ok, err := addReleaseEnvToProwJobSpec(&spec, release, nil, tag, "", false, "amd64", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	expected := "registry.ci.openshift.org/ocp/release:4.17.0-0.nightly-2025-01-01-000000"
	env := spec.PodSpec.Containers[0].Env
	if val, found := findEnv(env, "RELEASE_IMAGE_LATEST"); !found || val != expected {
		t.Errorf("RELEASE_IMAGE_LATEST: expected %q, got %q (found=%v)", expected, val, found)
	}
	if val, found := findEnv(env, "RELEASE_IMAGE_INITIAL"); !found || val != expected {
		t.Errorf("RELEASE_IMAGE_INITIAL: expected %q, got %q (found=%v)", expected, val, found)
	}
}

func TestAddReleaseEnvToProwJobSpec_Reference(t *testing.T) {
	release := newRelease(true, "quay.io/openshift-release-dev/ocp-release")
	tag := &imagev1.TagReference{Name: "4.17.0-0.nightly-2025-01-01-000000", Reference: true}
	spec := prowjobv1.ProwJobSpec{
		PodSpec: &corev1.PodSpec{
			Containers: []corev1.Container{{Name: "test"}},
		},
	}

	ok, err := addReleaseEnvToProwJobSpec(&spec, release, nil, tag, "", false, "amd64", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	expected := "quay.io/openshift-release-dev/ocp-release:rc_payload__4.17.0-0.nightly-2025-01-01-000000"
	env := spec.PodSpec.Containers[0].Env
	if val, found := findEnv(env, "RELEASE_IMAGE_LATEST"); !found || val != expected {
		t.Errorf("RELEASE_IMAGE_LATEST: expected %q, got %q (found=%v)", expected, val, found)
	}
	if val, found := findEnv(env, "RELEASE_IMAGE_INITIAL"); !found || val != expected {
		t.Errorf("RELEASE_IMAGE_INITIAL: expected %q, got %q (found=%v)", expected, val, found)
	}
}

func TestAddReleaseEnvToProwJobSpec_ReferenceUpgrade(t *testing.T) {
	release := newRelease(true, "quay.io/openshift-release-dev/ocp-release")
	tag := &imagev1.TagReference{Name: "4.17.0-0.nightly-2025-01-01-000000", Reference: true}
	prevPullSpec := "quay.io/openshift-release-dev/ocp-release:rc_payload__4.17.0-0.nightly-2024-12-31-000000"
	spec := prowjobv1.ProwJobSpec{
		PodSpec: &corev1.PodSpec{
			Containers: []corev1.Container{{Name: "test"}},
		},
	}

	ok, err := addReleaseEnvToProwJobSpec(&spec, release, nil, tag, prevPullSpec, true, "amd64", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	expectedLatest := "quay.io/openshift-release-dev/ocp-release:rc_payload__4.17.0-0.nightly-2025-01-01-000000"
	env := spec.PodSpec.Containers[0].Env
	if val, found := findEnv(env, "RELEASE_IMAGE_LATEST"); !found || val != expectedLatest {
		t.Errorf("RELEASE_IMAGE_LATEST: expected %q, got %q (found=%v)", expectedLatest, val, found)
	}
	if val, found := findEnv(env, "RELEASE_IMAGE_INITIAL"); !found || val != prevPullSpec {
		t.Errorf("RELEASE_IMAGE_INITIAL: expected %q, got %q (found=%v)", prevPullSpec, val, found)
	}
}

func TestAddReleaseEnvToProwJobSpec_ArchVariants(t *testing.T) {
	testCases := []struct {
		arch             string
		expectedLatest   string
		expectedInitial  string
		extraLatestName  string
		extraInitialName string
	}{
		{
			arch:            "arm64",
			expectedLatest:  "RELEASE_IMAGE_ARM64_LATEST",
			expectedInitial: "RELEASE_IMAGE_ARM64_INITIAL",
		},
		{
			arch:            "s390x",
			expectedLatest:  "RELEASE_IMAGE_S390X_LATEST",
			expectedInitial: "RELEASE_IMAGE_S390X_INITIAL",
		},
		{
			arch:            "ppc64le",
			expectedLatest:  "RELEASE_IMAGE_PPC64LE_LATEST",
			expectedInitial: "RELEASE_IMAGE_PPC64LE_INITIAL",
		},
		{
			arch:             "multi",
			expectedLatest:   "RELEASE_IMAGE_LATEST",
			expectedInitial:  "RELEASE_IMAGE_INITIAL",
			extraLatestName:  "RELEASE_IMAGE_MULTI_LATEST",
			extraInitialName: "RELEASE_IMAGE_MULTI_INITIAL",
		},
	}

	for _, tc := range testCases {
		for _, isRef := range []bool{false, true} {
			name := fmt.Sprintf("%s/reference=%v", tc.arch, isRef)
			t.Run(name, func(t *testing.T) {
				var refRepo string
				if isRef {
					refRepo = "quay.io/openshift-release-dev/ocp-release"
				}
				release := newRelease(isRef, refRepo)
				tag := &imagev1.TagReference{Name: "4.17.0-0.nightly-2025-01-01-000000", Reference: isRef}
				spec := prowjobv1.ProwJobSpec{
					PodSpec: &corev1.PodSpec{
						Containers: []corev1.Container{{Name: "test"}},
					},
				}

				ok, err := addReleaseEnvToProwJobSpec(&spec, release, nil, tag, "", false, tc.arch, "")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !ok {
					t.Fatal("expected ok=true")
				}

				var expectedPullSpec string
				if isRef {
					expectedPullSpec = "quay.io/openshift-release-dev/ocp-release:rc_payload__4.17.0-0.nightly-2025-01-01-000000"
				} else {
					expectedPullSpec = "registry.ci.openshift.org/ocp/release:4.17.0-0.nightly-2025-01-01-000000"
				}

				env := spec.PodSpec.Containers[0].Env
				if val, found := findEnv(env, tc.expectedLatest); !found || val != expectedPullSpec {
					t.Errorf("%s: expected %q, got %q (found=%v)", tc.expectedLatest, expectedPullSpec, val, found)
				}
				if val, found := findEnv(env, tc.expectedInitial); !found || val != expectedPullSpec {
					t.Errorf("%s: expected %q, got %q (found=%v)", tc.expectedInitial, expectedPullSpec, val, found)
				}
				if tc.extraLatestName != "" {
					if val, found := findEnv(env, tc.extraLatestName); !found || val != expectedPullSpec {
						t.Errorf("%s: expected %q, got %q (found=%v)", tc.extraLatestName, expectedPullSpec, val, found)
					}
				}
				if tc.extraInitialName != "" {
					if val, found := findEnv(env, tc.extraInitialName); !found || val != expectedPullSpec {
						t.Errorf("%s: expected %q, got %q (found=%v)", tc.extraInitialName, expectedPullSpec, val, found)
					}
				}
			})
		}
	}
}

const layeredImage = "quay.io/redhat-user-workloads/crt-redhat-acm-tenant/hypershift-operator/hypershift-operator-main@sha256:9651b2b8c64bd9ac53b6318101f4a0c849ebc1641e9ab0be5f69eed70c3e0c63"

func newLayeredRelease(streamEnvVar string) *releasecontroller.Release {
	return &releasecontroller.Release{
		Source: &imagev1.ImageStream{},
		Target: &imagev1.ImageStream{
			Status: imagev1.ImageStreamStatus{
				PublicDockerImageRepository: "registry.ci.openshift.org/hypershift/hypershift",
			},
		},
		Config: &releasecontroller.ReleaseConfig{
			Name:               "hypershift",
			As:                 releasecontroller.ReleaseConfigModeLayered,
			LayeredImageEnvVar: streamEnvVar,
		},
	}
}

func newLayeredTag(image string) *imagev1.TagReference {
	tag := &imagev1.TagReference{
		Name:      "0.0.1-0.nightly-2026-09-28-020433",
		Reference: true,
	}
	if len(image) > 0 {
		tag.From = &corev1.ObjectReference{Kind: "DockerImage", Name: image}
	}
	return tag
}

func TestAddReleaseEnvToProwJobSpec_Layered(t *testing.T) {
	testCases := []struct {
		name        string
		streamVar   string
		jobVar      string
		expectedVar string
	}{
		{
			name:        "defaults when nothing is configured",
			expectedVar: releasecontroller.DefaultLayeredImageEnvVar,
		},
		{
			name:        "stream wide variable is used",
			streamVar:   "HYPERSHIFT_OPERATOR_IMAGE",
			expectedVar: "HYPERSHIFT_OPERATOR_IMAGE",
		},
		{
			name:        "job variable overrides the stream wide variable",
			streamVar:   "HYPERSHIFT_OPERATOR_IMAGE",
			jobVar:      "HYPERSHIFT_CONTROL_PLANE_OPERATOR_IMAGE",
			expectedVar: "HYPERSHIFT_CONTROL_PLANE_OPERATOR_IMAGE",
		},
	}

	t.Parallel()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			spec := prowjobv1.ProwJobSpec{
				PodSpec: &corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "test",
						Env: []corev1.EnvVar{
							{Name: "IMAGE_FORMAT", Value: "placeholder"},
							{Name: "IMAGE_HYPERSHIFT_OPERATOR", Value: "placeholder"},
						},
					}},
				},
			}

			// Layered releases are never mirrored, so no mirror imagestream exists.
			ok, err := addReleaseEnvToProwJobSpec(&spec, newLayeredRelease(tc.streamVar), nil, newLayeredTag(layeredImage), "", false, "amd64", tc.jobVar)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !ok {
				t.Fatal("expected ok=true")
			}

			container := spec.PodSpec.Containers[0]
			if val, found := findEnv(container.Env, tc.expectedVar); !found || val != layeredImage {
				t.Errorf("%s: expected %q, got %q (found=%v)", tc.expectedVar, layeredImage, val, found)
			}
			expectedArg := fmt.Sprintf("--multi-stage-param=%s=%s", tc.expectedVar, layeredImage)
			if !slices.Contains(container.Args, expectedArg) {
				t.Errorf("expected arg %q, got %v", expectedArg, container.Args)
			}
			if !slices.Contains(container.Args, "--multi-stage-param=RELEASE_CONTROLLER_JOB=true") {
				t.Errorf("expected the release controller job param to be set, got %v", container.Args)
			}
			// Layered images are not payloads, the job installs OpenShift as normal.
			for _, name := range []string{"RELEASE_IMAGE_LATEST", "RELEASE_IMAGE_INITIAL"} {
				if val, found := findEnv(container.Env, name); found {
					t.Errorf("%s: expected to be unset, got %q", name, val)
				}
			}
			for _, name := range []string{"IMAGE_FORMAT", "IMAGE_HYPERSHIFT_OPERATOR"} {
				if val, _ := findEnv(container.Env, name); val != "placeholder" {
					t.Errorf("%s: expected to be left untouched, got %q", name, val)
				}
			}
		})
	}
}

func TestAddReleaseEnvToProwJobSpec_LayeredReplacesDeclaredEnvVar(t *testing.T) {
	spec := prowjobv1.ProwJobSpec{
		PodSpec: &corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "test",
				Env:  []corev1.EnvVar{{Name: releasecontroller.DefaultLayeredImageEnvVar, Value: "placeholder"}},
			}},
		},
	}

	ok, err := addReleaseEnvToProwJobSpec(&spec, newLayeredRelease(""), nil, newLayeredTag(layeredImage), "", false, "amd64", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	env := spec.PodSpec.Containers[0].Env
	if val, found := findEnv(env, releasecontroller.DefaultLayeredImageEnvVar); !found || val != layeredImage {
		t.Errorf("%s: expected %q, got %q (found=%v)", releasecontroller.DefaultLayeredImageEnvVar, layeredImage, val, found)
	}
	var count int
	for _, e := range env {
		if e.Name == releasecontroller.DefaultLayeredImageEnvVar {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected %s to be declared once, got %d times", releasecontroller.DefaultLayeredImageEnvVar, count)
	}
}

// An environment variable that specifies both a value and a valueFrom is
// rejected by the API server, so overwriting a variable the job sourced from
// elsewhere must clear the source as well.
func TestAddReleaseEnvToProwJobSpec_LayeredClearsDeclaredValueFrom(t *testing.T) {
	spec := prowjobv1.ProwJobSpec{
		PodSpec: &corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "test",
				Env: []corev1.EnvVar{{
					Name: releasecontroller.DefaultLayeredImageEnvVar,
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
							Key:                  "some-key",
						},
					},
				}},
			}},
		},
	}

	ok, err := addReleaseEnvToProwJobSpec(&spec, newLayeredRelease(""), nil, newLayeredTag(layeredImage), "", false, "amd64", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	for _, e := range spec.PodSpec.Containers[0].Env {
		if e.Name != releasecontroller.DefaultLayeredImageEnvVar {
			continue
		}
		if e.Value != layeredImage {
			t.Errorf("%s: expected %q, got %q", e.Name, layeredImage, e.Value)
		}
		if e.ValueFrom != nil {
			t.Errorf("%s: expected valueFrom to be cleared, got %+v", e.Name, e.ValueFrom)
		}
	}
}

func TestAddReleaseEnvToProwJobSpec_LayeredWithoutImage(t *testing.T) {
	spec := prowjobv1.ProwJobSpec{
		Job: "periodic-hypershift-e2e",
		PodSpec: &corev1.PodSpec{
			Containers: []corev1.Container{{Name: "test"}},
		},
	}

	if _, err := addReleaseEnvToProwJobSpec(&spec, newLayeredRelease(""), nil, newLayeredTag(""), "", false, "amd64", ""); err == nil {
		t.Fatal("expected an error when the tag does not reference an image")
	}
}

func TestAddReleaseEnvToProwJobSpec_ExistingEnvVar(t *testing.T) {
	release := newRelease(true, "quay.io/openshift-release-dev/ocp-release")
	tag := &imagev1.TagReference{Name: "4.17.0-0.nightly-2025-01-01-000000", Reference: true}
	spec := prowjobv1.ProwJobSpec{
		PodSpec: &corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "test",
				Env: []corev1.EnvVar{
					{Name: "RELEASE_IMAGE_LATEST", Value: "placeholder"},
				},
			}},
		},
	}

	ok, err := addReleaseEnvToProwJobSpec(&spec, release, nil, tag, "", false, "amd64", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	expected := "quay.io/openshift-release-dev/ocp-release:rc_payload__4.17.0-0.nightly-2025-01-01-000000"
	env := spec.PodSpec.Containers[0].Env
	if val, found := findEnv(env, "RELEASE_IMAGE_LATEST"); !found || val != expected {
		t.Errorf("RELEASE_IMAGE_LATEST: expected %q, got %q (found=%v)", expected, val, found)
	}
}
