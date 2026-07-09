package releasecontroller

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"

	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	v1alpha2 "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
)

func ApprovedReleasesByTeam(lister v1alpha2.ReleasePayloadLister, team string) ([]*v1alpha1.ReleasePayload, error) {
	payloads, err := lister.List(labels.SelectorFromSet(labels.Set{fmt.Sprintf("release.openshift.io/%s_state", strings.ToLower(team)): "Accepted"}))
	if err != nil {
		return nil, err
	}
	return payloads, nil
}

func ApprovedReleases(lister v1alpha2.ReleasePayloadLister) ([]*v1alpha1.ReleasePayload, error) {
	payloads, err := lister.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	var approved []*v1alpha1.ReleasePayload
	for _, payload := range payloads {
		for label, value := range payload.Labels {
			if strings.HasPrefix(label, "release.openshift.io/") && strings.HasSuffix(label, "_state") && value == "Accepted" {
				approved = append(approved, payload)
				break
			}
		}
	}
	return approved, nil
}

func PopulatePayloadPhases(release *Release, lister v1alpha2.ReleasePayloadNamespaceLister) {
	if phases := BuildPayloadPhases(lister); phases != nil {
		release.PayloadPhases = phases
	}
}

func BuildPayloadPhases(lister v1alpha2.ReleasePayloadNamespaceLister) map[string]string {
	if lister == nil {
		return nil
	}
	payloads, err := lister.List(labels.Everything())
	if err != nil {
		klog.V(4).Infof("Unable to list ReleasePayloads: %v", err)
		return nil
	}
	phases := make(map[string]string, len(payloads))
	for _, p := range payloads {
		phases[p.Name] = GetReleasePhase(p)
	}
	return phases
}

func GetReleasePhase(payload *v1alpha1.ReleasePayload) string {
	created := false
	for _, condition := range payload.Status.Conditions {
		switch condition.Type {
		case v1alpha1.ConditionPayloadAccepted:
			if condition.Status == v1.ConditionTrue {
				return ReleasePhaseAccepted
			}
		case v1alpha1.ConditionPayloadRejected:
			if condition.Status == v1.ConditionTrue {
				return ReleasePhaseRejected
			}
		case v1alpha1.ConditionPayloadFailed:
			if condition.Status == v1.ConditionTrue {
				return ReleasePhaseFailed
			}
		case v1alpha1.ConditionPayloadCreated:
			if condition.Status == v1.ConditionTrue {
				created = true
			}
		}
	}
	if created {
		return ReleasePhaseReady
	}
	return ReleasePhasePending
}
