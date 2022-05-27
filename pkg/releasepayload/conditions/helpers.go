package conditions

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ByReleasePayloadConditionType sorts a list of ReleasePayloadConditions by their Type
type ByReleasePayloadConditionType []metav1.Condition

func (in ByReleasePayloadConditionType) Less(i, j int) bool {
	return in[i].Type < in[j].Type
}

func (in ByReleasePayloadConditionType) Len() int {
	return len(in)
}

func (in ByReleasePayloadConditionType) Swap(i, j int) {
	in[i], in[j] = in[j], in[i]
}
