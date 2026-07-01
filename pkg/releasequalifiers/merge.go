package releasequalifiers

import (
	"sort"

	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications"
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/jira"
)

// Merge returns a new ReleaseQualifiers by selecting global qualifiers that have a matching
// override entry. Each override opts in to a global qualifier; override values take precedence
// via mergeQualifier deep merge. Qualifiers not present in overrides are excluded from the result.
func (rqs ReleaseQualifiers) Merge(overrides ReleaseQualifiers) ReleaseQualifiers {
	result := make(ReleaseQualifiers)
	for qualifierId, qualifier := range rqs {
		if override, exists := overrides[qualifierId]; exists {
			result[qualifierId] = mergeQualifier(qualifier, override)
		}
	}
	return result
}

// Merge takes a ReleaseQualifier object and returns a new ReleaseQualifier that is the union of both
// Override values take precedence when both are defined
// Deep merge is performed for nested structures
func (rq ReleaseQualifier) Merge(override ReleaseQualifier) ReleaseQualifier {
	return mergeQualifier(rq, override)
}

// mergeQualifier merges two individual ReleaseQualifier structs
func mergeQualifier(base, override ReleaseQualifier) ReleaseQualifier {
	result := base

	// Override simple fields if they are set in override
	if override.BadgeName != "" {
		result.BadgeName = override.BadgeName
	}
	if override.Summary != "" {
		result.Summary = override.Summary
	}
	if override.Description != "" {
		result.Description = override.Description
	}
	if override.PayloadBadgeStatus != "" {
		result.PayloadBadgeStatus = override.PayloadBadgeStatus
	}

	// Override Enabled field if it's explicitly set in override
	if override.Enabled != nil {
		result.Enabled = override.Enabled
	}

	// Override Approval field if it's explicitly set in override
	if override.Approval != nil {
		result.Approval = override.Approval
	}

	// Override FailureLabels if present in override
	if override.FailureLabels != nil {
		result.FailureLabels = override.FailureLabels
	}

	// Merge notifications if present in override
	if override.Notifications != nil {
		if result.Notifications == nil {
			result.Notifications = &notifications.Notifications{}
		}
		result.Notifications = mergeNotifications(*result.Notifications, *override.Notifications)
	}

	return result
}

// mergeNotifications merges two Notifications structs
func mergeNotifications(base, override notifications.Notifications) *notifications.Notifications {
	result := base

	// Merge Jira notifications
	if override.Jira != nil {
		if result.Jira == nil {
			result.Jira = &jira.Notification{}
		}
		result.Jira = mergeJiraNotifications(*result.Jira, *override.Jira)
	}

	return &result
}

// mergeJiraNotifications merges two JiraNotification structs
func mergeJiraNotifications(base, override jira.Notification) *jira.Notification {
	result := base

	// Override simple fields if they are set in override
	if override.Project != "" {
		result.Project = override.Project
	}
	if override.Component != "" {
		result.Component = override.Component
	}
	if override.Assignee != "" {
		result.Assignee = override.Assignee
	}
	if override.Summary != "" {
		result.Summary = override.Summary
	}
	if override.Description != "" {
		result.Description = override.Description
	}
	if override.Thread != "" {
		result.Thread = override.Thread
	}

	// Merge escalations by name
	escalationMap := make(map[string]jira.Escalation)

	// Add base escalations
	for _, escalation := range result.Escalations {
		escalationMap[escalation.Name] = escalation
	}

	// Merge override escalations
	for _, escalation := range override.Escalations {
		escalationMap[escalation.Name] = escalation
	}

	// Convert back to slice
	result.Escalations = make([]jira.Escalation, 0, len(escalationMap))
	for _, escalation := range escalationMap {
		result.Escalations = append(result.Escalations, escalation)
	}

	sort.Sort(jira.ByJiraEscalationName(result.Escalations))
	return &result
}
