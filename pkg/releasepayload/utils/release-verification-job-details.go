package utils

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/blang/semver"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
)

const (
	StreamStable    = "Stable"
	StreamCandidate = "Candidate"
)

var (
	candidates  = regexp.MustCompile(`[efr]c.\d+`)
	expressions = []*regexp.Regexp{
		regexp.MustCompile(`^(?P<prerelease>[\d-]+)\.(?P<stream>okd-scos|okd|\w+)-(?P<architecture>\w+)?-?(?P<timestamp>\d{4}-\d{2}-\d{2}-\d{6})-(?P<job>[\w-\\.]+?)(?:-(?P<count>\d+))?$`),
		regexp.MustCompile(`^(?P<prerelease>[efr]c\.\d+)?-?upgrade-from-(?P<upgrade_from>[\d\\.]+(?:-[efr]c.\d+)?)-?(?P<job>\w+)-?(?P<count>\d+)?$`),
		regexp.MustCompile(`^(?P<stream>okd-scos|okd)\.(?P<prerelease>(?:[erf]c\.\d+)+|\d+)-(?P<job>[\w-\\.]+?)(?:-(?P<count>\d+))?$`),
		regexp.MustCompile(`^(?P<prerelease>(?:[erf]c|okd-scos|okd)\.\d+)-(?P<job>[\w-\\.]+?)(?:-(?P<count>\d+))?$`),
		regexp.MustCompile(`^(?P<job>[\w-\\.]+?)(?:-(?P<count>\d+))?$`),
	}
)

type PreReleaseDetails struct {
	PreRelease          string
	Stream              string
	Timestamp           string
	CIConfigurationName string
	Count               string
	UpgradeFrom         string
	Architecture        string
}

type ReleaseVerificationJobDetails struct {
	Name    string
	Version semver.Version
	*PreReleaseDetails
}

func NewReleaseVerificationJobDetails(name string) (*ReleaseVerificationJobDetails, error) {
	version, err := releasecontroller.SemverParseTolerant(name)
	if err != nil {
		return nil, fmt.Errorf("unable to parse ReleaseVerificationJobName: %v", err)
	}
	details := NewPreReleaseDetails(version.Pre)
	if err = validateDetails(details); err != nil {
		return nil, fmt.Errorf("error validating PreReleaseDetails for %q: %v", name, err)
	}
	return &ReleaseVerificationJobDetails{
		Name:              name,
		Version:           version,
		PreReleaseDetails: details,
	}, nil
}

func NewPreReleaseDetails(prerelease []semver.PRVersion) *PreReleaseDetails {
	return parsePreReleaseString(PreReleaseString(prerelease))
}

func parsePreReleaseString(prerelease string) *PreReleaseDetails {
	details := &PreReleaseDetails{}
	results := parse(prerelease)
	for key, value := range results {
		switch key {
		case "stream":
			details.Stream = value
		case "timestamp":
			details.Timestamp = value
		case "job":
			details.CIConfigurationName = value
		case "count":
			details.Count = value
		case "prerelease":
			details.PreRelease = value
		case "architecture":
			details.Architecture = value
		case "upgrade_from":
			details.UpgradeFrom = value
		}
	}
	// Handle OCP Streams...
	if !strings.Contains(details.Stream, "okd") {
		if len(details.PreRelease) == 0 {
			details.Stream = StreamStable
		}
		if candidates.MatchString(details.PreRelease) {
			details.Stream = StreamCandidate
		}
	}
	return details
}

func PreReleaseString(pre []semver.PRVersion) string {
	var pieces []string
	for _, item := range pre {
		pieces = append(pieces, item.String())
	}
	return strings.Join(pieces, ".")
}

func parse(prerelease string) map[string]string {
	result := make(map[string]string)
	for _, expression := range expressions {
		if expression.MatchString(prerelease) {
			matches := expression.FindStringSubmatch(prerelease)
			for i, name := range expression.SubexpNames() {
				if i != 0 && name != "" {
					result[name] = matches[i]
				}
			}
			break
		}
	}
	return result
}

func validateDetails(details *PreReleaseDetails) error {
	// We expect everything to have a specified Stream...
	if len(details.Stream) == 0 {
		return fmt.Errorf("stream is required")
	}
	return nil
}
