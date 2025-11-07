package releasequalifiers

import (
	"reflect"
	"testing"

	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications"
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/jira"
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/slack"
)

var (
	TRUE  = BoolPtr(true)
	FALSE = BoolPtr(false)
)

func TestReleaseQualifiers_Merge(t *testing.T) {
	tests := []struct {
		name     string
		base     ReleaseQualifiers
		override ReleaseQualifiers
		expected ReleaseQualifiers
	}{
		{
			name:     "empty base and override",
			base:     ReleaseQualifiers{},
			override: ReleaseQualifiers{},
			expected: ReleaseQualifiers{},
		},
		{
			name: "empty base with override",
			base: ReleaseQualifiers{},
			override: ReleaseQualifiers{
				"test": {
					Enabled:   TRUE,
					BadgeName: "TEST",
				},
			},
			expected: ReleaseQualifiers{},
		},
		{
			name: "base with empty override",
			base: ReleaseQualifiers{
				"test": {
					Enabled:   TRUE,
					BadgeName: "TEST",
				},
			},
			override: ReleaseQualifiers{},
			expected: ReleaseQualifiers{},
		},
		{
			name: "simple field override",
			base: ReleaseQualifiers{
				"test": {
					Enabled:   FALSE,
					BadgeName: "OLD",
					Summary:   "Old Summary",
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Enabled:     TRUE,
					BadgeName:   "NEW",
					Description: "New Description",
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Enabled:     TRUE,
					BadgeName:   "NEW",
					Summary:     "Old Summary", // Not overridden
					Description: "New Description",
				},
			},
		},
		{
			name: "add new qualifier",
			base: ReleaseQualifiers{
				"existing": {
					Enabled:   TRUE,
					BadgeName: "EXIST",
				},
			},
			override: ReleaseQualifiers{
				"new": {
					Enabled:   TRUE,
					BadgeName: "NEW",
				},
			},
			expected: ReleaseQualifiers{},
		},
		{
			name: "merge notifications - add slack to existing jira",
			base: ReleaseQualifiers{
				"test": {
					Enabled: TRUE,
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "TEST",
						},
					},
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "test", Channel: "#test"},
							},
						},
					},
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Enabled: TRUE,
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "TEST",
						},
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "test", Channel: "#test"},
							},
						},
					},
				},
			},
		},
		{
			name: "merge escalations - replace existing by name",
			base: ReleaseQualifiers{
				"test": {
					Enabled: TRUE,
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "old", Period: "24h", MinFailures: 2},
								{Name: "keep", Period: "12h", MinFailures: 1},
							},
						},
					},
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "old", Period: "12h", MinFailures: 1}, // Replace
								{Name: "new", Period: "6h", MinFailures: 1},  // Add new
							},
						},
					},
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Enabled: TRUE,
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "keep", Period: "12h", MinFailures: 1}, // Kept
								{Name: "new", Period: "6h", MinFailures: 1},   // Added
								{Name: "old", Period: "12h", MinFailures: 1},  // Replaced
							},
						},
					},
				},
			},
		},
		{
			name: "merge jira escalations",
			base: ReleaseQualifiers{
				"test": {
					Enabled: TRUE,
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "BASE",
							Escalations: []jira.Escalation{
								{Name: "low", Failures: 1, Priority: "low"},
								{Name: "keep", Failures: 2, Priority: "normal"},
							},
						},
					},
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project:     "OVERRIDE", // Override project
							Summary:     "Overriding summary",
							Description: "Overriding description",
							Escalations: []jira.Escalation{
								{Name: "low", Failures: 2, Priority: "normal"}, // Replace
								{Name: "high", Failures: 5, Priority: "high"},  // Add new
							},
						},
					},
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Enabled: TRUE,
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project:     "OVERRIDE",
							Summary:     "Overriding summary",
							Description: "Overriding description",
							Escalations: []jira.Escalation{
								{Name: "high", Failures: 5, Priority: "high"},   // Added
								{Name: "keep", Failures: 2, Priority: "normal"}, // Kept
								{Name: "low", Failures: 2, Priority: "normal"},  // Replaced
							},
						},
					},
				},
			},
		},
		{
			name: "nil notifications in base",
			base: ReleaseQualifiers{
				"test": {
					Enabled: TRUE,
					// Notifications are nil
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "test", Channel: "#test"},
							},
						},
					},
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Enabled: TRUE,
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "test", Channel: "#test"},
							},
						},
					},
				},
			},
		},
		{
			name: "nil slack in base notifications",
			base: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{Project: "TEST"},
						// Slack is nil
					},
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "test", Channel: "#test"},
							},
						},
					},
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{Project: "TEST"},
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "test", Channel: "#test"},
							},
						},
					},
				},
			},
		},
		{
			name: "empty escalations",
			base: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{},
						},
					},
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "new", Channel: "#new"},
							},
						},
					},
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "new", Channel: "#new"},
							},
						},
					},
				},
			},
		},
		{
			name: "bool field override (enabled)",
			base: ReleaseQualifiers{
				"test": {
					Enabled: FALSE,
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Enabled: TRUE,
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Enabled: TRUE,
				},
			},
		},
		{
			name: "string field override with empty string",
			base: ReleaseQualifiers{
				"test": {
					BadgeName: "ORIGINAL",
				},
			},
			override: ReleaseQualifiers{
				"test": {
					BadgeName: "", // Empty string should not override
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					BadgeName: "ORIGINAL", // Should keep original
				},
			},
		},
		{
			name: "string field override with non-empty string",
			base: ReleaseQualifiers{
				"test": {
					BadgeName: "ORIGINAL",
				},
			},
			override: ReleaseQualifiers{
				"test": {
					BadgeName: "NEW",
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					BadgeName: "NEW",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.base.Merge(tt.override)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Merge() = %+v, want %+v", result, tt.expected)
			}
		})
	}
}

func TestReleaseQualifiers_Merge_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		base     ReleaseQualifiers
		override ReleaseQualifiers
		expected ReleaseQualifiers
		checkNil bool
	}{
		{
			name:     "nil base and override",
			base:     nil,
			override: nil,
			expected: ReleaseQualifiers{},
			checkNil: false,
		},
		{
			name: "complex nested merge",
			base: ReleaseQualifiers{
				"complex": {
					Enabled:   FALSE,
					BadgeName: "COMPLEX",
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "level1", Period: "1h", MinFailures: 1, Channel: "#level1", Mentions: []string{"@user1"}},
								{Name: "level2", Period: "2h", MinFailures: 2, Channel: "#level2", Mentions: []string{"@user2"}},
							},
						},
						Jira: &jira.Notification{
							Project:   "PROJ1",
							Component: "COMP1",
							Escalations: []jira.Escalation{
								{Name: "jira1", Failures: 1, Priority: "low", Mentions: []string{"@jira1"}},
							},
						},
					},
				},
			},
			override: ReleaseQualifiers{
				"complex": {
					Enabled: TRUE, // Override enabled
					// BadgeName not set, should keep original
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "level1", Period: "30m", MinFailures: 1, Channel: "#level1-new", Mentions: []string{"@user1", "@user3"}}, // Replace
								{Name: "level3", Period: "3h", MinFailures: 3, Channel: "#level3", Mentions: []string{"@user3"}},                // Add new
							},
						},
						Jira: &jira.Notification{
							Project: "PROJ2", // Override project
							// Component not set, should keep original
							Escalations: []jira.Escalation{
								{Name: "jira1", Failures: 2, Priority: "normal", Mentions: []string{"@jira1", "@jira2"}}, // Replace
								{Name: "jira2", Failures: 5, Priority: "high", Mentions: []string{"@jira2"}},             // Add new
							},
						},
					},
				},
			},
			expected: ReleaseQualifiers{
				"complex": {
					Enabled:   TRUE,
					BadgeName: "COMPLEX",
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "level1", Period: "30m", MinFailures: 1, Channel: "#level1-new", Mentions: []string{"@user1", "@user3"}}, // Replaced
								{Name: "level2", Period: "2h", MinFailures: 2, Channel: "#level2", Mentions: []string{"@user2"}},                // Kept
								{Name: "level3", Period: "3h", MinFailures: 3, Channel: "#level3", Mentions: []string{"@user3"}},                // Added
							},
						},
						Jira: &jira.Notification{
							Project:   "PROJ2",
							Component: "COMP1",
							Escalations: []jira.Escalation{
								{Name: "jira1", Failures: 2, Priority: "normal", Mentions: []string{"@jira1", "@jira2"}}, // Replaced
								{Name: "jira2", Failures: 5, Priority: "high", Mentions: []string{"@jira2"}},             // Added
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.base.Merge(tt.override)
			if result == nil {
				t.Error("Expected non-nil result for nil maps")
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Merge() = %+v, want %+v", result, tt.expected)
			}
		})
	}
}

func TestReleaseQualifiers_Merge_PreserveOrder(t *testing.T) {
	tests := []struct {
		name     string
		base     ReleaseQualifiers
		override ReleaseQualifiers
		expected ReleaseQualifiers
	}{
		{
			name: "merge doesn't break the alphabetical order of escalations",
			base: ReleaseQualifiers{
				"order": {
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "first", Period: "1h"},
								{Name: "second", Period: "2h"},
								{Name: "third", Period: "3h"},
							},
						},
					},
				},
			},
			override: ReleaseQualifiers{
				"order": {
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "second", Period: "2h-new"}, // Replace second
								{Name: "fourth", Period: "4h"},     // Add fourth
							},
						},
					},
				},
			},
			expected: ReleaseQualifiers{
				"order": {
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "first", Period: "1h"},
								{Name: "fourth", Period: "4h"},
								{Name: "second", Period: "2h-new"}, // Replaced
								{Name: "third", Period: "3h"},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.base.Merge(tt.override)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Merge() = %+v, want %+v", result, tt.expected)
			}
		})
	}
}

func TestReleaseQualifiers_Merge_EmptyStringHandling(t *testing.T) {
	tests := []struct {
		name     string
		base     ReleaseQualifiers
		override ReleaseQualifiers
		expected ReleaseQualifiers
	}{
		{
			name: "empty strings should not override existing values",
			base: ReleaseQualifiers{
				"test": {
					BadgeName:   "ORIGINAL",
					Summary:     "Original Summary",
					Description: "Original Description",
				},
			},
			override: ReleaseQualifiers{
				"test": {
					BadgeName:   "", // Empty string should not override
					Summary:     "New Summary",
					Description: "", // Empty string should not override
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					BadgeName:   "ORIGINAL",             // Empty string should not override
					Summary:     "New Summary",          // Non-empty string should override
					Description: "Original Description", // Empty string should not override
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.base.Merge(tt.override)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Merge() = %+v, want %+v", result, tt.expected)
			}
		})
	}
}

func TestReleaseQualifiers_Merge_ComprehensiveEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		base     ReleaseQualifiers
		override ReleaseQualifiers
		expected ReleaseQualifiers
	}{
		{
			name: "nil base with non-nil override",
			base: nil,
			override: ReleaseQualifiers{
				"test": {
					Enabled:   TRUE,
					BadgeName: "TEST",
				},
			},
			expected: ReleaseQualifiers{},
		},
		{
			name: "non-nil base with nil override",
			base: ReleaseQualifiers{
				"test": {
					Enabled:   TRUE,
					BadgeName: "TEST",
				},
			},
			override: nil,
			expected: ReleaseQualifiers{},
		},
		{
			name: "override enabled from nil to false",
			base: ReleaseQualifiers{
				"test": {
					BadgeName: "TEST",
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Enabled: FALSE,
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Enabled:   FALSE,
					BadgeName: "TEST",
				},
			},
		},
		{
			name: "override enabled from false to true",
			base: ReleaseQualifiers{
				"test": {
					Enabled:   FALSE,
					BadgeName: "TEST",
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Enabled: TRUE,
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Enabled:   TRUE,
					BadgeName: "TEST",
				},
			},
		},
		{
			name: "merge with labels override",
			base: ReleaseQualifiers{
				"test": {
					Labels: []string{"old-label"},
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Labels: []string{"new-label1", "new-label2"},
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Labels: []string{"new-label1", "new-label2"},
				},
			},
		},
		{
			name: "override labels with empty slice",
			base: ReleaseQualifiers{
				"test": {
					Labels: []string{"old-label"},
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Labels: []string{}, // Override with empty slice
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Labels: []string{},
				},
			},
		},
		{
			name: "all payload badge status combinations",
			base: ReleaseQualifiers{
				"test1": {
					PayloadBadge: PayloadBadgeYes,
				},
				"test2": {
					PayloadBadge: PayloadBadgeNo,
				},
			},
			override: ReleaseQualifiers{
				"test1": {
					PayloadBadge: PayloadBadgeOnSuccess,
				},
				"test2": {
					PayloadBadge: PayloadBadgeOnFailure,
				},
			},
			expected: ReleaseQualifiers{
				"test1": {
					PayloadBadge: PayloadBadgeOnSuccess,
				},
				"test2": {
					PayloadBadge: PayloadBadgeOnFailure,
				},
			},
		},
		{
			name: "empty escalation lists",
			base: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{},
						},
						Jira: &jira.Notification{
							Escalations: []jira.Escalation{},
						},
					},
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "new", Period: "1h", MinFailures: 1},
							},
						},
						Jira: &jira.Notification{
							Escalations: []jira.Escalation{
								{Name: "new", Failures: 1, Priority: "low"},
							},
						},
					},
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Slack: &slack.Notification{
							Escalations: []slack.Escalation{
								{Name: "new", Period: "1h", MinFailures: 1},
							},
						},
						Jira: &jira.Notification{
							Escalations: []jira.Escalation{
								{Name: "new", Failures: 1, Priority: "low"},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.base.Merge(tt.override)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Merge() = %+v, want %+v", result, tt.expected)
			}
		})
	}
}

func TestReleaseQualifier_Merge(t *testing.T) {
	tests := []struct {
		name     string
		base     ReleaseQualifier
		override ReleaseQualifier
		expected ReleaseQualifier
	}{
		{
			name:     "empty base and override",
			base:     ReleaseQualifier{},
			override: ReleaseQualifier{},
			expected: ReleaseQualifier{},
		},
		{
			name: "empty base with override",
			base: ReleaseQualifier{},
			override: ReleaseQualifier{
				Enabled:   TRUE,
				BadgeName: "TEST",
			},
			expected: ReleaseQualifier{
				Enabled:   TRUE,
				BadgeName: "TEST",
			},
		},
		{
			name: "base with empty override",
			base: ReleaseQualifier{
				Enabled:   TRUE,
				BadgeName: "TEST",
			},
			override: ReleaseQualifier{},
			expected: ReleaseQualifier{
				Enabled:   TRUE,
				BadgeName: "TEST",
			},
		},
		{
			name: "override all simple fields",
			base: ReleaseQualifier{
				Enabled:      FALSE,
				BadgeName:    "OLD_BADGE",
				Summary:      "Old Summary",
				Description:  "Old Description",
				PayloadBadge: PayloadBadgeNo,
			},
			override: ReleaseQualifier{
				Enabled:      TRUE,
				BadgeName:    "NEW_BADGE",
				Summary:      "New Summary",
				Description:  "New Description",
				PayloadBadge: PayloadBadgeYes,
			},
			expected: ReleaseQualifier{
				Enabled:      TRUE,
				BadgeName:    "NEW_BADGE",
				Summary:      "New Summary",
				Description:  "New Description",
				PayloadBadge: PayloadBadgeYes,
			},
		},
		{
			name: "partial override - only some fields",
			base: ReleaseQualifier{
				Enabled:      FALSE,
				BadgeName:    "BASE_BADGE",
				Summary:      "Base Summary",
				Description:  "Base Description",
				PayloadBadge: PayloadBadgeNo,
			},
			override: ReleaseQualifier{
				BadgeName: "OVERRIDE_BADGE",
				Summary:   "Override Summary",
			},
			expected: ReleaseQualifier{
				Enabled:      FALSE,
				BadgeName:    "OVERRIDE_BADGE",
				Summary:      "Override Summary",
				Description:  "Base Description",
				PayloadBadge: PayloadBadgeNo,
			},
		},
		{
			name: "empty strings don't override",
			base: ReleaseQualifier{
				BadgeName:   "ORIGINAL",
				Summary:     "Original Summary",
				Description: "Original Description",
			},
			override: ReleaseQualifier{
				BadgeName:   "",
				Summary:     "New Summary",
				Description: "",
			},
			expected: ReleaseQualifier{
				BadgeName:   "ORIGINAL",
				Summary:     "New Summary",
				Description: "Original Description",
			},
		},
		{
			name: "override enabled from nil to false",
			base: ReleaseQualifier{
				BadgeName: "TEST",
			},
			override: ReleaseQualifier{
				Enabled: FALSE,
			},
			expected: ReleaseQualifier{
				Enabled:   FALSE,
				BadgeName: "TEST",
			},
		},
		{
			name: "override enabled from false to true",
			base: ReleaseQualifier{
				Enabled:   FALSE,
				BadgeName: "TEST",
			},
			override: ReleaseQualifier{
				Enabled: TRUE,
			},
			expected: ReleaseQualifier{
				Enabled:   TRUE,
				BadgeName: "TEST",
			},
		},
		{
			name: "override labels",
			base: ReleaseQualifier{
				Labels: []string{"old-label1", "old-label2"},
			},
			override: ReleaseQualifier{
				Labels: []string{"new-label1", "new-label2", "new-label3"},
			},
			expected: ReleaseQualifier{
				Labels: []string{"new-label1", "new-label2", "new-label3"},
			},
		},
		{
			name: "override labels with empty slice",
			base: ReleaseQualifier{
				Labels: []string{"old-label"},
			},
			override: ReleaseQualifier{
				Labels: []string{},
			},
			expected: ReleaseQualifier{
				Labels: []string{},
			},
		},
		{
			name: "override PayloadBadge variants",
			base: ReleaseQualifier{
				PayloadBadge: PayloadBadgeYes,
			},
			override: ReleaseQualifier{
				PayloadBadge: PayloadBadgeOnSuccess,
			},
			expected: ReleaseQualifier{
				PayloadBadge: PayloadBadgeOnSuccess,
			},
		},
		{
			name: "add Slack notifications to empty base",
			base: ReleaseQualifier{
				Enabled: TRUE,
			},
			override: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{
								Name:        "test",
								Period:      "24h",
								MinFailures: 1,
								Channel:     "#test",
							},
						},
					},
				},
			},
			expected: ReleaseQualifier{
				Enabled: TRUE,
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{
								Name:        "test",
								Period:      "24h",
								MinFailures: 1,
								Channel:     "#test",
							},
						},
					},
				},
			},
		},
		{
			name: "add Jira notifications to empty base",
			base: ReleaseQualifier{
				Enabled: TRUE,
			},
			override: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project:     "TEST",
						Component:   "TestComp",
						Assignee:    "test@example.com",
						Escalations: []jira.Escalation{},
					},
				},
			},
			expected: ReleaseQualifier{
				Enabled: TRUE,
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project:     "TEST",
						Component:   "TestComp",
						Assignee:    "test@example.com",
						Escalations: []jira.Escalation{},
					},
				},
			},
		},
		{
			name: "merge Slack escalations - add new",
			base: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "low", Period: "24h", MinFailures: 1},
						},
					},
				},
			},
			override: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "high", Period: "72h", MinFailures: 5},
						},
					},
				},
			},
			expected: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "high", Period: "72h", MinFailures: 5},
							{Name: "low", Period: "24h", MinFailures: 1},
						},
					},
				},
			},
		},
		{
			name: "merge Slack escalations - replace existing",
			base: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "test", Period: "24h", MinFailures: 1, Channel: "#old"},
						},
					},
				},
			},
			override: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "test", Period: "48h", MinFailures: 3, Channel: "#new"},
						},
					},
				},
			},
			expected: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "test", Period: "48h", MinFailures: 3, Channel: "#new"},
						},
					},
				},
			},
		},
		{
			name: "merge Jira escalations - add new",
			base: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project: "TEST",
						Escalations: []jira.Escalation{
							{Name: "low", Failures: 1, Priority: "low"},
						},
					},
				},
			},
			override: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Escalations: []jira.Escalation{
							{Name: "high", Failures: 5, Priority: "high"},
						},
					},
				},
			},
			expected: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project: "TEST",
						Escalations: []jira.Escalation{
							{Name: "high", Failures: 5, Priority: "high"},
							{Name: "low", Failures: 1, Priority: "low"},
						},
					},
				},
			},
		},
		{
			name: "merge Jira escalations - replace existing",
			base: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project: "TEST",
						Escalations: []jira.Escalation{
							{Name: "test", Failures: 1, Priority: "low"},
						},
					},
				},
			},
			override: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Escalations: []jira.Escalation{
							{Name: "test", Failures: 5, Priority: "high"},
						},
					},
				},
			},
			expected: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project: "TEST",
						Escalations: []jira.Escalation{
							{Name: "test", Failures: 5, Priority: "high"},
						},
					},
				},
			},
		},
		{
			name: "override Jira Project field",
			base: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project:     "BASE_PROJECT",
						Component:   "BaseComp",
						Escalations: []jira.Escalation{},
					},
				},
			},
			override: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project:     "OVERRIDE_PROJECT",
						Escalations: []jira.Escalation{},
					},
				},
			},
			expected: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project:     "OVERRIDE_PROJECT",
						Component:   "BaseComp",
						Escalations: []jira.Escalation{},
					},
				},
			},
		},
		{
			name: "add both Slack and Jira notifications",
			base: ReleaseQualifier{
				Enabled: TRUE,
			},
			override: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "slack-test", Period: "24h", MinFailures: 1},
						},
					},
					Jira: &jira.Notification{
						Project: "TEST",
						Escalations: []jira.Escalation{
							{Name: "jira-test", Failures: 1, Priority: "low"},
						},
					},
				},
			},
			expected: ReleaseQualifier{
				Enabled: TRUE,
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "slack-test", Period: "24h", MinFailures: 1},
						},
					},
					Jira: &jira.Notification{
						Project: "TEST",
						Escalations: []jira.Escalation{
							{Name: "jira-test", Failures: 1, Priority: "low"},
						},
					},
				},
			},
		},
		{
			name: "complex merge - all fields",
			base: ReleaseQualifier{
				Enabled:      FALSE,
				BadgeName:    "BASE",
				Summary:      "Base Summary",
				Description:  "Base Description",
				PayloadBadge: PayloadBadgeNo,
				Labels:       []string{"base-label"},
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "base-slack", Period: "24h", MinFailures: 1},
						},
					},
					Jira: &jira.Notification{
						Project: "BASE",
						Escalations: []jira.Escalation{
							{Name: "base-jira", Failures: 1, Priority: "low"},
						},
					},
				},
			},
			override: ReleaseQualifier{
				Enabled:      TRUE,
				BadgeName:    "OVERRIDE",
				Summary:      "Override Summary",
				PayloadBadge: PayloadBadgeOnSuccess,
				Labels:       []string{"override-label1", "override-label2"},
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "override-slack", Period: "48h", MinFailures: 3},
						},
					},
					Jira: &jira.Notification{
						Project: "OVERRIDE",
						Escalations: []jira.Escalation{
							{Name: "override-jira", Failures: 5, Priority: "high"},
						},
					},
				},
			},
			expected: ReleaseQualifier{
				Enabled:      TRUE,
				BadgeName:    "OVERRIDE",
				Summary:      "Override Summary",
				Description:  "Base Description",
				PayloadBadge: PayloadBadgeOnSuccess,
				Labels:       []string{"override-label1", "override-label2"},
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "base-slack", Period: "24h", MinFailures: 1},
							{Name: "override-slack", Period: "48h", MinFailures: 3},
						},
					},
					Jira: &jira.Notification{
						Project: "OVERRIDE",
						Escalations: []jira.Escalation{
							{Name: "base-jira", Failures: 1, Priority: "low"},
							{Name: "override-jira", Failures: 5, Priority: "high"},
						},
					},
				},
			},
		},
		{
			name: "empty base with notifications",
			base: ReleaseQualifier{},
			override: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "test", Period: "24h", MinFailures: 1},
						},
					},
				},
			},
			expected: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "test", Period: "24h", MinFailures: 1},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.base.Merge(tt.override)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Merge() = %+v, want %+v", result, tt.expected)
			}
		})
	}
}

func TestReleaseQualifier_Merge_PointerReceiver(t *testing.T) {
	tests := []struct {
		name     string
		base     *ReleaseQualifier
		override ReleaseQualifier
		expected ReleaseQualifier
	}{
		{
			name: "pointer receiver with basic override",
			base: &ReleaseQualifier{
				Enabled:   FALSE,
				BadgeName: "BASE",
			},
			override: ReleaseQualifier{
				Enabled:   TRUE,
				BadgeName: "OVERRIDE",
			},
			expected: ReleaseQualifier{
				Enabled:   TRUE,
				BadgeName: "OVERRIDE",
			},
		},
		{
			name: "pointer receiver preserves base when override is empty",
			base: &ReleaseQualifier{
				Enabled:     TRUE,
				BadgeName:   "BASE",
				Summary:     "Base Summary",
				Description: "Base Description",
			},
			override: ReleaseQualifier{},
			expected: ReleaseQualifier{
				Enabled:     TRUE,
				BadgeName:   "BASE",
				Summary:     "Base Summary",
				Description: "Base Description",
			},
		},
		{
			name: "pointer receiver with notifications",
			base: &ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "base", Period: "24h", MinFailures: 1},
						},
					},
				},
			},
			override: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "override", Period: "48h", MinFailures: 2},
						},
					},
				},
			},
			expected: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Slack: &slack.Notification{
						Escalations: []slack.Escalation{
							{Name: "base", Period: "24h", MinFailures: 1},
							{Name: "override", Period: "48h", MinFailures: 2},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.base.Merge(tt.override)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Merge() = %+v, want %+v", result, tt.expected)
			}
		})
	}
}
