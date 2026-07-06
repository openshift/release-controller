package releasequalifiers

import "fmt"

// Validate enforces any rules that all ReleaseQualifier objects must adhere to
func (rq ReleaseQualifier) Validate() error {
	if rq.Approval != nil && *rq.Approval {
		return rq.validateApprovalQualifier()
	}
	return nil
}

// validateApprovalQualifier enforces that approval-based qualifiers only specify
// the allowed fields: Enabled, BadgeName, Summary, Description, and PayloadBadgeStatus.
func (rq ReleaseQualifier) validateApprovalQualifier() error {
	if len(rq.FailureLabels) > 0 {
		return fmt.Errorf("approval qualifiers must not specify failureLabels")
	}
	if rq.Notifications != nil {
		return fmt.Errorf("approval qualifiers must not specify notifications")
	}
	return nil
}
