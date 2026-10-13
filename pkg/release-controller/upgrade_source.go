package releasecontroller

import (
	"fmt"
	"strings"

	"github.com/blang/semver"
)

// ValidatePreviousMinorOverride validates the source policy and its selection mode.
func ValidatePreviousMinorOverride(upgrade bool, upgradeFrom string, upgradeFromRelease *UpgradeRelease, override *PreviousMinorOverride) error {
	if override == nil {
		return nil
	}
	if !upgrade || upgradeFrom != ReleaseUpgradeFromPreviousMinor || upgradeFromRelease != nil {
		return fmt.Errorf("previousMinorOverride requires upgrade=true, upgradeFrom=PreviousMinor, and no upgradeFromRelease")
	}
	if strings.TrimSpace(override.Stream) == "" {
		return fmt.Errorf("previousMinorOverride requires a source stream")
	}
	if _, err := ParseMajorMinor(override.TargetVersion); err != nil {
		return fmt.Errorf("previousMinorOverride targetVersion: %w", err)
	}
	if _, err := ParseMajorMinor(override.Version); err != nil {
		return fmt.Errorf("previousMinorOverride version: %w", err)
	}
	return nil
}

// ParseMajorMinor parses exactly a semantic major.minor without patch or prerelease.
func ParseMajorMinor(value string) (semver.Version, error) {
	if len(strings.Split(value, ".")) == 2 {
		if version, err := semver.Parse(value + ".0"); err == nil && len(version.Pre) == 0 && len(version.Build) == 0 {
			return version, nil
		}
	}
	return semver.Version{}, fmt.Errorf("%q must be a major.minor version", value)
}
