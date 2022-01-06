package releasecontroller

import (
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	prowjobsv1 "k8s.io/test-infra/prow/apis/prowjobs/v1"
)

func ProwJobVerificationStatus(obj *unstructured.Unstructured) (*VerificationStatus, bool) {
	s, _, err := unstructured.NestedString(obj.Object, "status", "state")
	if err != nil {
		return nil, false
	}
	url, _, _ := unstructured.NestedString(obj.Object, "status", "url")
	var transitionTime string
	var status *VerificationStatus
	switch prowjobsv1.ProwJobState(s) {
	case prowjobsv1.SuccessState:
		transitionTime, _, _ = unstructured.NestedString(obj.Object, "status", "completionTime")
		status = &VerificationStatus{State: ReleaseVerificationStateSucceeded, URL: url}
	case prowjobsv1.FailureState, prowjobsv1.ErrorState, prowjobsv1.AbortedState:
		transitionTime, _, _ = unstructured.NestedString(obj.Object, "status", "completionTime")
		status = &VerificationStatus{State: ReleaseVerificationStateFailed, URL: url}
	case prowjobsv1.TriggeredState, prowjobsv1.PendingState, prowjobsv1.ProwJobState(""):
		transitionTime, _, _ = unstructured.NestedString(obj.Object, "status", "pendingTime")
		if transitionTime == "" {
			transitionTime, _, _ = unstructured.NestedString(obj.Object, "status", "startTime")
		}
		status = &VerificationStatus{State: ReleaseVerificationStatePending, URL: url}
	default:
		klog.Errorf("Unrecognized prow job state %q on job %s", s, obj.GetName())
		return nil, false
	}
	if t, err := time.Parse(time.RFC3339, transitionTime); err == nil {
		trTime := metav1.NewTime(t)
		status.TransitionTime = &trTime
	}
	return status, true
}
