package utils

import (
	"github.com/blang/semver"
	"reflect"
	"testing"
)

func TestNewReleaseVerificationJobName(t *testing.T) {
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
				Name: "4.11.0-0.ci-2022-06-03-013657-aws-serial",
				Version: semver.Version{
					Major: 4,
					Minor: 11,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "",
							VersionNum: 0,
							IsNum:      true,
						},
						{
							VersionStr: "ci-2022-06-03-013657-aws-serial",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "0",
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
				Name: "4.11.0-0.ci-2022-06-03-013657-aws-serial-1",
				Version: semver.Version{
					Major: 4,
					Minor: 11,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "",
							VersionNum: 0,
							IsNum:      true,
						},
						{
							VersionStr: "ci-2022-06-03-013657-aws-serial-1",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "0",
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
				Name: "4.11.0-0.nightly-2022-06-03-013657-aws-serial",
				Version: semver.Version{
					Major: 4,
					Minor: 11,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "",
							VersionNum: 0,
							IsNum:      true,
						},
						{
							VersionStr: "nightly-2022-06-03-013657-aws-serial",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "0",
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
				Name: "4.11.0-0.nightly-2022-06-03-013657-aws-serial-2",
				Version: semver.Version{
					Major: 4,
					Minor: 11,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "",
							VersionNum: 0,
							IsNum:      true,
						},
						{
							VersionStr: "nightly-2022-06-03-013657-aws-serial-2",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "0",
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
				Name: "4.11.0-0.okd-2022-06-03-013657-aws-serial",
				Version: semver.Version{
					Major: 4,
					Minor: 11,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "",
							VersionNum: 0,
							IsNum:      true,
						},
						{
							VersionStr: "okd-2022-06-03-013657-aws-serial",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "0",
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
				Name: "4.11.0-0.okd-2022-06-03-013657-aws-serial-1",
				Version: semver.Version{
					Major: 4,
					Minor: 11,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "",
							VersionNum: 0,
							IsNum:      true,
						},
						{
							VersionStr: "okd-2022-06-03-013657-aws-serial-1",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "0",
					Stream:              "okd",
					Timestamp:           "2022-06-03-013657",
					CIConfigurationName: "aws-serial",
					Count:               "1",
				},
			},
			wantErr: false,
		},
		{
			name:        "OKD-SCOSJob",
			prowjobName: "4.20.0-0.okd-scos-2025-06-19-225747-aws",
			want: &ReleaseVerificationJobDetails{
				Name: "4.20.0-0.okd-scos-2025-06-19-225747-aws",
				Version: semver.Version{
					Major: 4,
					Minor: 20,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "",
							VersionNum: 0,
							IsNum:      true,
						},
						{
							VersionStr: "okd-scos-2025-06-19-225747-aws",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "0",
					Stream:              "okd-scos",
					Timestamp:           "2025-06-19-225747",
					CIConfigurationName: "aws",
					Count:               "",
				},
			},
			wantErr: false,
		},
		{
			name:        "OKD-SCOSJobWithRetries",
			prowjobName: "4.20.0-0.okd-scos-2025-06-19-225747-aws-2",
			want: &ReleaseVerificationJobDetails{
				Name: "4.20.0-0.okd-scos-2025-06-19-225747-aws-2",
				Version: semver.Version{
					Major: 4,
					Minor: 20,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "",
							VersionNum: 0,
							IsNum:      true,
						},
						{
							VersionStr: "okd-scos-2025-06-19-225747-aws-2",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "0",
					Stream:              "okd-scos",
					Timestamp:           "2025-06-19-225747",
					CIConfigurationName: "aws",
					Count:               "2",
				},
			},
			wantErr: false,
		},
		{
			name:        "OKD-SCOSReleaseJob",
			prowjobName: "4.19.0-okd-scos.5-upgrade",
			want: &ReleaseVerificationJobDetails{
				Name: "4.19.0-okd-scos.5-upgrade",
				Version: semver.Version{
					Major: 4,
					Minor: 19,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "okd-scos",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "5-upgrade",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "5",
					Stream:              "okd-scos",
					Timestamp:           "",
					CIConfigurationName: "upgrade",
					Count:               "",
				},
			},
			wantErr: false,
		},
		{
			name:        "OKD-SCOSReleaseJobWithRetries",
			prowjobName: "4.19.0-okd-scos.5-upgrade-minor-1",
			want: &ReleaseVerificationJobDetails{
				Name: "4.19.0-okd-scos.5-upgrade-minor-1",
				Version: semver.Version{
					Major: 4,
					Minor: 19,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "okd-scos",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "5-upgrade-minor-1",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "5",
					Stream:              "okd-scos",
					Timestamp:           "",
					CIConfigurationName: "upgrade-minor",
					Count:               "1",
				},
			},
			wantErr: false,
		},
		{
			name:        "OKD-SCOS-ECReleaseJob",
			prowjobName: "4.20.0-okd-scos.ec.4-upgrade-minor",
			want: &ReleaseVerificationJobDetails{
				Name: "4.20.0-okd-scos.ec.4-upgrade-minor",
				Version: semver.Version{
					Major: 4,
					Minor: 20,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "okd-scos",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "ec",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "4-upgrade-minor",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "ec.4",
					Stream:              "okd-scos",
					Timestamp:           "",
					CIConfigurationName: "upgrade-minor",
					Count:               "",
				},
			},
			wantErr: false,
		}, {
			name:        "ReleaseCandidateJob",
			prowjobName: "4.11.0-rc.0-aws-serial",
			want: &ReleaseVerificationJobDetails{
				Name: "4.11.0-rc.0-aws-serial",
				Version: semver.Version{
					Major: 4,
					Minor: 11,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "rc",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "0-aws-serial",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "rc.0",
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
				Name: "4.11.0-rc.0-aws-serial-1",
				Version: semver.Version{
					Major: 4,
					Minor: 11,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "rc",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "0-aws-serial-1",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "rc.0",
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
				Name: "4.11.0-fc.0-aws-serial",
				Version: semver.Version{
					Major: 4,
					Minor: 11,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "fc",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "0-aws-serial",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "fc.0",
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
				Name: "4.11.0-fc.0-aws-serial-2",
				Version: semver.Version{
					Major: 4,
					Minor: 11,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "fc",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "0-aws-serial-2",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "fc.0",
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
				Name: "4.13.0-ec.1-aws-sdn-serial",
				Version: semver.Version{
					Major: 4,
					Minor: 13,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "ec",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "1-aws-sdn-serial",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "ec.1",
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
				Name: "4.13.0-ec.1-aws-sdn-serial-2",
				Version: semver.Version{
					Major: 4,
					Minor: 13,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "ec",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "1-aws-sdn-serial-2",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "ec.1",
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
				Name: "4.10.17-aws-serial",
				Version: semver.Version{
					Major: 4,
					Minor: 10,
					Patch: 17,
					Pre: []semver.PRVersion{
						{
							VersionStr: "aws-serial",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "",
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
				Name: "4.10.17-aws-serial-3",
				Version: semver.Version{
					Major: 4,
					Minor: 10,
					Patch: 17,
					Pre: []semver.PRVersion{
						{
							VersionStr: "aws-serial-3",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "",
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
				Name: "4.10.41-aws-sdn-upgrade-4.10-micro",
				Version: semver.Version{
					Major: 4,
					Minor: 10,
					Patch: 41,
					Pre: []semver.PRVersion{
						{
							VersionStr: "aws-sdn-upgrade-4",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "10-micro",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "",
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
				Name: "4.10.41-aws-sdn-upgrade-4.10-micro-1",
				Version: semver.Version{
					Major: 4,
					Minor: 10,
					Patch: 41,
					Pre: []semver.PRVersion{
						{
							VersionStr: "aws-sdn-upgrade-4",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "10-micro-1",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "",
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
				Name: "4.11.14-upgrade-from-4.11.13-aws",
				Version: semver.Version{
					Major: 4,
					Minor: 11,
					Patch: 14,
					Pre: []semver.PRVersion{
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
							VersionStr: "13-aws",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "",
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
			want: &ReleaseVerificationJobDetails{
				Name: "4.10.41-alpha.aws-sdn-upgrade-4.10-micro",
				Version: semver.Version{
					Major: 4,
					Minor: 10,
					Patch: 41,
					Pre: []semver.PRVersion{
						{
							VersionStr: "alpha",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "aws-sdn-upgrade-4",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "10-micro",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					Stream:              StreamStable,
					CIConfigurationName: "alpha.aws-sdn-upgrade-4.10-micro",
				},
			},
			wantErr:     false,
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
				Name: "4.12.0-rc.0-upgrade-from-4.11.10-aws",
				Version: semver.Version{
					Major: 4,
					Minor: 12,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "rc",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "0-upgrade-from-4",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "",
							VersionNum: 11,
							IsNum:      true,
						},
						{
							VersionStr: "10-aws",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "rc.0",
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
				Name: "4.12.0-rc.0-upgrade-from-4.11.10-aws-3",
				Version: semver.Version{
					Major: 4,
					Minor: 12,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "rc",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "0-upgrade-from-4",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "",
							VersionNum: 11,
							IsNum:      true,
						},
						{
							VersionStr: "10-aws-3",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "rc.0",
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
				Name: "4.12.0-rc.7-upgrade-from-4.12.0-rc.6-aws",
				Version: semver.Version{
					Major: 4,
					Minor: 12,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "rc",
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
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "rc.7",
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
				Name: "4.12.0-rc.7-upgrade-from-4.12.0-rc.6-aws-2",
				Version: semver.Version{
					Major: 4,
					Minor: 12,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "rc",
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
							VersionStr: "6-aws-2",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "rc.7",
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
				Name: "4.12.0-rc.7-upgrade-from-4.12.0-aws",
				Version: semver.Version{
					Major: 4,
					Minor: 12,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "rc",
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
							VersionStr: "0-aws",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "rc.7",
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
				Name: "4.12.0-rc.7-upgrade-from-4.12.0-aws-2",
				Version: semver.Version{
					Major: 4,
					Minor: 12,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "rc",
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
							VersionStr: "0-aws-2",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "rc.7",
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
				Name: "4.12.6-upgrade-from-4.12.5-aws",
				Version: semver.Version{
					Major: 4,
					Minor: 12,
					Patch: 6,
					Pre: []semver.PRVersion{
						{
							VersionStr: "upgrade-from-4",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "",
							VersionNum: 12,
							IsNum:      true,
						},
						{
							VersionStr: "5-aws",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "",
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
				Name: "4.12.6-upgrade-from-4.12.5-aws-2",
				Version: semver.Version{
					Major: 4,
					Minor: 12,
					Patch: 6,
					Pre: []semver.PRVersion{
						{
							VersionStr: "upgrade-from-4",
							VersionNum: 0,
							IsNum:      false,
						},
						{
							VersionStr: "",
							VersionNum: 12,
							IsNum:      true,
						},
						{
							VersionStr: "5-aws-2",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "",
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
				Name: "4.12.6-upgrade-from-4.12.0-rc.6-aws",
				Version: semver.Version{
					Major: 4,
					Minor: 12,
					Patch: 6,
					Pre: []semver.PRVersion{
						{
							VersionStr: "upgrade-from-4",
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
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "",
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
				Name: "4.12.6-upgrade-from-4.12.0-rc.6-aws-2",
				Version: semver.Version{
					Major: 4,
					Minor: 12,
					Patch: 6,
					Pre: []semver.PRVersion{
						{
							VersionStr: "upgrade-from-4",
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
							VersionStr: "6-aws-2",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "",
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
				Name: "4.13.0-0.nightly-multi-2022-11-11-162833-multi-aws-ovn-upgrade",
				Version: semver.Version{
					Major: 4,
					Minor: 13,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "",
							VersionNum: 0,
							IsNum:      true,
						},
						{
							VersionStr: "nightly-multi-2022-11-11-162833-multi-aws-ovn-upgrade",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "0",
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
				Name: "4.13.0-0.nightly-multi-2022-11-11-162833-multi-aws-ovn-upgrade-2",
				Version: semver.Version{
					Major: 4,
					Minor: 13,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "",
							VersionNum: 0,
							IsNum:      true,
						},
						{
							VersionStr: "nightly-multi-2022-11-11-162833-multi-aws-ovn-upgrade-2",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "0",
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
				Name: "4.13.0-0.nightly-multi-2022-11-11-162833-multi-aws-ovn-5w4rkb2",
				Version: semver.Version{
					Major: 4,
					Minor: 13,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "",
							VersionNum: 0,
							IsNum:      true,
						},
						{
							VersionStr: "nightly-multi-2022-11-11-162833-multi-aws-ovn-5w4rkb2",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "0",
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
				Name: "4.13.0-0.nightly-multi-2022-11-11-162833-multi-aws-ovn-5w4rkb2-3",
				Version: semver.Version{
					Major: 4,
					Minor: 13,
					Patch: 0,
					Pre: []semver.PRVersion{
						{
							VersionStr: "",
							VersionNum: 0,
							IsNum:      true,
						},
						{
							VersionStr: "nightly-multi-2022-11-11-162833-multi-aws-ovn-5w4rkb2-3",
							VersionNum: 0,
							IsNum:      false,
						},
					},
				},
				PreReleaseDetails: &PreReleaseDetails{
					PreRelease:          "0",
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
			got, err := NewReleaseVerificationJobDetails(tt.prowjobName)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewReleaseVerificationJobDetails() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewReleaseVerificationJobDetails() got = %v, want %v", got, tt.want)
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
			line: "0.ci-2022-06-02-152750-aws-serial",
			want: map[string]string{
				"prerelease":   "0",
				"stream":       "ci",
				"architecture": "",
				"timestamp":    "2022-06-02-152750",
				"job":          "aws-serial",
				"count":        "",
			},
		},
		{
			name: "PreReleaseWithRetries",
			line: "0.ci-2022-06-02-152750-aws-serial-1",
			want: map[string]string{
				"prerelease":   "0",
				"stream":       "ci",
				"architecture": "",
				"timestamp":    "2022-06-02-152750",
				"job":          "aws-serial",
				"count":        "1",
			},
		},
		{
			name: "MultiArchPreRelease",
			line: "0.nightly-ppc64le-2022-06-02-152750-aws-serial",
			want: map[string]string{
				"prerelease":   "0",
				"stream":       "nightly",
				"architecture": "ppc64le",
				"timestamp":    "2022-06-02-152750",
				"job":          "aws-serial",
				"count":        "",
			},
		},
		{
			name: "MultiArchPreReleaseWithRetries",
			line: "0.nightly-s390x-2022-06-02-152750-aws-serial-3",
			want: map[string]string{
				"prerelease":   "0",
				"stream":       "nightly",
				"architecture": "s390x",
				"timestamp":    "2022-06-02-152750",
				"job":          "aws-serial",
				"count":        "3",
			},
		},
		{
			name: "Candidate",
			line: "metal-ipi-ovn-ipv6",
			want: map[string]string{
				"job":   "metal-ipi-ovn-ipv6",
				"count": "",
			},
		},
		{
			name: "CandidateWithRetries",
			line: "metal-ipi-ovn-ipv6-2",
			want: map[string]string{
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
			line: "upgrade-from-4.11.13-aws",
			want: map[string]string{
				"upgrade_from":     "4.11.13",
				"job": "aws",
				"count":            "",
				"prerelease":       "",
			},
		},
		{
			name: "UpgradeFromWithRetries",
			line: "upgrade-from-4.11.13-gcp-1",
			want: map[string]string{
				"upgrade_from":     "4.11.13",
				"job": "gcp",
				"count":            "1",
				"prerelease":       "",
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
