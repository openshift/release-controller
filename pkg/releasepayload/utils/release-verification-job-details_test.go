package utils

import (
	"reflect"
	"testing"

	"github.com/blang/semver"
)

func TestParseReleaseVerificationJobName(t *testing.T) {
	tests := []struct {
		name        string
		prowjobName string
		want        *ReleaseVerificationJobDetails
		wantErr     bool
	}{
		{
			name:        "CIJob",
			prowjobName: "4.11.0-0.ci-2022-06-03-013657-aws-serial",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "0",
					Stream:              "ci",
					Timestamp:           "2022-06-03-013657",
					CIConfigurationName: "aws-serial",
					Count:               "",
				},
			},
			wantErr: false,
		},
		{
			name:        "CIJobWithRetries",
			prowjobName: "4.11.0-0.ci-2022-06-03-013657-aws-serial-1",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "0",
					Stream:              "ci",
					Timestamp:           "2022-06-03-013657",
					CIConfigurationName: "aws-serial",
					Count:               "1",
				},
			},
			wantErr: false,
		},
		{
			name:        "NightlyJob",
			prowjobName: "4.11.0-0.nightly-2022-06-03-013657-aws-serial",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "0",
					Stream:              "nightly",
					Timestamp:           "2022-06-03-013657",
					CIConfigurationName: "aws-serial",
					Count:               "",
				},
			},
			wantErr: false,
		},
		{
			name:        "NightlyJobWithRetries",
			prowjobName: "4.11.0-0.nightly-2022-06-03-013657-aws-serial-2",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "0",
					Stream:              "nightly",
					Timestamp:           "2022-06-03-013657",
					CIConfigurationName: "aws-serial",
					Count:               "2",
				},
			},
			wantErr: false,
		},
		{
			name:        "OKDJob",
			prowjobName: "4.11.0-0.okd-2022-06-03-013657-aws-serial",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "0",
					Stream:              "okd",
					Timestamp:           "2022-06-03-013657",
					CIConfigurationName: "aws-serial",
					Count:               "",
				},
			},
			wantErr: false,
		},
		{
			name:        "OKDJobWithRetries",
			prowjobName: "4.11.0-0.okd-2022-06-03-013657-aws-serial-1",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "0",
					Stream:              "okd",
					Timestamp:           "2022-06-03-013657",
					CIConfigurationName: "aws-serial",
					Count:               "1",
				},
			},
			wantErr: false,
		},
		{
			name:        "ReleaseCandidateJob",
			prowjobName: "4.11.0-rc.0-aws-serial",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "rc.0",
					Stream:              "Candidate",
					Timestamp:           "",
					CIConfigurationName: "aws-serial",
					Count:               "",
				},
			},
			wantErr: false,
		},
		{
			name:        "ReleaseCandidateJobWithRetries",
			prowjobName: "4.11.0-rc.0-aws-serial-1",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "rc.0",
					Stream:              "Candidate",
					Timestamp:           "",
					CIConfigurationName: "aws-serial",
					Count:               "1",
				},
			},
			wantErr: false,
		},
		{
			name:        "FeatureCandidateJob",
			prowjobName: "4.11.0-fc.0-aws-serial",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "fc.0",
					Stream:              "Candidate",
					Timestamp:           "",
					CIConfigurationName: "aws-serial",
					Count:               "",
				},
			},
			wantErr: false,
		},
		{
			name:        "FeatureCandidateJobWithRetries",
			prowjobName: "4.11.0-fc.0-aws-serial-2",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "fc.0",
					Stream:              "Candidate",
					Timestamp:           "",
					CIConfigurationName: "aws-serial",
					Count:               "2",
				},
			},
			wantErr: false,
		},
		{
			name:        "EngineeringCandidateJob",
			prowjobName: "4.13.0-ec.1-aws-sdn-serial",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 13,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "ec.1",
					Stream:              "Candidate",
					Timestamp:           "",
					CIConfigurationName: "aws-sdn-serial",
					Count:               "",
				},
			},
			wantErr: false,
		},
		{
			name:        "EngineeringCandidateJobWithRetries",
			prowjobName: "4.13.0-ec.1-aws-sdn-serial-2",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 13,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "ec.1",
					Stream:              "Candidate",
					Timestamp:           "",
					CIConfigurationName: "aws-sdn-serial",
					Count:               "2",
				},
			},
			wantErr: false,
		},
		{
			name:        "ProductionJob",
			prowjobName: "4.10.17-aws-serial",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 10,
				Z: 17,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "",
					Stream:              "Stable",
					Timestamp:           "",
					CIConfigurationName: "aws-serial",
					Count:               "",
				},
			},
			wantErr: false,
		},
		{
			name:        "ProductionJobWithRetries",
			prowjobName: "4.10.17-aws-serial-3",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 10,
				Z: 17,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "",
					Stream:              "Stable",
					Timestamp:           "",
					CIConfigurationName: "aws-serial",
					Count:               "3",
				},
			},
			wantErr: false,
		},
		{
			name:        "ProductionJobWithEmbeddedVersionString",
			prowjobName: "4.10.41-aws-sdn-upgrade-4.10-micro",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 10,
				Z: 41,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "",
					Stream:              "Stable",
					Timestamp:           "",
					CIConfigurationName: "aws-sdn-upgrade-4.10-micro",
					Count:               "",
				},
			},
			wantErr: false,
		},
		{
			name:        "ProductionJobWithEmbeddedVersionStringWithRetries",
			prowjobName: "4.10.41-aws-sdn-upgrade-4.10-micro-1",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 10,
				Z: 41,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "",
					Stream:              "Stable",
					Timestamp:           "",
					CIConfigurationName: "aws-sdn-upgrade-4.10-micro",
					Count:               "1",
					UpgradeFrom:         "",
				},
			},
			wantErr: false,
		},
		{
			name:        "AutomaticReleaseUpgradeTest",
			prowjobName: "4.11.14-upgrade-from-4.11.13-aws",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 14,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "",
					Stream:              "Stable",
					Timestamp:           "",
					CIConfigurationName: "aws",
					Count:               "",
					UpgradeFrom:         "4.11.13",
				},
			},
			wantErr: false,
		},
		{
			name:        "UnsupportedPrereleaseVersion",
			prowjobName: "4.10.41-alpha.aws-sdn-upgrade-4.10-micro",
			wantErr:     true,
		},
		{
			name:        "InvalidSemanticVersion",
			prowjobName: "x.10.17-aws-serial",
			wantErr:     true,
		},
		{
			name:        "UUIDBasedProwJobName",
			prowjobName: "13773708-610b-11ed-ade3-0a580a805f16",
			wantErr:     true,
		},
		{
			name:        "CandidateAutomaticReleaseUpgradeTest",
			prowjobName: "4.12.0-rc.0-upgrade-from-4.11.10-aws",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 12,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "rc.0",
					Stream:              "Candidate",
					Timestamp:           "",
					CIConfigurationName: "aws",
					Count:               "",
					UpgradeFrom:         "4.11.10",
				},
			},
			wantErr: false,
		},
		{
			name:        "CandidateAutomaticReleaseUpgradeWithCountTest",
			prowjobName: "4.12.0-rc.0-upgrade-from-4.11.10-aws-3",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 12,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "rc.0",
					Stream:              "Candidate",
					Timestamp:           "",
					CIConfigurationName: "aws",
					Count:               "3",
					UpgradeFrom:         "4.11.10",
				},
			},
			wantErr: false,
		},
		{
			name:        "CandidateAutomaticReleaseUpgradeToCandidateTest",
			prowjobName: "4.12.0-rc.7-upgrade-from-4.12.0-rc.6-aws",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 12,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "rc.7",
					Stream:              "Candidate",
					Timestamp:           "",
					CIConfigurationName: "aws",
					Count:               "",
					UpgradeFrom:         "4.12.0-rc.6",
				},
			},
			wantErr: false,
		},
		{
			name:        "CandidateAutomaticReleaseUpgradeToCandidateWithCountTest",
			prowjobName: "4.12.0-rc.7-upgrade-from-4.12.0-rc.6-aws-2",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 12,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "rc.7",
					Stream:              "Candidate",
					Timestamp:           "",
					CIConfigurationName: "aws",
					Count:               "2",
					UpgradeFrom:         "4.12.0-rc.6",
				},
			},
			wantErr: false,
		},
		{
			name:        "CandidateAutomaticReleaseUpgradeToStableTest",
			prowjobName: "4.12.0-rc.7-upgrade-from-4.12.0-aws",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 12,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "rc.7",
					Stream:              "Candidate",
					Timestamp:           "",
					CIConfigurationName: "aws",
					Count:               "",
					UpgradeFrom:         "4.12.0",
				},
			},
			wantErr: false,
		},
		{
			name:        "CandidateAutomaticReleaseUpgradeToStableWithCountTest",
			prowjobName: "4.12.0-rc.7-upgrade-from-4.12.0-aws-2",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 12,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "rc.7",
					Stream:              "Candidate",
					Timestamp:           "",
					CIConfigurationName: "aws",
					Count:               "2",
					UpgradeFrom:         "4.12.0",
				},
			},
			wantErr: false,
		},
		{
			name:        "StableAutomaticReleaseUpgradeTest",
			prowjobName: "4.12.6-upgrade-from-4.12.5-aws",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 12,
				Z: 6,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "",
					Stream:              "Stable",
					Timestamp:           "",
					CIConfigurationName: "aws",
					Count:               "",
					UpgradeFrom:         "4.12.5",
				},
			},
			wantErr: false,
		},
		{
			name:        "StableAutomaticReleaseUpgradeWithCountTest",
			prowjobName: "4.12.6-upgrade-from-4.12.5-aws-2",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 12,
				Z: 6,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "",
					Stream:              "Stable",
					Timestamp:           "",
					CIConfigurationName: "aws",
					Count:               "2",
					UpgradeFrom:         "4.12.5",
				},
			},
			wantErr: false,
		},
		{
			name:        "StableAutomaticReleaseUpgradeToCandidateTest",
			prowjobName: "4.12.6-upgrade-from-4.12.0-rc.6-aws",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 12,
				Z: 6,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "",
					Stream:              "Stable",
					Timestamp:           "",
					CIConfigurationName: "aws",
					Count:               "",
					UpgradeFrom:         "4.12.0-rc.6",
				},
			},
			wantErr: false,
		},
		{
			name:        "StableAutomaticReleaseUpgradeToCandidateWithCountTest",
			prowjobName: "4.12.6-upgrade-from-4.12.0-rc.6-aws-2",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 12,
				Z: 6,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "",
					Stream:              "Stable",
					Timestamp:           "",
					CIConfigurationName: "aws",
					Count:               "2",
					UpgradeFrom:         "4.12.0-rc.6",
				},
			},
			wantErr: false,
		},
		{
			name:        "NightlyMultiArchJob",
			prowjobName: "4.13.0-0.nightly-multi-2022-11-11-162833-multi-aws-ovn-upgrade",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 13,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "0",
					Stream:              "nightly",
					Timestamp:           "2022-11-11-162833",
					CIConfigurationName: "multi-aws-ovn-upgrade",
					Count:               "",
					Architecture:        "multi",
				},
			},
			wantErr: false,
		},
		{
			name:        "NightlyMultiArchJobWithRetries",
			prowjobName: "4.13.0-0.nightly-multi-2022-11-11-162833-multi-aws-ovn-upgrade-2",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 13,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "0",
					Stream:              "nightly",
					Timestamp:           "2022-11-11-162833",
					CIConfigurationName: "multi-aws-ovn-upgrade",
					Count:               "2",
					Architecture:        "multi",
				},
			},
			wantErr: false,
		},
		{
			name:        "NightlyMultiArchTruncatedJob",
			prowjobName: "4.13.0-0.nightly-multi-2022-11-11-162833-multi-aws-ovn-5w4rkb2",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 13,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "0",
					Stream:              "nightly",
					Timestamp:           "2022-11-11-162833",
					CIConfigurationName: "multi-aws-ovn-5w4rkb2",
					Count:               "",
					Architecture:        "multi",
				},
			},
			wantErr: false,
		},
		{
			name:        "NightlyMultiArchTruncatedJobWithRetries",
			prowjobName: "4.13.0-0.nightly-multi-2022-11-11-162833-multi-aws-ovn-5w4rkb2-3",
			want: &ReleaseVerificationJobDetails{
				X: 4,
				Y: 13,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "0",
					Stream:              "nightly",
					Timestamp:           "2022-11-11-162833",
					CIConfigurationName: "multi-aws-ovn-5w4rkb2",
					Count:               "3",
					Architecture:        "multi",
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseReleaseVerificationJobName(tt.prowjobName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseReleaseVerificationJobName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseReleaseVerificationJobName() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReleaseVerificationJobDetails_ToString(t *testing.T) {
	tests := []struct {
		name    string
		details ReleaseVerificationJobDetails
		want    string
	}{
		{
			name: "PreRelease",
			details: ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "0",
					Stream:              "nightly",
					Timestamp:           "2022-06-03-013657",
					CIConfigurationName: "aws-serial",
					Count:               "",
				},
			},
			want: "4.11.0-0.nightly-2022-06-03-013657-aws-serial",
		},
		{
			name: "Candidate",
			details: ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "fc.0",
					Stream:              "Candidate",
					Timestamp:           "",
					CIConfigurationName: "aws-serial",
					Count:               "2",
				},
			},
			want: "4.11.0-fc.0-aws-serial-2",
		},
		{
			name: "Stable",
			details: ReleaseVerificationJobDetails{
				X: 4,
				Y: 10,
				Z: 17,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "",
					Stream:              "Stable",
					Timestamp:           "",
					CIConfigurationName: "aws-serial",
					Count:               "3",
				},
			},
			want: "4.10.17-aws-serial-3",
		},
		{
			name: "AutomaticReleaseUpgrade",
			details: ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 14,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "",
					Stream:              "Stable",
					Timestamp:           "",
					CIConfigurationName: "aws",
					Count:               "",
					UpgradeFrom:         "4.11.13",
				},
			},
			want: "4.11.14-upgrade-from-4.11.13-aws",
		},
		{
			name: "AutomaticReleaseUpgradeFromCandidate",
			details: ReleaseVerificationJobDetails{
				X: 4,
				Y: 11,
				Z: 14,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "",
					Stream:              "Stable",
					Timestamp:           "",
					CIConfigurationName: "aws",
					Count:               "",
					UpgradeFrom:         "4.11.0-rc.0",
				},
			},
			want: "4.11.14-upgrade-from-4.11.0-rc.0-aws",
		},
		{
			name: "CandidateAutomaticReleaseUpgradeFromCandidate",
			details: ReleaseVerificationJobDetails{
				X: 4,
				Y: 12,
				Z: 0,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "rc.0",
					Stream:              "Candidate",
					Timestamp:           "",
					CIConfigurationName: "aws",
					Count:               "",
					UpgradeFrom:         "4.11.0-rc.9",
				},
			},
			want: "4.12.0-rc.0-upgrade-from-4.11.0-rc.9-aws",
		},
		{
			name: "MultiArchPreRelease",
			details: ReleaseVerificationJobDetails{
				X: 4,
				Y: 12,
				Z: 6,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "0",
					Stream:              "nightly",
					Timestamp:           "2022-06-03-013657",
					CIConfigurationName: "aws-serial",
					Count:               "",
					Architecture:        "s390x",
				},
			},
			want: "4.12.6-0.nightly-s390x-2022-06-03-013657-aws-serial",
		},
		{
			name: "MultiArchPreReleaseWithRetries",
			details: ReleaseVerificationJobDetails{
				X: 4,
				Y: 12,
				Z: 6,
				PreReleaseDetails: &PreReleaseDetails{
					Build:               "0",
					Stream:              "nightly",
					Timestamp:           "2022-06-03-013657",
					CIConfigurationName: "aws-serial",
					Count:               "2",
					Architecture:        "ppc64le",
				},
			},
			want: "4.12.6-0.nightly-ppc64le-2022-06-03-013657-aws-serial-2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.details.ToString(); got != tt.want {
				t.Errorf("ToString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parse(t *testing.T) {
	tests := []struct {
		name string
		line string
		want map[string]string
	}{
		{
			name: "PreRelease",
			line: "ci-2022-06-02-152750-aws-serial",
			want: map[string]string{
				"stream":       "ci",
				"architecture": "",
				"timestamp":    "2022-06-02-152750",
				"job":          "aws-serial",
				"count":        "",
			},
		},
		{
			name: "PreReleaseWithRetries",
			line: "ci-2022-06-02-152750-aws-serial-1",
			want: map[string]string{
				"stream":       "ci",
				"architecture": "",
				"timestamp":    "2022-06-02-152750",
				"job":          "aws-serial",
				"count":        "1",
			},
		},
		{
			name: "MultiArchPreRelease",
			line: "nightly-ppc64le-2022-06-02-152750-aws-serial",
			want: map[string]string{
				"stream":       "nightly",
				"architecture": "ppc64le",
				"timestamp":    "2022-06-02-152750",
				"job":          "aws-serial",
				"count":        "",
			},
		},
		{
			name: "MultiArchPreReleaseWithRetries",
			line: "nightly-s390x-2022-06-02-152750-aws-serial-3",
			want: map[string]string{
				"stream":       "nightly",
				"architecture": "s390x",
				"timestamp":    "2022-06-02-152750",
				"job":          "aws-serial",
				"count":        "3",
			},
		},
		{
			name: "Candidate",
			line: "0-metal-ipi-ovn-ipv6",
			want: map[string]string{
				"build": "0",
				"job":   "metal-ipi-ovn-ipv6",
				"count": "",
			},
		},
		{
			name: "CandidateWithRetries",
			line: "0-metal-ipi-ovn-ipv6-2",
			want: map[string]string{
				"build": "0",
				"job":   "metal-ipi-ovn-ipv6",
				"count": "2",
			},
		},
		{
			name: "Stable",
			line: "aws-serial",
			want: map[string]string{
				"job":   "aws-serial",
				"count": "",
			},
		},
		{
			name: "StableWithRetries",
			line: "aws-serial-3",
			want: map[string]string{
				"job":   "aws-serial",
				"count": "3",
			},
		},
		{
			name: "UpgradeFrom",
			line: "0-upgrade-from-4.11.13",
			want: map[string]string{
				"build": "0",
				"job":   "upgrade-from-4.11.13",
				"count": "",
			},
		},
		{
			name: "UpgradeFromWithRetries",
			line: "0-upgrade-from-4.11.13-1",
			want: map[string]string{
				"build": "0",
				"job":   "upgrade-from-4.11.13",
				"count": "1",
			},
		},
		{
			name: "Unimplemented",
			line: "*this_is_garbage*",
			want: map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parse(tt.line); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parsePreRelease(t *testing.T) {
	tests := []struct {
		name       string
		prerelease []semver.PRVersion
		want       *PreReleaseDetails
		wantErr    bool
	}{
		{
			name: "PreRelease",
			prerelease: []semver.PRVersion{
				{
					VersionNum: 0,
					IsNum:      true,
				},
				{
					VersionStr: "nightly-2022-06-03-121459-aws-single-node-serial",
					VersionNum: 0,
					IsNum:      false,
				},
			},
			want: &PreReleaseDetails{
				Build:               "0",
				Stream:              "nightly",
				Timestamp:           "2022-06-03-121459",
				CIConfigurationName: "aws-single-node-serial",
			},
			wantErr: false,
		},
		{
			name: "PreReleaseWithEmbeddedVersion",
			prerelease: []semver.PRVersion{
				{
					VersionNum: 0,
					IsNum:      true,
				},
				{
					VersionStr: "ci-2022-06-03-002248-azure-sdn-upgrade-4",
					VersionNum: 0,
					IsNum:      false,
				},
				{
					VersionStr: "10-minor-1",
					VersionNum: 0,
					IsNum:      false,
				},
			},
			want: &PreReleaseDetails{
				Build:               "0",
				Stream:              "ci",
				Timestamp:           "2022-06-03-002248",
				CIConfigurationName: "azure-sdn-upgrade-4.10-minor",
				Count:               "1",
			},
			wantErr: false,
		},
		{
			name: "Candidate",
			prerelease: []semver.PRVersion{
				{
					VersionStr: "fc",
					VersionNum: 0,
					IsNum:      false,
				},
				{
					VersionStr: "metal-ipi-ovn-ipv6-2",
					VersionNum: 0,
					IsNum:      false,
				},
			},
			want: &PreReleaseDetails{
				Build:               "fc",
				Stream:              "Candidate",
				CIConfigurationName: "metal-ipi-ovn-ipv6",
				Count:               "2",
			},
			wantErr: false,
		},
		{
			name: "CandidateAutomaticUpgrade",
			prerelease: []semver.PRVersion{
				{
					VersionStr: "fc",
					VersionNum: 0,
					IsNum:      false,
				},
				{
					VersionStr: "5-upgrade-from-4",
					VersionNum: 0,
					IsNum:      false,
				},
				{
					VersionStr: "",
					VersionNum: 11,
					IsNum:      true,
				},
				{
					VersionStr: "10-gcp",
					VersionNum: 0,
					IsNum:      false,
				},
			},
			want: &PreReleaseDetails{
				Build:               "fc.5",
				Stream:              "Candidate",
				CIConfigurationName: "gcp",
				UpgradeFrom:         "4.11.10",
			},
			wantErr: false,
		},
		{
			name: "Stable",
			prerelease: []semver.PRVersion{
				{
					VersionStr: "aws-serial",
					VersionNum: 0,
					IsNum:      false,
				},
			},
			want: &PreReleaseDetails{
				Stream:              "Stable",
				CIConfigurationName: "aws-serial",
			},
			wantErr: false,
		},
		{
			name: "StableAutomaticUpgrade",
			prerelease: []semver.PRVersion{
				{
					VersionStr: "upgrade-from-4",
					VersionNum: 0,
					IsNum:      false,
				},
				{
					VersionStr: "",
					VersionNum: 11,
					IsNum:      true,
				},
				{
					VersionStr: "10-gcp",
					VersionNum: 0,
					IsNum:      false,
				},
			},
			want: &PreReleaseDetails{
				Stream:              "Stable",
				CIConfigurationName: "gcp",
				UpgradeFrom:         "4.11.10",
			},
			wantErr: false,
		},
		{
			name: "InvalidStableAutomaticUpgrade",
			prerelease: []semver.PRVersion{
				{
					VersionStr: "upgrade-from-previous-minor",
					VersionNum: 0,
					IsNum:      false,
				},
			},
			want: &PreReleaseDetails{
				Stream:              "Stable",
				CIConfigurationName: "upgrade-from-previous-minor",
			},
			wantErr: false,
		},
		{
			name: "CandidateAutomaticUpgradeFromCandidate",
			prerelease: []semver.PRVersion{
				{
					VersionStr: "fc",
					VersionNum: 0,
					IsNum:      false,
				},
				{
					VersionStr: "7-upgrade-from-4",
					VersionNum: 0,
					IsNum:      false,
				},
				{
					VersionStr: "",
					VersionNum: 12,
					IsNum:      true,
				},
				{
					VersionStr: "0-rc",
					VersionNum: 0,
					IsNum:      false,
				},
				{
					VersionStr: "6-aws",
					VersionNum: 0,
					IsNum:      false,
				},
			},
			want: &PreReleaseDetails{
				Build:               "fc.7",
				Stream:              "Candidate",
				CIConfigurationName: "aws",
				UpgradeFrom:         "4.12.0-rc.6",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePreRelease(tt.prerelease)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePreRelease() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePreRelease() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_splitVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		details *PreReleaseDetails
		want    *PreReleaseDetails
	}{
		{
			name:    "PreRelease",
			version: "ci-2022-06-02-150548-aws-ovn-upgrade-4.10-micro",
			details: &PreReleaseDetails{
				Build: "0",
			},
			want: &PreReleaseDetails{
				Build:               "0",
				Stream:              "ci",
				Timestamp:           "2022-06-02-150548",
				CIConfigurationName: "aws-ovn-upgrade-4.10-micro",
			},
		},
		{
			name:    "PreReleaseWithRetry",
			version: "ci-2022-06-02-150548-aws-ovn-upgrade-4.10-micro-2",
			details: &PreReleaseDetails{
				Build: "0",
			},
			want: &PreReleaseDetails{
				Build:               "0",
				Stream:              "ci",
				Timestamp:           "2022-06-02-150548",
				CIConfigurationName: "aws-ovn-upgrade-4.10-micro",
				Count:               "2",
			},
		},
		{
			name:    "Candidate",
			version: "0-metal-ipi-ovn-ipv6",
			details: &PreReleaseDetails{
				Build: "fc",
			},
			want: &PreReleaseDetails{
				Build:               "fc.0",
				CIConfigurationName: "metal-ipi-ovn-ipv6",
			},
		},
		{
			name:    "CandidateWithRetry",
			version: "0-metal-ipi-ovn-ipv6-1",
			details: &PreReleaseDetails{
				Build: "fc",
			},
			want: &PreReleaseDetails{
				Build:               "fc.0",
				CIConfigurationName: "metal-ipi-ovn-ipv6",
				Count:               "1",
			},
		},
		{
			name:    "Stable",
			version: "aws-serial",
			details: &PreReleaseDetails{
				Stream: "Stable",
			},
			want: &PreReleaseDetails{
				Stream:              "Stable",
				CIConfigurationName: "aws-serial",
			},
		},
		{
			name:    "StableWithRetry",
			version: "aws-serial-3",
			details: &PreReleaseDetails{
				Stream: "Stable",
			},
			want: &PreReleaseDetails{
				Stream:              "Stable",
				CIConfigurationName: "aws-serial",
				Count:               "3",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			splitVersion(tt.version, tt.details)
			if !reflect.DeepEqual(tt.details, tt.want) {
				t.Errorf("splitVersion() returned = %v, wanted %v", tt.details, tt.want)
			}
		})
	}
}
