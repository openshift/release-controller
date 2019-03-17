package main

import (
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	prowapiv1 "github.com/openshift/release-controller/pkg/prow/apiv1"
)

func prowJobVerificationStatus(obj *unstructured.Unstructured) (*VerificationStatus, bool) {
	s, _, err := unstructured.NestedString(obj.Object, "status", "state")
	if err != nil {
		return nil, false
	}
	url, _, _ := unstructured.NestedString(obj.Object, "status", "url")
	switch prowapiv1.ProwJobState(s) {
	case prowapiv1.SuccessState:
		return &VerificationStatus{State: releaseVerificationStateSucceeded, URL: url}, true
	case prowapiv1.FailureState, prowapiv1.ErrorState, prowapiv1.AbortedState:
		return &VerificationStatus{State: releaseVerificationStateFailed, URL: url}, true
	case prowapiv1.TriggeredState, prowapiv1.PendingState, prowapiv1.ProwJobState(""):
		return &VerificationStatus{State: releaseVerificationStatePending, URL: url}, true
	default:
		glog.Errorf("Unrecognized prow job state %q on job %s", s, obj.GetName())
		return nil, false
	}
}
