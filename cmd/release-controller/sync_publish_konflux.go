package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	imagev1 "github.com/openshift/api/image/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	"github.com/openshift/release-controller/pkg/prow"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
)

const (
	// konfluxKubeconfigMountPath is where the kubeconfig secret of a konfluxRelease publish
	// step is mounted inside the job that creates the Release.
	konfluxKubeconfigMountPath = "/tmp/konflux"
	// konfluxKubeconfigVolumeName names the kubeconfig volume of that job.
	konfluxKubeconfigVolumeName = "konflux-kubeconfig"
	// konfluxReleaseResource is the fully qualified resource created on the Konflux cluster.
	konfluxReleaseResource = "releases.appstudio.redhat.com"
	// konfluxReleaseAPIVersion is the API version of that resource.
	konfluxReleaseAPIVersion = "appstudio.redhat.com/v1alpha1"
)

// ensureKonfluxRelease creates a Konflux Release for an accepted release tag, exactly once.
//
// Unlike the other publish steps, creating a Konflux Release is not idempotent: creating it
// twice ships the release twice. syncAccepted is level driven and re-enters this function on
// every sync of the stream, so the work is guarded three times over:
//
//  1. An annotation on the image stream tag records the Release once it has been created.
//     This is the authoritative guard; it survives job garbage collection and controller
//     restarts.
//  2. The job itself, which covers the window between creating the job and stamping the
//     annotation.
//  3. The Release is given a deterministic name and the job tolerates AlreadyExists, so even
//     if both of the above are lost a second attempt is a no-op rather than a second release.
//
// Failures are reported as events rather than returned as errors. syncAccepted aggregates
// the errors of its publish steps, and a permanent failure returned from here would requeue
// the stream forever; returning a terminal error does not help either, because
// releasecontroller.IsTerminalError does not unwrap aggregates. The absence of the result
// annotation on an accepted tag is the signal that this step did not run.
func (c *Controller) ensureKonfluxRelease(release *releasecontroller.Release, stepName string, config *releasecontroller.PublishKonfluxRelease, tag *imagev1.TagReference) error {
	annotation := releasecontroller.KonfluxReleaseAnnotation(stepName)
	if len(tag.Annotations[annotation]) > 0 {
		// Steady state: the Release was already created for this tag.
		return nil
	}

	snapshot := releasecontroller.KonfluxSnapshotForTag(tag)
	if len(snapshot) == 0 {
		// The Snapshot is stamped by whatever promoted the image into this stream. There is
		// nothing the controller can do to recover, and retrying cannot help, so report it
		// and move on. The event recorder collapses the repeats into a single counted event.
		klog.Warningf("Release tag %s has no %s annotation, cannot create a Konflux Release for publish step %s", tag.Name, releasecontroller.ReleaseAnnotationKonfluxSnapshot, stepName)
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "KonfluxSnapshotMissing",
			"release tag %s has no %s annotation, cannot create a Konflux Release for publish step %s",
			tag.Name, releasecontroller.ReleaseAnnotationKonfluxSnapshot, stepName)
		return nil
	}

	releaseName := konfluxReleaseName(tag.Name, stepName, snapshot)
	job, err := c.ensureKonfluxReleaseJob(release, config, tag, snapshot, releaseName)
	if err != nil || job == nil {
		return err
	}

	succeeded, complete := jobIsComplete(job)
	switch {
	case !complete:
		// The job carries ReleaseAnnotationSource, so its completion re-enqueues the stream.
		return nil
	case !succeeded:
		klog.Warningf("Konflux release job %s/%s failed for release tag %s", c.jobNamespace, job.Name, tag.Name)
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "KonfluxReleaseFailed",
			"job %s/%s failed to create Konflux Release %s/%s for release tag %s; delete the job to retry",
			c.jobNamespace, job.Name, config.Namespace, releaseName, tag.Name)
		return nil
	}

	if err := c.setReleaseTagAnnotations(release, tag.Name, map[string]string{annotation: releaseName}); err != nil {
		return fmt.Errorf("unable to record Konflux Release %s for %s: %w", releaseName, tag.Name, err)
	}
	klog.V(2).Infof("Created Konflux Release %s/%s for release tag %s", config.Namespace, releaseName, tag.Name)
	c.eventRecorder.Eventf(release.Source, corev1.EventTypeNormal, "KonfluxReleaseCreated",
		"created Konflux Release %s/%s for release tag %s", config.Namespace, releaseName, tag.Name)
	return nil
}

// ensureKonfluxReleaseJob creates the job that applies the Release to the Konflux cluster.
// The job takes the name of the Release it creates, so that the job found here is always the
// job for this Release and no other. See konfluxReleaseName.
//
// No preconditions are passed to ensureJob: it deletes and recreates a job whose annotations
// do not match, and the generation of the stream changes often enough that doing so would
// create the Release more than once.
func (c *Controller) ensureKonfluxReleaseJob(release *releasecontroller.Release, config *releasecontroller.PublishKonfluxRelease, tag *imagev1.TagReference, snapshot, releaseName string) (*batchv1.Job, error) {
	return c.ensureJob(releaseName, nil, func() (*batchv1.Job, error) {
		manifest, err := konfluxReleaseManifest(release, config, tag, snapshot, releaseName)
		if err != nil {
			return nil, err
		}

		// The script prefix of newReleaseJobBase logs in to the internal registry, which this
		// job has no use for, so it is deliberately discarded. The secret is a kubeconfig
		// rather than a pull secret, so it is mounted separately below.
		job, _ := newReleaseJobBase(releaseName, config.CLIImage, "")

		job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: konfluxKubeconfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: config.SecretName},
			},
		})
		container := &job.Spec.Template.Spec.Containers[0]
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      konfluxKubeconfigVolumeName,
			MountPath: konfluxKubeconfigMountPath,
		})
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  "KUBECONFIG",
			Value: konfluxKubeconfigMountPath + "/kubeconfig",
		})

		// An existing Release is treated as success so that a retry can never start a second
		// release pipeline, whether the Release was created by an earlier attempt of this job
		// or by anything else.
		container.Command = []string{
			"/bin/bash", "-c", `
			set -euo pipefail
			if oc -n "$2" get ` + konfluxReleaseResource + ` "$3" >/dev/null 2>&1; then
				echo "Konflux Release $2/$3 already exists, nothing to do"
				exit 0
			fi
			if ! output="$(printf '%s' "$1" | oc -n "$2" create -f - 2>&1)"; then
				echo "${output}"
				echo "${output}" | grep -q "already exists"
				exit 0
			fi
			echo "${output}"
			`,
			"",
			manifest, config.Namespace, releaseName,
		}

		job.Annotations[releasecontroller.ReleaseAnnotationSource] = fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name)
		job.Annotations[releasecontroller.ReleaseAnnotationTarget] = fmt.Sprintf("%s/%s", release.Target.Namespace, release.Target.Name)
		job.Annotations[releasecontroller.ReleaseAnnotationGeneration] = strconv.FormatInt(release.Target.Generation, 10)
		job.Annotations[releasecontroller.ReleaseAnnotationReleaseTag] = tag.Name

		klog.V(2).Infof("Creating Konflux release job %s/%s for %s, releasing snapshot %s via release plan %s/%s", c.jobNamespace, releaseName, tag.Name, snapshot, config.Namespace, config.ReleasePlan)
		return job, nil
	})
}

// konfluxReleaseManifest builds the Release that the job applies. The Konflux API types are
// not vendored: the object is four fields, and vendoring them would pull in a large and fast
// moving dependency tree for no benefit.
func konfluxReleaseManifest(release *releasecontroller.Release, config *releasecontroller.PublishKonfluxRelease, tag *imagev1.TagReference, snapshot, releaseName string) (string, error) {
	data, err := releasecontroller.ExpandKonfluxData(config.Data, map[string]string{
		releasecontroller.KonfluxVarReleaseTag:  tag.Name,
		releasecontroller.KonfluxVarReleaseName: release.Config.Name,
		releasecontroller.KonfluxVarSnapshot:    snapshot,
	})
	if err != nil {
		return "", err
	}

	spec := map[string]any{
		"snapshot":    snapshot,
		"releasePlan": config.ReleasePlan,
	}
	if len(data) > 0 {
		spec["data"] = data
	}

	manifest, err := json.Marshal(map[string]any{
		"apiVersion": konfluxReleaseAPIVersion,
		"kind":       "Release",
		"metadata": map[string]any{
			"name":      releaseName,
			"namespace": config.Namespace,
			// Provenance, for whoever is looking at this Release from the Konflux side.
			"annotations": map[string]string{
				releasecontroller.ReleaseAnnotationSource:     fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
				releasecontroller.ReleaseAnnotationReleaseTag: tag.Name,
			},
		},
		"spec": spec,
	})
	if err != nil {
		return "", fmt.Errorf("unable to serialize Konflux Release for %s: %w", tag.Name, err)
	}
	return string(manifest), nil
}

// konfluxReleaseName returns the name of the Release created for a tag by a publish step, and
// of the job that creates it. The two deliberately share a name: the existence and result of
// the job is taken as evidence about the Release it was built for, so the job has to identify
// exactly what the Release does.
//
// The Snapshot contributes to the hash so that a tag whose Snapshot changes gets its own
// Release, and its own job, rather than inheriting the previous one.
func konfluxReleaseName(tagName, stepName, snapshot string) string {
	base := strings.ToLower(fmt.Sprintf("%s-konflux-%s", tagName, stepName))
	return prow.GenerateSafeProwJobName(base, prow.ProwjobSafeHash(tagName, stepName, snapshot))
}
