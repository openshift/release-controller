package releasequalifiers

import (
	"reflect"
	"testing"

	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications"
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/jira"
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
			name: "merge notifications - add jira to existing qualifier",
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
						Jira: &jira.Notification{
							Component: "NewComp",
							Escalations: []jira.Escalation{
								{Name: "test", Failures: 1, Priority: "low"},
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
							Project:   "TEST",
							Component: "NewComp",
							Escalations: []jira.Escalation{
								{Name: "test", Failures: 1, Priority: "low"},
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
						Jira: &jira.Notification{
							Project: "TEST",
							Escalations: []jira.Escalation{
								{Name: "old", Failures: 2, Priority: "low"},
								{Name: "keep", Failures: 1, Priority: "normal"},
							},
						},
					},
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Escalations: []jira.Escalation{
								{Name: "old", Failures: 1, Priority: "normal"}, // Replace
								{Name: "new", Failures: 1, Priority: "high"},   // Add new
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
							Escalations: []jira.Escalation{
								{Name: "keep", Failures: 1, Priority: "normal"}, // Kept
								{Name: "new", Failures: 1, Priority: "high"},    // Added
								{Name: "old", Failures: 1, Priority: "normal"},  // Replaced
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
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "TEST",
							Escalations: []jira.Escalation{
								{Name: "test", Failures: 1, Priority: "low"},
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
							Escalations: []jira.Escalation{
								{Name: "test", Failures: 1, Priority: "low"},
							},
						},
					},
				},
			},
		},
		{
			name: "nil jira in base notifications",
			base: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{},
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "TEST",
							Escalations: []jira.Escalation{
								{Name: "test", Failures: 1, Priority: "low"},
							},
						},
					},
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "TEST",
							Escalations: []jira.Escalation{
								{Name: "test", Failures: 1, Priority: "low"},
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
						Jira: &jira.Notification{
							Project:     "TEST",
							Escalations: []jira.Escalation{},
						},
					},
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
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
						Jira: &jira.Notification{
							Project: "TEST",
							Escalations: []jira.Escalation{
								{Name: "new", Failures: 1, Priority: "low"},
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
						Jira: &jira.Notification{
							Project:   "PROJ1",
							Component: "COMP1",
							Escalations: []jira.Escalation{
								{Name: "jira1", Failures: 1, Priority: "low", Mentions: []string{"@jira1"}},
								{Name: "jira2", Failures: 2, Priority: "normal", Mentions: []string{"@jira2"}},
							},
						},
					},
				},
			},
			override: ReleaseQualifiers{
				"complex": {
					Enabled: TRUE, // Override enabled
					// Name not set, should keep original
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "PROJ2", // Override project
							// Component not set, should keep original
							Escalations: []jira.Escalation{
								{Name: "jira1", Failures: 2, Priority: "normal", Mentions: []string{"@jira1", "@jira3"}}, // Replace
								{Name: "jira3", Failures: 5, Priority: "high", Mentions: []string{"@jira3"}},             // Add new
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
						Jira: &jira.Notification{
							Project:   "PROJ2",
							Component: "COMP1",
							Escalations: []jira.Escalation{
								{Name: "jira1", Failures: 2, Priority: "normal", Mentions: []string{"@jira1", "@jira3"}}, // Replaced
								{Name: "jira2", Failures: 2, Priority: "normal", Mentions: []string{"@jira2"}},           // Kept
								{Name: "jira3", Failures: 5, Priority: "high", Mentions: []string{"@jira3"}},             // Added
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
						Jira: &jira.Notification{
							Project: "TEST",
							Escalations: []jira.Escalation{
								{Name: "first", Failures: 1, Priority: "low"},
								{Name: "second", Failures: 2, Priority: "normal"},
								{Name: "third", Failures: 3, Priority: "high"},
							},
						},
					},
				},
			},
			override: ReleaseQualifiers{
				"order": {
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Escalations: []jira.Escalation{
								{Name: "second", Failures: 2, Priority: "high"}, // Replace second
								{Name: "fourth", Failures: 4, Priority: "high"}, // Add fourth
							},
						},
					},
				},
			},
			expected: ReleaseQualifiers{
				"order": {
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Project: "TEST",
							Escalations: []jira.Escalation{
								{Name: "first", Failures: 1, Priority: "low"},
								{Name: "fourth", Failures: 4, Priority: "high"},
								{Name: "second", Failures: 2, Priority: "high"}, // Replaced
								{Name: "third", Failures: 3, Priority: "high"},
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
			name: "override approval from nil to true",
			base: ReleaseQualifiers{
				"test": {
					BadgeName: "TEST",
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Approval: TRUE,
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Approval:  TRUE,
					BadgeName: "TEST",
				},
			},
		},
		{
			name: "override approval from true to false",
			base: ReleaseQualifiers{
				"test": {
					Approval:  TRUE,
					BadgeName: "TEST",
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Approval: FALSE,
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Approval:  FALSE,
					BadgeName: "TEST",
				},
			},
		},
		{
			name: "nil approval does not override existing",
			base: ReleaseQualifiers{
				"test": {
					Approval:  TRUE,
					BadgeName: "TEST",
				},
			},
			override: ReleaseQualifiers{
				"test": {
					BadgeName: "NEW",
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					Approval:  TRUE,
					BadgeName: "NEW",
				},
			},
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
					FailureLabels: []string{"old-label"},
				},
			},
			override: ReleaseQualifiers{
				"test": {
					FailureLabels: []string{"new-label1", "new-label2"},
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					FailureLabels: []string{"new-label1", "new-label2"},
				},
			},
		},
		{
			name: "override labels with empty slice",
			base: ReleaseQualifiers{
				"test": {
					FailureLabels: []string{"old-label"},
				},
			},
			override: ReleaseQualifiers{
				"test": {
					FailureLabels: []string{}, // Override with empty slice
				},
			},
			expected: ReleaseQualifiers{
				"test": {
					FailureLabels: []string{},
				},
			},
		},
		{
			name: "all payload badge status combinations",
			base: ReleaseQualifiers{
				"test1": {
					PayloadBadgeStatus: BadgeStatusYes,
				},
				"test2": {
					PayloadBadgeStatus: BadgeStatusNo,
				},
			},
			override: ReleaseQualifiers{
				"test1": {
					PayloadBadgeStatus: BadgeStatusOnSuccess,
				},
				"test2": {
					PayloadBadgeStatus: BadgeStatusOnFailure,
				},
			},
			expected: ReleaseQualifiers{
				"test1": {
					PayloadBadgeStatus: BadgeStatusOnSuccess,
				},
				"test2": {
					PayloadBadgeStatus: BadgeStatusOnFailure,
				},
			},
		},
		{
			name: "empty escalation lists",
			base: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
						Jira: &jira.Notification{
							Escalations: []jira.Escalation{},
						},
					},
				},
			},
			override: ReleaseQualifiers{
				"test": {
					Notifications: &notifications.Notifications{
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
				Enabled:            FALSE,
				BadgeName:          "OLD_BADGE",
				Summary:            "Old Summary",
				Description:        "Old Description",
				PayloadBadgeStatus: BadgeStatusNo,
			},
			override: ReleaseQualifier{
				Enabled:            TRUE,
				BadgeName:          "NEW_BADGE",
				Summary:            "New Summary",
				Description:        "New Description",
				PayloadBadgeStatus: BadgeStatusYes,
			},
			expected: ReleaseQualifier{
				Enabled:            TRUE,
				BadgeName:          "NEW_BADGE",
				Summary:            "New Summary",
				Description:        "New Description",
				PayloadBadgeStatus: BadgeStatusYes,
			},
		},
		{
			name: "partial override - only some fields",
			base: ReleaseQualifier{
				Enabled:            FALSE,
				BadgeName:          "BASE_BADGE",
				Summary:            "Base Summary",
				Description:        "Base Description",
				PayloadBadgeStatus: BadgeStatusNo,
			},
			override: ReleaseQualifier{
				BadgeName: "OVERRIDE_BADGE",
				Summary:   "Override Summary",
			},
			expected: ReleaseQualifier{
				Enabled:            FALSE,
				BadgeName:          "OVERRIDE_BADGE",
				Summary:            "Override Summary",
				Description:        "Base Description",
				PayloadBadgeStatus: BadgeStatusNo,
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
				FailureLabels: []string{"old-label1", "old-label2"},
			},
			override: ReleaseQualifier{
				FailureLabels: []string{"new-label1", "new-label2", "new-label3"},
			},
			expected: ReleaseQualifier{
				FailureLabels: []string{"new-label1", "new-label2", "new-label3"},
			},
		},
		{
			name: "override labels with empty slice",
			base: ReleaseQualifier{
				FailureLabels: []string{"old-label"},
			},
			override: ReleaseQualifier{
				FailureLabels: []string{},
			},
			expected: ReleaseQualifier{
				FailureLabels: []string{},
			},
		},
		{
			name: "override Badge variants",
			base: ReleaseQualifier{
				PayloadBadgeStatus: BadgeStatusYes,
			},
			override: ReleaseQualifier{
				PayloadBadgeStatus: BadgeStatusOnSuccess,
			},
			expected: ReleaseQualifier{
				PayloadBadgeStatus: BadgeStatusOnSuccess,
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
			name: "complex merge - all fields",
			base: ReleaseQualifier{
				Enabled:            FALSE,
				BadgeName:          "BASE",
				Summary:            "Base Summary",
				Description:        "Base Description",
				PayloadBadgeStatus: BadgeStatusNo,
				FailureLabels:      []string{"base-label"},
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project: "BASE",
						Escalations: []jira.Escalation{
							{Name: "base-jira", Failures: 1, Priority: "low"},
						},
					},
				},
			},
			override: ReleaseQualifier{
				Enabled:            TRUE,
				BadgeName:          "OVERRIDE",
				Summary:            "Override Summary",
				PayloadBadgeStatus: BadgeStatusOnSuccess,
				FailureLabels:      []string{"override-label1", "override-label2"},
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project: "OVERRIDE",
						Escalations: []jira.Escalation{
							{Name: "override-jira", Failures: 5, Priority: "high"},
						},
					},
				},
			},
			expected: ReleaseQualifier{
				Enabled:            TRUE,
				BadgeName:          "OVERRIDE",
				Summary:            "Override Summary",
				Description:        "Base Description",
				PayloadBadgeStatus: BadgeStatusOnSuccess,
				FailureLabels:      []string{"override-label1", "override-label2"},
				Notifications: &notifications.Notifications{
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
					Jira: &jira.Notification{
						Project: "TEST",
						Escalations: []jira.Escalation{
							{Name: "test", Failures: 1, Priority: "low"},
						},
					},
				},
			},
			expected: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project: "TEST",
						Escalations: []jira.Escalation{
							{Name: "test", Failures: 1, Priority: "low"},
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
					Jira: &jira.Notification{
						Project: "TEST",
						Escalations: []jira.Escalation{
							{Name: "base", Failures: 1, Priority: "low"},
						},
					},
				},
			},
			override: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Escalations: []jira.Escalation{
							{Name: "override", Failures: 2, Priority: "normal"},
						},
					},
				},
			},
			expected: ReleaseQualifier{
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{
						Project: "TEST",
						Escalations: []jira.Escalation{
							{Name: "base", Failures: 1, Priority: "low"},
							{Name: "override", Failures: 2, Priority: "normal"},
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
