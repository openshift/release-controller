package releasecontroller

import (
	"fmt"

	"github.com/blang/semver"
	imagev1 "github.com/openshift/api/image/v1"
)

type SemanticVersions []SemanticVersion

func NewSemanticVersions(tags []*imagev1.TagReference) SemanticVersions {
	v := make(SemanticVersions, 0, len(tags))
	for _, tag := range tags {
		if version, err := semver.Parse(tag.Name); err == nil {
			v = append(v, SemanticVersion{Version: &version, Tag: tag})
		} else {
			v = append(v, SemanticVersion{Tag: tag})
		}
	}
	return v
}

func (v SemanticVersions) Tags() []*imagev1.TagReference {
	tags := make([]*imagev1.TagReference, 0, len(v))
	for _, version := range v {
		tags = append(tags, version.Tag)
	}
	return tags
}

func (v SemanticVersions) Less(i, j int) bool {
	a, b := v[i].Version, v[j].Version
	if a == nil && b != nil {
		return false
	}
	if a != nil && b == nil {
		return true
	}
	if a != nil {
		c := a.Compare(*b)
		if c > 0 {
			return true
		}
		if c < 0 {
			return false
		}
	}
	return v[i].Tag.Name > v[j].Tag.Name
}

func (v SemanticVersions) Len() int      { return len(v) }
func (v SemanticVersions) Swap(i, j int) { v[i], v[j] = v[j], v[i] }

type SemanticVersion struct {
	Version *semver.Version
	Tag     *imagev1.TagReference
}

type NewestSemVerFromSummaries struct {
	versions  []semver.Version
	summaries []UpgradeHistory
}

func NewNewestSemVerFromSummaries(summaries []UpgradeHistory) NewestSemVerFromSummaries {
	versions := make([]semver.Version, len(summaries))
	for i, summary := range summaries {
		if v, err := semver.Parse(summary.From); err == nil {
			versions[i] = v
		}
	}
	return NewestSemVerFromSummaries{
		versions:  versions,
		summaries: summaries,
	}
}

func (s NewestSemVerFromSummaries) Less(i, j int) bool {
	c := s.versions[i].Compare(s.versions[j])
	if c > 0 {
		return true
	}
	if c < 0 {
		return false
	}
	if s.summaries[i].From > s.summaries[j].From {
		return true
	}
	if s.summaries[i].From < s.summaries[j].From {
		return false
	}
	return s.summaries[i].Total >= s.summaries[j].Total
}
func (s NewestSemVerFromSummaries) Swap(i, j int) {
	s.summaries[i], s.summaries[j] = s.summaries[j], s.summaries[i]
	s.versions[i], s.versions[j] = s.versions[j], s.versions[i]
}
func (s NewestSemVerFromSummaries) Len() int { return len(s.summaries) }

type NewestSemVerToSummaries struct {
	versions  []semver.Version
	summaries []UpgradeHistory
}

func NewNewestSemVerToSummaries(summaries []UpgradeHistory) NewestSemVerToSummaries {
	versions := make([]semver.Version, len(summaries))
	for i, summary := range summaries {
		if v, err := semver.Parse(summary.To); err == nil {
			versions[i] = v
		}
	}
	return NewestSemVerToSummaries{
		versions:  versions,
		summaries: summaries,
	}
}

func (s NewestSemVerToSummaries) Less(i, j int) bool {
	c := s.versions[i].Compare(s.versions[j])
	if c > 0 {
		return true
	}
	if c < 0 {
		return false
	}
	return s.summaries[i].To > s.summaries[j].To
}
func (s NewestSemVerToSummaries) Swap(i, j int) {
	s.summaries[i], s.summaries[j] = s.summaries[j], s.summaries[i]
	s.versions[i], s.versions[j] = s.versions[j], s.versions[i]
}
func (s NewestSemVerToSummaries) Len() int { return len(s.summaries) }

func IncrementSemanticVersion(v semver.Version) (semver.Version, error) {
	if len(v.Pre) == 0 {
		copied := v
		copied.Patch++
		return copied, nil
	}
	for i := len(v.Pre) - 1; i >= 0; i-- {
		if v.Pre[i].IsNum {
			copied := v
			copied.Pre[i].VersionNum++
			return copied, nil
		}
	}
	return v, fmt.Errorf("the version %s cannot be incremented, no numeric prerelease portions", v.String())
}

// semverParseTolerant works around https://github.com/blang/semver/issues/55 until
// it is resolved.
func SemverParseTolerant(v string) (semver.Version, error) {
	ver, err := semver.ParseTolerant(v)
	if err == nil {
		return ver, nil
	}
	ver, strictErr := semver.Parse(v)
	if strictErr == nil {
		return ver, nil
	}
	return semver.Version{}, err
}

func SemverToMajorMinor(sr semver.Version) string {
	return fmt.Sprintf("%d.%d", sr.Major, sr.Minor)
}
