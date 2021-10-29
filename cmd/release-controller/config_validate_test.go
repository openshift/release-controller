package main

import (
	"github.com/openshift/release-controller/pkg/release-controller"
	"testing"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

func TestVerifyPeriodicFields(t *testing.T) {
	testCases := []struct {
		name        string
		input       release_controller.ReleaseConfig
		expectedErr bool
	}{{
		name: "Valid Cron",
		input: release_controller.ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]release_controller.ReleasePeriodic{
				"aws": {
					Cron:    "0 8,20 * * *",
					ProwJob: &release_controller.ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: false,
	}, {
		name: "Valid Interval",
		input: release_controller.ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]release_controller.ReleasePeriodic{
				"aws": {
					Interval: "6h",
					ProwJob:  &release_controller.ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: false,
	}, {
		name: "Missing Fields",
		input: release_controller.ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]release_controller.ReleasePeriodic{
				"aws": {
					ProwJob: &release_controller.ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: true,
	}, {
		name: "Interval and Cron",
		input: release_controller.ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]release_controller.ReleasePeriodic{
				"aws": {
					Cron:     "0 8,20 * * *",
					Interval: "6h",
					ProwJob:  &release_controller.ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: true,
	}, {
		name: "Invalid Cron",
		input: release_controller.ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]release_controller.ReleasePeriodic{
				"aws": {
					Cron:    "0 8,25 * * *",
					ProwJob: &release_controller.ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: true,
	}, {
		name: "Invalid Interval",
		input: release_controller.ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]release_controller.ReleasePeriodic{
				"aws": {
					Interval: "6g",
					ProwJob:  &release_controller.ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: true,
	}}
	for _, testCase := range testCases {
		errors := verifyPeriodicFields([]release_controller.ReleaseConfig{testCase.input})
		if testCase.expectedErr && len(errors) == 0 {
			t.Errorf("%s: Expected error but none given", testCase.name)
		}
		if !testCase.expectedErr && len(errors) > 0 {
			t.Errorf("%s: Did not expect error, received errors: %v", testCase.name, utilerrors.NewAggregate(errors))
		}
	}
}

func TestFindDuplicatePeriodics(t *testing.T) {
	goodConfigs := []release_controller.ReleaseConfig{{
		Name: "4.5.0-0.nightly",
		Periodic: map[string]release_controller.ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &release_controller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &release_controller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.4-stable-to-4.5-nightly",
				},
			},
		},
	}, {
		Name: "4.6.0-0.nightly",
		Periodic: map[string]release_controller.ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &release_controller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.6-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &release_controller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
				},
			},
		},
	}}
	sameConfigDuplicate := []release_controller.ReleaseConfig{{
		Name: "4.5.0-0.nightly",
		Periodic: map[string]release_controller.ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &release_controller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade2": {
				Upgrade: true,
				ProwJob: &release_controller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &release_controller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.4-stable-to-4.5-nightly",
				},
			},
		},
	}, {
		Name: "4.6.0-0.nightly",
		Periodic: map[string]release_controller.ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &release_controller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.6-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &release_controller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
				},
			},
		},
	}}
	differentConfigDuplicate := []release_controller.ReleaseConfig{{
		Name: "4.5.0-0.nightly",
		Periodic: map[string]release_controller.ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &release_controller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &release_controller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.4-stable-to-4.5-nightly",
				},
			},
		},
	}, {
		Name: "4.6.0-0.nightly",
		Periodic: map[string]release_controller.ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &release_controller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &release_controller.ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
				},
			},
		},
	}}

	testCases := []struct {
		name          string
		configs       []release_controller.ReleaseConfig
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
		configs     []release_controller.ReleaseConfig
		expectedErr bool
	}{{
		name: "Good config",
		configs: []release_controller.ReleaseConfig{{
			Name: "4.6.0-0.nightly",
			Verify: map[string]release_controller.ReleaseVerification{
				"upgrade-minor": {
					Upgrade:     true,
					UpgradeFrom: "PreviousMinor",
					ProwJob: &release_controller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			},
			Periodic: map[string]release_controller.ReleasePeriodic{
				"upgrade-minor": {
					Upgrade: true,
					UpgradeFromRelease: &release_controller.UpgradeRelease{
						Prerelease: &release_controller.UpgradePrerelease{
							VersionBounds: release_controller.UpgradeVersionBounds{
								Lower: "4.5.0",
								Upper: "4.6.0-0",
							},
						},
					},
					ProwJob: &release_controller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			}},
		},
		expectedErr: false,
	}, {
		name: "Bad Verification",
		configs: []release_controller.ReleaseConfig{{
			Name: "4.6.0-0.nightly",
			Verify: map[string]release_controller.ReleaseVerification{
				"upgrade-minor": {
					Upgrade:     true,
					UpgradeFrom: "PreviousMinor",
					UpgradeFromRelease: &release_controller.UpgradeRelease{
						Prerelease: &release_controller.UpgradePrerelease{
							VersionBounds: release_controller.UpgradeVersionBounds{
								Lower: "4.5.0",
								Upper: "4.6.0-0",
							},
						},
					},
					ProwJob: &release_controller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			},
			Periodic: map[string]release_controller.ReleasePeriodic{
				"upgrade-minor": {
					Upgrade: true,
					UpgradeFromRelease: &release_controller.UpgradeRelease{
						Prerelease: &release_controller.UpgradePrerelease{
							VersionBounds: release_controller.UpgradeVersionBounds{
								Lower: "4.5.0",
								Upper: "4.6.0-0",
							},
						},
					},
					ProwJob: &release_controller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			},
		}},
		expectedErr: true,
	}, {
		name: "Bad Periodic",
		configs: []release_controller.ReleaseConfig{{
			Name: "4.6.0-0.nightly",
			Verify: map[string]release_controller.ReleaseVerification{
				"upgrade-minor": {
					Upgrade:     true,
					UpgradeFrom: "PreviousMinor",
					ProwJob: &release_controller.ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			},
			Periodic: map[string]release_controller.ReleasePeriodic{
				"upgrade-minor": {
					Upgrade:     true,
					UpgradeFrom: "PreviousMinor",
					UpgradeFromRelease: &release_controller.UpgradeRelease{
						Prerelease: &release_controller.UpgradePrerelease{
							VersionBounds: release_controller.UpgradeVersionBounds{
								Lower: "4.5.0",
								Upper: "4.6.0-0",
							},
						},
					},
					ProwJob: &release_controller.ProwJobVerification{
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
