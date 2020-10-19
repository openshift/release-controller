package main

import (
	"testing"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
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
