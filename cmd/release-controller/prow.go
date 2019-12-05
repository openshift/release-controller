package main

import (
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	prowjobsv1 "k8s.io/test-infra/prow/apis/prowjobs/v1"
)

func prowJobVerificationStatus(obj *unstructured.Unstructured) (*VerificationStatus, bool) {
	s, _, err := unstructured.NestedString(obj.Object, "status", "state")
	if err != nil {
		return nil, false
	}
	url, _, _ := unstructured.NestedString(obj.Object, "status", "url")
	switch prowjobsv1.ProwJobState(s) {
	case prowjobsv1.SuccessState:
		return &VerificationStatus{State: releaseVerificationStateSucceeded, URL: url}, true
	case prowjobsv1.FailureState, prowjobsv1.ErrorState, prowjobsv1.AbortedState:
		return &VerificationStatus{State: releaseVerificationStateFailed, URL: url}, true
	case prowjobsv1.TriggeredState, prowjobsv1.PendingState, prowjobsv1.ProwJobState(""):
		return &VerificationStatus{State: releaseVerificationStatePending, URL: url}, true
	default:
		glog.Errorf("Unrecognized prow job state %q on job %s", s, obj.GetName())
		return nil, false
	}
}
