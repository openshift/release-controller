package releasecontroller

import (
	"encoding/json"
	"testing"
)

func TestValidatePreviousMinorOverride(t *testing.T) {
	for _, tc := range []struct {
		name                                  string
		target, source, stream, upgradeFrom   string
		noUpgrade, explicitRelease, wantError bool
	}{
		{name: "valid", target: "5.0", source: "4.22", stream: "4-scos-stable", upgradeFrom: ReleaseUpgradeFromPreviousMinor},
		{name: "reusable for other versions", target: "6.0", source: "5.3", stream: "5-stable", upgradeFrom: ReleaseUpgradeFromPreviousMinor},
		{name: "missing target", source: "4.22", stream: "4-scos-stable", upgradeFrom: ReleaseUpgradeFromPreviousMinor, wantError: true},
		{name: "target has patch", target: "5.0.0", source: "4.22", stream: "4-scos-stable", upgradeFrom: ReleaseUpgradeFromPreviousMinor, wantError: true},
		{name: "target has prerelease", target: "5.0-rc", source: "4.22", stream: "4-scos-stable", upgradeFrom: ReleaseUpgradeFromPreviousMinor, wantError: true},
		{name: "source has patch", target: "5.0", source: "4.22.0", stream: "4-scos-stable", upgradeFrom: ReleaseUpgradeFromPreviousMinor, wantError: true},
		{name: "missing source", target: "5.0", stream: "4-scos-stable", upgradeFrom: ReleaseUpgradeFromPreviousMinor, wantError: true},
		{name: "missing stream", target: "5.0", source: "4.22", upgradeFrom: ReleaseUpgradeFromPreviousMinor, wantError: true},
		{name: "not an upgrade", target: "5.0", source: "4.22", stream: "4-scos-stable", upgradeFrom: ReleaseUpgradeFromPreviousMinor, noUpgrade: true, wantError: true},
		{name: "wrong mode", target: "5.0", source: "4.22", stream: "4-scos-stable", upgradeFrom: ReleaseUpgradeFromPreviousPatch, wantError: true},
		{name: "implicit mode", target: "5.0", source: "4.22", stream: "4-scos-stable", wantError: true},
		{name: "conflicting release selector", target: "5.0", source: "4.22", stream: "4-scos-stable", upgradeFrom: ReleaseUpgradeFromPreviousMinor, explicitRelease: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			override := &PreviousMinorOverride{TargetVersion: tc.target, Version: tc.source, Stream: tc.stream}
			var explicit *UpgradeRelease
			if tc.explicitRelease {
				explicit = &UpgradeRelease{Candidate: &UpgradeCandidate{Version: "4.22", Stream: "okd-scos"}}
			}
			for _, periodic := range []bool{false, true} {
				config := ReleaseConfig{Name: "5-scos-stable", As: ReleaseConfigModeStable}
				if periodic {
					config.Periodic = map[string]ReleasePeriodic{"upgrade-minor": {Upgrade: !tc.noUpgrade, UpgradeFrom: tc.upgradeFrom, UpgradeFromRelease: explicit, PreviousMinorOverride: override}}
				} else {
					config.Verify = map[string]ReleaseVerification{"upgrade-minor": {Upgrade: !tc.noUpgrade, UpgradeFrom: tc.upgradeFrom, UpgradeFromRelease: explicit, PreviousMinorOverride: override}}
				}
				raw, err := json.Marshal(struct {
					ReleaseConfig
					Expires string `json:"expires"`
				}{ReleaseConfig: config, Expires: "72h"})
				if err != nil {
					t.Fatal(err)
				}
				_, err = ParseReleaseConfig(string(raw), nil)
				if (err != nil) != tc.wantError {
					t.Errorf("periodic=%v: ParseReleaseConfig error = %v, wantError=%v", periodic, err, tc.wantError)
				}
			}
		})
	}
}
