package main

import (
	"bytes"
	"crypto/sha512"
	"encoding/base32"
	"fmt"
	"strings"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	imagev1 "github.com/openshift/api/image/v1"

	prowjobv1 "k8s.io/test-infra/prow/apis/prowjobs/v1"
	prowconfig "k8s.io/test-infra/prow/config"
	prowutil "k8s.io/test-infra/prow/pjutil"
)

func (c *Controller) ensureProwJobForReleaseTag(release *Release, verifyName string, verifyType ReleaseVerification, releaseTag *imagev1.TagReference, previousTag, previousReleasePullSpec string) (*unstructured.Unstructured, error) {
	jobName := verifyType.ProwJob.Name
	prowJobName := fmt.Sprintf("%s-%s", releaseTag.Name, verifyName)
	obj, exists, err := c.prowLister.GetByKey(fmt.Sprintf("%s/%s", c.prowNamespace, prowJobName))
	if err != nil {
		return nil, err
	}
	if exists {
		// TODO: check metadata on object
		return obj.(*unstructured.Unstructured), nil
	}

	config := c.prowConfigLoader.Config()
	if config == nil {
		err := fmt.Errorf("the prow job %s is not valid: no prow jobs have been defined", jobName)
		c.eventRecorder.Event(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", err.Error())
		return nil, terminalError{err}
	}
	periodicConfig, ok := hasProwJob(config, jobName)
	if !ok {
		err := fmt.Errorf("the prow job %s is not valid: no job with that name", jobName)
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", err.Error())
		return nil, terminalError{err}
	}
	if err := validateProwJob(periodicConfig); err != nil {
		err := fmt.Errorf("the prowjob %s is not valid: %v", jobName, err)
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", err.Error())
		return nil, terminalError{err}
	}

	spec := prowutil.PeriodicSpec(*periodicConfig)
	mirror, _ := c.getMirror(release, releaseTag.Name)
	ok, err = addReleaseEnvToProwJobSpec(&spec, release, mirror, releaseTag, previousReleasePullSpec, "")
	if err != nil {
		return nil, err
	}
	pj := prowutil.NewProwJob(spec, map[string]string{
		"release.openshift.io/verify": "true",
	}, map[string]string{
		releaseAnnotationSource: fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
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

	pj.Annotations[releaseAnnotationToTag] = releaseTag.Name
	if verifyType.Upgrade && len(previousTag) > 0 {
		pj.Annotations[releaseAnnotationFromTag] = previousTag
	}
	out, err := c.prowClient.Create(objectToUnstructured(&pj), metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		// find a cached version or do a live call
		job, exists, err := c.prowLister.GetByKey(fmt.Sprintf("%s/%s", c.prowNamespace, prowJobName))
		if err != nil {
			return nil, err
		}
		if exists {
			return job.(*unstructured.Unstructured), nil
		}
		return c.prowClient.Get(prowJobName, metav1.GetOptions{})
	}
	if errors.IsInvalid(err) {
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", "the prow job %s is not valid: %v", jobName, err)
		return nil, terminalError{err}
	}
	if err != nil {
		return nil, err
	}
	glog.V(2).Infof("Created new prow job %s", pj.Name)
	return out, nil
}

func (c *Controller) ensureProwJobForAdditionalTest(release *Release, testName string, testType ReleaseAdditionalTest, releaseTag *imagev1.TagReference) (*unstructured.Unstructured, error) {
	jobName := testType.ProwJob.Name
	prowJobName := fmt.Sprintf("%s-%s", releaseTag.Name, testName)
	obj, exists, err := c.prowLister.GetByKey(fmt.Sprintf("%s/%s", c.prowNamespace, prowJobName))
	if err != nil {
		return nil, err
	}
	if exists {
		// TODO: check metadata on object
		return obj.(*unstructured.Unstructured), nil
	}

	config := c.prowConfigLoader.Config()
	if config == nil {
		err := fmt.Errorf("the prow job %s is not valid: no prow jobs have been defined", jobName)
		c.eventRecorder.Event(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", err.Error())
		return nil, terminalError{err}
	}
	periodicConfig, ok := hasProwJob(config, jobName)
	if !ok {
		err := fmt.Errorf("the prow job %s is not valid: no job with that name", jobName)
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", err.Error())
		return nil, terminalError{err}
	}

	spec := prowutil.PeriodicSpec(*periodicConfig)
	mirror, _ := c.getMirror(release, releaseTag.Name)
	previousReleasePullSpec := ""
	previousTag := ""
	if testType.Upgrade {
		previousReleasePullSpec = testType.UpgradeRef
		previousTag = testType.UpgradeTag
	}
	ok, err = addReleaseEnvToProwJobSpec(&spec, release, mirror, releaseTag, previousReleasePullSpec, previousTag)
	if err != nil {
		return nil, err
	}
	pj := prowutil.NewProwJob(spec, map[string]string{
		"release.openshift.io/verify": "true",
	}, map[string]string{
		releaseAnnotationSource: fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
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

	pj.Annotations[releaseAnnotationToTag] = releaseTag.Name
	if testType.Upgrade && len(previousTag) > 0 {
		pj.Annotations[releaseAnnotationFromTag] = previousTag
	}
	out, err := c.prowClient.Create(objectToUnstructured(&pj), metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		// find a cached version or do a live call
		job, exists, err := c.prowLister.GetByKey(fmt.Sprintf("%s/%s", c.prowNamespace, prowJobName))
		if err != nil {
			return nil, err
		}
		if exists {
			return job.(*unstructured.Unstructured), nil
		}
		return c.prowClient.Get(prowJobName, metav1.GetOptions{})
	}
	if errors.IsInvalid(err) {
		c.eventRecorder.Eventf(release.Source, corev1.EventTypeWarning, "ProwJobInvalid", "the prow job %s is not valid: %v", jobName, err)
		return nil, terminalError{err}
	}
	if err != nil {
		return nil, err
	}
	glog.V(2).Infof("Created new prow job %s", pj.Name)
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

func addReleaseEnvToProwJobSpec(spec *prowjobv1.ProwJobSpec, release *Release, mirror *imagev1.ImageStream, releaseTag *imagev1.TagReference, previousReleasePullSpec, previousTag string) (bool, error) {
// Generate the annotations, labels and container env for a prowjob
	if spec.PodSpec == nil {
		// Jenkins jobs cannot be parameterized
		return true, nil
	}
	for i := range spec.PodSpec.Containers {
		c := &spec.PodSpec.Containers[i]
		for j := range c.Env {
			switch name := c.Env[j].Name; {
			case name == "RELEASE_IMAGE_LATEST":
				c.Env[j].Value = release.Target.Status.PublicDockerImageRepository + ":" + releaseTag.Name
			case name == "RELEASE_IMAGE_INITIAL":
				if len(previousReleasePullSpec) == 0 {
					return false, nil
				}
				c.Env[j].Value = previousReleasePullSpec
			case name == releaseAnnotationFromTag:
				if len(previousTag) != 0 {
					c.Env[j].Value = previousTag
				}
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
	}
	return true, nil
}

func hasProwJob(config *prowconfig.Config, name string) (*prowconfig.Periodic, bool) {
	for i := range config.Periodics {
		if config.Periodics[i].Name == name {
			return &config.Periodics[i], true
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

// oneWayEncoding can be used to encode hex to a 62-character set (0 and 1 are duplicates) for use in
// short display names that are safe for use in kubernetes as resource names.
var oneWayNameEncoding = base32.NewEncoding("bcdfghijklmnpqrstvwxyz0123456789").WithPadding(base32.NoPadding)

func namespaceSafeHash(values ...string) string {
	hash := sha512.New()

	// the inputs form a part of the hash
	for _, s := range values {
		hash.Write([]byte(s))
	}

	// Object names can't be too long so we truncate
	// the hash. This increases chances of collision
	// but we can tolerate it as our input space is
	// tiny.
	return oneWayNameEncoding.EncodeToString(hash.Sum(nil)[:])
}
