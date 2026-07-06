package main

import (
	"os"
	"testing"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"github.com/openshift/release-controller/pkg/releasequalifiers"
	"gopkg.in/yaml.v2"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

func TestVerifyPeriodicFields(t *testing.T) {
	testCases := []struct {
		name        string
		input       releasecontroller.ReleaseConfig
		expectedErr bool
	}{{
		name: "Valid Cron",
		input: releasecontroller.ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]releasecontroller.ReleasePeriodic{
				"aws": {
					Cron:    "0 8,20 * * *",
					ProwJob: &releasecontroller.ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: false,
	}, {
		name: "Valid Interval",
		input: releasecontroller.ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]releasecontroller.ReleasePeriodic{
				"aws": {
					Interval: "6h",
					ProwJob:  &releasecontroller.ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: false,
	}, {
		name: "Missing Fields",
		input: releasecontroller.ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]releasecontroller.ReleasePeriodic{
				"aws": {
					ProwJob: &releasecontroller.ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: true,
	}, {
		name: "Interval and Cron",
		input: releasecontroller.ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]releasecontroller.ReleasePeriodic{
				"aws": {
					Cron:     "0 8,20 * * *",
					Interval: "6h",
					ProwJob:  &releasecontroller.ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: true,
	}, {
		name: "Invalid Cron",
		input: releasecontroller.ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]releasecontroller.ReleasePeriodic{
				"aws": {
					Cron:    "0 8,25 * * *",
					ProwJob: &releasecontroller.ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: true,
	}, {
		name: "Invalid Interval",
		input: releasecontroller.ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]releasecontroller.ReleasePeriodic{
				"aws": {
					Interval: "6g",
					ProwJob:  &releasecontroller.ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: true,
	}}
	for _, testCase := range testCases {
		errors := verifyPeriodicFields([]releasecontroller.ReleaseConfig{testCase.input})
		if testCase.expectedErr && len(errors) == 0 {
			t.Errorf("%s: Expected error but none given", testCase.name)
		}
		if !testCase.expectedErr && len(errors) > 0 {
			t.Errorf("%s: Did not expect error, received errors: %v", testCase.name, utilerrors.NewAggregate(errors))
		}
	}
}

func TestFindDuplicatePeriodics(t *testing.T) {
	goodConfigs := []releasecontroller.ReleaseConfig{{
		Name: "4.5.0-0.nightly",
		Periodic: map[string]releasecontroller.ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &releasecontroller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &releasecontroller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.4-stable-to-4.5-nightly",
				},
			},
		},
	}, {
		Name: "4.6.0-0.nightly",
		Periodic: map[string]releasecontroller.ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &releasecontroller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.6-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &releasecontroller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
				},
			},
		},
	}}
	sameConfigDuplicate := []releasecontroller.ReleaseConfig{{
		Name: "4.5.0-0.nightly",
		Periodic: map[string]releasecontroller.ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &releasecontroller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade2": {
				Upgrade: true,
				ProwJob: &releasecontroller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &releasecontroller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.4-stable-to-4.5-nightly",
				},
			},
		},
	}, {
		Name: "4.6.0-0.nightly",
		Periodic: map[string]releasecontroller.ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &releasecontroller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.6-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &releasecontroller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
				},
			},
		},
	}}
	differentConfigDuplicate := []releasecontroller.ReleaseConfig{{
		Name: "4.5.0-0.nightly",
		Periodic: map[string]releasecontroller.ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &releasecontroller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &releasecontroller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.4-stable-to-4.5-nightly",
				},
			},
		},
	}, {
		Name: "4.6.0-0.nightly",
		Periodic: map[string]releasecontroller.ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &releasecontroller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &releasecontroller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
				},
			},
		},
	}}

	testCases := []struct {
		name          string
		configs       []releasecontroller.ReleaseConfig
		errorExpected bool
	}{{
		name:          "Valid configs",
		configs:       goodConfigs,
		errorExpected: false,
	}, {
		name:          "Duplicate in one config",
		configs:       sameConfigDuplicate,
		errorExpected: true,
	}, {
		name:          "Duplicate in different configs",
		configs:       differentConfigDuplicate,
		errorExpected: true,
	}}

	for _, testCase := range testCases {
		errs := findDuplicatePeriodics(testCase.configs)
		if len(errs) == 0 && testCase.errorExpected {
			t.Errorf("%s: Expected error but received no errors", testCase.name)
		} else if len(errs) > 0 && !testCase.errorExpected {
			t.Errorf("%s: Expected no error, but got errors: %v", testCase.name, errs)
		}
	}
}

func TestValidateAsyncJobs(t *testing.T) {
	testCases := []struct {
		name        string
		configs     []releasecontroller.ReleaseConfig
		expectedErr bool
	}{
		{
			name: "async with optional is valid",
			configs: []releasecontroller.ReleaseConfig{{
				Name: "4.19.0-0.nightly",
				Verify: map[string]releasecontroller.ReleaseVerification{
					"my-async-job": {
						Optional: true,
						Async:    true,
						ProwJob:  &releasecontroller.ProwJobVerification{Name: "some-job"},
					},
				},
			}},
			expectedErr: false,
		},
		{
			name: "async without optional is invalid",
			configs: []releasecontroller.ReleaseConfig{{
				Name: "4.19.0-0.nightly",
				Verify: map[string]releasecontroller.ReleaseVerification{
					"my-async-job": {
						Optional: false,
						Async:    true,
						ProwJob:  &releasecontroller.ProwJobVerification{Name: "some-job"},
					},
				},
			}},
			expectedErr: true,
		},
		{
			name: "async with maxRetries is invalid",
			configs: []releasecontroller.ReleaseConfig{{
				Name: "4.19.0-0.nightly",
				Verify: map[string]releasecontroller.ReleaseVerification{
					"my-async-job": {
						Optional:   true,
						Async:      true,
						MaxRetries: 3,
						ProwJob:    &releasecontroller.ProwJobVerification{Name: "some-job"},
					},
				},
			}},
			expectedErr: true,
		},
	}
	for _, testCase := range testCases {
		errs := validateAsyncJobs(testCase.configs)
		if len(errs) > 0 && !testCase.expectedErr {
			t.Errorf("%s: got error when error was not expected: %v", testCase.name, errs)
		}
		if len(errs) == 0 && testCase.expectedErr {
			t.Errorf("%s: did not get error when error was expected", testCase.name)
		}
	}
}

func TestValidateUpgradeJobs(t *testing.T) {
	testCases := []struct {
		name        string
		configs     []releasecontroller.ReleaseConfig
		expectedErr bool
	}{{
		name: "Good config",
		configs: []releasecontroller.ReleaseConfig{{
			Name: "4.6.0-0.nightly",
			Verify: map[string]releasecontroller.ReleaseVerification{
				"upgrade-minor": {
					Upgrade:     true,
					UpgradeFrom: "PreviousMinor",
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			},
			Periodic: map[string]releasecontroller.ReleasePeriodic{
				"upgrade-minor": {
					Upgrade: true,
					UpgradeFromRelease: &releasecontroller.UpgradeRelease{
						Prerelease: &releasecontroller.UpgradePrerelease{
							VersionBounds: releasecontroller.UpgradeVersionBounds{
								Lower: "4.5.0",
								Upper: "4.6.0-0",
							},
						},
					},
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			}},
		},
		expectedErr: false,
	}, {
		name: "Bad Verification",
		configs: []releasecontroller.ReleaseConfig{{
			Name: "4.6.0-0.nightly",
			Verify: map[string]releasecontroller.ReleaseVerification{
				"upgrade-minor": {
					Upgrade:     true,
					UpgradeFrom: "PreviousMinor",
					UpgradeFromRelease: &releasecontroller.UpgradeRelease{
						Prerelease: &releasecontroller.UpgradePrerelease{
							VersionBounds: releasecontroller.UpgradeVersionBounds{
								Lower: "4.5.0",
								Upper: "4.6.0-0",
							},
						},
					},
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			},
			Periodic: map[string]releasecontroller.ReleasePeriodic{
				"upgrade-minor": {
					Upgrade: true,
					UpgradeFromRelease: &releasecontroller.UpgradeRelease{
						Prerelease: &releasecontroller.UpgradePrerelease{
							VersionBounds: releasecontroller.UpgradeVersionBounds{
								Lower: "4.5.0",
								Upper: "4.6.0-0",
							},
						},
					},
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			},
		}},
		expectedErr: true,
	}, {
		name: "Bad Periodic",
		configs: []releasecontroller.ReleaseConfig{{
			Name: "4.6.0-0.nightly",
			Verify: map[string]releasecontroller.ReleaseVerification{
				"upgrade-minor": {
					Upgrade:     true,
					UpgradeFrom: "PreviousMinor",
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			},
			Periodic: map[string]releasecontroller.ReleasePeriodic{
				"upgrade-minor": {
					Upgrade:     true,
					UpgradeFrom: "PreviousMinor",
					UpgradeFromRelease: &releasecontroller.UpgradeRelease{
						Prerelease: &releasecontroller.UpgradePrerelease{
							VersionBounds: releasecontroller.UpgradeVersionBounds{
								Lower: "4.5.0",
								Upper: "4.6.0-0",
							},
						},
					},
					ProwJob: &releasecontroller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			},
		}},
		expectedErr: true,
	}}
	for _, testCase := range testCases {
		errs := validateUpgradeJobs(testCase.configs)
		if len(errs) > 0 && !testCase.expectedErr {
			t.Errorf("%s: got error when error was not expected: %v", testCase.name, errs)
		}
		if len(errs) == 0 && testCase.expectedErr {
			t.Errorf("%s: did not get error when error was expected", testCase.name)
		}
	}
}

func TestValidateQualifiersConfiguration(t *testing.T) {
	testCases := []struct {
		name        string
		configs     []releasecontroller.ReleaseConfig
		expectedErr bool
	}{
		{
			name: "Good config with no qualifiers",
			configs: []releasecontroller.ReleaseConfig{{
				Name: "4.19.0-0.nightly",
				Verify: map[string]releasecontroller.ReleaseVerification{
					"osd-aws": {
						Optional:   true,
						MaxRetries: 2,
						ProwJob: &releasecontroller.ProwJobVerification{
							Name: "periodic-ci-openshift-release-master-nightly-4.19-osd-aws",
						},
					},
				},
			},
			},
			expectedErr: false,
		},
		{
			name: "Good config with empty qualifier",
			configs: []releasecontroller.ReleaseConfig{{
				Name: "4.19.0-0.nightly",
				Verify: map[string]releasecontroller.ReleaseVerification{
					"osd-aws": {
						Optional:   true,
						MaxRetries: 2,
						ProwJob: &releasecontroller.ProwJobVerification{
							Name: "periodic-ci-openshift-release-master-nightly-4.19-osd-aws",
						},
						Qualifiers: releasequalifiers.ReleaseQualifiers{
							"test": {},
						},
					},
				},
			},
			},
			expectedErr: false,
		},
		{
			name: "Good config with simple overrides",
			configs: []releasecontroller.ReleaseConfig{{
				Name: "4.19.0-0.nightly",
				Verify: map[string]releasecontroller.ReleaseVerification{
					"osd-aws": {
						Optional:   true,
						MaxRetries: 2,
						ProwJob: &releasecontroller.ProwJobVerification{
							Name: "periodic-ci-openshift-release-master-nightly-4.19-osd-aws",
						},
						Qualifiers: releasequalifiers.ReleaseQualifiers{
							"test": {
								Enabled:            releasequalifiers.BoolPtr(false),
								BadgeName:          "TEST",
								Description:        "An updated description when displaying badge details",
								PayloadBadgeStatus: releasequalifiers.BadgeStatusNo,
							},
						},
					},
				},
			}},
			expectedErr: false,
		},
		{
			name: "Approval=true in release config is invalid",
			configs: []releasecontroller.ReleaseConfig{{
				Name: "4.19.0-0.nightly",
				Verify: map[string]releasecontroller.ReleaseVerification{
					"osd-aws": {
						Optional: true,
						ProwJob: &releasecontroller.ProwJobVerification{
							Name: "periodic-ci-openshift-release-master-nightly-4.19-osd-aws",
						},
						Qualifiers: releasequalifiers.ReleaseQualifiers{
							"sdn-migration": {
								Approval:  releasequalifiers.BoolPtr(true),
								Enabled:   releasequalifiers.BoolPtr(true),
								BadgeName: "SDN Migration",
							},
						},
					},
				},
			}},
			expectedErr: true,
		},
		{
			name: "Approval=false in release config is invalid",
			configs: []releasecontroller.ReleaseConfig{{
				Name: "4.19.0-0.nightly",
				Verify: map[string]releasecontroller.ReleaseVerification{
					"osd-aws": {
						Optional: true,
						ProwJob: &releasecontroller.ProwJobVerification{
							Name: "periodic-ci-openshift-release-master-nightly-4.19-osd-aws",
						},
						Qualifiers: releasequalifiers.ReleaseQualifiers{
							"sdn-migration": {
								Approval:  releasequalifiers.BoolPtr(false),
								BadgeName: "SDN Migration",
							},
						},
					},
				},
			}},
			expectedErr: true,
		},
	}
	for _, testCase := range testCases {
		errs := validateQualifiers(testCase.configs)
		if len(errs) > 0 && !testCase.expectedErr {
			t.Errorf("%s: got error when error was not expected: %v", testCase.name, errs)
		}
		if len(errs) == 0 && testCase.expectedErr {
			t.Errorf("%s: did not get error when error was expected", testCase.name)
		}
	}
}

func TestFindDuplicateQualifierIDs(t *testing.T) {
	testCases := []struct {
		name        string
		qualifiers  yaml.MapSlice
		expectedErr bool
	}{
		{
			name:        "no qualifiers",
			qualifiers:  yaml.MapSlice{},
			expectedErr: false,
		},
		{
			name: "unique qualifier IDs",
			qualifiers: yaml.MapSlice{
				{Key: "fips", Value: nil},
				{Key: "techpreview", Value: nil},
				{Key: "metal", Value: nil},
			},
			expectedErr: false,
		},
		{
			name: "duplicate qualifier IDs",
			qualifiers: yaml.MapSlice{
				{Key: "fips", Value: nil},
				{Key: "techpreview", Value: nil},
				{Key: "fips", Value: nil},
			},
			expectedErr: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			errs := findDuplicateQualifierIDs(testCase.qualifiers)
			if len(errs) > 0 && !testCase.expectedErr {
				t.Errorf("got error when error was not expected: %v", errs)
			}
			if len(errs) == 0 && testCase.expectedErr {
				t.Errorf("did not get error when error was expected")
			}
		})
	}
}

func TestValidateReleaseQualifiersConfig(t *testing.T) {
	testCases := []struct {
		name        string
		yamlContent string
		expectedErr bool
	}{
		{
			name: "valid config with unique qualifiers",
			yamlContent: `qualifiers:
  fips:
    enabled: true
    badgeName: FIPS
  techpreview:
    enabled: true
    badgeName: Tech Preview
`,
			expectedErr: false,
		},
		{
			name: "duplicate qualifier IDs",
			yamlContent: `qualifiers:
  fips:
    enabled: true
    badgeName: FIPS
  fips:
    enabled: false
    badgeName: FIPS2
`,
			expectedErr: true,
		},
		{
			name: "valid approval qualifier",
			yamlContent: `qualifiers:
  sdn-migration:
    approval: true
    enabled: true
    badgeName: SDN Migration
    summary: SDN Migration Approval
    description: Earned via team approval
    payloadBadgeStatus: OnSuccess
`,
			expectedErr: false,
		},
		{
			name: "invalid approval qualifier with failureLabels",
			yamlContent: `qualifiers:
  sdn-migration:
    approval: true
    enabled: true
    badgeName: SDN Migration
    labels:
      - some-label
`,
			expectedErr: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tmpFile := t.TempDir() + "/qualifiers.yaml"
			if err := os.WriteFile(tmpFile, []byte(testCase.yamlContent), 0644); err != nil {
				t.Fatalf("failed to write temp file: %v", err)
			}
			errs := validateReleaseQualifiersConfig(tmpFile)
			hasErr := false
			for _, err := range errs {
				if err != nil {
					hasErr = true
					break
				}
			}
			if hasErr && !testCase.expectedErr {
				t.Errorf("got errors when none expected: %v", errs)
			}
			if !hasErr && testCase.expectedErr {
				t.Errorf("expected errors but got none")
			}
		})
	}
}
