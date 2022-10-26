package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/openshift/release-controller/pkg/prow"
	"strings"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"

	imagev1 "github.com/openshift/api/image/v1"

	prowjobv1 "k8s.io/test-infra/prow/apis/prowjobs/v1"
	prowconfig "k8s.io/test-infra/prow/config"
	prowutil "k8s.io/test-infra/prow/pjutil"
)

func (c *Controller) ensureProwJobForReleaseTag(release *releasecontroller.Release, verifyName, verifySuffix string, verifyType releasecontroller.ReleaseVerification, releaseTag *imagev1.TagReference, previousTag, previousReleasePullSpec string, extraLabels map[string]string) (*unstructured.Unstructured, error) {
	jobName := verifyType.ProwJob.Name
	fullProwJobName := fmt.Sprintf("%s-%s", releaseTag.Name, verifyName)
	prowJobName := prow.GenerateSafeProwJobName(fullProwJobName, verifySuffix)

	isAggregatedJob := verifyType.AggregatedProwJob != nil && verifyType.AggregatedProwJob.AnalysisJobCount > 0
	if isAggregatedJob {
		// Provide a way to override the default aggregator prow job
		if verifyType.AggregatedProwJob.ProwJob != nil && len(verifyType.AggregatedProwJob.ProwJob.Name) > 0 {
			jobName = verifyType.AggregatedProwJob.ProwJob.Name
		} else {
			jobName = defaultAggregateProwJobName
		}
		// Postfix the name to differentiate it from the analysis jobs
		prowJobName = prow.GenerateSafeProwJobName(fullProwJobName, "aggregator")
	}
	obj, exists, err := c.prowLister.GetByKey(fmt.Sprintf("%s/%s", c.prowNamespace, prowJobName))
	if err != nil {
		return nil, err
	}
	if exists {
		// TODO: check metadata on object
		return obj.(*unstructured.Unstructured), nil
	}

	// It is very important that you do not modify anything returned from c.prowConfigLoader.Config():
	// https://github.com/kubernetes/test-infra/blob/a68e557705be5e4b7d3cf45bb4fe1e93b82c5682/prow/config/agent.go#L389
	// These are in-memory copies of the mounted prow configurations and changing them results in subsequent lookups that
	// will contain the updates, and inevitably they will be replaced if/when the config changes.
	config := c.prowConfigLoader.Config()
	if config == nil {
		err := fmt.Errorf("the prow job %s is not valid: no prow jobs have been defined", jobName)
		c.eventRecorder.Event(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", err.Error())
		return nil, releasecontroller.CreateTerminalError(err)
	}
	periodicConfig, ok := hasProwJob(config, jobName)
	if !ok {
		err := fmt.Errorf("the prow job %s is not valid: no job with that name", jobName)
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", err.Error())
		return nil, releasecontroller.CreateTerminalError(err)
	}
	if err := validateProwJob(periodicConfig); err != nil {
		err := fmt.Errorf("the prowjob %s is not valid: %v", jobName, err)
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", err.Error())
		return nil, releasecontroller.CreateTerminalError(err)
	}

	spec := prowutil.PeriodicSpec(*periodicConfig)
	if isAggregatedJob {
		spec.Job = fmt.Sprintf("%s-%s", verifyName, jobName)
	}
	// If the release-controller was given a list of build cluster names to use, this will
	// distribute the prowjobs onto them.  If not, then the jobs will be run on the
	// build cluster defined inside the job itself.
	for _, buildClusterDistribution := range c.buildClusterDistributions {
		if buildClusterDistribution.Contains(periodicConfig.Cluster) {
			spec.Cluster = buildClusterDistribution.Get()
		}
	}
	// Currently, all "metal" jobs must be run on the `build05` cluster
	if strings.Contains(jobName, "metal") {
		spec.Cluster = "build05"
	}
	// Currently, all "telco5g" jobs must be run on the `build05` cluster
	if strings.Contains(jobName, "telco5g") {
		spec.Cluster = "build05"
	}
	mirror, _ := releasecontroller.GetMirror(release, releaseTag.Name, c.releaseLister)
	ok, err = addReleaseEnvToProwJobSpec(&spec, release, mirror, releaseTag, previousReleasePullSpec, verifyType.Upgrade, c.graph.Architecture)
	if err != nil {
		return nil, err
	}
	if isAggregatedJob || verifyType.MultiJobAnalysis {
		status, err := addAnalysisEnvToProwJobSpec(&spec, releaseTag.Name, verifyType.ProwJob.Name)
		if err != nil {
			return nil, err
		}
		ok = ok && status
	}
	pj := prowutil.NewProwJob(spec, extraLabels, map[string]string{
		releasecontroller.ReleaseAnnotationSource: fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
	})
	// Override default UUID naming of prowjob
	pj.Name = prowJobName
	if !ok {
		now := metav1.Now()
		// return a synthetic job to indicate that this test is impossible to run (no spec, or
		// this is an upgrade job and no upgrade is possible)
		pj.Status = prowjobv1.ProwJobStatus{
			StartTime:      now,
			CompletionTime: &now,
			Description:    "Job was not defined or does not have any inputs",
			State:          prowjobv1.SuccessState,
		}
		return objectToUnstructured(&pj), nil
	}

	pj.Annotations[releasecontroller.ReleaseAnnotationToTag] = releaseTag.Name
	if verifyType.Upgrade && len(previousTag) > 0 {
		pj.Annotations[releasecontroller.ReleaseAnnotationFromTag] = previousTag
	}
	pj.Annotations[releasecontroller.ReleaseAnnotationArchitecture] = c.graph.Architecture
	out, err := c.prowClient.Create(context.TODO(), objectToUnstructured(&pj), metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		// find a cached version or do a live call
		job, exists, err := c.prowLister.GetByKey(fmt.Sprintf("%s/%s", c.prowNamespace, prowJobName))
		if err != nil {
			return nil, err
		}
		if exists {
			return job.(*unstructured.Unstructured), nil
		}
		return c.prowClient.Get(context.TODO(), prowJobName, metav1.GetOptions{})
	}
	if errors.IsInvalid(err) {
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", "the prow job %s is not valid: %v", jobName, err)
		return nil, releasecontroller.CreateTerminalError(err)
	}
	if err != nil {
		return nil, err
	}
	klog.V(2).Infof("Created new prow job %s", pj.Name)
	return out, nil
}

func objectToUnstructured(obj runtime.Object) *unstructured.Unstructured {
	buf := &bytes.Buffer{}
	if err := unstructured.UnstructuredJSONScheme.Encode(obj, buf); err != nil {
		panic(err)
	}
	u := &unstructured.Unstructured{}
	if _, _, err := unstructured.UnstructuredJSONScheme.Decode(buf.Bytes(), nil, u); err != nil {
		panic(err)
	}
	return u
}

func addReleaseEnvToProwJobSpec(spec *prowjobv1.ProwJobSpec, release *releasecontroller.Release, mirror *imagev1.ImageStream, releaseTag *imagev1.TagReference, previousReleasePullSpec string, isUpgrade bool, architecture string) (bool, error) {
	if spec.PodSpec == nil {
		// Jenkins jobs cannot be parameterized
		return true, nil
	}
	hasReleaseImage := false
	hasUpgradeImage := false
	for i := range spec.PodSpec.Containers {
		c := &spec.PodSpec.Containers[i]
		for j := range c.Env {
			switch name := c.Env[j].Name; {
			case name == "RELEASE_IMAGE_LATEST":
				hasReleaseImage = true
				c.Env[j].Value = release.Target.Status.PublicDockerImageRepository + ":" + releaseTag.Name
			case name == "RELEASE_IMAGE_INITIAL":
				if len(previousReleasePullSpec) == 0 {
					return false, nil
				}
				hasUpgradeImage = true
				c.Env[j].Value = previousReleasePullSpec
			case name == "IMAGE_FORMAT":
				if mirror == nil {
					return false, fmt.Errorf("unable to determine IMAGE_FORMAT for prow job %s", spec.Job)
				}
				c.Env[j].Value = mirror.Status.PublicDockerImageRepository + ":${component}"
			case strings.HasPrefix(name, "IMAGE_"):
				suffix := strings.TrimPrefix(name, "IMAGE_")
				if len(suffix) == 0 {
					break
				}
				if mirror == nil {
					return false, fmt.Errorf("unable to determine IMAGE_FORMAT for prow job %s", spec.Job)
				}
				suffix = strings.ToLower(strings.Replace(suffix, "_", "-", -1))
				c.Env[j].Value = mirror.Status.PublicDockerImageRepository + ":" + suffix
			}
		}
		if !hasReleaseImage {
			switch architecture {
			case "arm64":
				c.Env = append(c.Env, corev1.EnvVar{Name: "RELEASE_IMAGE_ARM64_LATEST", Value: release.Target.Status.PublicDockerImageRepository + ":" + releaseTag.Name})
			case "s390x":
				c.Env = append(c.Env, corev1.EnvVar{Name: "RELEASE_IMAGE_S390X_LATEST", Value: release.Target.Status.PublicDockerImageRepository + ":" + releaseTag.Name})
			case "ppc64le":
				c.Env = append(c.Env, corev1.EnvVar{Name: "RELEASE_IMAGE_PPC64LE_LATEST", Value: release.Target.Status.PublicDockerImageRepository + ":" + releaseTag.Name})
			default:
				c.Env = append(c.Env, corev1.EnvVar{Name: "RELEASE_IMAGE_LATEST", Value: release.Target.Status.PublicDockerImageRepository + ":" + releaseTag.Name})
			}
		}
		if !isUpgrade {
			// If an initial release is specified in the ci-operator config, ci-operator will always try to pull it. This can cause jobs to fail
			// if there are not at least 2 accepted releases for the release being tested. To prevent this issue, always set RELEASE_IMAGE_INITIAL,
			// even for non-upgrade jobs
			switch architecture {
			case "arm64":
				c.Env = append(c.Env, corev1.EnvVar{Name: "RELEASE_IMAGE_ARM64_INITIAL", Value: release.Target.Status.PublicDockerImageRepository + ":" + releaseTag.Name})
			case "s390x":
				c.Env = append(c.Env, corev1.EnvVar{Name: "RELEASE_IMAGE_S390X_INITIAL", Value: release.Target.Status.PublicDockerImageRepository + ":" + releaseTag.Name})
			case "ppc64le":
				c.Env = append(c.Env, corev1.EnvVar{Name: "RELEASE_IMAGE_PPC64LE_INITIAL", Value: release.Target.Status.PublicDockerImageRepository + ":" + releaseTag.Name})
			default:
				c.Env = append(c.Env, corev1.EnvVar{Name: "RELEASE_IMAGE_INITIAL", Value: release.Target.Status.PublicDockerImageRepository + ":" + releaseTag.Name})
			}
		} else if !hasUpgradeImage {
			if len(previousReleasePullSpec) == 0 {
				return false, nil
			}
			switch architecture {
			case "arm64":
				c.Env = append(c.Env, corev1.EnvVar{Name: "RELEASE_IMAGE_ARM64_INITIAL", Value: previousReleasePullSpec})
			case "s390x":
				c.Env = append(c.Env, corev1.EnvVar{Name: "RELEASE_IMAGE_S390X_INITIAL", Value: previousReleasePullSpec})
			case "ppc64le":
				c.Env = append(c.Env, corev1.EnvVar{Name: "RELEASE_IMAGE_PPC64LE_INITIAL", Value: previousReleasePullSpec})
			default:
				c.Env = append(c.Env, corev1.EnvVar{Name: "RELEASE_IMAGE_INITIAL", Value: previousReleasePullSpec})
			}
		}
	}
	return true, nil
}

// To ensure that we do not modify anything returned from the ProwConfigLoader, I've added a DeepCopy here...
func hasProwJob(config *prowconfig.Config, name string) (*prowconfig.Periodic, bool) {
	for i := range config.Periodics {
		if config.Periodics[i].Name == name {
			return &prowconfig.Periodic{
				JobBase: *config.Periodics[i].DeepCopy(),
			}, true
		}
	}
	return nil, false
}

func validateProwJob(pj *prowconfig.Periodic) error {
	if pj.Cluster == "" || pj.Cluster == prowjobv1.DefaultClusterAlias {
		return fmt.Errorf("the jobs cluster must be set to a value that is not %s, was %q", prowjobv1.DefaultClusterAlias, pj.Cluster)
	}
	return nil
}
