package releasequalifiers

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestNewConfigLoader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		configPath    string
		configContent string
		wantErr       bool
		wantEmpty     bool
	}{
		{
			name:       "EmptyConfigPath",
			configPath: "",
			wantErr:    false,
			wantEmpty:  true,
		},
		{
			name:       "MissingFile",
			configPath: "/nonexistent/path/to/config.yaml",
			wantErr:    false,
			wantEmpty:  true,
		},
		{
			name:       "ValidConfigFile",
			configPath: "config.yaml",
			configContent: `qualifiers:
  test-qualifier:
    enabled: true
    badgeName: TestBadge
    summary: Test Summary
`,
			wantErr:   false,
			wantEmpty: false,
		},
		{
			name:       "InvalidYAML",
			configPath: "config.yaml",
			configContent: `invalid: yaml: content:
  this is not valid
    - badly indented
`,
			wantErr:   false, // NewConfigLoader logs warning but doesn't fail
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var configPath string

			if tt.configPath != "" && tt.configPath != "/nonexistent/path/to/config.yaml" {
				// Create temporary file
				tmpDir := t.TempDir()
				configPath = filepath.Join(tmpDir, tt.configPath)

				if tt.configContent != "" {
					if err := os.WriteFile(configPath, []byte(tt.configContent), 0644); err != nil {
						t.Fatalf("Failed to write test config file: %v", err)
					}
				}
			} else {
				configPath = tt.configPath
			}

			loader, err := NewConfigLoader(configPath)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewConfigLoader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if loader == nil {
				t.Fatal("Expected non-nil ConfigLoader")
			}

			config := loader.Get()

			if tt.wantEmpty && len(config) != 0 {
				t.Errorf("Expected empty config, got %d qualifiers", len(config))
			}

			if !tt.wantEmpty && len(config) == 0 {
				t.Error("Expected non-empty config, got empty")
			}
		})
	}
}

func TestConfigLoaderGet(t *testing.T) {
	t.Parallel()

	// Create a valid config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `qualifiers:
  test-qualifier:
    enabled: true
    approval: true
    badgeName: TestBadge
    summary: Test Summary
  another-qualifier:
    enabled: false
    badgeName: AnotherBadge
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	loader, err := NewConfigLoader(configPath)
	if err != nil {
		t.Fatalf("NewConfigLoader() error = %v", err)
	}

	config := loader.Get()

	if len(config) != 2 {
		t.Errorf("Expected 2 qualifiers, got %d", len(config))
	}

	testQual, exists := config["test-qualifier"]
	if !exists {
		t.Error("Expected test-qualifier to exist")
	}
	if testQual.BadgeName != "TestBadge" {
		t.Errorf("Expected BadgeName 'TestBadge', got '%s'", testQual.BadgeName)
	}
	if testQual.Approval == nil || !*testQual.Approval {
		t.Error("Expected test-qualifier Approval to be true")
	}

	anotherQual, exists := config["another-qualifier"]
	if !exists {
		t.Error("Expected another-qualifier to exist")
	}
	if anotherQual.BadgeName != "AnotherBadge" {
		t.Errorf("Expected BadgeName 'AnotherBadge', got '%s'", anotherQual.BadgeName)
	}
	if anotherQual.Approval != nil {
		t.Errorf("Expected another-qualifier Approval to be nil (omitted), got %v", *anotherQual.Approval)
	}
}

func TestConfigLoaderGetReturnsACopy(t *testing.T) {
	t.Parallel()

	// Create a valid config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `qualifiers:
  test-qualifier:
    enabled: true
    badgeName: TestBadge
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	loader, err := NewConfigLoader(configPath)
	if err != nil {
		t.Fatalf("NewConfigLoader() error = %v", err)
	}

	// Get config and modify it
	config1 := loader.Get()
	config1["new-qualifier"] = ReleaseQualifier{
		BadgeName: "NewBadge",
	}

	// Get config again - should not have the modification
	config2 := loader.Get()

	if len(config2) != 1 {
		t.Errorf("Expected 1 qualifier (external modification should not affect internal state), got %d", len(config2))
	}

	if _, exists := config2["new-qualifier"]; exists {
		t.Error("External modification affected internal state - Get() should return a copy")
	}
}

func TestConfigLoaderConcurrency(t *testing.T) {
	t.Parallel()

	// Create a valid config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `qualifiers:
  test-qualifier:
    enabled: true
    badgeName: TestBadge
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	loader, err := NewConfigLoader(configPath)
	if err != nil {
		t.Fatalf("NewConfigLoader() error = %v", err)
	}

	// Test concurrent reads
	var wg sync.WaitGroup
	numReaders := 10
	iterations := 100

	for range numReaders {
		wg.Go(func() {
			for range iterations {
				config := loader.Get()
				if len(config) != 1 {
					t.Errorf("Expected 1 qualifier, got %d", len(config))
				}
			}
		})
	}

	wg.Wait()
}

func TestConfigLoaderStartWatching(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping file watching test in short mode")
	}

	t.Parallel()

	// Create initial config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	initialContent := `qualifiers:
  test-qualifier:
    enabled: true
    badgeName: InitialBadge
`
	if err := os.WriteFile(configPath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to write initial config file: %v", err)
	}

	loader, err := NewConfigLoader(configPath)
	if err != nil {
		t.Fatalf("NewConfigLoader() error = %v", err)
	}

	ctx := t.Context()

	// Start watching
	if err := loader.StartWatching(ctx); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}

	// Verify initial config
	config := loader.Get()
	if config["test-qualifier"].BadgeName != "InitialBadge" {
		t.Errorf("Expected BadgeName 'InitialBadge', got '%s'", config["test-qualifier"].BadgeName)
	}

	// Update config file
	updatedContent := `qualifiers:
  test-qualifier:
    enabled: true
    badgeName: UpdatedBadge
`
	if err := os.WriteFile(configPath, []byte(updatedContent), 0644); err != nil {
		t.Fatalf("Failed to update config file: %v", err)
	}

	// Wait for file watcher to detect change and reload
	time.Sleep(500 * time.Millisecond)

	// Verify updated config
	config = loader.Get()
	if config["test-qualifier"].BadgeName != "UpdatedBadge" {
		t.Errorf("Expected BadgeName 'UpdatedBadge' after reload, got '%s'", config["test-qualifier"].BadgeName)
	}
}

func TestConfigLoaderStartWatchingInvalidUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping file watching test in short mode")
	}

	t.Parallel()

	// Create initial valid config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	validContent := `qualifiers:
  test-qualifier:
    enabled: true
    badgeName: ValidBadge
`
	if err := os.WriteFile(configPath, []byte(validContent), 0644); err != nil {
		t.Fatalf("Failed to write initial config file: %v", err)
	}

	loader, err := NewConfigLoader(configPath)
	if err != nil {
		t.Fatalf("NewConfigLoader() error = %v", err)
	}

	ctx := t.Context()

	// Start watching
	if err := loader.StartWatching(ctx); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}

	// Verify initial config
	config := loader.Get()
	if config["test-qualifier"].BadgeName != "ValidBadge" {
		t.Errorf("Expected BadgeName 'ValidBadge', got '%s'", config["test-qualifier"].BadgeName)
	}

	// Update with invalid YAML
	invalidContent := `this is: not: valid: yaml:
  - badly formatted
`
	if err := os.WriteFile(configPath, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("Failed to update config file: %v", err)
	}

	// Wait for file watcher to detect change
	time.Sleep(500 * time.Millisecond)

	// Verify config remained unchanged (kept previous valid config)
	config = loader.Get()
	if config["test-qualifier"].BadgeName != "ValidBadge" {
		t.Errorf("Expected BadgeName 'ValidBadge' (should keep previous valid config), got '%s'", config["test-qualifier"].BadgeName)
	}
}

func TestConfigLoaderConfigMapSimulation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping ConfigMap simulation test in short mode")
	}

	t.Parallel()

	// Simulate a real Kubernetes ConfigMap mount structure:
	//   mountRoot/
	//   ├── ..data -> ..2026_01_01_v1/
	//   ├── ..2026_01_01_v1/
	//   │   └── release-qualifiers.yaml
	//   └── release-qualifiers.yaml -> ..data/release-qualifiers.yaml
	//
	// The config path is the top-level symlink (release-qualifiers.yaml),
	// NOT a path through ..data.
	tmpDir := t.TempDir()

	// Create timestamped data directory v1
	dataDir1 := filepath.Join(tmpDir, "..2026_01_01_v1")
	if err := os.Mkdir(dataDir1, 0755); err != nil {
		t.Fatalf("Failed to create data directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir1, "release-qualifiers.yaml"), []byte(`qualifiers:
  test-qualifier:
    enabled: true
    badgeName: Version1
`), 0644); err != nil {
		t.Fatalf("Failed to write initial config file: %v", err)
	}

	// Create ..data symlink pointing to v1
	dataSym := filepath.Join(tmpDir, "..data")
	if err := os.Symlink(dataDir1, dataSym); err != nil {
		t.Fatalf("Failed to create ..data symlink: %v", err)
	}

	// Create top-level file symlink (this is what the controller uses as configPath)
	configSym := filepath.Join(tmpDir, "release-qualifiers.yaml")
	if err := os.Symlink(filepath.Join("..data", "release-qualifiers.yaml"), configSym); err != nil {
		t.Fatalf("Failed to create config symlink: %v", err)
	}

	loader, err := NewConfigLoader(configSym)
	if err != nil {
		t.Fatalf("NewConfigLoader() error = %v", err)
	}

	ctx := t.Context()
	if err := loader.StartWatching(ctx); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}

	// Verify initial config
	config := loader.Get()
	if config["test-qualifier"].BadgeName != "Version1" {
		t.Errorf("Expected BadgeName 'Version1', got '%s'", config["test-qualifier"].BadgeName)
	}

	// Simulate ConfigMap update: Kubernetes creates a new timestamped dir,
	// then atomically replaces the ..data symlink.
	dataDir2 := filepath.Join(tmpDir, "..2026_01_01_v2")
	if err := os.Mkdir(dataDir2, 0755); err != nil {
		t.Fatalf("Failed to create new data directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir2, "release-qualifiers.yaml"), []byte(`qualifiers:
  test-qualifier:
    enabled: true
    badgeName: Version2
`), 0644); err != nil {
		t.Fatalf("Failed to write updated config file: %v", err)
	}

	// Atomic symlink swap (remove + create)
	if err := os.Remove(dataSym); err != nil {
		t.Fatalf("Failed to remove old ..data symlink: %v", err)
	}
	if err := os.Symlink(dataDir2, dataSym); err != nil {
		t.Fatalf("Failed to create new ..data symlink: %v", err)
	}

	// Wait for file watcher to detect change
	time.Sleep(500 * time.Millisecond)

	config = loader.Get()
	if config["test-qualifier"].BadgeName != "Version2" {
		t.Errorf("Expected BadgeName 'Version2' after ConfigMap update, got '%s'", config["test-qualifier"].BadgeName)
	}
}

func TestConfigLoaderSubscribe(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping subscriber test in short mode")
	}

	t.Parallel()

	// Create initial config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	initialContent := `qualifiers:
  test-qualifier:
    enabled: true
    badgeName: InitialBadge
`
	if err := os.WriteFile(configPath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to write initial config file: %v", err)
	}

	loader, err := NewConfigLoader(configPath)
	if err != nil {
		t.Fatalf("NewConfigLoader() error = %v", err)
	}

	// Track callback invocations
	callbackInvoked := make(chan ReleaseQualifiers, 1)
	loader.Subscribe(func(config ReleaseQualifiers) {
		callbackInvoked <- config
	})

	ctx := t.Context()

	// Start watching
	if err := loader.StartWatching(ctx); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}

	// Update config file
	updatedContent := `qualifiers:
  test-qualifier:
    enabled: true
    badgeName: UpdatedBadge
`
	if err := os.WriteFile(configPath, []byte(updatedContent), 0644); err != nil {
		t.Fatalf("Failed to update config file: %v", err)
	}

	// Wait for callback
	select {
	case config := <-callbackInvoked:
		if config["test-qualifier"].BadgeName != "UpdatedBadge" {
			t.Errorf("Callback received BadgeName '%s', expected 'UpdatedBadge'", config["test-qualifier"].BadgeName)
		}
	case <-time.After(2 * time.Second):
		t.Error("Callback was not invoked within timeout")
	}
}

func TestConfigLoaderEmptyPath(t *testing.T) {
	t.Parallel()

	loader, err := NewConfigLoader("")
	if err != nil {
		t.Fatalf("NewConfigLoader() error = %v", err)
	}

	config := loader.Get()
	if len(config) != 0 {
		t.Errorf("Expected empty config with empty path, got %d qualifiers", len(config))
	}

	// StartWatching should not fail with empty path
	ctx := t.Context()

	if err := loader.StartWatching(ctx); err != nil {
		t.Errorf("StartWatching() with empty path should not fail, got error: %v", err)
	}
}

func TestConfigLoaderApprovalField(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `qualifiers:
  approval-true:
    enabled: true
    approval: true
    badgeName: ApprovalTrue
    summary: Qualifier with approval enabled
  approval-false:
    enabled: true
    approval: false
    badgeName: ApprovalFalse
    summary: Qualifier with approval disabled
  approval-omitted:
    enabled: true
    badgeName: ApprovalOmitted
    summary: Qualifier with approval omitted
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	loader, err := NewConfigLoader(configPath)
	if err != nil {
		t.Fatalf("NewConfigLoader() error = %v", err)
	}

	config := loader.Get()

	if len(config) != 3 {
		t.Fatalf("Expected 3 qualifiers, got %d", len(config))
	}

	// approval: true
	q1, exists := config["approval-true"]
	if !exists {
		t.Fatal("Expected approval-true to exist")
	}
	if q1.Approval == nil {
		t.Fatal("approval-true: Expected Approval to be non-nil")
	}
	if !*q1.Approval {
		t.Error("approval-true: Expected Approval to be true")
	}

	// approval: false
	q2, exists := config["approval-false"]
	if !exists {
		t.Fatal("Expected approval-false to exist")
	}
	if q2.Approval == nil {
		t.Fatal("approval-false: Expected Approval to be non-nil")
	}
	if *q2.Approval {
		t.Error("approval-false: Expected Approval to be false")
	}

	// approval omitted
	q3, exists := config["approval-omitted"]
	if !exists {
		t.Fatal("Expected approval-omitted to exist")
	}
	if q3.Approval != nil {
		t.Errorf("approval-omitted: Expected Approval to be nil, got %v", *q3.Approval)
	}
}

func TestConfigLoaderComplexConfig(t *testing.T) {
	t.Parallel()

	// Create a complex config file with multiple qualifiers
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	complexContent := `qualifiers:
  qualifier-1:
    enabled: true
    approval: true
    badgeName: Badge1
    summary: First qualifier
    description: Detailed description for first qualifier
    payloadBadgeStatus: Yes
    failureLabels:
      - label1
      - label2
    notifications:
      jira:
        project: TEST
        component: TestComponent
        assignee: test.user@example.com
        summary: Test Jira Summary
        description: Test Jira Description
        thread: jira-thread-1
        escalations:
          - name: minor
            failures: 3
            priority: Minor
            mentions:
              - jira.user1
          - name: major
            overLastRuns: 10
            passPercentage: 70
            overPeriod: 2d
            priority: Major
            needsInfo:
              - jira.user2

  qualifier-2:
    enabled: false
    approval: false
    badgeName: Badge2
    summary: Second qualifier
    payloadBadgeStatus: OnSuccess
    notifications:
      jira:
        project: CRIT
        component: CriticalComponent
        thread: jira-thread-2
        escalations:
          - name: critical
            failures: 1
            priority: Critical
            mentions:
              - "@user1"

  qualifier-3:
    enabled: true
    badgeName: Badge3
    payloadBadgeStatus: OnFailure
`
	if err := os.WriteFile(configPath, []byte(complexContent), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	loader, err := NewConfigLoader(configPath)
	if err != nil {
		t.Fatalf("NewConfigLoader() error = %v", err)
	}

	config := loader.Get()

	if len(config) != 3 {
		t.Errorf("Expected 3 qualifiers, got %d", len(config))
	}

	// Verify qualifier-1
	q1, exists := config["qualifier-1"]
	if !exists {
		t.Fatal("Expected qualifier-1 to exist")
	}
	if q1.BadgeName != "Badge1" {
		t.Errorf("qualifier-1: Expected BadgeName 'Badge1', got '%s'", q1.BadgeName)
	}
	if q1.Summary != "First qualifier" {
		t.Errorf("qualifier-1: Expected Summary 'First qualifier', got '%s'", q1.Summary)
	}
	if q1.Description != "Detailed description for first qualifier" {
		t.Errorf("qualifier-1: Expected description, got '%s'", q1.Description)
	}
	if q1.PayloadBadgeStatus != BadgeStatusYes {
		t.Errorf("qualifier-1: Expected PayloadBadgeStatus 'Yes', got '%s'", q1.PayloadBadgeStatus)
	}
	if q1.Enabled == nil || !*q1.Enabled {
		t.Error("qualifier-1: Expected Enabled to be true")
	}
	if q1.Approval == nil || !*q1.Approval {
		t.Error("qualifier-1: Expected Approval to be true")
	}
	if !cmp.Equal(q1.FailureLabels, []string{"label1", "label2"}) {
		t.Errorf("qualifier-1: Expected labels [label1, label2], got %v", q1.FailureLabels)
	}
	if q1.Notifications == nil {
		t.Fatal("qualifier-1: Expected Notifications to be set")
	}
	if q1.Notifications.Jira == nil {
		t.Fatal("qualifier-1: Expected Jira notifications to be set")
	}
	if q1.Notifications.Jira.Project != "TEST" {
		t.Errorf("qualifier-1: Expected Jira project 'TEST', got '%s'", q1.Notifications.Jira.Project)
	}
	if q1.Notifications.Jira.Component != "TestComponent" {
		t.Errorf("qualifier-1: Expected Jira component 'TestComponent', got '%s'", q1.Notifications.Jira.Component)
	}
	if q1.Notifications.Jira.Assignee != "test.user@example.com" {
		t.Errorf("qualifier-1: Expected Jira assignee 'test.user@example.com', got '%s'", q1.Notifications.Jira.Assignee)
	}
	if q1.Notifications.Jira.Summary != "Test Jira Summary" {
		t.Errorf("qualifier-1: Expected Jira summary 'Test Jira Summary', got '%s'", q1.Notifications.Jira.Summary)
	}
	if q1.Notifications.Jira.Description != "Test Jira Description" {
		t.Errorf("qualifier-1: Expected Jira description 'Test Jira Description', got '%s'", q1.Notifications.Jira.Description)
	}
	if q1.Notifications.Jira.Thread != "jira-thread-1" {
		t.Errorf("qualifier-1: Expected Jira thread 'jira-thread-1', got '%s'", q1.Notifications.Jira.Thread)
	}
	if len(q1.Notifications.Jira.Escalations) != 2 {
		t.Errorf("qualifier-1: Expected 2 Jira escalations, got %d", len(q1.Notifications.Jira.Escalations))
	} else {
		jiraEsc1 := q1.Notifications.Jira.Escalations[0]
		if jiraEsc1.Name != "minor" {
			t.Errorf("qualifier-1: Expected Jira escalation[0] name 'minor', got '%s'", jiraEsc1.Name)
		}
		if jiraEsc1.Failures != 3 {
			t.Errorf("qualifier-1: Expected Jira escalation[0] failures 3, got %d", jiraEsc1.Failures)
		}
		if jiraEsc1.Priority != "Minor" {
			t.Errorf("qualifier-1: Expected Jira escalation[0] priority 'Minor', got '%s'", jiraEsc1.Priority)
		}
		if !cmp.Equal(jiraEsc1.Mentions, []string{"jira.user1"}) {
			t.Errorf("qualifier-1: Expected Jira escalation[0] mentions [jira.user1], got %v", jiraEsc1.Mentions)
		}

		jiraEsc2 := q1.Notifications.Jira.Escalations[1]
		if jiraEsc2.Name != "major" {
			t.Errorf("qualifier-1: Expected Jira escalation[1] name 'major', got '%s'", jiraEsc2.Name)
		}
		if jiraEsc2.OverLastRuns == nil || *jiraEsc2.OverLastRuns != 10 {
			t.Errorf("qualifier-1: Expected Jira escalation[1] overLastRuns 10, got %v", jiraEsc2.OverLastRuns)
		}
		if jiraEsc2.PassPercentage == nil || *jiraEsc2.PassPercentage != 70 {
			t.Errorf("qualifier-1: Expected Jira escalation[1] passPercentage 70, got %v", jiraEsc2.PassPercentage)
		}
		if jiraEsc2.OverPeriod != "2d" {
			t.Errorf("qualifier-1: Expected Jira escalation[1] overPeriod '2d', got '%s'", jiraEsc2.OverPeriod)
		}
		if jiraEsc2.Priority != "Major" {
			t.Errorf("qualifier-1: Expected Jira escalation[1] priority 'Major', got '%s'", jiraEsc2.Priority)
		}
		if !cmp.Equal(jiraEsc2.NeedsInfo, []string{"jira.user2"}) {
			t.Errorf("qualifier-1: Expected Jira escalation[1] needsInfo [jira.user2], got %v", jiraEsc2.NeedsInfo)
		}
	}

	// Verify qualifier-2
	q2, exists := config["qualifier-2"]
	if !exists {
		t.Fatal("Expected qualifier-2 to exist")
	}
	if q2.Enabled == nil || *q2.Enabled {
		t.Error("qualifier-2: Expected Enabled to be false")
	}
	if q2.Approval == nil || *q2.Approval {
		t.Error("qualifier-2: Expected Approval to be false")
	}
	if q2.Notifications == nil {
		t.Fatal("qualifier-2: Expected Notifications to be set")
	}
	if q2.Notifications.Jira == nil {
		t.Fatal("qualifier-2: Expected Jira notifications to be set")
	}
	if q2.Notifications.Jira.Project != "CRIT" {
		t.Errorf("qualifier-2: Expected Jira project 'CRIT', got '%s'", q2.Notifications.Jira.Project)
	}
	if q2.Notifications.Jira.Component != "CriticalComponent" {
		t.Errorf("qualifier-2: Expected Jira component 'CriticalComponent', got '%s'", q2.Notifications.Jira.Component)
	}
	if q2.Notifications.Jira.Thread != "jira-thread-2" {
		t.Errorf("qualifier-2: Expected Jira thread 'jira-thread-2', got '%s'", q2.Notifications.Jira.Thread)
	}
	if len(q2.Notifications.Jira.Escalations) != 1 {
		t.Errorf("qualifier-2: Expected 1 Jira escalation, got %d", len(q2.Notifications.Jira.Escalations))
	} else {
		jiraEsc := q2.Notifications.Jira.Escalations[0]
		if jiraEsc.Name != "critical" {
			t.Errorf("qualifier-2: Expected Jira escalation name 'critical', got '%s'", jiraEsc.Name)
		}
		if jiraEsc.Failures != 1 {
			t.Errorf("qualifier-2: Expected Jira escalation failures 1, got %d", jiraEsc.Failures)
		}
		if jiraEsc.Priority != "Critical" {
			t.Errorf("qualifier-2: Expected Jira escalation priority 'Critical', got '%s'", jiraEsc.Priority)
		}
		if !cmp.Equal(jiraEsc.Mentions, []string{"@user1"}) {
			t.Errorf("qualifier-2: Expected Jira mentions [@user1], got %v", jiraEsc.Mentions)
		}
	}

	// Verify qualifier-3
	q3, exists := config["qualifier-3"]
	if !exists {
		t.Fatal("Expected qualifier-3 to exist")
	}
	if q3.PayloadBadgeStatus != BadgeStatusOnFailure {
		t.Errorf("qualifier-3: Expected PayloadBadgeStatus 'OnFailure', got '%s'", q3.PayloadBadgeStatus)
	}
	if q3.Approval != nil {
		t.Errorf("qualifier-3: Expected Approval to be nil (omitted), got %v", *q3.Approval)
	}
	if q3.Notifications != nil {
		t.Error("qualifier-3: Expected Notifications to be nil")
	}
}
