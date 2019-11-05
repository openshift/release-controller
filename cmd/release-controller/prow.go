package main

import (
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	prowjobsv1 "k8s.io/test-infra/prow/apis/prowjobs/v1"
)

func prowJobVerificationStatus(obj *unstructured.Unstructured) (*VerificationStatus, bool) {
	s, _, err := unstructured.NestedString(obj.Object, "status", "state")
	if err != nil {
		return nil, false
	}
	url, _, _ := unstructured.NestedString(obj.Object, "status", "url")
	var transitionTime string
	var status *VerificationStatus
	switch prowapiv1.ProwJobState(s) {
	case prowapiv1.SuccessState:
		transitionTime, _, _ = unstructured.NestedString(obj.Object, "status", "completionTime")
		status = &VerificationStatus{State: releaseVerificationStateSucceeded, URL: url}
	case prowapiv1.FailureState, prowapiv1.ErrorState, prowapiv1.AbortedState:
		transitionTime, _, _ = unstructured.NestedString(obj.Object, "status", "completionTime")
		status = &VerificationStatus{State: releaseVerificationStateFailed, URL: url}
	case prowapiv1.TriggeredState, prowapiv1.PendingState, prowapiv1.ProwJobState(""):
		transitionTime, _, _ = unstructured.NestedString(obj.Object, "status", "pendingTime")
		if transitionTime == "" {
			transitionTime, _, _ = unstructured.NestedString(obj.Object, "status", "startTime")
		}
		status = &VerificationStatus{State: releaseVerificationStatePending, URL: url}
	default:
		glog.Errorf("Unrecognized prow job state %q on job %s", s, obj.GetName())
		return nil, false
	}
	if t, err := time.Parse(time.RFC3339, transitionTime); err == nil {
		trTime := metav1.NewTime(t)
		status.TransitionTime = &trTime
	}
	return status, true
}
