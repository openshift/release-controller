package main

import (
	"strings"
	"testing"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	imagev1 "github.com/openshift/api/image/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCalculateReferenceMirrorImageStream(t *testing.T) {
	testCases := []struct {
		name       string
		release    *releasecontroller.Release
		expectTags int
		expectSkip int
	}{
		{
			name: "copies spec tags with From references",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{
								Name:      "cli",
								Reference: true,
								From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/cli@sha256:abc"},
							},
							{
								Name:      "installer",
								Reference: true,
								From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/installer@sha256:def"},
							},
						},
					},
				},
			},
			expectTags: 2,
		},
		{
			name: "skips tags without From",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{
								Name:      "has-from",
								Reference: true,
								From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/img@sha256:aaa"},
							},
							{
								Name:      "no-from",
								Reference: true,
							},
						},
					},
				},
			},
			expectTags: 1,
		},
		{
			name: "no tags produces empty mirror",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{
					Spec: imagev1.ImageStreamSpec{},
				},
			},
			expectTags: 0,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			is := &imagev1.ImageStream{}
			if err := calculateReferenceMirrorImageStream(tc.release, is); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(is.Spec.Tags) != tc.expectTags {
				t.Errorf("expected %d tags, got %d", tc.expectTags, len(is.Spec.Tags))
			}
			for _, tag := range is.Spec.Tags {
				if tag.ImportPolicy.Scheduled {
					t.Errorf("tag %q should not have scheduled import", tag.Name)
				}
			}
		})
	}
}

func TestReleaseTagFromForReferenceRelease(t *testing.T) {
	testCases := []struct {
		name       string
		release    *releasecontroller.Release
		expectFrom bool
		expectSpec string
	}{
		{
			name: "reference release sets From with DockerImage pullspec",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Name: "4.18-art-latest", Namespace: "ocp"},
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli", Reference: true, From: &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/cli@sha256:abc"}},
						},
					},
				},
				Target: &imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Name: "release", Namespace: "ocp"},
				},
				Config: &releasecontroller.ReleaseConfig{
					Name: "4.18.0-0.nightly",
					ReferenceRelease: &releasecontroller.ReferenceRelease{
						PushRepository: "quay.io/openshift-release-dev/ocp-release-push",
						PullRepository: "quay.io/openshift-release-dev/ocp-release-pull",
						SecretName:     "quay-secret",
					},
				},
			},
			expectFrom: true,
			expectSpec: "quay.io/openshift-release-dev/ocp-release-pull:" + releasecontroller.ReferencePayloadTagPrefix + "4.18.0-0.nightly-2025-01-15-120000",
		},
		{
			name: "non-reference release does not set From",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Name: "4.18-art-latest", Namespace: "ocp"},
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli"},
						},
					},
					Status: imagev1.ImageStreamStatus{
						Tags: []imagev1.NamedTagEventList{{Tag: "cli", Items: []imagev1.TagEvent{{Image: "sha256:abc"}}}},
					},
				},
				Target: &imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Name: "release", Namespace: "ocp"},
					Status: imagev1.ImageStreamStatus{
						PublicDockerImageRepository: "registry.ci.openshift.org/ocp/release",
					},
				},
				Config: &releasecontroller.ReleaseConfig{
					Name: "4.18.0-0.ci",
				},
			},
			expectFrom: false,
		},
		{
			name: "reference spec tags without ReferenceRelease config does not set From",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Name: "4.18-art-latest", Namespace: "ocp"},
					Spec: imagev1.ImageStreamSpec{
						Tags: []imagev1.TagReference{
							{Name: "cli", Reference: true, From: &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/cli@sha256:abc"}},
						},
					},
				},
				Target: &imagev1.ImageStream{
					ObjectMeta: metav1.ObjectMeta{Name: "release", Namespace: "ocp"},
					Status: imagev1.ImageStreamStatus{
						PublicDockerImageRepository: "registry.ci.openshift.org/ocp/release",
					},
				},
				Config: &releasecontroller.ReleaseConfig{
					Name: "4.18.0-0.ci",
				},
			},
			expectFrom: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tagName := tc.release.Config.Name + "-2025-01-15-120000"

			tag := imagev1.TagReference{
				Name:         tagName,
				Reference:    releasecontroller.HasReferenceSpecTags(tc.release.Source),
				ImportPolicy: imagev1.TagImportPolicy{ImportMode: imagev1.ImportModePreserveOriginal},
			}
			if releasecontroller.IsReferenceRelease(tc.release) {
				tag.From = &corev1.ObjectReference{
					Kind: "DockerImage",
					Name: releasecontroller.ReleasePullSpec(tc.release, tag.Name),
				}
			}

			if tc.expectFrom {
				if tag.From == nil {
					t.Fatal("expected From to be set, got nil")
				}
				if tag.From.Kind != "DockerImage" {
					t.Errorf("expected From.Kind %q, got %q", "DockerImage", tag.From.Kind)
				}
				if tag.From.Name != tc.expectSpec {
					t.Errorf("expected From.Name %q, got %q", tc.expectSpec, tag.From.Name)
				}
			} else {
				if tag.From != nil {
					t.Errorf("expected From to be nil, got %+v", tag.From)
				}
			}
		})
	}
}

func TestBuildReferenceReleaseJob(t *testing.T) {
	mirror := &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "4.17-art-latest-2024-08-30-110931",
			Namespace: "ocp",
			Annotations: map[string]string{
				releasecontroller.ReleaseAnnotationSource:     "ocp/4.17-art-latest",
				releasecontroller.ReleaseAnnotationTarget:     "ocp/release",
				releasecontroller.ReleaseAnnotationReleaseTag: "4.17.0-0.nightly-2024-08-30-110931",
			},
		},
		Spec: imagev1.ImageStreamSpec{
			Tags: []imagev1.TagReference{
				{
					Name:      "cli",
					Reference: true,
					From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/cli@sha256:abc"},
				},
				{
					Name:      "installer",
					Reference: true,
					From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/installer@sha256:def"},
				},
			},
		},
	}

	release := &releasecontroller.Release{
		Source: &imagev1.ImageStream{
			ObjectMeta: metav1.ObjectMeta{Name: "4.17-art-latest", Namespace: "ocp"},
			Spec: imagev1.ImageStreamSpec{
				Tags: []imagev1.TagReference{
					{
						Name:      "cli",
						Reference: true,
						From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/cli@sha256:abc"},
					},
				},
			},
		},
		Target: &imagev1.ImageStream{
			ObjectMeta: metav1.ObjectMeta{Name: "release", Namespace: "ocp", Generation: 5},
		},
		Config: &releasecontroller.ReleaseConfig{
			Name:             "4.17.0-0.nightly",
			OverrideCLIImage: "quay.io/org/cli:latest",
			ReferenceMode:    "source",
			ReferenceRelease: &releasecontroller.ReferenceRelease{
				PushRepository: "quay.io/openshift-release-dev/ocp-release",
				PullRepository: "quay.io/openshift-release-dev/ocp-release",
				SecretName:     "quay-secret",
			},
		},
	}
	releaseName := "4.17.0-0.nightly-2024-08-30-110931"

	t.Run("produces correct job spec", func(t *testing.T) {
		job, err := buildReferenceReleaseJob(release, releaseName, mirror, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if job.Name != releaseName {
			t.Errorf("expected job name %q, got %q", releaseName, job.Name)
		}

		container := job.Spec.Template.Spec.Containers[0]
		if container.Image != "quay.io/org/cli:latest" {
			t.Errorf("expected CLI image %q, got %q", "quay.io/org/cli:latest", container.Image)
		}

		cmd := strings.Join(container.Command, " ")
		if !strings.Contains(cmd, "oc get imagestream") {
			t.Errorf("expected command to contain 'oc get imagestream' fetch step, got: %s", cmd)
		}
		if !strings.Contains(cmd, "awk") {
			t.Errorf("expected command to contain awk status-strip step, got: %s", cmd)
		}
		if !strings.Contains(cmd, "--from-image-stream-file=/tmp/mirror.yaml") {
			t.Errorf("expected command to contain --from-image-stream-file, got: %s", cmd)
		}
		if !strings.Contains(cmd, "--name=$3") {
			t.Errorf("expected command to contain --name=$3, got: %s", cmd)
		}

		expectedToImage := "quay.io/openshift-release-dev/ocp-release:rc_payload__" + releaseName
		if !strings.Contains(cmd, expectedToImage) {
			t.Errorf("expected command args to contain %q, got: %s", expectedToImage, cmd)
		}

		args := container.Command
		if args[4] != mirror.Name {
			t.Errorf("expected $1 arg to be mirror name %q, got %q", mirror.Name, args[4])
		}
		if args[5] != mirror.Namespace {
			t.Errorf("expected $2 arg to be mirror namespace %q, got %q", mirror.Namespace, args[5])
		}
		if args[6] != releaseName {
			t.Errorf("expected $3 arg to be release name %q, got %q", releaseName, args[6])
		}

		// Verify annotations
		if job.Annotations[releasecontroller.ReleaseAnnotationSource] != "ocp/4.17-art-latest" {
			t.Errorf("expected source annotation %q, got %q", "ocp/4.17-art-latest", job.Annotations[releasecontroller.ReleaseAnnotationSource])
		}
		if job.Annotations[releasecontroller.ReleaseAnnotationReleaseTag] != releaseName {
			t.Errorf("expected releaseTag annotation %q, got %q", releaseName, job.Annotations[releasecontroller.ReleaseAnnotationReleaseTag])
		}
	})

	t.Run("uses reference repository secret", func(t *testing.T) {
		job, err := buildReferenceReleaseJob(release, releaseName, mirror, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		foundSecret := false
		for _, vol := range job.Spec.Template.Spec.Volumes {
			if vol.Secret != nil && vol.Secret.SecretName == "quay-secret" {
				foundSecret = true
			}
		}
		if !foundSecret {
			t.Errorf("expected volume with secret %q", "quay-secret")
		}
	})

	t.Run("falls back to pullSecretName when referenceRepositorySecretName is empty", func(t *testing.T) {
		releaseCopy := *release
		configCopy := *release.Config
		configCopy.ReferenceRelease = &releasecontroller.ReferenceRelease{
			PushRepository: configCopy.ReferenceRelease.PushRepository,
			PullRepository: configCopy.ReferenceRelease.PullRepository,
		}
		configCopy.PullSecretName = "pull-secret"
		releaseCopy.Config = &configCopy

		job, err := buildReferenceReleaseJob(&releaseCopy, releaseName, mirror, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		foundSecret := false
		for _, vol := range job.Spec.Template.Spec.Volumes {
			if vol.Secret != nil && vol.Secret.SecretName == "pull-secret" {
				foundSecret = true
			}
		}
		if !foundSecret {
			t.Errorf("expected volume with fallback secret %q", "pull-secret")
		}
	})

	t.Run("falls back to cli spec tag when overrideCLIImage is empty", func(t *testing.T) {
		releaseCopy := *release
		configCopy := *release.Config
		configCopy.OverrideCLIImage = ""
		releaseCopy.Config = &configCopy

		job, err := buildReferenceReleaseJob(&releaseCopy, releaseName, mirror, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		container := job.Spec.Template.Spec.Containers[0]
		if container.Image != "quay.io/org/cli@sha256:abc" {
			t.Errorf("expected CLI image from spec tag %q, got %q", "quay.io/org/cli@sha256:abc", container.Image)
		}
	})

	t.Run("fails when no CLI image source is available", func(t *testing.T) {
		releaseCopy := *release
		configCopy := *release.Config
		configCopy.OverrideCLIImage = ""
		releaseCopy.Config = &configCopy

		mirrorNoCLI := &imagev1.ImageStream{
			ObjectMeta: mirror.ObjectMeta,
			Spec: imagev1.ImageStreamSpec{
				Tags: []imagev1.TagReference{
					{
						Name:      "installer",
						Reference: true,
						From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/installer@sha256:def"},
					},
				},
			},
		}

		_, err := buildReferenceReleaseJob(&releaseCopy, releaseName, mirrorNoCLI, false)
		if err == nil {
			t.Fatal("expected error when no CLI image is available")
		}
		if !strings.Contains(err.Error(), "CLI image") {
			t.Errorf("expected error about CLI image, got: %v", err)
		}
	})

	t.Run("nil ReferenceRelease returns error", func(t *testing.T) {
		releaseCopy := *release
		configCopy := *release.Config
		configCopy.ReferenceRelease = nil
		releaseCopy.Config = &configCopy

		_, err := buildReferenceReleaseJob(&releaseCopy, releaseName, mirror, false)
		if err == nil {
			t.Fatal("expected error when ReferenceRelease is nil")
		}
		if !strings.Contains(err.Error(), "no ReferenceRelease configuration") {
			t.Errorf("expected error about missing ReferenceRelease configuration, got: %v", err)
		}
	})

	t.Run("empty PushRepository returns error", func(t *testing.T) {
		releaseCopy := *release
		configCopy := *release.Config
		configCopy.ReferenceRelease = &releasecontroller.ReferenceRelease{
			PullRepository: configCopy.ReferenceRelease.PullRepository,
			PushRepository: "",
		}
		releaseCopy.Config = &configCopy

		_, err := buildReferenceReleaseJob(&releaseCopy, releaseName, mirror, false)
		if err == nil {
			t.Fatal("expected error when PushRepository is empty")
		}
		if !strings.Contains(err.Error(), "PushRepository is empty") {
			t.Errorf("expected error about empty PushRepository, got: %v", err)
		}
	})

	t.Run("manifest list mode disabled", func(t *testing.T) {
		job, err := buildReferenceReleaseJob(release, releaseName, mirror, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		args := job.Spec.Template.Spec.Containers[0].Command
		// The last arg is the manifest list mode value
		lastArg := args[len(args)-1]
		if lastArg != "false" {
			t.Errorf("expected manifest list mode %q, got %q", "false", lastArg)
		}
	})

	t.Run("manifest list mode enabled", func(t *testing.T) {
		job, err := buildReferenceReleaseJob(release, releaseName, mirror, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		args := job.Spec.Template.Spec.Containers[0].Command
		lastArg := args[len(args)-1]
		if lastArg != "true" {
			t.Errorf("expected manifest list mode %q, got %q", "true", lastArg)
		}
	})

	t.Run("manifest list mode disabled by config override", func(t *testing.T) {
		releaseCopy := *release
		configCopy := *release.Config
		configCopy.DisableManifestListMode = true
		releaseCopy.Config = &configCopy

		job, err := buildReferenceReleaseJob(&releaseCopy, releaseName, mirror, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		args := job.Spec.Template.Spec.Containers[0].Command
		lastArg := args[len(args)-1]
		if lastArg != "false" {
			t.Errorf("expected manifest list mode %q when DisableManifestListMode is set, got %q", "false", lastArg)
		}
	})
}

func TestBuildReferenceRemovalJob(t *testing.T) {
	release := &releasecontroller.Release{
		Source: &imagev1.ImageStream{
			ObjectMeta: metav1.ObjectMeta{Name: "4.22-art-latest", Namespace: "ocp"},
		},
		Target: &imagev1.ImageStream{
			ObjectMeta: metav1.ObjectMeta{Name: "release", Namespace: "ocp", Generation: 3},
		},
		Config: &releasecontroller.ReleaseConfig{
			Name: "4.22.0-0.ci",
			ReferenceRelease: &releasecontroller.ReferenceRelease{
				PushRepository: "quay.io/openshift-release-dev/ocp-release",
				PullRepository: "quay.io/openshift-release-dev/ocp-release",
				SecretName:     "quay-secret",
			},
		},
	}
	releaseName := "4.22.0-0.ci-2026-01-30-070825"
	cliImage := "quay.io/org/cli:latest"

	t.Run("produces correct oc image mirror command", func(t *testing.T) {
		job, err := buildReferenceRemovalJob(release, releaseName, cliImage)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedJobName := "remove-" + releaseName
		if job.Name != expectedJobName {
			t.Errorf("expected job name %q, got %q", expectedJobName, job.Name)
		}

		container := job.Spec.Template.Spec.Containers[0]
		if container.Image != cliImage {
			t.Errorf("expected CLI image %q, got %q", cliImage, container.Image)
		}

		cmd := strings.Join(container.Command, " ")
		if !strings.Contains(cmd, "oc image mirror") {
			t.Errorf("expected command to contain 'oc image mirror', got: %s", cmd)
		}

		expectedFrom := "quay.io/openshift-release-dev/ocp-release:rc_payload__" + releaseName
		if !strings.Contains(cmd, expectedFrom) {
			t.Errorf("expected command to contain from image %q, got: %s", expectedFrom, cmd)
		}

		expectedTo := "quay.io/openshift-release-dev/ocp-release:remove__rc_payload__" + releaseName
		if !strings.Contains(cmd, expectedTo) {
			t.Errorf("expected command to contain to image %q, got: %s", expectedTo, cmd)
		}
	})

	t.Run("uses reference repository secret", func(t *testing.T) {
		job, err := buildReferenceRemovalJob(release, releaseName, cliImage)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		foundSecret := false
		for _, vol := range job.Spec.Template.Spec.Volumes {
			if vol.Secret != nil && vol.Secret.SecretName == "quay-secret" {
				foundSecret = true
			}
		}
		if !foundSecret {
			t.Errorf("expected volume with secret %q", "quay-secret")
		}
	})

	t.Run("falls back to pullSecretName when referenceRepositorySecretName is empty", func(t *testing.T) {
		releaseCopy := *release
		configCopy := *release.Config
		configCopy.ReferenceRelease = &releasecontroller.ReferenceRelease{
			PushRepository: configCopy.ReferenceRelease.PushRepository,
			PullRepository: configCopy.ReferenceRelease.PullRepository,
		}
		configCopy.PullSecretName = "pull-secret"
		releaseCopy.Config = &configCopy

		job, err := buildReferenceRemovalJob(&releaseCopy, releaseName, cliImage)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		foundSecret := false
		for _, vol := range job.Spec.Template.Spec.Volumes {
			if vol.Secret != nil && vol.Secret.SecretName == "pull-secret" {
				foundSecret = true
			}
		}
		if !foundSecret {
			t.Errorf("expected volume with fallback secret %q", "pull-secret")
		}
	})

	t.Run("nil ReferenceRelease returns error", func(t *testing.T) {
		releaseCopy := *release
		configCopy := *release.Config
		configCopy.ReferenceRelease = nil
		releaseCopy.Config = &configCopy

		_, err := buildReferenceRemovalJob(&releaseCopy, releaseName, cliImage)
		if err == nil {
			t.Fatal("expected error when ReferenceRelease is nil")
		}
		if !strings.Contains(err.Error(), "no ReferenceRelease configuration") {
			t.Errorf("expected error about missing ReferenceRelease configuration, got: %v", err)
		}
	})

	t.Run("empty PushRepository returns error", func(t *testing.T) {
		releaseCopy := *release
		configCopy := *release.Config
		configCopy.ReferenceRelease = &releasecontroller.ReferenceRelease{
			PullRepository: configCopy.ReferenceRelease.PullRepository,
			PushRepository: "",
		}
		releaseCopy.Config = &configCopy

		_, err := buildReferenceRemovalJob(&releaseCopy, releaseName, cliImage)
		if err == nil {
			t.Fatal("expected error when PushRepository is empty")
		}
		if !strings.Contains(err.Error(), "PushRepository is empty") {
			t.Errorf("expected error about empty PushRepository, got: %v", err)
		}
	})

	t.Run("from and to images are correctly constructed", func(t *testing.T) {
		job, err := buildReferenceRemovalJob(release, releaseName, cliImage)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		args := job.Spec.Template.Spec.Containers[0].Command
		expectedFrom := "quay.io/openshift-release-dev/ocp-release:rc_payload__" + releaseName
		expectedTo := "quay.io/openshift-release-dev/ocp-release:remove__rc_payload__" + releaseName

		foundFrom := false
		foundTo := false
		for _, arg := range args {
			if arg == expectedFrom {
				foundFrom = true
			}
			if arg == expectedTo {
				foundTo = true
			}
		}
		if !foundFrom {
			t.Errorf("expected from image %q in command args, got: %v", expectedFrom, args)
		}
		if !foundTo {
			t.Errorf("expected to image %q in command args, got: %v", expectedTo, args)
		}
	})
}
