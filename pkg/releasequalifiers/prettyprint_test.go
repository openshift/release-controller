package releasequalifiers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications"
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/jira"
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/slack"
)

func TestReleaseQualifiers_PrettyPrint(t *testing.T) {
	tests := []struct {
		name         string
		qualifiers   ReleaseQualifiers
		expectedKeys []string
	}{
		{
			name: "pretty print with sorted keys and escalations",
			qualifiers: ReleaseQualifiers{
				"zebra": {
					Enabled:     BoolPtr(true),
					Name:        "ZEB",
					Summary:     "Zebra Component",
					Description: "Detailed zebra description",
					Badge:       BadgeStatusOnSuccess,
				},
				"alpha": {
					Enabled:     BoolPtr(false),
					Name:        "ALP",
					Summary:     "Alpha Component",
					Description: "Detailed alpha description",
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project:     "TEST",
							Component:   "alpha-comp",
							Summary:     "Alpha jira summary",
							Description: "Alpha jira description",
							Assignee:    "alpha@example.com",
							Escalations: []jira.Escalation{
								{Name: "zulu", Failures: 1, Priority: "low"},
								{Name: "alpha", Failures: 2, Priority: "high"},
							},
						},
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "zulu", Period: "1h", MinFailures: 1, Channel: "#zulu"},
								{Name: "alpha", Period: "2h", MinFailures: 2, Channel: "#alpha"},
							},
						},
					},
				},
				"beta": {
					Enabled: BoolPtr(true),
					Name:    "BET",
					Summary: "Beta Component",
				},
			},
			expectedKeys: []string{"alpha", "beta", "zebra"},
		},
		{
			name:         "empty qualifiers",
			qualifiers:   ReleaseQualifiers{},
			expectedKeys: []string{},
		},
		{
			name: "single qualifier with all fields",
			qualifiers: ReleaseQualifiers{
				"comprehensive": {
					Enabled:     BoolPtr(true),
					Name:        "COMP",
					Summary:     "Comprehensive test",
					Description: "Full description",
					Badge:       BadgeStatusNo,
					Labels:      []string{"label1", "label2"},
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project:     "PROJ",
							Component:   "comp",
							Summary:     "Jira summary",
							Description: "Jira description",
							Assignee:    "user@test.com",
							Escalations: []jira.Escalation{
								{Name: "esc1", Failures: 3, Priority: "High", Mentions: []string{"@team"}},
								{Name: "esc2", Failures: 5, Priority: "Critical"},
							},
						},
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "slack1", Period: "1h", MinFailures: 2, Channel: "#ch1", Mentions: []string{"@oncall"}},
								{Name: "slack2", Period: "30m", MinFailures: 1, Channel: "#ch2"},
							},
						},
					},
				},
			},
			expectedKeys: []string{"comprehensive"},
		},
		{
			name: "multiple qualifiers with varying complexity",
			qualifiers: ReleaseQualifiers{
				"simple": {
					Enabled: BoolPtr(false),
					Name:    "SIM",
				},
				"with-labels": {
					Enabled: BoolPtr(true),
					Name:    "LAB",
					Labels:  []string{"critical", "urgent"},
				},
				"with-payload": {
					Enabled: BoolPtr(true),
					Name:    "PAY",
					Badge:   BadgeStatusOnFailure,
				},
			},
			expectedKeys: []string{"simple", "with-labels", "with-payload"},
		},
		{
			name: "qualifiers with only notifications",
			qualifiers: ReleaseQualifiers{
				"jira-only": {
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "TEST",
						},
					},
				},
				"slack-only": {
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "test", Period: "1h", MinFailures: 1, Channel: "#test"},
							},
						},
					},
				},
			},
			expectedKeys: []string{"jira-only", "slack-only"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prettyJSON, err := tt.qualifiers.PrettyPrint()
			if err != nil {
				t.Fatalf("Error pretty printing: %v", err)
			}

			// Verify that the JSON is valid
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(prettyJSON), &parsed); err != nil {
				t.Errorf("Generated JSON is not valid: %v", err)
			}

			// Verify that all expected keys are present
			for _, key := range tt.expectedKeys {
				if _, exists := parsed[key]; !exists {
					t.Errorf("Expected key '%s' not found in parsed JSON", key)
				}
			}

			// Verify JSON is properly indented if not empty
			if len(tt.expectedKeys) > 0 {
				if !strings.Contains(prettyJSON, "\n") {
					t.Error("Expected pretty-printed JSON with newlines")
				}
			}

			// Skip detailed validation for empty qualifiers
			if len(tt.expectedKeys) == 0 {
				return
			}

			// Perform specific validations based on test case
			switch tt.name {
			case "pretty print with sorted keys and escalations":
				// Verify that keys are sorted alphabetically
				if !strings.Contains(prettyJSON, `"alpha":`) {
					t.Error("Expected 'alpha' key to be present")
				}
				if !strings.Contains(prettyJSON, `"beta":`) {
					t.Error("Expected 'beta' key to be present")
				}
				if !strings.Contains(prettyJSON, `"zebra":`) {
					t.Error("Expected 'zebra' key to be present")
				}

				// Find the positions of the keys
				alphaPos := strings.Index(prettyJSON, `"alpha":`)
				betaPos := strings.Index(prettyJSON, `"beta":`)
				zebraPos := strings.Index(prettyJSON, `"zebra":`)

				// Verify alphabetical order
				if alphaPos >= betaPos || betaPos >= zebraPos {
					t.Errorf("Keys are not in alphabetical order. Positions: alpha=%d, beta=%d, zebra=%d", alphaPos, betaPos, zebraPos)
				}

				// Verify that the alpha qualifier has notifications with sorted escalations
				alphaQualifier, ok := parsed["alpha"].(map[string]interface{})
				if !ok {
					t.Error("Expected alpha qualifier to be a map")
					return
				}

				// Verify alpha has description and other fields
				if desc, ok := alphaQualifier["description"].(string); !ok || desc == "" {
					t.Error("Expected alpha to have description field")
				}

				n, ok := alphaQualifier["notifications"].(map[string]interface{})
				if !ok {
					t.Error("Expected notifications to be a map")
					return
				}

				// Check Jira escalations are sorted by name and have all fields
				if j, ok := n["jira"].(map[string]interface{}); ok {
					// Check Jira fields
					if _, ok := j["component"].(string); !ok {
						t.Error("Expected component field in Jira")
					}
					if _, ok := j["summary"].(string); !ok {
						t.Error("Expected summary field in Jira")
					}
					if _, ok := j["description"].(string); !ok {
						t.Error("Expected description field in Jira")
					}
					if _, ok := j["assignee"].(string); !ok {
						t.Error("Expected assignee field in Jira")
					}

					if escalations, ok := j["escalations"].([]interface{}); ok {
						if len(escalations) >= 2 {
							firstEsc := escalations[0].(map[string]interface{})
							secondEsc := escalations[1].(map[string]interface{})

							if firstEsc["name"] != "alpha" || secondEsc["name"] != "zulu" {
								t.Errorf("Expected escalations to be sorted by name. Got: %v, %v", firstEsc["name"], secondEsc["name"])
							}
						}
					}
				}

				// Check Slack escalations are sorted by name
				if s, ok := n["slack"].(map[string]interface{}); ok {
					if escalations, ok := s["escalations"].([]interface{}); ok {
						if len(escalations) >= 2 {
							firstEsc := escalations[0].(map[string]interface{})
							secondEsc := escalations[1].(map[string]interface{})

							if firstEsc["name"] != "alpha" || secondEsc["name"] != "zulu" {
								t.Errorf("Expected escalations to be sorted by name. Got: %v, %v", firstEsc["name"], secondEsc["name"])
							}
						}
					}
				}

				// Verify zebra has badge and description
				zebraQualifier, ok := parsed["zebra"].(map[string]interface{})
				if !ok {
					t.Error("Expected zebra qualifier to be a map")
					return
				}
				if badge, ok := zebraQualifier["badge"].(string); !ok || badge != string(BadgeStatusOnSuccess) {
					t.Error("Expected zebra to have badge field")
				}
				if desc, ok := zebraQualifier["description"].(string); !ok || desc == "" {
					t.Error("Expected zebra to have description field")
				}

			case "single qualifier with all fields":
				comp, ok := parsed["comprehensive"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected comprehensive qualifier to be a map")
				}

				// Verify all fields are present (except labels, which is not included by formatQualifierForJSON)
				if _, ok := comp["enabled"]; !ok {
					t.Error("Expected enabled field")
				}
				if _, ok := comp["name"]; !ok {
					t.Error("Expected name field")
				}
				if _, ok := comp["summary"]; !ok {
					t.Error("Expected summary field")
				}
				if _, ok := comp["description"]; !ok {
					t.Error("Expected description field")
				}
				if badge, ok := comp["badge"].(string); !ok || badge != string(BadgeStatusNo) {
					t.Error("Expected badge field with 'No' value")
				}
				// Note: labels field is not included in formatQualifierForJSON output

				// Verify notifications with both Jira and Slack escalations
				n, ok := comp["notifications"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected notifications to be a map")
				}

				if _, ok := n["jira"]; !ok {
					t.Error("Expected jira notifications")
				}
				if _, ok := n["slack"]; !ok {
					t.Error("Expected slack notifications")
				}

			case "multiple qualifiers with varying complexity":
				// Verify simple qualifier
				if simple, ok := parsed["simple"].(map[string]interface{}); ok {
					if enabled, ok := simple["enabled"].(bool); !ok || enabled {
						t.Error("Expected simple qualifier to have enabled=false")
					}
				}

				// Verify with-labels qualifier exists (labels not included in formatQualifierForJSON)
				if _, ok := parsed["with-labels"]; !ok {
					t.Error("Expected with-labels qualifier to be present")
				}

				// Verify with-payload qualifier has badge
				if withPayload, ok := parsed["with-payload"].(map[string]interface{}); ok {
					if badge, ok := withPayload["badge"].(string); !ok || badge != string(BadgeStatusOnFailure) {
						t.Error("Expected with-payload qualifier to have badge=OnFailure")
					}
				}

				// Verify alphabetical ordering: simple, with-labels, with-payload
				simplePos := strings.Index(prettyJSON, `"simple":`)
				labelsPos := strings.Index(prettyJSON, `"with-labels":`)
				payloadPos := strings.Index(prettyJSON, `"with-payload":`)

				if simplePos >= labelsPos || labelsPos >= payloadPos {
					t.Errorf("Keys not in alphabetical order. Positions: simple=%d, with-labels=%d, with-payload=%d", simplePos, labelsPos, payloadPos)
				}

			case "qualifiers with only notifications":
				// Verify jira-only has jira notifications
				if jiraOnly, ok := parsed["jira-only"].(map[string]interface{}); ok {
					if n, ok := jiraOnly["notifications"].(map[string]interface{}); ok {
						if _, ok := n["jira"]; !ok {
							t.Error("Expected jira-only to have jira notifications")
						}
						if _, ok := n["slack"]; ok {
							t.Error("Did not expect jira-only to have slack notifications")
						}
					}
				}

				// Verify slack-only has Slack notifications
				if slackOnly, ok := parsed["slack-only"].(map[string]interface{}); ok {
					if n, ok := slackOnly["notifications"].(map[string]interface{}); ok {
						if _, ok := n["slack"]; !ok {
							t.Error("Expected slack-only to have slack notifications")
						}
						if _, ok := n["jira"]; ok {
							t.Error("Did not expect slack-only to have jira notifications")
						}
					}
				}

				// Verify alphabetical ordering
				jiraPos := strings.Index(prettyJSON, `"jira-only":`)
				slackPos := strings.Index(prettyJSON, `"slack-only":`)
				if jiraPos >= slackPos {
					t.Errorf("Keys not in alphabetical order. Positions: jira-only=%d, slack-only=%d", jiraPos, slackPos)
				}
			}
		})
	}
}

func TestReleaseQualifier_PrettyPrint(t *testing.T) {
	tests := []struct {
		name      string
		qualifier ReleaseQualifier
		wantErr   bool
		validate  func(t *testing.T, result string)
	}{
		{
			name: "minimal qualifier with only enabled",
			qualifier: ReleaseQualifier{
				Enabled: BoolPtr(true),
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}
				if enabled, ok := parsed["enabled"].(bool); !ok || !enabled {
					t.Error("Expected enabled to be true")
				}
			},
		},
		{
			name: "full qualifier with all fields",
			qualifier: ReleaseQualifier{
				Enabled:     BoolPtr(true),
				Name:        "TEST",
				Summary:     "Test Summary",
				Description: "Test Description",
				Badge:       "payload-test",
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project:     "PROJ",
						Component:   "comp",
						Summary:     "Jira Summary",
						Description: "Jira Description",
						Assignee:    "user@example.com",
						Escalations: []jira.Escalation{
							{
								Name:     "critical",
								Failures: 5,
								Priority: "Critical",
								Mentions: []string{"@team"},
							},
						},
					},
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{
								Name:        "alert",
								Period:      "1h",
								MinFailures: 3,
								Channel:     "#alerts",
								Mentions:    []string{"@oncall"},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				// Verify all top-level fields are present
				if _, ok := parsed["enabled"]; !ok {
					t.Error("Expected enabled field")
				}
				if _, ok := parsed["name"]; !ok {
					t.Error("Expected name field")
				}
				if _, ok := parsed["summary"]; !ok {
					t.Error("Expected summary field")
				}
				if _, ok := parsed["description"]; !ok {
					t.Error("Expected description field")
				}
				if _, ok := parsed["badge"]; !ok {
					t.Error("Expected badge field")
				}
				if _, ok := parsed["notifications"]; !ok {
					t.Error("Expected notifications field")
				}

				// Verify the JSON is properly formatted (has indentation)
				if !strings.Contains(result, "\n") {
					t.Error("Expected pretty-printed JSON with newlines")
				}
			},
		},
		{
			name: "qualifier with jira notifications only",
			qualifier: ReleaseQualifier{
				Enabled: BoolPtr(false),
				Name:    "JIRA-ONLY",
				Summary: "Jira Only Test",
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project:   "TEST",
						Component: "component-a",
						Summary:   "Test Issue",
						Escalations: []jira.Escalation{
							{Name: "low", Failures: 1, Priority: "Low"},
							{Name: "high", Failures: 10, Priority: "High"},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				n, ok := parsed["notifications"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected notifications to be a map")
				}

				if _, ok := n["jira"]; !ok {
					t.Error("Expected jira notifications")
				}
				if _, ok := n["slack"]; ok {
					t.Error("Did not expect slack notifications")
				}
			},
		},
		{
			name: "qualifier with slack notifications only",
			qualifier: ReleaseQualifier{
				Enabled: BoolPtr(true),
				Name:    "SLACK-ONLY",
				Summary: "Slack Only Test",
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "warn", Period: "30m", MinFailures: 2, Channel: "#warnings"},
							{Name: "critical", Period: "5m", MinFailures: 5, Channel: "#critical"},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				n, ok := parsed["notifications"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected notifications to be a map")
				}

				if _, ok := n["slack"]; !ok {
					t.Error("Expected slack notifications")
				}
				if _, ok := n["jira"]; ok {
					t.Error("Did not expect jira notifications")
				}
			},
		},
		{
			name: "qualifier with empty notifications",
			qualifier: ReleaseQualifier{
				Enabled:       BoolPtr(true),
				Name:          "EMPTY",
				Notifications: &notifications.Notifications{},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				// Empty notifications should still be in the output
				if _, ok := parsed["notifications"]; !ok {
					t.Error("Expected notifications field even if empty")
				}
			},
		},
		{
			name: "qualifier with escalations with mentions",
			qualifier: ReleaseQualifier{
				Enabled: BoolPtr(true),
				Name:    "MENTIONS",
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project: "TEST",
						Escalations: []jira.Escalation{
							{
								Name:     "with-mentions",
								Failures: 3,
								Priority: "Medium",
								Mentions: []string{"@team-a", "@team-b"},
							},
						},
					},
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{
								Name:        "with-mentions",
								Period:      "1h",
								MinFailures: 2,
								Channel:     "#test",
								Mentions:    []string{"@oncall", "@manager"},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				n, ok := parsed["notifications"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected notifications to be a map")
				}

				// Check Jira mentions
				if jiraMap, ok := n["jira"].(map[string]interface{}); ok {
					if escalations, ok := jiraMap["escalations"].([]interface{}); ok && len(escalations) > 0 {
						esc := escalations[0].(map[string]interface{})
						if mentions, ok := esc["mentions"].([]interface{}); !ok || len(mentions) != 2 {
							t.Error("Expected Jira escalation to have 2 mentions")
						}
					}
				}

				// Check Slack mentions
				if slackMap, ok := n["slack"].(map[string]interface{}); ok {
					if escalations, ok := slackMap["escalations"].([]interface{}); ok && len(escalations) > 0 {
						esc := escalations[0].(map[string]interface{})
						if mentions, ok := esc["mentions"].([]interface{}); !ok || len(mentions) != 2 {
							t.Error("Expected Slack escalation to have 2 mentions")
						}
					}
				}
			},
		},
		{
			name: "qualifier with description field",
			qualifier: ReleaseQualifier{
				Enabled:     BoolPtr(true),
				Name:        "DESC-TEST",
				Summary:     "Summary text",
				Description: "This is a detailed description for the qualifier",
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				if desc, ok := parsed["description"].(string); !ok || desc != "This is a detailed description for the qualifier" {
					t.Errorf("Expected description field with correct value, got: %v", parsed["description"])
				}
			},
		},
		{
			name: "qualifier with payload badge",
			qualifier: ReleaseQualifier{
				Enabled: BoolPtr(true),
				Name:    "BADGE-TEST",
				Badge:   BadgeStatusYes,
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				if badge, ok := parsed["badge"].(string); !ok || badge != string(BadgeStatusYes) {
					t.Errorf("Expected badge field with value 'Yes', got: %v", parsed["badge"])
				}
			},
		},
		{
			name: "qualifier with all jira fields",
			qualifier: ReleaseQualifier{
				Enabled: BoolPtr(true),
				Name:    "FULL-JIRA",
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project:     "MYPROJ",
						Component:   "my-component",
						Summary:     "Issue summary",
						Description: "Issue description",
						Assignee:    "user@example.com",
						Escalations: []jira.Escalation{
							{
								Name:     "no-mentions",
								Failures: 5,
								Priority: "High",
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				n, ok := parsed["notifications"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected notifications to be a map")
				}

				jiraMap, ok := n["jira"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected jira to be a map")
				}

				// Check all Jira fields
				if project, ok := jiraMap["project"].(string); !ok || project != "MYPROJ" {
					t.Errorf("Expected project field, got: %v", jiraMap["project"])
				}
				if component, ok := jiraMap["component"].(string); !ok || component != "my-component" {
					t.Errorf("Expected component field, got: %v", jiraMap["component"])
				}
				if summary, ok := jiraMap["summary"].(string); !ok || summary != "Issue summary" {
					t.Errorf("Expected summary field, got: %v", jiraMap["summary"])
				}
				if description, ok := jiraMap["description"].(string); !ok || description != "Issue description" {
					t.Errorf("Expected description field, got: %v", jiraMap["description"])
				}
				if assignee, ok := jiraMap["assignee"].(string); !ok || assignee != "user@example.com" {
					t.Errorf("Expected assignee field, got: %v", jiraMap["assignee"])
				}

				// Check escalations without mentions
				if escalations, ok := jiraMap["escalations"].([]interface{}); ok && len(escalations) > 0 {
					esc := escalations[0].(map[string]interface{})
					if _, ok := esc["mentions"]; ok {
						t.Error("Did not expect mentions field in escalation without mentions")
					}
					if name, ok := esc["name"].(string); !ok || name != "no-mentions" {
						t.Errorf("Expected name field, got: %v", esc["name"])
					}
				}
			},
		},
		{
			name: "qualifier with labels",
			qualifier: ReleaseQualifier{
				Enabled: BoolPtr(true),
				Name:    "LABELS-TEST",
				Labels:  []string{"bug", "priority-high", "team-a"},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				if labels, ok := parsed["labels"].([]interface{}); !ok || len(labels) != 3 {
					t.Errorf("Expected labels field with 3 items, got: %v", parsed["labels"])
				}
			},
		},
		{
			name: "qualifier with slack escalations without mentions",
			qualifier: ReleaseQualifier{
				Enabled: BoolPtr(true),
				Name:    "SLACK-NO-MENTIONS",
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{
								Name:        "no-mentions-escalation",
								Period:      "2h",
								MinFailures: 10,
								Channel:     "#test-channel",
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				n, ok := parsed["notifications"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected notifications to be a map")
				}

				slackMap, ok := n["slack"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected slack to be a map")
				}

				escalations, ok := slackMap["escalations"].([]interface{})
				if !ok || len(escalations) == 0 {
					t.Fatal("Expected escalations array")
				}

				esc := escalations[0].(map[string]interface{})
				if _, ok := esc["mentions"]; ok {
					t.Error("Did not expect mentions field in escalation without mentions")
				}
				if name, ok := esc["name"].(string); !ok || name != "no-mentions-escalation" {
					t.Errorf("Expected name field, got: %v", esc["name"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.qualifier.PrettyPrint()
			if (err != nil) != tt.wantErr {
				t.Errorf("ReleaseQualifier.PrettyPrint() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}
