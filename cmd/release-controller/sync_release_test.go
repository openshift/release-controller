package main

import (
	"testing"

	v1 "github.com/openshift/api/image/v1"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEnsureRemoveTagJob(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		payload     *v1alpha1.ReleasePayload
		release     *releasecontroller.Release
		expectedJob *batchv1.Job
		expectedErr error
	}{
		{
			name: "Example",
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "5.0.0-0.ci-2026-01-21-185246",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCoordinates: v1alpha1.PayloadCoordinates{
						Namespace:          "ocp",
						ImagestreamName:    "release-5",
						ImagestreamTagName: "5.0.0-0.ci-2026-01-21-185246",
					},
				},
			},
			release: &releasecontroller.Release{
				Source: &v1.ImageStream{},
				Target: &v1.ImageStream{},
				Config: &releasecontroller.ReleaseConfig{
					AlternateImageRepository:           "quay.io/openshift-release-dev/dev-release",
					AlternateImageRepositorySecretName: "release-controller-quay-mirror-secret",
				},
			},
			expectedJob: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "5.0.0-0.ci-2026-01-21-185246-remove-tag",
					Annotations: map[string]string{
						"release.openshift.io/releaseTag": "5.0.0-0.ci-2026-01-21-185246",
						"release.openshift.io/target":     "ocp/release-5",
					},
				},
				Spec: batchv1.JobSpec{
					Parallelism:  releasecontroller.Int32p(1),
					BackoffLimit: releasecontroller.Int32p(3),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							ServiceAccountName: "builder",
							RestartPolicy:      corev1.RestartPolicyNever,
							Containers: []corev1.Container{
								{
									Name:  "build",
									Image: "registry.ci.openshift.org/ocp/4.21:cli",

									ImagePullPolicy: corev1.PullAlways,

									Env: []corev1.EnvVar{
										{Name: "HOME", Value: "/tmp"},
										{Name: "XDG_RUNTIME_DIR", Value: "/tmp/run"},
									},
									Command: []string{
										"/bin/bash",
										"-c",
										"\n\t\t\tset -eu\n\t\t\tmkdir $HOME/.docker/\n\t\t\tmkdir -p \"${XDG_RUNTIME_DIR}\"\n\t\t\tcp -Lf /tmp/pull-secret/* $HOME/.docker/\n\t\t\toc registry login --to $HOME/.docker/config.json\n\t\t\t\n\t\t\toc image mirror --keep-manifest-list=true $1 $2\n\t\t\t",
										"",
										"quay.io/openshift-release-dev/dev-release:5.0.0-0.ci-2026-01-21-185246",
										"quay.io/openshift-release-dev/dev-release:remove__rc_payload__5.0.0-0.ci-2026-01-21-185246",
									},
									TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "pull-secret",
											MountPath: "/tmp/pull-secret",
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "pull-secret",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "release-controller-quay-mirror-secret",
										},
									},
								},
							},
						},
					},
				},
				Status: batchv1.JobStatus{},
			},
			expectedErr: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			//job, err := ensureRemoveTagJob(testCase.payload, testCase.release)
			//
			//if !errors.Is(err, testCase.expectedErr) {
			//	t.Fatalf("ensureRemoveTagJob got err: %v, wanted err: %v", err, testCase.expectedErr)
			//}
			//
			//if !reflect.DeepEqual(job, testCase.expectedJob) {
			//	t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expectedJob, job)
			//}
			//
			//b, err := json.MarshalIndent(job, "", "  ")
			//if err != nil {
			//	t.Fatalf("unable to marshal output: %v", err)
			//}
			//
			//fmt.Println(string(b))
		})
	}
}
