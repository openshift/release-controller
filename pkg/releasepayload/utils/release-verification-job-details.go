package utils

import (
	"fmt"
	"github.com/blang/semver"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"regexp"
	"strings"
)

const (
	StreamStable    = "Stable"
	StreamCandidate = "Candidate"
)

var (
	stableRelease       = regexp.MustCompile(`(?P<job>[\w\d-\\.]+?)(?:-(?P<count>\d+))?$`)
	candidateRelease    = regexp.MustCompile(`^(?P<build>[\d-]+)-(?P<job>[\w\d-\\.]+?)(?:-(?P<count>\d+))?$`)
	prerelease          = regexp.MustCompile(`^(?P<stream>[\w\d]+)-(?P<architecture>\w+)?-?(?P<timestamp>\d{4}-\d{2}-\d{2}-\d{6})-(?P<job>[\w\d-\\.]+?)(?:-(?P<count>\d+))?$`)
	upgradeFrom         = regexp.MustCompile(`^(?P<build>[\w\d\\.]+)-(?P<job>upgrade-from[\w\d-\\.]+?)(?:-(?P<count>\d+))?$`)
	automaticUpgradeJob = regexp.MustCompile(`^upgrade-from-(?P<version>[\d\\.]+)-?(?P<candidate>[e|f|r]c.\d+)?-?(?P<platform>\w+)-?(?P<count>\d+)?$`)
)

type PreReleaseDetails struct {
	Build               string
	Stream              string
	Timestamp           string
	CIConfigurationName string
	Count               string
	UpgradeFrom         string
	Architecture        string
}

type ReleaseVerificationJobDetails struct {
	X, Y, Z uint64
	*PreReleaseDetails
}

func (d ReleaseVerificationJobDetails) ToString() string {
	count := ""
	if len(d.Count) > 0 {
		count = fmt.Sprintf("-%s", d.Count)
	}
	switch d.Stream {
	case StreamStable:
		if len(d.UpgradeFrom) > 0 {
			return fmt.Sprintf("%d.%d.%d-upgrade-from-%s-%s%s", d.X, d.Y, d.Z, d.UpgradeFrom, d.CIConfigurationName, count)
		}
		return fmt.Sprintf("%d.%d.%d-%s%s", d.X, d.Y, d.Z, d.CIConfigurationName, count)
	case StreamCandidate:
		if len(d.UpgradeFrom) > 0 {
			return fmt.Sprintf("%d.%d.%d-%s-upgrade-from-%s-%s%s", d.X, d.Y, d.Z, d.Build, d.UpgradeFrom, d.CIConfigurationName, count)
		}
		return fmt.Sprintf("%d.%d.%d-%s-%s%s", d.X, d.Y, d.Z, d.Build, d.CIConfigurationName, count)
	default:
		if len(d.Architecture) > 0 {
			return fmt.Sprintf("%d.%d.%d-%s.%s-%s-%s-%s%s", d.X, d.Y, d.Z, d.Build, d.Stream, d.Architecture, d.Timestamp, d.CIConfigurationName, count)
		}
		return fmt.Sprintf("%d.%d.%d-%s.%s-%s-%s%s", d.X, d.Y, d.Z, d.Build, d.Stream, d.Timestamp, d.CIConfigurationName, count)
	}
}

func ParseReleaseVerificationJobName(name string) (*ReleaseVerificationJobDetails, error) {
	version, err := releasecontroller.SemverParseTolerant(name)
	if err != nil {
		return nil, fmt.Errorf("error: %v", err)
	}
	pr, err := parsePreRelease(version.Pre)
	if err != nil {
		return nil, fmt.Errorf("error: %v", err)
	}
	return &ReleaseVerificationJobDetails{
		X:                 version.Major,
		Y:                 version.Minor,
		Z:                 version.Patch,
		PreReleaseDetails: pr,
	}, nil
}

func parsePreRelease(prerelease []semver.PRVersion) (*PreReleaseDetails, error) {
	details := &PreReleaseDetails{}
	switch len(prerelease) {
	case 1:
		details.Stream = StreamStable
		splitVersion(prerelease[0].VersionStr, details)
	case 2:
		switch prerelease[0].IsNum {
		case true:
			details.Build = fmt.Sprintf("%d", prerelease[0].VersionNum)
		case false:
			switch prerelease[0].VersionStr {
			case "ec", "fc", "rc":
				details.Stream = StreamCandidate
				details.Build = prerelease[0].VersionStr
			default:
				details.Stream = StreamStable
				splitVersion(generateCIConfigurationName(prerelease), details)
				return details, nil
			}
		}
		splitVersion(prerelease[1].VersionStr, details)
	case 3:
		switch prerelease[0].IsNum {
		case true:
			details.Build = fmt.Sprintf("%d", prerelease[0].VersionNum)
		case false:
			switch {
			case strings.HasPrefix(prerelease[0].VersionStr, "upgrade-from-"):
				details.Stream = StreamStable
				splitVersion(generateCIConfigurationName(prerelease), details)
				if automaticUpgradeJob.MatchString(details.CIConfigurationName) {
					matches := automaticUpgradeJob.FindStringSubmatch(details.CIConfigurationName)
					if len(matches) == 5 {
						candidate := ""
						if len(matches[2]) > 0 {
							candidate = fmt.Sprintf("-%s", matches[2])
						}
						details.UpgradeFrom = fmt.Sprintf("%s%s", matches[1], candidate)
						details.CIConfigurationName = matches[3]
						if len(matches[4]) > 0 {
							details.Count = matches[4]
						}
					}
				}
				return details, nil
			default:
				return nil, fmt.Errorf("unsupported prerelease specified: %s", generateCIConfigurationName(prerelease))
			}
		}
		splitVersion(fmt.Sprintf("%s.%s", prerelease[1].VersionStr, prerelease[2].VersionStr), details)
	case 4:
		switch prerelease[0].IsNum {
		case true:
			details.Build = fmt.Sprintf("%d", prerelease[0].VersionNum)
		case false:
			switch prerelease[0].VersionStr {
			case "ec", "fc", "rc":
				details.Stream = StreamCandidate
				splitVersion(generateCIConfigurationName(prerelease), details)
				if strings.HasPrefix(details.CIConfigurationName, "upgrade-from-") {
					if automaticUpgradeJob.MatchString(details.CIConfigurationName) {
						matches := automaticUpgradeJob.FindStringSubmatch(details.CIConfigurationName)
						if len(matches) == 5 {
							candidate := ""
							if len(matches[2]) > 0 {
								candidate = fmt.Sprintf("-%s", matches[2])
							}
							details.UpgradeFrom = fmt.Sprintf("%s%s", matches[1], candidate)
							details.CIConfigurationName = matches[3]
							if len(matches[4]) > 0 {
								details.Count = matches[4]
							}
						}
					}
				}
				return details, nil
			default:
				details.Stream = StreamStable
				details.CIConfigurationName = generateCIConfigurationName(prerelease)
				if strings.HasPrefix(details.CIConfigurationName, "upgrade-from-") {
					if automaticUpgradeJob.MatchString(details.CIConfigurationName) {
						matches := automaticUpgradeJob.FindStringSubmatch(details.CIConfigurationName)
						if len(matches) == 5 {
							candidate := ""
							if len(matches[2]) > 0 {
								candidate = fmt.Sprintf("-%s", matches[2])
							}
							details.UpgradeFrom = fmt.Sprintf("%s%s", matches[1], candidate)
							details.CIConfigurationName = matches[3]
							if len(matches[4]) > 0 {
								details.Count = matches[4]
							}
						}
					}
				}
				return details, nil
			}
		}
		splitVersion(prerelease[1].VersionStr, details)
	case 5:
		switch prerelease[0].IsNum {
		case true:
			details.Build = fmt.Sprintf("%d", prerelease[0].VersionNum)
		case false:
			switch prerelease[0].VersionStr {
			case "ec", "fc", "rc":
				details.Stream = StreamCandidate
				splitVersion(generateCIConfigurationName(prerelease), details)
				if strings.HasPrefix(details.CIConfigurationName, "upgrade-from-") {
					if automaticUpgradeJob.MatchString(details.CIConfigurationName) {
						matches := automaticUpgradeJob.FindStringSubmatch(details.CIConfigurationName)
						if len(matches) == 5 {
							candidate := ""
							if len(matches[2]) > 0 {
								candidate = fmt.Sprintf("-%s", matches[2])
							}
							details.UpgradeFrom = fmt.Sprintf("%s%s", matches[1], candidate)
							details.CIConfigurationName = matches[3]
							if len(matches[4]) > 0 {
								details.Count = matches[4]
							}
						}
					}
				}
				return details, nil
			default:
				details.Stream = StreamStable
				details.CIConfigurationName = generateCIConfigurationName(prerelease)
				return details, nil
			}
		}
		splitVersion(prerelease[1].VersionStr, details)
	default:
		// It should be impossible to reach here, but just in case...
		return nil, fmt.Errorf("unable to parse prerelese: %v", prerelease)
	}
	return details, nil
}

func splitVersion(version string, details *PreReleaseDetails) {
	for key, value := range parse(version) {
		switch key {
		case "stream":
			details.Stream = value
		case "timestamp":
			details.Timestamp = value
		case "job":
			details.CIConfigurationName = value
		case "count":
			details.Count = value
		case "build":
			switch {
			case len(details.Build) > 0:
				details.Build = fmt.Sprintf("%s.%s", details.Build, value)
			default:
				details.Build = value
			}
		case "architecture":
			details.Architecture = value
		}
	}
}

func parse(line string) map[string]string {
	var re *regexp.Regexp
	result := make(map[string]string)

	switch {
	case upgradeFrom.MatchString(line):
		re = upgradeFrom
	case prerelease.MatchString(line):
		re = prerelease
	case candidateRelease.MatchString(line):
		re = candidateRelease
	case stableRelease.MatchString(line):
		re = stableRelease
	default:
		return result
	}

	matches := re.FindStringSubmatch(line)
	for i, name := range re.SubexpNames() {
		if i != 0 && name != "" {
			result[name] = matches[i]
		}
	}
	return result
}

func generateCIConfigurationName(prerelease []semver.PRVersion) string {
	var pieces []string
	for idx := range prerelease {
		switch {
		case prerelease[idx].IsNum:
			pieces = append(pieces, fmt.Sprintf("%d", prerelease[idx].VersionNum))
		default:
			pieces = append(pieces, prerelease[idx].VersionStr)
		}
	}
	return strings.Join(pieces, ".")
}
