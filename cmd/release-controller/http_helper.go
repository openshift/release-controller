package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/template"

	"github.com/blang/semver"

	blackfriday "gopkg.in/russross/blackfriday.v2"

	imagev1 "github.com/openshift/api/image/v1"
)

type ReleasePage struct {
	Streams []ReleaseStream
}

type ReleaseStream struct {
	Release *Release
	Tags    []*imagev1.TagReference

	Upgrades *ReleaseUpgrades
}

type ReleaseStreamTag struct {
	Release  *Release
	Tag      *imagev1.TagReference
	Previous *imagev1.TagReference
	Older    []*imagev1.TagReference
}

type ReleaseUpgrades struct {
	Width int
	Tags  []ReleaseTagUpgrade
}

type ReleaseTagUpgrade struct {
	Internal []UpgradeSummary
	External []UpgradeSummary
	Visual   []ReleaseTagUpgradeVisual
}

type ReleaseTagUpgradeVisual struct {
	Begin, Current, End *UpgradeSummary
}

func phaseCell(tag imagev1.TagReference) string {
	phase := tag.Annotations[releaseAnnotationPhase]
	switch phase {
	case releasePhaseRejected:
		return fmt.Sprintf("<td class=\"%s\" title=\"%s\">%s</td>",
			phaseAlert(tag),
			template.HTMLEscapeString(tag.Annotations[releaseAnnotationMessage]),
			template.HTMLEscapeString(phase),
		)
	}
	return fmt.Sprintf("<td class=\"%s\">", phaseAlert(tag)) + template.HTMLEscapeString(phase) + "</td>"
}

func phaseAlert(tag imagev1.TagReference) string {
	phase := tag.Annotations[releaseAnnotationPhase]
	switch phase {
	case releasePhasePending:
		return ""
	case releasePhaseReady:
		return ""
	case releasePhaseAccepted:
		return "text-success"
	case releasePhaseFailed:
		return "text-danger"
	case releasePhaseRejected:
		return "text-danger"
	default:
		return "text-danger"
	}
}

func styleForUpgrade(upgrade *UpgradeSummary) string {
	switch upgradeSummaryState(upgrade) {
	case releaseVerificationStateFailed:
		return "border-color: #dc3545"
	case releaseVerificationStateSucceeded:
		return "border-color: #28a745"
	default:
		return "border-color: #007bff"
	}
}

func upgradeCells(upgrades *ReleaseUpgrades, index int) string {
	buf := &bytes.Buffer{}
	for _, visual := range upgrades.Tags[index].Visual {
		buf.WriteString(`<td class="upgrade-track">`)
		switch {
		case visual.Current != nil:
			style := styleForUpgrade(visual.Current)
			fmt.Fprintf(buf, `<span title="%s" style="%s" class="upgrade-track-line middle"></span>`, fmt.Sprintf("%d/%d succeeded", visual.Current.Success, visual.Current.Total), style)
		case visual.Begin != nil && visual.End != nil:
			style := styleForUpgrade(visual.Begin)
			fmt.Fprintf(buf, `<span title="%s" style="%s" class="upgrade-track-dot"></span>`, fmt.Sprintf("%d/%d succeeded", visual.Begin.Success, visual.Begin.Total), style)
			fmt.Fprintf(buf, `<span style="%s" class="upgrade-track-line middle"></span>`, style)
		case visual.Begin != nil:
			style := styleForUpgrade(visual.Begin)
			fmt.Fprintf(buf, `<span title="%s" style="%s" class="upgrade-track-dot"></span>`, fmt.Sprintf("%d/%d succeeded", visual.Begin.Success, visual.Begin.Total), style)
			fmt.Fprintf(buf, `<span style="%s" class="upgrade-track-line start"></span>`, style)
		case visual.End != nil:
			style := styleForUpgrade(visual.End)
			fmt.Fprintf(buf, `<span title="%s" style="%s" class="upgrade-track-dot"></span>`, fmt.Sprintf("%d/%d succeeded", visual.End.Success, visual.End.Total), style)
			fmt.Fprintf(buf, `<span style="%s" class="upgrade-track-line end"></span>`, style)
		}
		buf.WriteString(`</td>`)
	}
	remaining := upgrades.Width - len(upgrades.Tags[index].Visual)
	if remaining > 0 {
		buf.WriteString(fmt.Sprintf(`<td colspan="%d"></td>`, remaining))
	}
	buf.WriteString(`<td>`)
	for _, external := range upgrades.Tags[index].External {
		buf.WriteString(fmt.Sprintf(`<span>%s</span> `, external.From))
	}
	buf.WriteString(`</td>`)
	return buf.String()
}

func canLink(tag imagev1.TagReference) bool {
	return tag.Annotations[releaseAnnotationPhase] != releasePhasePending
}

func links(tag imagev1.TagReference, release *Release) string {
	links := tag.Annotations[releaseAnnotationVerify]
	if len(links) == 0 {
		return ""
	}
	var status VerificationStatusMap
	if err := json.Unmarshal([]byte(links), &status); err != nil {
		return "error"
	}
	keys := make([]string, 0, len(release.Config.Verify))
	for k, v := range release.Config.Verify {
		if v.Upgrade {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf := &bytes.Buffer{}
	for _, key := range keys {
		if s, ok := status[key]; ok {
			if len(s.Url) > 0 {
				switch s.State {
				case releaseVerificationStateFailed:
					buf.WriteString(" <a title=\"Failed\" class=\"text-danger\" href=\"")
				case releaseVerificationStateSucceeded:
					buf.WriteString(" <a title=\"Succeeded\" class=\"text-success\" href=\"")
				default:
					buf.WriteString(" <a title=\"Pending\" class=\"\" href=\"")
				}
				buf.WriteString(template.HTMLEscapeString(s.Url))
				buf.WriteString("\">")
				buf.WriteString(template.HTMLEscapeString(key))
				buf.WriteString("</a>")
				continue
			}
			switch s.State {
			case releaseVerificationStateFailed:
				buf.WriteString(" <span title=\"Failed\" class=\"text-danger\">")
			case releaseVerificationStateSucceeded:
				buf.WriteString(" <span title=\"Succeeded\" class=\"text-success\">")
			default:
				buf.WriteString(" <span title=\"Pending\" class=\"\">")
			}
			buf.WriteString(template.HTMLEscapeString(key))
			buf.WriteString("</span>")
			continue
		}
		final := tag.Annotations[releaseAnnotationPhase] == releasePhaseRejected || tag.Annotations[releaseAnnotationPhase] == releasePhaseAccepted
		if !release.Config.Verify[key].Disabled && !final {
			buf.WriteString(" <span title=\"Pending\">")
			buf.WriteString(template.HTMLEscapeString(key))
			buf.WriteString("</span>")
		}
	}
	return buf.String()
}

func extendedLinks(tag imagev1.TagReference, release *Release) string {
	links := tag.Annotations[releaseAnnotationVerify]
	if len(links) == 0 {
		return ""
	}
	var status VerificationStatusMap
	if err := json.Unmarshal([]byte(links), &status); err != nil {
		return "error"
	}
	keys := make([]string, 0, len(release.Config.Verify))
	for k := range release.Config.Verify {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf := &bytes.Buffer{}
	for _, key := range keys {
		if s, ok := status[key]; ok {
			if len(s.Url) > 0 {
				switch s.State {
				case releaseVerificationStateFailed:
					buf.WriteString("<li><a class=\"text-danger\" href=\"")
				case releaseVerificationStateSucceeded:
					buf.WriteString("<li><a class=\"text-success\" href=\"")
				default:
					buf.WriteString("<li><a class=\"\" href=\"")
				}
				buf.WriteString(template.HTMLEscapeString(s.Url))
				buf.WriteString("\">")
				buf.WriteString(template.HTMLEscapeString(key))
				switch s.State {
				case releaseVerificationStateFailed:
					buf.WriteString(" Failed")
				case releaseVerificationStateSucceeded:
					buf.WriteString(" Succeeded")
				default:
					buf.WriteString(" Pending")
				}
				buf.WriteString("</a>")
				if pj := release.Config.Verify[key].ProwJob; pj != nil {
					buf.WriteString(" ")
					buf.WriteString(pj.Name)
				}
				continue
			}
			switch s.State {
			case releaseVerificationStateFailed:
				buf.WriteString("<li><span class=\"text-danger\">")
			case releaseVerificationStateSucceeded:
				buf.WriteString("<li><span class=\"text-success\">")
			default:
				buf.WriteString("<li><span class=\"\">")
			}
			buf.WriteString(template.HTMLEscapeString(key))
			switch s.State {
			case releaseVerificationStateFailed:
				buf.WriteString(" Failed")
			case releaseVerificationStateSucceeded:
				buf.WriteString(" Succeeded")
			default:
				buf.WriteString(" Pending")
			}
			buf.WriteString("</span>")
			if pj := release.Config.Verify[key].ProwJob; pj != nil {
				buf.WriteString(" ")
				buf.WriteString(pj.Name)
			}
			continue
		}
		final := tag.Annotations[releaseAnnotationPhase] == releasePhaseRejected || tag.Annotations[releaseAnnotationPhase] == releasePhaseAccepted
		if !release.Config.Verify[key].Disabled && !final {
			buf.WriteString("<li><span title=\"Pending\">")
			buf.WriteString(template.HTMLEscapeString(key))
			buf.WriteString("</span>")
		}
	}
	return buf.String()
}

func hasPublishTag(config *ReleaseConfig) (string, bool) {
	for _, v := range config.Publish {
		if v.TagRef != nil {
			return v.TagRef.Name, true
		}
	}
	return "", false
}

func findPreviousRelease(tag *imagev1.TagReference, older []*imagev1.TagReference, release *Release) *imagev1.TagReference {
	if len(older) == 0 {
		return nil
	}
	if name, ok := hasPublishTag(release.Config); ok {
		if published := findSpecTag(release.Target.Spec.Tags, name); published != nil && published.From != nil {
			target := published.From.Name
			for _, old := range older {
				if old.Name == target {
					return old
				}
			}
		}
	}
	for _, old := range older {
		if old.Annotations[releaseAnnotationPhase] == releasePhaseAccepted {
			return old
		}
	}
	for _, old := range older {
		return old
	}
	return nil
}

type nopFlusher struct{}

func (_ nopFlusher) Flush() {}

func renderChangelog(w io.Writer, markdown string, pull, tag string, info *ReleaseStreamTag) {
	result := blackfriday.Run([]byte(markdown))

	// minor changelog styling tweaks
	fmt.Fprintf(w, `
<style>
	h1 { font-size: 2rem; margin-bottom: 1rem }
	h2 { font-size: 1.5rem; margin-top: 2rem; margin-bottom: 1rem  }
	h3 { font-size: 1.35rem; margin-top: 2rem; margin-bottom: 1rem  }
	h4 { font-size: 1.2rem; margin-top: 2rem; margin-bottom: 1rem  }
	h3 a { text-transform: uppercase; font-size: 1rem; }
</style>
`)

	fmt.Fprintf(w, "<p><a href=\"/\">Back to index</a></p>\n")
	w.Write(result)
}

func calculateReleaseUpgrades(release *Release, tags []*imagev1.TagReference, graph *UpgradeGraph) *ReleaseUpgrades {
	tagNames := make([]string, 0, len(tags))
	internalTags := make(map[string]int)
	for i, tag := range tags {
		internalTags[tag.Name] = i
		tagNames = append(tagNames, tag.Name)
	}
	tagUpgrades := make([]ReleaseTagUpgrade, 0, len(tags))
	maxWidth := 0

	// calculate inbound and output edges to each row, materialize them as a
	// tabular form with indicators whether a row is starting, continuing, or
	// ending
	var visual []ReleaseTagUpgradeVisual
	summaries := graph.SummarizeUpgradesTo(tagNames...)
	for _, name := range tagNames {
		var internal, external []UpgradeSummary
		internal, summaries = takeUpgradesTo(summaries, name)
		internal, external = takeUpgradesFromNames(internal, internalTags)
		sort.Slice(internal, func(i, j int) bool {
			return internalTags[internal[i].From] < internalTags[internal[j].From]
		})
		sort.Sort(newNewestSemVerFromSummaries(external))
		tagUpgrade := ReleaseTagUpgrade{}
		if len(internal) > 0 {
			tagUpgrade.Internal = internal
		}
		if len(external) > 0 {
			tagUpgrade.External = external
		}

		// mark the end of any current row
		for i, row := range visual {
			current := row.Current
			if current == nil || current.From != name {
				continue
			}
			visual[i].End = current
			visual[i].Current = nil
		}

		// try to place each internal in the first available position on the row
	Internal:
		for i := range internal {
			for j, row := range visual {
				if row.Begin != nil || row.Current != nil {
					continue
				}
				// don't join columns of different status
				if last := row.End; last != nil {
					if upgradeSummaryState(last) != upgradeSummaryState(&internal[i]) {
						continue
					}
				}
				visual[j].Begin = &internal[i]
				continue Internal
			}
			visual = append(visual, ReleaseTagUpgradeVisual{Begin: &internal[i]})
		}
		if len(visual) > maxWidth {
			maxWidth = len(visual)
		}

		// copy the row
		if len(visual) > 0 {
			tagUpgrade.Visual = make([]ReleaseTagUpgradeVisual, len(visual))
			copy(tagUpgrade.Visual, visual)
		}
		tagUpgrades = append(tagUpgrades, tagUpgrade)

		// set up the row for the next iteration
		for i := range visual {
			b := visual[i].Begin
			if b == nil {
				b = visual[i].Current
			}
			visual[i] = ReleaseTagUpgradeVisual{Current: b}
		}
		for i := len(visual) - 1; i >= 0; i-- {
			row := visual[i]
			if row.Current == nil {
				visual = visual[:i]
				continue
			}
			break
		}
	}

	return &ReleaseUpgrades{
		Width: maxWidth,
		Tags:  tagUpgrades,
	}
}

func upgradeSummaryState(summary *UpgradeSummary) string {
	if summary.Success > 0 {
		return releaseVerificationStateSucceeded
	}
	if summary.Failure > 0 {
		return releaseVerificationStateFailed
	}
	return releaseVerificationStatePending
}

// takeUpgradesTo returns all leading summaries with To equal to tag, and the rest of the
// slice.
func takeUpgradesTo(summaries []UpgradeSummary, tag string) ([]UpgradeSummary, []UpgradeSummary) {
	for i, summary := range summaries {
		if summary.To == tag {
			continue
		}
		return summaries[:i], summaries[i:]
	}
	return summaries, nil
}

// takeUpgradesFromNames splits the provided summaries slice into two slices - those with Froms
// in names and those without.
func takeUpgradesFromNames(summaries []UpgradeSummary, names map[string]int) (withNames []UpgradeSummary, withoutNames []UpgradeSummary) {
	for i := range summaries {
		if _, ok := names[summaries[i].From]; ok {
			continue
		}
		left := make([]UpgradeSummary, 0, len(summaries)-i)
		var right []UpgradeSummary
		for _, summary := range summaries[i:] {
			if _, ok := names[summaries[i].From]; ok {
				left = append(left, summary)
			} else {
				right = append(right, summary)
			}
		}
		return left, right
	}
	return summaries, nil
}

type newestSemVerFromSummaries struct {
	versions  []semver.Version
	summaries []UpgradeSummary
}

func newNewestSemVerFromSummaries(summaries []UpgradeSummary) newestSemVerFromSummaries {
	versions := make([]semver.Version, len(summaries))
	for i, summary := range summaries {
		if v, err := semver.Parse(summary.From); err != nil {
			versions[i] = v
		}
	}
	return newestSemVerFromSummaries{
		versions:  versions,
		summaries: summaries,
	}
}

func (s newestSemVerFromSummaries) Less(i, j int) bool {
	c := s.versions[i].Compare(s.versions[j])
	if c > 0 {
		return true
	}
	if c < 0 {
		return false
	}
	return s.summaries[i].From > s.summaries[j].From
}
func (s newestSemVerFromSummaries) Swap(i, j int) {
	s.summaries[i], s.summaries[j] = s.summaries[j], s.summaries[i]
	s.versions[i], s.versions[j] = s.versions[j], s.versions[i]
}
func (s newestSemVerFromSummaries) Len() int { return len(s.summaries) }

type newestSemVerToSummaries struct {
	versions  []semver.Version
	summaries []UpgradeSummary
}

func newNewestSemVerToSummaries(summaries []UpgradeSummary) newestSemVerToSummaries {
	versions := make([]semver.Version, len(summaries))
	for i, summary := range summaries {
		if v, err := semver.Parse(summary.To); err != nil {
			versions[i] = v
		}
	}
	return newestSemVerToSummaries{
		versions:  versions,
		summaries: summaries,
	}
}

func (s newestSemVerToSummaries) Less(i, j int) bool {
	c := s.versions[i].Compare(s.versions[j])
	if c > 0 {
		return true
	}
	if c < 0 {
		return false
	}
	return s.summaries[i].To > s.summaries[j].To
}
func (s newestSemVerToSummaries) Swap(i, j int) {
	s.summaries[i], s.summaries[j] = s.summaries[j], s.summaries[i]
	s.versions[i], s.versions[j] = s.versions[j], s.versions[i]
}
func (s newestSemVerToSummaries) Len() int { return len(s.summaries) }
