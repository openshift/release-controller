// +k8s:deepcopy-gen=package

package notifications

import (
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/jira"
)

// Notifications defines the notification settings for a ReleaseQualifier
// It supports multiple notification channels like Slack and Jira
// +k8s:deepcopy-gen=true
type Notifications struct {
	// Jira contains Jira-specific notification configuration
	Jira *jira.Notification `json:"jira,omitempty"`
}
