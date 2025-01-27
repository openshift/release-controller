package releasecontroller

import (
	"fmt"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	v1alpha2 "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"strings"
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

func GetReleasePhase(payload *v1alpha1.ReleasePayload) string {
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
		}
	}
	return ""
}
