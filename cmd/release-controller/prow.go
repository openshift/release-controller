package main

import (
	"github.com/openshift/release-controller/pkg/release-controller"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	prowjobsv1 "k8s.io/test-infra/prow/apis/prowjobs/v1"
)

func prowJobVerificationStatus(obj *unstructured.Unstructured) (*release_controller.VerificationStatus, bool) {
	s, _, err := unstructured.NestedString(obj.Object, "status", "state")
	if err != nil {
		return nil, false
	}
	url, _, _ := unstructured.NestedString(obj.Object, "status", "url")
	var transitionTime string
	var status *release_controller.VerificationStatus
	switch prowjobsv1.ProwJobState(s) {
	case prowjobsv1.SuccessState:
		transitionTime, _, _ = unstructured.NestedString(obj.Object, "status", "completionTime")
		status = &release_controller.VerificationStatus{State: release_controller.ReleaseVerificationStateSucceeded, URL: url}
	case prowjobsv1.FailureState, prowjobsv1.ErrorState, prowjobsv1.AbortedState:
		transitionTime, _, _ = unstructured.NestedString(obj.Object, "status", "completionTime")
		status = &release_controller.VerificationStatus{State: release_controller.ReleaseVerificationStateFailed, URL: url}
	case prowjobsv1.TriggeredState, prowjobsv1.PendingState, prowjobsv1.ProwJobState(""):
		transitionTime, _, _ = unstructured.NestedString(obj.Object, "status", "pendingTime")
		if transitionTime == "" {
			transitionTime, _, _ = unstructured.NestedString(obj.Object, "status", "startTime")
		}
		status = &release_controller.VerificationStatus{State: release_controller.ReleaseVerificationStatePending, URL: url}
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
