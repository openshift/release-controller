package main

import (
	"testing"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"github.com/openshift/release-controller/pkg/releasequalifiers"
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
							"rosa": {},
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
							"hcm": {
								Enabled:      releasequalifiers.BoolPtr(false),
								BadgeName:    "HCM Updated",
								Description:  "An updated description when displaying badge details",
								PayloadBadge: releasequalifiers.PayloadBadgeNo,
							},
						},
					},
				},
			}},
			expectedErr: false,
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
