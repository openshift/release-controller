package notifications

import (
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/jira"
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/slack"
)

// Notifications defines the notification settings for a ReleaseQualifier
// It supports multiple notification channels like Slack and Jira
type Notifications struct {
	// Slack contains Slack-specific notification configuration
	Slack *slack.Notification `json:"slack,omitempty"`

	// Jira contains Jira-specific notification configuration
	Jira *jira.Notification `json:"jira,omitempty"`
}

// Notification interface to define a common framework for all ReleaseQualifierNotifications to adhere to
type Notification interface {
	Send()
}
