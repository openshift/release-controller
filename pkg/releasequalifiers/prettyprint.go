package releasequalifiers

import (
	"encoding/json"
	"sort"

	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications"
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/jira"
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/slack"
)

// PrettyPrint returns a pretty-printed JSON representation of ReleaseQualifiers
// with alphabetically sorted keys for consistent output
func (rqs ReleaseQualifiers) PrettyPrint() (string, error) {
	sortedQualifiers := rqs.ToSortableSlice()
	sort.Sort(sortedQualifiers)

	// Create a map with sorted keys
	sortedMap := make(map[QualifierId]interface{})

	// Build the sorted map with properly formatted qualifiers
	for _, q := range sortedQualifiers {
		sortedMap[q.ID] = formatQualifierForJSON(q.Qualifier)
	}

	// Marshal with pretty printing
	jsonData, err := json.MarshalIndent(sortedMap, "", "  ")
	if err != nil {
		return "", err
	}

	return string(jsonData), nil
}

// PrettyPrint returns a pretty-printed JSON representation of ReleaseQualifier
// with alphabetically sorted keys for consistent output
func (rq ReleaseQualifier) PrettyPrint() (string, error) {
	// Marshal with pretty printing
	jsonData, err := json.MarshalIndent(rq, "", "  ")
	if err != nil {
		return "", err
	}
	return string(jsonData), nil
}

// formatQualifierForJSON formats a ReleaseQualifier struct for JSON output with sorted fields
func formatQualifierForJSON(qualifier ReleaseQualifier) map[string]interface{} {
	result := make(map[string]interface{})

	// Add fields in alphabetical order
	if qualifier.BadgeName != "" {
		result["badgeName"] = qualifier.BadgeName
	}

	if qualifier.Description != "" {
		result["description"] = qualifier.Description
	}

	if qualifier.Enabled != nil {
		result["enabled"] = *qualifier.Enabled
	}

	if qualifier.Notifications != nil {
		result["notifications"] = formatNotificationsForJSON(*qualifier.Notifications)
	}

	if qualifier.PayloadBadge != "" {
		result["payloadBadge"] = qualifier.PayloadBadge
	}

	if qualifier.Summary != "" {
		result["summary"] = qualifier.Summary
	}

	return result
}

// formatNotificationsForJSON formats a Notifications struct for JSON output with sorted fields
func formatNotificationsForJSON(notifications notifications.Notifications) map[string]interface{} {
	result := make(map[string]interface{})

	// Add fields in alphabetical order
	if notifications.Jira != nil {
		result["jira"] = formatJiraNotificationForJSON(*notifications.Jira)
	}

	if notifications.Slack != nil {
		result["slack"] = formatSlackNotificationForJSON(*notifications.Slack)
	}

	return result
}

// formatJiraNotificationForJSON formats a JiraNotification struct for JSON output with sorted fields
func formatJiraNotificationForJSON(jira jira.Notification) map[string]interface{} {
	result := make(map[string]interface{})

	// Add fields in alphabetical order
	if jira.Assignee != "" {
		result["assignee"] = jira.Assignee
	}

	if jira.Component != "" {
		result["component"] = jira.Component
	}

	if jira.Description != "" {
		result["description"] = jira.Description
	}

	if len(jira.Escalations) > 0 {
		result["escalations"] = formatJiraEscalationsForJSON(jira.Escalations)
	}

	if jira.Project != "" {
		result["project"] = jira.Project
	}

	if jira.Summary != "" {
		result["summary"] = jira.Summary
	}

	return result
}

// formatJiraEscalationsForJSON formats a slice of JiraEscalation for JSON output with sorted fields
func formatJiraEscalationsForJSON(escalations []jira.Escalation) []map[string]interface{} {
	// Sort escalations by name for consistent output
	sortedEscalations := make([]jira.Escalation, len(escalations))
	copy(sortedEscalations, escalations)
	sort.Slice(sortedEscalations, func(i, j int) bool {
		return sortedEscalations[i].Name < sortedEscalations[j].Name
	})

	result := make([]map[string]interface{}, len(sortedEscalations))

	for i, escalation := range sortedEscalations {
		escMap := make(map[string]interface{})

		// Add fields in alphabetical order
		escMap["failures"] = escalation.Failures

		if len(escalation.Mentions) > 0 {
			escMap["mentions"] = escalation.Mentions
		}

		escMap["name"] = escalation.Name
		escMap["priority"] = escalation.Priority

		result[i] = escMap
	}

	return result
}

// formatSlackNotificationForJSON formats a SlackNotification struct for JSON output with sorted fields
func formatSlackNotificationForJSON(slack slack.Notification) map[string]interface{} {
	result := make(map[string]interface{})

	// Add fields in alphabetical order
	if len(slack.Escalations) > 0 {
		result["escalations"] = formatSlackEscalationsForJSON(slack.Escalations)
	}

	return result
}

// formatSlackEscalationsForJSON formats a slice of SlackEscalation for JSON output with sorted fields
func formatSlackEscalationsForJSON(escalations []slack.Escalation) []map[string]interface{} {
	// Sort escalations by name for consistent output
	sortedEscalations := make([]slack.Escalation, len(escalations))
	copy(sortedEscalations, escalations)
	sort.Slice(sortedEscalations, func(i, j int) bool {
		return sortedEscalations[i].Name < sortedEscalations[j].Name
	})

	result := make([]map[string]interface{}, len(sortedEscalations))

	for i, escalation := range sortedEscalations {
		escMap := make(map[string]interface{})

		// Add fields in alphabetical order
		escMap["channel"] = escalation.Channel
		escMap["minFailures"] = escalation.MinFailures

		if len(escalation.Mentions) > 0 {
			escMap["mentions"] = escalation.Mentions
		}

		escMap["name"] = escalation.Name
		escMap["period"] = escalation.Period

		result[i] = escMap
	}

	return result
}
