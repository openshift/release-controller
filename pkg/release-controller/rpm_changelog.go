package releasecontroller

import (
	"strings"
)

const rpmChangelogSeparatorPrefix = "===RPM_CL_SEP:"
const rpmChangelogSeparatorSuffix = "==="

// ParseRpmChangelogOutput splits the combined output of
// `rpm -q --queryformat '===RPM_CL_SEP:%{NAME}===\n' --changelog pkg1 pkg2 ...`
// into a map of package-name → changelog text.
func ParseRpmChangelogOutput(output string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(output, "\n")

	var currentPkg string
	var currentLines []string

	flush := func() {
		if currentPkg != "" {
			result[currentPkg] = strings.TrimSpace(strings.Join(currentLines, "\n"))
		}
	}

	for _, line := range lines {
		if strings.HasPrefix(line, rpmChangelogSeparatorPrefix) && strings.HasSuffix(line, rpmChangelogSeparatorSuffix) {
			flush()
			currentPkg = line[len(rpmChangelogSeparatorPrefix) : len(line)-len(rpmChangelogSeparatorSuffix)]
			currentLines = nil
			continue
		}
		currentLines = append(currentLines, line)
	}
	flush()

	return result
}

// FilterChangelogBetweenVersions returns only the changelog entries newer than
// oldVersion. RPM changelog entries start with a header line like:
//
//	* Thu Mar 19 2026 Author Name <email> - 2:8.2.2637-22.2
//
// Entries are ordered newest-first. We include entries from the top until we find
// a header whose version (the part after " - ") matches oldVersion, then stop.
// If version parsing fails, we return the full changelog as a fallback.
func FilterChangelogBetweenVersions(fullChangelog, oldVersion string) string {
	if fullChangelog == "" || oldVersion == "" {
		return fullChangelog
	}

	// Normalize oldVersion for matching: strip epoch "0:" prefix, strip dist
	// tag suffixes like ".el10_2" for a more lenient match.
	oldNormalized := normalizeVersion(oldVersion)

	lines := strings.Split(fullChangelog, "\n")
	var result []string
	foundBoundary := false

	for _, line := range lines {
		if isChangelogHeader(line) {
			headerVersion := extractVersionFromHeader(line)
			if headerVersion != "" {
				headerNormalized := normalizeVersion(headerVersion)
				if headerNormalized == oldNormalized || strings.HasPrefix(oldNormalized, headerNormalized) || strings.HasPrefix(headerNormalized, oldNormalized) {
					foundBoundary = true
					break
				}
			}
		}
		result = append(result, line)
	}

	if !foundBoundary {
		return fullChangelog
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}

// isChangelogHeader returns true if the line looks like an RPM changelog header:
// "* <weekday> <month> <day> <year> ..."
func isChangelogHeader(line string) bool {
	return strings.HasPrefix(line, "* ")
}

// extractVersionFromHeader extracts the version from a changelog header line.
// The version appears after " - " at the end of the header.
// Example: "* Thu Mar 19 2026 Author <email> - 2:8.2.2637-22.2" → "2:8.2.2637-22.2"
func extractVersionFromHeader(line string) string {
	idx := strings.LastIndex(line, " - ")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(line[idx+3:])
}

// normalizeVersion strips epoch "0:" and common dist tag suffixes for lenient matching.
func normalizeVersion(v string) string {
	// Strip epoch prefix (e.g. "0:", "2:", "32:")
	if idx := strings.Index(v, ":"); idx > 0 && idx < 4 {
		allDigits := true
		for i := range idx {
			if v[i] < '0' || v[i] > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			v = v[idx+1:]
		}
	}
	return v
}
