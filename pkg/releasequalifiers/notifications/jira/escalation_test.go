package jira

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

// TestEscalationConfigurationPatterns tests the various Jira escalation configuration patterns
// as specified in the design document section "Jira Escalation Configuration Options"
func TestEscalationConfigurationPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		yamlConfig     string
		wantEscalation Escalation
	}{
		{
			name: "SimpleConsecutiveFailures",
			yamlConfig: `
name: low
failures: 3
priority: Low
`,
			wantEscalation: Escalation{
				Name:     "low",
				Failures: 3,
				Priority: "Low",
			},
		},
		{
			name: "FailuresOverWindow",
			yamlConfig: `
name: monitor
failures: 2
overLastRuns: 10
priority: Normal
`,
			wantEscalation: Escalation{
				Name:         "monitor",
				Failures:     2,
				OverLastRuns: intPtr(10),
				Priority:     "Normal",
			},
		},
		{
			name: "PassPercentageOverWindow",
			yamlConfig: `
name: quality
overLastRuns: 10
passPercentage: 60
priority: High
`,
			wantEscalation: Escalation{
				Name:           "quality",
				OverLastRuns:   intPtr(10),
				PassPercentage: intPtr(60),
				Priority:       "High",
			},
		},
		{
			name: "TimeBoundedPassPercentage",
			yamlConfig: `
name: reliability
overLastRuns: 20
overPeriod: 2d
passPercentage: 80
priority: Critical
needsInfo:
  - scuppett
`,
			wantEscalation: Escalation{
				Name:           "reliability",
				OverLastRuns:   intPtr(20),
				OverPeriod:     "2d",
				PassPercentage: intPtr(80),
				Priority:       "Critical",
				NeedsInfo:      []string{"scuppett"},
			},
		},
		{
			name: "TimeBoundedFailures",
			yamlConfig: `
name: critical
overLastRuns: 20
overPeriod: 2d
failures: 10
priority: Blocker
`,
			wantEscalation: Escalation{
				Name:         "critical",
				OverLastRuns: intPtr(20),
				OverPeriod:   "2d",
				Failures:     10,
				Priority:     "Blocker",
			},
		},
		{
			name: "WithMentions",
			yamlConfig: `
name: high
failures: 6
priority: High
mentions:
  - scuppett
  - team-lead
`,
			wantEscalation: Escalation{
				Name:     "high",
				Failures: 6,
				Priority: "High",
				Mentions: []string{"scuppett", "team-lead"},
			},
		},
		{
			name: "WithBothMentionsAndNeedsInfo",
			yamlConfig: `
name: major
overLastRuns: 15
passPercentage: 70
priority: Major
mentions:
  - oncall
needsInfo:
  - product-owner
  - qa-lead
`,
			wantEscalation: Escalation{
				Name:           "major",
				OverLastRuns:   intPtr(15),
				PassPercentage: intPtr(70),
				Priority:       "Major",
				Mentions:       []string{"oncall"},
				NeedsInfo:      []string{"product-owner", "qa-lead"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Escalation
			if err := yaml.Unmarshal([]byte(tt.yamlConfig), &got); err != nil {
				t.Fatalf("Failed to unmarshal YAML: %v", err)
			}

			if diff := cmp.Diff(tt.wantEscalation, got); diff != "" {
				t.Errorf("Escalation mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestNotificationWithMultipleEscalations tests a complete Notification configuration
// with multiple escalation levels as shown in the design document
func TestNotificationWithMultipleEscalations(t *testing.T) {
	t.Parallel()

	yamlConfig := `
project: OCPBUGS
component: foo
assignee: test.user@example.com
summary: Test Summary
description: Test Description
thread: test-thread
escalations:
  - name: low
    failures: 2
    priority: Low
  - name: normal
    failures: 4
    priority: Normal
  - name: high
    failures: 6
    priority: High
    mentions:
      - scuppett
  - name: critical
    failures: 8
    priority: Critical
`

	var got Notification
	if err := yaml.Unmarshal([]byte(yamlConfig), &got); err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	want := Notification{
		Project:     "OCPBUGS",
		Component:   "foo",
		Assignee:    "test.user@example.com",
		Summary:     "Test Summary",
		Description: "Test Description",
		Thread:      "test-thread",
		Escalations: []Escalation{
			{
				Name:     "low",
				Failures: 2,
				Priority: "Low",
			},
			{
				Name:     "normal",
				Failures: 4,
				Priority: "Normal",
			},
			{
				Name:     "high",
				Failures: 6,
				Priority: "High",
				Mentions: []string{"scuppett"},
			},
			{
				Name:     "critical",
				Failures: 8,
				Priority: "Critical",
			},
		},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Notification mismatch (-want +got):\n%s", diff)
	}

	// Verify we have the expected number of escalations
	if len(got.Escalations) != 4 {
		t.Errorf("Expected 4 escalations, got %d", len(got.Escalations))
	}
}

// TestEscalationEdgeCases tests edge cases and validation scenarios
func TestEscalationEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		yamlConfig string
		validate   func(t *testing.T, esc Escalation)
	}{
		{
			name: "ZeroPassPercentage",
			yamlConfig: `
name: test
overLastRuns: 10
passPercentage: 0
priority: Low
`,
			validate: func(t *testing.T, esc Escalation) {
				if esc.PassPercentage == nil {
					t.Error("PassPercentage should not be nil")
				} else if *esc.PassPercentage != 0 {
					t.Errorf("Expected PassPercentage 0, got %d", *esc.PassPercentage)
				}
			},
		},
		{
			name: "HundredPercentPassPercentage",
			yamlConfig: `
name: test
overLastRuns: 10
passPercentage: 100
priority: Low
`,
			validate: func(t *testing.T, esc Escalation) {
				if esc.PassPercentage == nil {
					t.Error("PassPercentage should not be nil")
				} else if *esc.PassPercentage != 100 {
					t.Errorf("Expected PassPercentage 100, got %d", *esc.PassPercentage)
				}
			},
		},
		{
			name: "SingleFailure",
			yamlConfig: `
name: test
failures: 1
priority: Low
`,
			validate: func(t *testing.T, esc Escalation) {
				if esc.Failures != 1 {
					t.Errorf("Expected Failures 1, got %d", esc.Failures)
				}
			},
		},
		{
			name: "LargeWindow",
			yamlConfig: `
name: test
overLastRuns: 1000
passPercentage: 95
priority: Low
`,
			validate: func(t *testing.T, esc Escalation) {
				if esc.OverLastRuns == nil {
					t.Error("OverLastRuns should not be nil")
				} else if *esc.OverLastRuns != 1000 {
					t.Errorf("Expected OverLastRuns 1000, got %d", *esc.OverLastRuns)
				}
			},
		},
		{
			name: "VariousTimePeriods",
			yamlConfig: `
name: test
overLastRuns: 10
overPeriod: 7d
failures: 5
priority: Low
`,
			validate: func(t *testing.T, esc Escalation) {
				if esc.OverPeriod != "7d" {
					t.Errorf("Expected OverPeriod '7d', got '%s'", esc.OverPeriod)
				}
			},
		},
		{
			name: "EmptyMentionsAndNeedsInfo",
			yamlConfig: `
name: test
failures: 3
priority: Low
mentions: []
needsInfo: []
`,
			validate: func(t *testing.T, esc Escalation) {
				if esc.Mentions == nil {
					t.Error("Mentions should be empty slice, not nil")
				}
				if esc.NeedsInfo == nil {
					t.Error("NeedsInfo should be empty slice, not nil")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Escalation
			if err := yaml.Unmarshal([]byte(tt.yamlConfig), &got); err != nil {
				t.Fatalf("Failed to unmarshal YAML: %v", err)
			}

			tt.validate(t, got)
		})
	}
}

// TestEscalationSorting tests that escalations can be sorted by name
func TestEscalationSorting(t *testing.T) {
	t.Parallel()

	escalations := []Escalation{
		{Name: "critical", Priority: "Critical"},
		{Name: "low", Priority: "Low"},
		{Name: "high", Priority: "High"},
		{Name: "medium", Priority: "Medium"},
	}

	// This would typically be used with sort.Sort()
	sorter := ByJiraEscalationName(escalations)

	if sorter.Len() != 4 {
		t.Errorf("Expected length 4, got %d", sorter.Len())
	}

	// Test Less
	if !sorter.Less(0, 1) { // "critical" < "low"
		t.Error("Expected 'critical' to be less than 'low'")
	}
	if sorter.Less(1, 0) { // "low" > "critical"
		t.Error("Expected 'low' to be greater than 'critical'")
	}

	// Test Swap
	sorter.Swap(0, 1)
	if escalations[0].Name != "low" || escalations[1].Name != "critical" {
		t.Error("Swap did not work correctly")
	}
}

// TestCompleteJiraConfiguration tests a complete Jira notification configuration
// combining all the configuration options from the design document
func TestCompleteJiraConfiguration(t *testing.T) {
	t.Parallel()

	yamlConfig := `
project: OCPBUGS
component: Release
assignee: trt-oncall@redhat.com
summary: "[{stream}:{qualifierId}:{jobId}] Qualifying job failure"
description: "The qualifying job {jobId} has failed {failureCount} times"
thread: main
escalations:
  - name: low
    failures: 2
    priority: Low
  - name: normal
    failures: 4
    priority: Normal
    overLastRuns: 10
  - name: high
    failures: 6
    priority: High
    mentions:
      - trt-lead
  - name: critical
    overLastRuns: 20
    overPeriod: 2d
    passPercentage: 50
    priority: Critical
    mentions:
      - trt-manager
    needsInfo:
      - component-owner
`

	var got Notification
	if err := yaml.Unmarshal([]byte(yamlConfig), &got); err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	// Validate basic fields
	if got.Project != "OCPBUGS" {
		t.Errorf("Expected Project 'OCPBUGS', got '%s'", got.Project)
	}
	if got.Component != "Release" {
		t.Errorf("Expected Component 'Release', got '%s'", got.Component)
	}
	if got.Assignee != "trt-oncall@redhat.com" {
		t.Errorf("Expected Assignee 'trt-oncall@redhat.com', got '%s'", got.Assignee)
	}
	if got.Thread != "main" {
		t.Errorf("Expected Thread 'main', got '%s'", got.Thread)
	}

	// Validate escalations
	if len(got.Escalations) != 4 {
		t.Fatalf("Expected 4 escalations, got %d", len(got.Escalations))
	}

	// Validate low escalation - simple consecutive failures
	low := got.Escalations[0]
	if low.Name != "low" {
		t.Errorf("Expected escalation[0] name 'low', got '%s'", low.Name)
	}
	if low.Failures != 2 {
		t.Errorf("Expected escalation[0] failures 2, got %d", low.Failures)
	}
	if low.Priority != "Low" {
		t.Errorf("Expected escalation[0] priority 'Low', got '%s'", low.Priority)
	}

	// Validate normal escalation - failures over window
	normal := got.Escalations[1]
	if normal.Name != "normal" {
		t.Errorf("Expected escalation[1] name 'normal', got '%s'", normal.Name)
	}
	if normal.Failures != 4 {
		t.Errorf("Expected escalation[1] failures 4, got %d", normal.Failures)
	}
	if normal.OverLastRuns == nil || *normal.OverLastRuns != 10 {
		t.Errorf("Expected escalation[1] overLastRuns 10, got %v", normal.OverLastRuns)
	}

	// Validate high escalation - with mentions
	high := got.Escalations[2]
	if high.Name != "high" {
		t.Errorf("Expected escalation[2] name 'high', got '%s'", high.Name)
	}
	if !cmp.Equal(high.Mentions, []string{"trt-lead"}) {
		t.Errorf("Expected escalation[2] mentions [trt-lead], got %v", high.Mentions)
	}

	// Validate critical escalation - time-bounded with pass percentage
	critical := got.Escalations[3]
	if critical.Name != "critical" {
		t.Errorf("Expected escalation[3] name 'critical', got '%s'", critical.Name)
	}
	if critical.OverLastRuns == nil || *critical.OverLastRuns != 20 {
		t.Errorf("Expected escalation[3] overLastRuns 20, got %v", critical.OverLastRuns)
	}
	if critical.OverPeriod != "2d" {
		t.Errorf("Expected escalation[3] overPeriod '2d', got '%s'", critical.OverPeriod)
	}
	if critical.PassPercentage == nil || *critical.PassPercentage != 50 {
		t.Errorf("Expected escalation[3] passPercentage 50, got %v", critical.PassPercentage)
	}
	if !cmp.Equal(critical.Mentions, []string{"trt-manager"}) {
		t.Errorf("Expected escalation[3] mentions [trt-manager], got %v", critical.Mentions)
	}
	if !cmp.Equal(critical.NeedsInfo, []string{"component-owner"}) {
		t.Errorf("Expected escalation[3] needsInfo [component-owner], got %v", critical.NeedsInfo)
	}
}

// intPtr is a helper function to create pointer to int
func intPtr(i int) *int {
	return &i
}
