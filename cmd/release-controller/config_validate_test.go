package main

import (
	"testing"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	prowconfig "k8s.io/test-infra/prow/config"
)

func TestVerifyPeriodicFields(t *testing.T) {
	testCases := []struct {
		name        string
		input       ReleaseConfig
		expectedErr bool
	}{{
		name: "Valid Cron",
		input: ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]ReleasePeriodic{
				"aws": {
					Cron:    "0 8,20 * * *",
					ProwJob: &ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: false,
	}, {
		name: "Valid Interval",
		input: ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]ReleasePeriodic{
				"aws": {
					Interval: "6h",
					ProwJob:  &ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: false,
	}, {
		name: "Missing Fields",
		input: ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]ReleasePeriodic{
				"aws": {
					ProwJob: &ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: true,
	}, {
		name: "Interval and Cron",
		input: ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]ReleasePeriodic{
				"aws": {
					Cron:     "0 8,20 * * *",
					Interval: "6h",
					ProwJob:  &ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: true,
	}, {
		name: "Invalid Cron",
		input: ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]ReleasePeriodic{
				"aws": {
					Cron:    "0 8,25 * * *",
					ProwJob: &ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: true,
	}, {
		name: "Invalid Interval",
		input: ReleaseConfig{
			Name: "TestRelease",
			Periodic: map[string]ReleasePeriodic{
				"aws": {
					Interval: "6g",
					ProwJob:  &ProwJobVerification{Name: "openshift-e2e-aws"},
				},
			},
		},
		expectedErr: true,
	}}
	for _, testCase := range testCases {
		errors := verifyPeriodicFields([]ReleaseConfig{testCase.input})
		if testCase.expectedErr && len(errors) == 0 {
			t.Errorf("%s: Expected error but none given", testCase.name)
		}
		if !testCase.expectedErr && len(errors) > 0 {
			t.Errorf("%s: Did not expect error, received errors: %v", testCase.name, utilerrors.NewAggregate(errors))
		}
	}
}

func TestFindDuplicatePeriodics(t *testing.T) {
	goodConfigs := []ReleaseConfig{{
		Name: "4.5.0-0.nightly",
		Periodic: map[string]ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.4-stable-to-4.5-nightly",
				},
			},
		},
	}, {
		Name: "4.6.0-0.nightly",
		Periodic: map[string]ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.6-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
				},
			},
		},
	}}
	sameConfigDuplicate := []ReleaseConfig{{
		Name: "4.5.0-0.nightly",
		Periodic: map[string]ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade2": {
				Upgrade: true,
				ProwJob: &ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.4-stable-to-4.5-nightly",
				},
			},
		},
	}, {
		Name: "4.6.0-0.nightly",
		Periodic: map[string]ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.6-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
				},
			},
		},
	}}
	differentConfigDuplicate := []ReleaseConfig{{
		Name: "4.5.0-0.nightly",
		Periodic: map[string]ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.4-stable-to-4.5-nightly",
				},
			},
		},
	}, {
		Name: "4.6.0-0.nightly",
		Periodic: map[string]ReleasePeriodic{
			"upgrade": {
				Upgrade: true,
				ProwJob: &ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-nightly",
				},
			},
			"upgrade-minor": {
				Upgrade:     true,
				UpgradeFrom: "PreviousMinor",
				ProwJob: &ProwJobVerification{
					Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
				},
			},
		},
	}}

	testCases := []struct {
		name          string
		configs       []ReleaseConfig
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
		configs     []ReleaseConfig
		expectedErr bool
	}{{
		name: "Good config",
		configs: []ReleaseConfig{{
			Name: "4.6.0-0.nightly",
			Verify: map[string]ReleaseVerification{
				"upgrade-minor": {
					Upgrade:     true,
					UpgradeFrom: "PreviousMinor",
					ProwJob: &ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			},
			Periodic: map[string]ReleasePeriodic{
				"upgrade-minor": {
					Upgrade: true,
					UpgradeFromRelease: &UpgradeRelease{
						Prerelease: &UpgradePrerelease{
							VersionBounds: UpgradeVersionBounds{
								Lower: "4.5.0",
								Upper: "4.6.0-0",
							},
						},
					},
					ProwJob: &ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			}},
		},
		expectedErr: false,
	}, {
		name: "Bad Verification",
		configs: []ReleaseConfig{{
			Name: "4.6.0-0.nightly",
			Verify: map[string]ReleaseVerification{
				"upgrade-minor": {
					Upgrade:     true,
					UpgradeFrom: "PreviousMinor",
					UpgradeFromRelease: &UpgradeRelease{
						Prerelease: &UpgradePrerelease{
							VersionBounds: UpgradeVersionBounds{
								Lower: "4.5.0",
								Upper: "4.6.0-0",
							},
						},
					},
					ProwJob: &ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			},
			Periodic: map[string]ReleasePeriodic{
				"upgrade-minor": {
					Upgrade: true,
					UpgradeFromRelease: &UpgradeRelease{
						Prerelease: &UpgradePrerelease{
							VersionBounds: UpgradeVersionBounds{
								Lower: "4.5.0",
								Upper: "4.6.0-0",
							},
						},
					},
					ProwJob: &ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			},
		}},
		expectedErr: true,
	}, {
		name: "Bad Periodic",
		configs: []ReleaseConfig{{
			Name: "4.6.0-0.nightly",
			Verify: map[string]ReleaseVerification{
				"upgrade-minor": {
					Upgrade:     true,
					UpgradeFrom: "PreviousMinor",
					ProwJob: &ProwJobVerification{
						Name: "release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-nightly",
					},
				},
			},
			Periodic: map[string]ReleasePeriodic{
				"upgrade-minor": {
					Upgrade:     true,
					UpgradeFrom: "PreviousMinor",
					UpgradeFromRelease: &UpgradeRelease{
						Prerelease: &UpgradePrerelease{
							VersionBounds: UpgradeVersionBounds{
								Lower: "4.5.0",
								Upper: "4.6.0-0",
							},
						},
					},
					ProwJob: &ProwJobVerification{
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

func TestValidateDefinedReleaseInformingJobs(t *testing.T) {
	testCases := []struct {
		name        string
		periodics   []prowconfig.Periodic
		rcPeriodics map[string]ReleasePeriodic
		rcVerify    map[string]ReleaseVerification
		expectedErr bool
	}{{
		name: "exact match",
		periodics: []prowconfig.Periodic{{
			JobBase: prowconfig.JobBase{
				Name: "periodic1",
				Labels: map[string]string{
					"ci-operator.openshift.io/release-controller": "true",
				},
			},
		}, {
			JobBase: prowconfig.JobBase{
				Name: "periodic2",
				Labels: map[string]string{
					"ci-operator.openshift.io/release-controller": "true",
				},
			},
		}, {
			JobBase: prowconfig.JobBase{
				Name: "periodic3",
				Labels: map[string]string{
					"ci-operator.openshift.io/release-controller": "true",
				},
			},
		}, {
			JobBase: prowconfig.JobBase{
				Name: "periodic4",
				Labels: map[string]string{
					"ci-operator.openshift.io/release-controller": "true",
				},
			},
		}},
		rcPeriodics: map[string]ReleasePeriodic{
			"first": {
				ProwJob: &ProwJobVerification{
					Name: "periodic1",
				},
			},
			"second": {
				ProwJob: &ProwJobVerification{
					Name: "periodic2",
				},
			},
		},
		rcVerify: map[string]ReleaseVerification{
			"third": {
				ProwJob: &ProwJobVerification{
					Name: "periodic3",
				},
			},
			"fourth": {
				ProwJob: &ProwJobVerification{
					Name: "periodic4",
				},
			},
		},
		expectedErr: false,
	}, {
		name: "missing job periodic defined in release-controller periodic",
		periodics: []prowconfig.Periodic{{
			JobBase: prowconfig.JobBase{
				Name: "periodic1",
				Labels: map[string]string{
					"ci-operator.openshift.io/release-controller": "true",
				},
			},
		}},
		rcPeriodics: map[string]ReleasePeriodic{
			"first": {
				ProwJob: &ProwJobVerification{
					Name: "periodic1",
				},
			},
			"second": {
				ProwJob: &ProwJobVerification{
					Name: "periodic2",
				},
			},
		},
		rcVerify:    map[string]ReleaseVerification{},
		expectedErr: true,
	}, {
		name: "missing release controller periodic",
		periodics: []prowconfig.Periodic{{
			JobBase: prowconfig.JobBase{
				Name: "periodic1",
				Labels: map[string]string{
					"ci-operator.openshift.io/release-controller": "true",
				},
			},
		}, {
			JobBase: prowconfig.JobBase{
				Name: "periodic2",
				Labels: map[string]string{
					"ci-operator.openshift.io/release-controller": "true",
				},
			},
		}},
		rcPeriodics: map[string]ReleasePeriodic{
			"first": {
				ProwJob: &ProwJobVerification{
					Name: "periodic1",
				},
			},
		},
		rcVerify:    map[string]ReleaseVerification{},
		expectedErr: true,
	}, {
		name: "disabled verification does not require match",
		periodics: []prowconfig.Periodic{{
			JobBase: prowconfig.JobBase{
				Name: "periodic3",
				Labels: map[string]string{
					"ci-operator.openshift.io/release-controller": "true",
				},
			},
		}},
		rcPeriodics: map[string]ReleasePeriodic{},
		rcVerify: map[string]ReleaseVerification{
			"third": {
				ProwJob: &ProwJobVerification{
					Name: "periodic3",
				},
			},
			"fourth": {
				Disabled: true,
				ProwJob: &ProwJobVerification{
					Name: "periodic4",
				},
			},
		},
		expectedErr: false,
	}, {
		name: "missing release controller verification",
		periodics: []prowconfig.Periodic{{
			JobBase: prowconfig.JobBase{
				Name: "periodic3",
				Labels: map[string]string{
					"ci-operator.openshift.io/release-controller": "true",
				},
			},
		}, {
			JobBase: prowconfig.JobBase{
				Name: "periodic4",
				Labels: map[string]string{
					"ci-operator.openshift.io/release-controller": "true",
				},
			},
		}},
		rcPeriodics: map[string]ReleasePeriodic{},
		rcVerify: map[string]ReleaseVerification{
			"third": {
				ProwJob: &ProwJobVerification{
					Name: "periodic3",
				},
			},
		},
		expectedErr: true,
	}, {
		name: "job config not labeled as release informing",
		periodics: []prowconfig.Periodic{{
			JobBase: prowconfig.JobBase{
				Name: "periodic1",
				Labels: map[string]string{
					"ci-operator.openshift.io/release-controller": "true",
				},
			},
		}, {
			JobBase: prowconfig.JobBase{
				Name: "periodic2",
			},
		}},
		rcPeriodics: map[string]ReleasePeriodic{
			"first": {
				ProwJob: &ProwJobVerification{
					Name: "periodic1",
				},
			},
			"second": {
				ProwJob: &ProwJobVerification{
					Name: "periodic2",
				},
			},
		},
		rcVerify:    map[string]ReleaseVerification{},
		expectedErr: true,
	}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			jobConfig := prowconfig.JobConfig{Periodics: testCase.periodics}
			releaseConfig := []ReleaseConfig{{Periodic: testCase.rcPeriodics, Verify: testCase.rcVerify}}
			errs := validateDefinedReleaseControllerJobs(jobConfig, releaseConfig)
			if errs != nil && !testCase.expectedErr {
				t.Errorf("Got errors when non were expected: %v", errs)
			}
			if errs == nil && testCase.expectedErr {
				t.Errorf("Got no errors when errors were expected")
			}
		})
	}
}
