// Package pjutil contains helpers for working with ProwJobs.
package apiv1

import (
	"k8s.io/apimachinery/pkg/util/validation"
	"path/filepath"
	"strconv"
	"strings"

	uuid "github.com/satori/go.uuid"
	"github.com/sirupsen/logrus"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewProwJob initializes a ProwJob out of a ProwJobSpec.
func NewProwJob(spec ProwJobSpec, extraLabels, extraAnnotations map[string]string) ProwJob {
	labels, annotations := LabelsAndAnnotationsForSpec(spec, extraLabels, extraAnnotations)

	return ProwJob{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "prow.k8s.io/v1",
			Kind:       "ProwJob",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        uuid.NewV1().String(),
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: spec,
		Status: ProwJobStatus{
			StartTime: metav1.Now(),
			State:     TriggeredState,
		},
	}
}

// PeriodicSpec initializes a ProwJobSpec for a given periodic job.
func PeriodicSpec(p Periodic) ProwJobSpec {
	pjs := specFromJobBase(p.JobBase)
	pjs.Type = PeriodicJob

	return pjs
}

func specFromJobBase(jb JobBase) ProwJobSpec {
	var namespace string
	if jb.Namespace != nil {
		namespace = *jb.Namespace
	}
	return ProwJobSpec{
		Job:             jb.Name,
		Agent:           ProwJobAgent(jb.Agent),
		Cluster:         jb.Cluster,
		Namespace:       namespace,
		MaxConcurrency:  jb.MaxConcurrency,
		ErrorOnEviction: jb.ErrorOnEviction,

		ExtraRefs:        jb.ExtraRefs,
		DecorationConfig: jb.DecorationConfig,

		PodSpec:         jb.Spec,
	}
}

// LabelsAndAnnotationsForSpec returns a minimal set of labels to add to prowjobs or its owned resources.
//
// User-provided extraLabels and extraAnnotations values will take precedence over auto-provided values.
func LabelsAndAnnotationsForSpec(spec ProwJobSpec, extraLabels, extraAnnotations map[string]string) (map[string]string, map[string]string) {
	jobNameForLabel := spec.Job
	if len(jobNameForLabel) > validation.LabelValueMaxLength {
		// TODO(fejta): consider truncating middle rather than end.
		jobNameForLabel = strings.TrimRight(spec.Job[:validation.LabelValueMaxLength], ".-")
		logrus.WithFields(logrus.Fields{
			"job":       spec.Job,
			"key":       ProwJobAnnotation,
			"value":     spec.Job,
			"truncated": jobNameForLabel,
		}).Info("Cannot use full job name, will truncate.")
	}
	labels := map[string]string{
		CreatedByProw:     "true",
		ProwJobTypeLabel:  string(spec.Type),
		ProwJobAnnotation: jobNameForLabel,
	}
	if spec.Type != PeriodicJob && spec.Refs != nil {
		labels[OrgLabel] = spec.Refs.Org
		labels[RepoLabel] = spec.Refs.Repo
		if len(spec.Refs.Pulls) > 0 {
			labels[PullLabel] = strconv.Itoa(spec.Refs.Pulls[0].Number)
		}
	}

	for k, v := range extraLabels {
		labels[k] = v
	}

	// let's validate labels
	for key, value := range labels {
		if errs := validation.IsValidLabelValue(value); len(errs) > 0 {
			// try to use basename of a path, if path contains invalid //
			base := filepath.Base(value)
			if errs := validation.IsValidLabelValue(base); len(errs) == 0 {
				labels[key] = base
				continue
			}
			logrus.WithFields(logrus.Fields{
				"key":    key,
				"value":  value,
				"errors": errs,
			}).Warn("Removing invalid label")
			delete(labels, key)
		}
	}

	annotations := map[string]string{
		ProwJobAnnotation: spec.Job,
	}
	for k, v := range extraAnnotations {
		annotations[k] = v
	}

	return labels, annotations
}
