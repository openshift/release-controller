package conditions

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
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

// The following logic is straight from: https://github.com/openshift/library-go/pull/1341
// I'm just copying it here to prevent having to do the vendoring dance and we can pick
// up the new version of library-go when the PR merges.

func SetCondition(conditions *[]metav1.Condition, newCondition metav1.Condition) {
	if conditions == nil {
		conditions = &[]metav1.Condition{}
	}
	existingCondition := FindCondition(*conditions, newCondition.Type)
	if existingCondition == nil {
		newCondition.LastTransitionTime = metav1.NewTime(time.Now())
		*conditions = append(*conditions, newCondition)
		return
	}

	if existingCondition.Status != newCondition.Status {
		existingCondition.Status = newCondition.Status
		existingCondition.LastTransitionTime = metav1.NewTime(time.Now())
	}

	existingCondition.Reason = newCondition.Reason
	existingCondition.Message = newCondition.Message
}

func RemoveCondition(conditions *[]metav1.Condition, conditionType string) {
	if conditions == nil {
		conditions = &[]metav1.Condition{}
	}
	newConditions := []metav1.Condition{}
	for _, condition := range *conditions {
		if condition.Type != conditionType {
			newConditions = append(newConditions, condition)
		}
	}

	*conditions = newConditions
}

func FindCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}

	return nil
}

func IsConditionTrue(conditions []metav1.Condition, conditionType string) bool {
	return IsConditionPresentAndEqual(conditions, conditionType, metav1.ConditionTrue)
}

func IsConditionFalse(conditions []metav1.Condition, conditionType string) bool {
	return IsConditionPresentAndEqual(conditions, conditionType, metav1.ConditionFalse)
}

func IsConditionPresentAndEqual(conditions []metav1.Condition, conditionType string, status metav1.ConditionStatus) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Status == status
		}
	}
	return false
}
