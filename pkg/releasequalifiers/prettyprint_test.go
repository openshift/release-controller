package releasequalifiers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications"
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/jira"
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
					Enabled:            BoolPtr(true),
					BadgeName:          "ZEB",
					Summary:            "Zebra Component",
					Description:        "Detailed zebra description",
					PayloadBadgeStatus: BadgeStatusOnSuccess,
				},
				"alpha": {
					Enabled:     BoolPtr(false),
					BadgeName:   "ALP",
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
					},
				},
				"beta": {
					Enabled:   BoolPtr(true),
					BadgeName: "BET",
					Summary:   "Beta Component",
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
					Enabled:            BoolPtr(true),
					BadgeName:          "COMP",
					Summary:            "Comprehensive test",
					Description:        "Full description",
					PayloadBadgeStatus: BadgeStatusNo,
					FailureLabels:      []string{"label1", "label2"},
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
					},
				},
			},
			expectedKeys: []string{"comprehensive"},
		},
		{
			name: "multiple qualifiers with varying complexity",
			qualifiers: ReleaseQualifiers{
				"simple": {
					Enabled:   BoolPtr(false),
					BadgeName: "SIM",
				},
				"with-labels": {
					Enabled:       BoolPtr(true),
					BadgeName:     "LAB",
					FailureLabels: []string{"critical", "urgent"},
				},
				"with-payload": {
					Enabled:            BoolPtr(true),
					BadgeName:          "PAY",
					PayloadBadgeStatus: BadgeStatusOnFailure,
				},
			},
			expectedKeys: []string{"simple", "with-labels", "with-payload"},
		},
		{
			name: "qualifier with jira notifications",
			qualifiers: ReleaseQualifiers{
				"jira-only": {
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "TEST",
						},
					},
				},
			},
			expectedKeys: []string{"jira-only"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prettyJSON, err := tt.qualifiers.PrettyPrint()
			if err != nil {
				t.Fatalf("Error pretty printing: %v", err)
			}

			// Verify that the JSON is valid
			var parsed map[string]any
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
				alphaQualifier, ok := parsed["alpha"].(map[string]any)
				if !ok {
					t.Error("Expected alpha qualifier to be a map")
					return
				}

				// Verify alpha has description and other fields
				if desc, ok := alphaQualifier["description"].(string); !ok || desc == "" {
					t.Error("Expected alpha to have description field")
				}

				n, ok := alphaQualifier["notifications"].(map[string]any)
				if !ok {
					t.Error("Expected notifications to be a map")
					return
				}

				// Check Jira escalations are sorted by name and have all fields
				if j, ok := n["jira"].(map[string]any); ok {
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

					if escalations, ok := j["escalations"].([]any); ok {
						if len(escalations) >= 2 {
							firstEsc := escalations[0].(map[string]any)
							secondEsc := escalations[1].(map[string]any)

							if firstEsc["name"] != "alpha" || secondEsc["name"] != "zulu" {
								t.Errorf("Expected escalations to be sorted by name. Got: %v, %v", firstEsc["name"], secondEsc["name"])
							}
						}
					}
				}

				// Verify zebra has badge and description
				zebraQualifier, ok := parsed["zebra"].(map[string]any)
				if !ok {
					t.Error("Expected zebra qualifier to be a map")
					return
				}
				if badge, ok := zebraQualifier["payloadBadgeStatus"].(string); !ok || badge != string(BadgeStatusOnSuccess) {
					t.Error("Expected zebra to have payloadBadgeStatus field")
				}
				if desc, ok := zebraQualifier["description"].(string); !ok || desc == "" {
					t.Error("Expected zebra to have description field")
				}

			case "single qualifier with all fields":
				comp, ok := parsed["comprehensive"].(map[string]any)
				if !ok {
					t.Fatal("Expected comprehensive qualifier to be a map")
				}

				// Verify all fields are present
				if _, ok := comp["enabled"]; !ok {
					t.Error("Expected enabled field")
				}
				if _, ok := comp["badgeName"]; !ok {
					t.Error("Expected badgeName field")
				}
				if _, ok := comp["summary"]; !ok {
					t.Error("Expected summary field")
				}
				if _, ok := comp["description"]; !ok {
					t.Error("Expected description field")
				}
				if badge, ok := comp["payloadBadgeStatus"].(string); !ok || badge != string(BadgeStatusNo) {
					t.Error("Expected payloadBadgeStatus field with 'No' value")
				}

				// Verify notifications with Jira escalations
				n, ok := comp["notifications"].(map[string]any)
				if !ok {
					t.Fatal("Expected notifications to be a map")
				}

				if _, ok := n["jira"]; !ok {
					t.Error("Expected jira notifications")
				}

			case "multiple qualifiers with varying complexity":
				// Verify simple qualifier
				if simple, ok := parsed["simple"].(map[string]any); ok {
					if enabled, ok := simple["enabled"].(bool); !ok || enabled {
						t.Error("Expected simple qualifier to have enabled=false")
					}
				}

				// Verify with-labels qualifier exists
				if _, ok := parsed["with-labels"]; !ok {
					t.Error("Expected with-labels qualifier to be present")
				}

				// Verify with-payload qualifier has badge
				if withPayload, ok := parsed["with-payload"].(map[string]any); ok {
					if badge, ok := withPayload["payloadBadgeStatus"].(string); !ok || badge != string(BadgeStatusOnFailure) {
						t.Error("Expected with-payload qualifier to have payloadBadgeStatus=OnFailure")
					}
				}

				// Verify alphabetical ordering: simple, with-labels, with-payload
				simplePos := strings.Index(prettyJSON, `"simple":`)
				labelsPos := strings.Index(prettyJSON, `"with-labels":`)
				payloadPos := strings.Index(prettyJSON, `"with-payload":`)

				if simplePos >= labelsPos || labelsPos >= payloadPos {
					t.Errorf("Keys not in alphabetical order. Positions: simple=%d, with-labels=%d, with-payload=%d", simplePos, labelsPos, payloadPos)
				}

			case "qualifier with jira notifications":
				// Verify jira-only has jira notifications
				if jiraOnly, ok := parsed["jira-only"].(map[string]any); ok {
					if n, ok := jiraOnly["notifications"].(map[string]any); ok {
						if _, ok := n["jira"]; !ok {
							t.Error("Expected jira-only to have jira notifications")
						}
					}
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
				var parsed map[string]any
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
				Enabled:            BoolPtr(true),
				BadgeName:          "TEST",
				Summary:            "Test Summary",
				Description:        "Test Description",
				PayloadBadgeStatus: "payload-test",
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
				},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]any
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				// Verify all top-level fields are present
				if _, ok := parsed["enabled"]; !ok {
					t.Error("Expected enabled field")
				}
				if _, ok := parsed["badgeName"]; !ok {
					t.Error("Expected badgeName field")
				}
				if _, ok := parsed["summary"]; !ok {
					t.Error("Expected summary field")
				}
				if _, ok := parsed["description"]; !ok {
					t.Error("Expected description field")
				}
				if _, ok := parsed["payloadBadgeStatus"]; !ok {
					t.Error("Expected payloadBadgeStatus field")
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
				Enabled:   BoolPtr(false),
				BadgeName: "JIRA-ONLY",
				Summary:   "Jira Only Test",
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
				var parsed map[string]any
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				n, ok := parsed["notifications"].(map[string]any)
				if !ok {
					t.Fatal("Expected notifications to be a map")
				}

				if _, ok := n["jira"]; !ok {
					t.Error("Expected jira notifications")
				}
			},
		},
		{
			name: "qualifier with empty notifications",
			qualifier: ReleaseQualifier{
				Enabled:       BoolPtr(true),
				BadgeName:     "EMPTY",
				Notifications: &notifications.Notifications{},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]any
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
				Enabled:   BoolPtr(true),
				BadgeName: "MENTIONS",
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
				},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]any
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				n, ok := parsed["notifications"].(map[string]any)
				if !ok {
					t.Fatal("Expected notifications to be a map")
				}

				// Check Jira mentions
				if jiraMap, ok := n["jira"].(map[string]any); ok {
					if escalations, ok := jiraMap["escalations"].([]any); ok && len(escalations) > 0 {
						esc := escalations[0].(map[string]any)
						if mentions, ok := esc["mentions"].([]any); !ok || len(mentions) != 2 {
							t.Error("Expected Jira escalation to have 2 mentions")
						}
					}
				}
			},
		},
		{
			name: "qualifier with description field",
			qualifier: ReleaseQualifier{
				Enabled:     BoolPtr(true),
				BadgeName:   "DESC-TEST",
				Summary:     "Summary text",
				Description: "This is a detailed description for the qualifier",
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]any
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
				Enabled:            BoolPtr(true),
				BadgeName:          "BADGE-TEST",
				PayloadBadgeStatus: BadgeStatusYes,
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]any
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				if badge, ok := parsed["payloadBadgeStatus"].(string); !ok || badge != string(BadgeStatusYes) {
					t.Errorf("Expected payloadBadgeStatus field with value 'Yes', got: %v", parsed["payloadBadgeStatus"])
				}
			},
		},
		{
			name: "qualifier with all jira fields",
			qualifier: ReleaseQualifier{
				Enabled:   BoolPtr(true),
				BadgeName: "FULL-JIRA",
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
				var parsed map[string]any
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				n, ok := parsed["notifications"].(map[string]any)
				if !ok {
					t.Fatal("Expected notifications to be a map")
				}

				jiraMap, ok := n["jira"].(map[string]any)
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
				if escalations, ok := jiraMap["escalations"].([]any); ok && len(escalations) > 0 {
					esc := escalations[0].(map[string]any)
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
				Enabled:       BoolPtr(true),
				BadgeName:     "LABELS-TEST",
				FailureLabels: []string{"bug", "priority-high", "team-a"},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]any
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				if labels, ok := parsed["failureLabels"].([]any); !ok || len(labels) != 3 {
					t.Errorf("Expected labels field with 3 items, got: %v", parsed["labels"])
				}
			},
		},
		{
			name: "qualifier with jira escalations without mentions",
			qualifier: ReleaseQualifier{
				Enabled:   BoolPtr(true),
				BadgeName: "JIRA-NO-MENTIONS",
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project: "TEST",
						Escalations: []jira.Escalation{
							{
								Name:     "no-mentions-escalation",
								Failures: 10,
								Priority: "High",
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				var parsed map[string]any
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Failed to parse JSON: %v", err)
				}

				n, ok := parsed["notifications"].(map[string]any)
				if !ok {
					t.Fatal("Expected notifications to be a map")
				}

				jiraMap, ok := n["jira"].(map[string]any)
				if !ok {
					t.Fatal("Expected jira to be a map")
				}

				escalations, ok := jiraMap["escalations"].([]any)
				if !ok || len(escalations) == 0 {
					t.Fatal("Expected escalations array")
				}

				esc := escalations[0].(map[string]any)
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
