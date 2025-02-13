package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kv1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog"

	imagev1 "github.com/openshift/api/image/v1"
)

func (c *Controller) ensureReleaseJob(release *releasecontroller.Release, name string, mirror *imagev1.ImageStream) (*batchv1.Job, error) {
	return c.ensureJob(name, nil, func() (*batchv1.Job, error) {
		toImage := fmt.Sprintf("%s:%s", release.Target.Status.PublicDockerImageRepository, name)
		cliImage := fmt.Sprintf("%s:cli", mirror.Status.DockerImageRepository)
		if len(release.Config.OverrideCLIImage) > 0 {
			cliImage = release.Config.OverrideCLIImage
		}

		job, prefix := newReleaseJobBase(name, cliImage, release.Config.PullSecretName)

		manifestListMode := "false"
		if c.manifestListMode && !release.Config.DisableManifestListMode {
			manifestListMode = "true"
		}

		job.Spec.Template.Spec.Containers[0].Command = []string{
			"/bin/bash", "-c",
			prefix + `
			oc adm release new "--name=$1" "--from-image-stream=$2" "--namespace=$3" "--to-image=$4" "--reference-mode=$5" "--keep-manifest-list=$6"
			`,
			"",
			name, mirror.Name, mirror.Namespace, toImage, release.Config.ReferenceMode, manifestListMode,
		}

		job.Annotations[releasecontroller.ReleaseAnnotationSource] = mirror.Annotations[releasecontroller.ReleaseAnnotationSource]
		job.Annotations[releasecontroller.ReleaseAnnotationTarget] = mirror.Annotations[releasecontroller.ReleaseAnnotationTarget]
		job.Annotations[releasecontroller.ReleaseAnnotationGeneration] = strconv.FormatInt(release.Target.Generation, 10)
		job.Annotations[releasecontroller.ReleaseAnnotationReleaseTag] = mirror.Annotations[releasecontroller.ReleaseAnnotationReleaseTag]

		klog.V(2).Infof("Running release creation job %s/%s for %s", c.jobNamespace, job.Name, name)
		return job, nil
	})
}

func (c *Controller) ensureRewriteJob(release *releasecontroller.Release, name string, mirror *imagev1.ImageStream, metadataJSON string) (*batchv1.Job, error) {
	ref := releasecontroller.FindTagReference(release.Source, name)
	generation := *ref.Generation
	preconditions := map[string]string{
		releasecontroller.ReleaseAnnotationGeneration: strconv.FormatInt(generation, 10),
	}
	return c.ensureJob(name, preconditions, func() (*batchv1.Job, error) {
		toImage := fmt.Sprintf("%s:%s", release.Source.Status.PublicDockerImageRepository, name)
		cliImage := fmt.Sprintf("%s:cli", mirror.Status.DockerImageRepository)
		if len(release.Config.OverrideCLIImage) > 0 {
			cliImage = release.Config.OverrideCLIImage
		}

		job, prefix := newReleaseJobBase(name, cliImage, release.Config.PullSecretName)

		manifestListMode := "false"
		if c.manifestListMode && !release.Config.DisableManifestListMode {
			manifestListMode = "true"
		}

		container := job.Spec.Template.Spec.Containers[0]

		// load the release image's cli image to status message if necessary
		if len(release.Config.OverrideCLIImage) == 0 {
			job.Spec.Template.Spec.InitContainers = append(job.Spec.Template.Spec.InitContainers, container)
			init0 := &job.Spec.Template.Spec.InitContainers[len(job.Spec.Template.Spec.InitContainers)-1]
			init0.Name = "image-cli"
			init0.Image = toImage
			init0.TerminationMessagePolicy = corev1.TerminationMessageReadFile
			init0.Command = []string{"/bin/sh", "-c", "cluster-version-operator image cli > /dev/termination-log"}
		}

		// mirror the release image contents to the local stream
		job.Spec.Template.Spec.InitContainers = append(job.Spec.Template.Spec.InitContainers, container)
		init1 := &job.Spec.Template.Spec.InitContainers[len(job.Spec.Template.Spec.InitContainers)-1]
		init1.Name = "mirror"
		init1.Command = []string{
			"/bin/bash", "-c",
			prefix + `
			oc adm release mirror "$1" --to-image-stream="$2" "--namespace=$3" "--keep-manifest-list=$4"
			`,
			"",
			toImage, mirror.Name, mirror.Namespace, manifestListMode,
		}
		// rebuild the payload using the provided metadata
		if _, ok := ref.Annotations[releasecontroller.ReleaseAnnotationMirrorImages]; ok {
			job.Spec.Template.Spec.Containers[0].Command = []string{
				"/bin/bash", "-c",
				prefix + `
			oc adm release new "--name=$1" "--from-image-stream=$2" "--namespace=$3" --to-image="$4" "--reference-mode=$5" "--metadata=$6" "--keep-manifest-list=$7"
			`,
				"",
				name, mirror.Name, mirror.Namespace, toImage, release.Config.ReferenceMode, metadataJSON, manifestListMode,
			}
		} else {
			job.Spec.Template.Spec.Containers[0].Command = []string{
				"/bin/bash", "-c",
				prefix + `
			oc adm release new "--name=$1" "--from-release=$2" --to-image="$3" "--reference-mode=$4" "--metadata=$5" "--keep-manifest-list=$6"
			`,
				"",
				name, toImage, toImage, release.Config.ReferenceMode, metadataJSON, manifestListMode,
			}
		}

		job.Annotations[releasecontroller.ReleaseAnnotationSource] = mirror.Annotations[releasecontroller.ReleaseAnnotationSource]
		job.Annotations[releasecontroller.ReleaseAnnotationTarget] = mirror.Annotations[releasecontroller.ReleaseAnnotationTarget]
		job.Annotations[releasecontroller.ReleaseAnnotationGeneration] = strconv.FormatInt(generation, 10)
		job.Annotations[releasecontroller.ReleaseAnnotationReleaseTag] = mirror.Annotations[releasecontroller.ReleaseAnnotationReleaseTag]

		klog.V(2).Infof("Running release rewrite job for %s", name)
		return job, nil
	})
}

func (c *Controller) ensureImportJob(release *releasecontroller.Release, name string, mirror *imagev1.ImageStream) (*batchv1.Job, error) {
	generation := *releasecontroller.FindTagReference(release.Source, name).Generation
	preconditions := map[string]string{
		releasecontroller.ReleaseAnnotationGeneration: strconv.FormatInt(generation, 10),
	}
	return c.ensureJob(name, preconditions, func() (*batchv1.Job, error) {
		toImage := fmt.Sprintf("%s:%s", release.Source.Status.PublicDockerImageRepository, name)
		cliImage := fmt.Sprintf("%s:cli", mirror.Status.DockerImageRepository)
		if len(release.Config.OverrideCLIImage) > 0 {
			cliImage = release.Config.OverrideCLIImage
		}

		job, prefix := newReleaseJobBase(name, cliImage, release.Config.PullSecretName)

		manifestListMode := "false"
		if c.manifestListMode && !release.Config.DisableManifestListMode {
			manifestListMode = "true"
		}

		container := job.Spec.Template.Spec.Containers[0]

		// load the release image's cli image to status message if necessary
		if len(release.Config.OverrideCLIImage) == 0 {
			job.Spec.Template.Spec.InitContainers = append(job.Spec.Template.Spec.InitContainers, container)
			init0 := &job.Spec.Template.Spec.InitContainers[0]
			init0.Name = "image-cli"
			init0.Image = toImage
			init0.TerminationMessagePolicy = corev1.TerminationMessageReadFile
			init0.Command = []string{"/bin/sh", "-c", "cluster-version-operator image cli > /dev/termination-log"}
		}

		// copy the contents of the release to the mirror
		job.Spec.Template.Spec.Containers[0].Command = []string{
			"/bin/bash", "-c",
			prefix + `
			oc adm release mirror "$1" --to-image-stream="$2" "--namespace=$3" "--keep-manifest-list=$4"
			`,
			"",
			toImage, mirror.Name, mirror.Namespace, manifestListMode,
		}

		job.Annotations[releasecontroller.ReleaseAnnotationSource] = mirror.Annotations[releasecontroller.ReleaseAnnotationSource]
		job.Annotations[releasecontroller.ReleaseAnnotationTarget] = mirror.Annotations[releasecontroller.ReleaseAnnotationTarget]
		job.Annotations[releasecontroller.ReleaseAnnotationGeneration] = strconv.FormatInt(generation, 10)
		job.Annotations[releasecontroller.ReleaseAnnotationReleaseTag] = mirror.Annotations[releasecontroller.ReleaseAnnotationReleaseTag]

		klog.V(2).Infof("Running release import job for %s", name)
		return job, nil
	})
}

func (c *Controller) ensureJob(name string, preconditions map[string]string, createFn func() (*batchv1.Job, error)) (*batchv1.Job, error) {
	// Request the deletion of any underlying pods as well...
	policy := metav1.DeletePropagationBackground
	job, err := c.jobLister.Jobs(c.jobNamespace).Get(name)
	if err == nil {
		for k, v := range preconditions {
			if job.Annotations[k] != v {
				klog.V(2).Infof("Job %s doesn't match precondition %s: %s != %s, deleting and recreating", job.Name, k, v, job.Annotations[k])
				err = c.jobClient.Jobs(c.jobNamespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &job.UID}, PropagationPolicy: &policy})
				return nil, err
			}
		}
		return job, nil
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}

	job, err = createFn()
	if err != nil {
		return nil, err
	}

	for k, v := range preconditions {
		if job.Annotations[k] != v {
			return nil, fmt.Errorf("job %s doesn't match provided preconditions, programmer error: %v", job.Name, preconditions)
		}
	}

	job, err = c.jobClient.Jobs(c.jobNamespace).Create(context.TODO(), job, metav1.CreateOptions{})
	if err == nil {
		return job, nil
	}
	if !errors.IsAlreadyExists(err) {
		return nil, err
	}

	// perform a live lookup if we are racing to create the job
	return c.jobClient.Jobs(c.jobNamespace).Get(context.TODO(), name, metav1.GetOptions{})
}

func (c *Controller) ensureRewriteJobImageRetrieved(release *releasecontroller.Release, job *batchv1.Job, mirror *imagev1.ImageStream) error {
	if releasecontroller.FindTagReference(mirror, "cli") != nil {
		return nil
	}
	defer c.queue.AddAfter(queueKey{namespace: release.Source.Namespace, name: release.Source.Name}, 10*time.Second)

	if job.Status.Active == 0 {
		klog.V(4).Infof("Deferring pod lookup for %s - no active pods", job.Name)
		return nil
	}
	statuses, err := findJobContainerStatus(c.podClient, job, "status.phase=Pending", "image-cli")
	if err != nil {
		return nil
	}
	var imageSpec string
	for _, status := range statuses {
		if status.State.Terminated == nil || status.State.Terminated.ExitCode != 0 || len(status.State.Terminated.Message) == 0 {
			continue
		}
		imageSpec = status.State.Terminated.Message
		break
	}
	if len(imageSpec) == 0 {
		klog.V(4).Infof("No image spec published yet for %s", job.Name)
		return nil
	}

	mirror = mirror.DeepCopy()
	mirror.Spec.Tags = append(mirror.Spec.Tags, imagev1.TagReference{
		Name: "cli",
		From: &corev1.ObjectReference{Kind: "DockerImage", Name: imageSpec},
	})

	if _, err := c.imageClient.ImageStreams(mirror.Namespace).Update(context.TODO(), mirror, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("unable to save \"cli\" image %q to the mirror: %v", imageSpec, err)
	}
	return nil
}

func findJobContainerStatus(podClient kv1core.PodsGetter, job *batchv1.Job, fieldSelector string, containerName string) ([]*corev1.ContainerStatus, error) {
	pods, err := podClient.Pods(job.Namespace).List(context.TODO(), metav1.ListOptions{
		FieldSelector: fieldSelector,
		LabelSelector: labels.SelectorFromSet(labels.Set{"controller-uid": string(job.UID)}).String(),
	})
	if err != nil || len(pods.Items) == 0 {
		klog.V(4).Infof("No pods for job %s: %v", job.Name, err)
		return nil, err
	}
	containerStatus := make([]*corev1.ContainerStatus, 0, len(pods.Items))
	for _, item := range pods.Items {
		if status := findContainerStatus(item.Status.InitContainerStatuses, containerName); status != nil {
			containerStatus = append(containerStatus, status)
			continue
		}
		if status := findContainerStatus(item.Status.ContainerStatuses, containerName); status != nil {
			containerStatus = append(containerStatus, status)
			continue
		}
	}
	return containerStatus, nil
}

func findContainerStatus(statuses []corev1.ContainerStatus, name string) *corev1.ContainerStatus {
	for i := range statuses {
		if name == statuses[i].Name {
			return &statuses[i]
		}
	}
	return nil
}

func newReleaseJobBase(name, cliImage, pullSecretName string) (*batchv1.Job, string) {
	var prefix string
	if len(pullSecretName) > 0 {
		prefix = `
			set -eu
			mkdir $HOME/.docker/
			mkdir -p "${XDG_RUNTIME_DIR}"
			cp -Lf /tmp/pull-secret/* $HOME/.docker/
			oc registry login --to $HOME/.docker/config.json
			`
	} else {
		prefix = `
			set -eu
			mkdir -p "${XDG_RUNTIME_DIR}"
			oc registry login
			`
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{},
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
							Image: cliImage,

							ImagePullPolicy: corev1.PullAlways,

							Env: []corev1.EnvVar{
								{Name: "HOME", Value: "/tmp"},
								{Name: "XDG_RUNTIME_DIR", Value: "/tmp/run"},
							},
							TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
						},
					},
				},
			},
		},
	}
	if len(pullSecretName) > 0 {
		job.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "pull-secret",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: pullSecretName,
					},
				},
			},
		}
		job.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{
				Name:      "pull-secret",
				MountPath: "/tmp/pull-secret",
			},
		}
	}
	return job, prefix
}

func jobIsComplete(job *batchv1.Job) (succeeded bool, complete bool) {
	if job.Status.CompletionTime != nil {
		return true, true
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return false, true
		}
	}
	return false, false
}

// ensureReleaseMirrorJob Runs job to mirror release to the Alternate Image Repository (i.e. quay.io)
// Command should be like:
// $ oc image mirror --keep-manifest-list=true registry.ci.openshift.org/ocp/release:4.17.0-0.ci-2024-08-30-110931 quay.io/openshift-release-dev/dev-release:4.17.0-0.ci-2024-08-30-110931
func (c *Controller) ensureReleaseMirrorJob(release *releasecontroller.Release, name string, mirror *imagev1.ImageStream) (*batchv1.Job, error) { //nolint:unused
	return c.ensureJob(name, nil, func() (*batchv1.Job, error) {
		fromImage := fmt.Sprintf("%s:%s", release.Target.Status.PublicDockerImageRepository, name)
		toImage := fmt.Sprintf("%s:%s", release.Config.AlternateImageRepository, name)

		cliImage := fmt.Sprintf("%s:cli", mirror.Status.DockerImageRepository)
		if len(release.Config.OverrideCLIImage) > 0 {
			cliImage = release.Config.OverrideCLIImage
		}

		job, prefix := newReleaseJobBase(name, cliImage, release.Config.PullSecretName)

		manifestListMode := "false"
		if c.manifestListMode && !release.Config.DisableManifestListMode {
			manifestListMode = "true"
		}

		job.Spec.Template.Spec.Containers[0].Command = []string{
			"/bin/bash", "-c",
			prefix + `
			oc image mirror "--keep-manifest-list=$1 $2 $3"
			`,
			"",
			manifestListMode, fromImage, toImage,
		}

		job.Annotations[releasecontroller.ReleaseAnnotationSource] = mirror.Annotations[releasecontroller.ReleaseAnnotationSource]
		job.Annotations[releasecontroller.ReleaseAnnotationTarget] = mirror.Annotations[releasecontroller.ReleaseAnnotationTarget]
		job.Annotations[releasecontroller.ReleaseAnnotationGeneration] = strconv.FormatInt(release.Target.Generation, 10)
		job.Annotations[releasecontroller.ReleaseAnnotationReleaseTag] = mirror.Annotations[releasecontroller.ReleaseAnnotationReleaseTag]

		klog.V(2).Infof("Running release mirror job %s/%s for %s to %s", c.jobNamespace, job.Name, name, toImage)
		return job, nil
	})
}
