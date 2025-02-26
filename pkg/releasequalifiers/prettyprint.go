package releasequalifiers

import (
	"encoding/json"
	"sort"
)

// PrettyPrint returns a pretty-printed JSON representation of ReleaseQualifiers
// with alphabetically sorted keys for consistent output
func (rqs ReleaseQualifiers) PrettyPrint() (string, error) {
	// use json marshaller to convert all structs to maps
	json1, err := json.Marshal(rqs)
	if err != nil {
		return "", err
	}
	qualifiersMap := make(ReleaseQualifiers)
	if err := json.Unmarshal(json1, &qualifiersMap); err != nil {
		return "", err
	}
	genericMap := make(map[string]any)
	for id, qualifier := range qualifiersMap {
		// unset labels
		qualifier.Labels = nil
		// sort escalation slices
		if qualifier.Notifications != nil {
			if qualifier.Notifications.Jira != nil {
				sort.Slice(qualifier.Notifications.Jira.Escalations, func(i, j int) bool {
					return qualifier.Notifications.Jira.Escalations[i].Name < qualifier.Notifications.Jira.Escalations[j].Name
				})
			}
			if qualifier.Notifications.Slack != nil {
				sort.Slice(qualifier.Notifications.Slack.Escalations, func(i, j int) bool {
					return qualifier.Notifications.Slack.Escalations[i].Name < qualifier.Notifications.Slack.Escalations[j].Name
				})
			}
		}
		genericMap[string(id)] = qualifier
	}
	// Marshal with pretty printing
	jsonData, err := json.MarshalIndent(genericMap, "", "  ")
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

