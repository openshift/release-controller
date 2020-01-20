package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/blang/semver"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imagev1 "github.com/openshift/api/image/v1"
)

type ReleasePage struct {
	BaseURL string
	Streams []ReleaseStream
}

type ReleaseStream struct {
	Release *Release
	Tags    []*imagev1.TagReference

	// Delayed is set if there is a pending release
	Delayed *ReleaseDelay

	Upgrades *ReleaseUpgrades
	Checks   []ReleaseCheckResult

	// Failing is set if none of the most recent 5 payloads were accepted
	Failing bool
}

type ReleaseDelay struct {
	Message string
}

type ReleaseCheckResult struct {
	Name     string
	Errors   []string
	Warnings []string
}

type ReleaseStreamTag struct {
	Release *Release
	Tag     *imagev1.TagReference

	PreviousRelease *Release
	Previous        *imagev1.TagReference

	Older  []*imagev1.TagReference
	Stable *StableReferences
}

type StableReferences struct {
	Releases StableReleases
}

type StableReleases []StableRelease

func (v StableReleases) Less(i, j int) bool {
	c := v[i].Version.Compare(v[j].Version)
	if c > 0 {
		return true
	}
	return false
}

func (v StableReleases) Len() int      { return len(v) }
func (v StableReleases) Swap(i, j int) { v[i], v[j] = v[j], v[i] }

type StableRelease struct {
	Release  *Release
	Version  semver.Version
	Versions SemanticVersions
}

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

type ReleaseUpgrades struct {
	Width int
	Tags  []ReleaseTagUpgrade
}

type ReleaseTagUpgrade struct {
	Internal []UpgradeHistory
	External []UpgradeHistory
	Visual   []ReleaseTagUpgradeVisual
}

type ReleaseTagUpgradeVisual struct {
	Begin, Current, End *UpgradeHistory
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

func styleForUpgrade(upgrade *UpgradeHistory) string {
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
	u := url.URL{}
	for _, visual := range upgrades.Tags[index].Visual {
		buf.WriteString(`<td class="upgrade-track">`)
		switch {
		case visual.Current != nil:
			style := styleForUpgrade(visual.Current)
			fmt.Fprintf(buf, `<span title="%s" style="%s" class="upgrade-track-line middle"></span>`, fmt.Sprintf("%d/%d succeeded", visual.Current.Success, visual.Current.Total), style)
		case visual.Begin != nil && visual.End != nil:
			style := styleForUpgrade(visual.Begin)
			u.Path = visual.Begin.To
			u.RawQuery = url.Values{"from": []string{visual.Begin.From}}.Encode()
			fmt.Fprintf(buf, `<a href="/releasetag/%s" title="%s" style="%s" class="upgrade-track-dot"></a>`, template.HTMLEscapeString(u.String()), fmt.Sprintf("%d/%d succeeded", visual.Begin.Success, visual.Begin.Total), style)
			fmt.Fprintf(buf, `<span style="%s" class="upgrade-track-line middle"></span>`, style)
		case visual.Begin != nil:
			style := styleForUpgrade(visual.Begin)
			u.Path = visual.Begin.To
			u.RawQuery = url.Values{"from": []string{visual.Begin.From}}.Encode()
			fmt.Fprintf(buf, `<a href="/releasetag/%s" title="%s" style="%s" class="upgrade-track-dot"></a>`, template.HTMLEscapeString(u.String()), fmt.Sprintf("%d/%d succeeded", visual.Begin.Success, visual.Begin.Total), style)
			fmt.Fprintf(buf, `<span style="%s" class="upgrade-track-line start"></span>`, style)
		case visual.End != nil:
			style := styleForUpgrade(visual.End)
			u.Path = visual.End.To
			u.RawQuery = url.Values{"from": []string{visual.End.From}}.Encode()
			fmt.Fprintf(buf, `<a href="/releasetag/%s" title="%s" style="%s" class="upgrade-track-dot"></a>`, template.HTMLEscapeString(u.String()), fmt.Sprintf("%d/%d succeeded", visual.End.Success, visual.End.Total), style)
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
		switch {
		case external.Success > 0:
			buf.WriteString(fmt.Sprintf(`<span class="text-success">%s</span> `, external.From))
		case external.Failure > 0:
			buf.WriteString(fmt.Sprintf(`<span class="text-danger">%s</span> `, external.From))
		default:
			buf.WriteString(fmt.Sprintf(`<span>%s</span> `, external.From))
		}
	}
	buf.WriteString(`</td>`)
	return buf.String()
}

func upgradeJobs(upgrades *ReleaseUpgrades, index int, tagCreationTimestampString string) string {
	buf := &bytes.Buffer{}
	buf.WriteString(`<td>`)
	// filter out upgrade job results more than 24 hours old because we don't want
	// buildcops looking at them.
	cutoff := metav1.NewTime(time.Now().Add(-24 * time.Hour))

	t, e := time.Parse(time.RFC3339, tagCreationTimestampString)
	if e != nil {
		// if we can't parse the date, assume it's current/new.
		t = time.Now()
	}
	tagCreationTimestamp := metav1.NewTime(t)
	if (&tagCreationTimestamp).Before(&cutoff) {
		buf.WriteString(`<em>--</em></td><td><em>--</em></td><td><em>--</em></td>`)
		return buf.String()
	}

	successCount := 0
	otherCount := 0
	for _, visual := range upgrades.Tags[index].Visual {
		if visual.Begin == nil {
			continue
		}
		for url, result := range visual.Begin.History {
			switch result.State {
			case releaseVerificationStateSucceeded:
				successCount++
			case releaseVerificationStateFailed:
				buf.WriteString(fmt.Sprintf(`<a class="text-danger" href=%s>%s</a><br>`, url, visual.Begin.From))
			default:
				otherCount++
			}
		}
	}

	for _, external := range upgrades.Tags[index].External {
		for url, result := range external.History {
			switch result.State {
			case releaseVerificationStateSucceeded:
				successCount++
			case releaseVerificationStateFailed:
				buf.WriteString(fmt.Sprintf(`<a class="text-danger" href=%s>%s</a><br>`, url, external.From))
			default:
				otherCount++
			}
		}
	}
	buf.WriteString(`</td>`)

	buf2 := &bytes.Buffer{}
	buf2.WriteString(fmt.Sprintf(`<td>%d</td><td>%d</td>`, successCount, otherCount))
	buf2.WriteString(buf.String())

	return buf2.String()
}
func canLink(tag imagev1.TagReference) bool {
	switch tag.Annotations[releaseAnnotationPhase] {
	case releasePhasePending:
		return false
	default:
		return true
	}
}

func tableLink(config *ReleaseConfig, tag imagev1.TagReference) string {
	if canLink(tag) {
		if value, ok := tag.Annotations[releaseAnnotationKeep]; ok {
			return fmt.Sprintf(`<td class="text-monospace"><a title="%s" class="%s" href="/releasestream/%s/release/%s">%s <span>*</span></a></td>`, template.HTMLEscapeString(value), phaseAlert(tag), template.HTMLEscapeString(config.Name), template.HTMLEscapeString(tag.Name), template.HTMLEscapeString(tag.Name))
		}
		return fmt.Sprintf(`<td class="text-monospace"><a class="%s" href="/releasestream/%s/release/%s">%s</a></td>`, phaseAlert(tag), template.HTMLEscapeString(config.Name), template.HTMLEscapeString(tag.Name), template.HTMLEscapeString(tag.Name))
	}
	return fmt.Sprintf(`<td class="text-monospace %s">%s</td>`, phaseAlert(tag), template.HTMLEscapeString(tag.Name))
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
	pending, failing := make([]string, 0, len(release.Config.Verify)), make([]string, 0, len(release.Config.Verify))
	for k, v := range release.Config.Verify {
		if v.Upgrade {
			continue
		}
		s, ok := status[k]
		if !ok {
			continue
		}
		switch s.State {
		case releaseVerificationStateSucceeded:
			continue
		case releaseVerificationStateFailed:
			failing = append(failing, k)
		default:
			pending = append(pending, k)
		}
	}
	var keys []string
	pendingCount, failingCount := len(pending), len(failing)
	if pendingCount <= 3 {
		sort.Strings(pending)
		keys = pending
	} else {
		keys = pending[:0]
	}
	if failingCount <= 3 {
		sort.Strings(failing)
		keys = append(keys, failing...)
	}

	buf := &bytes.Buffer{}
	for _, key := range keys {
		if s, ok := status[key]; ok {
			if len(s.URL) > 0 {
				switch s.State {
				case releaseVerificationStateSucceeded:
					continue
				case releaseVerificationStateFailed:
					buf.WriteString(" <a title=\"Failed\" class=\"text-danger\" href=\"")
				default:
					buf.WriteString(" <a title=\"Pending\" class=\"\" href=\"")
				}
				buf.WriteString(template.HTMLEscapeString(s.URL))
				buf.WriteString("\">")
				buf.WriteString(template.HTMLEscapeString(key))
				buf.WriteString("</a>")
				continue
			}
			switch s.State {
			case releaseVerificationStateSucceeded:
				continue
			case releaseVerificationStateFailed:
				buf.WriteString(" <span title=\"Failed\" class=\"text-danger\">")
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
	if pendingCount > 3 {
		buf.WriteString(" <a title=\"See details page for more\" class=\"\" href=\"")
		buf.WriteString(template.HTMLEscapeString(fmt.Sprintf("/releasestream/%s/release/%s", url.PathEscape(release.Config.Name), url.PathEscape(tag.Name))))
		buf.WriteString("\">")
		buf.WriteString(template.HTMLEscapeString(fmt.Sprintf("%d pending", len(failing))))
		buf.WriteString("</a>")
	}
	if failingCount > 3 {
		buf.WriteString(" <a title=\"See details page for more\" class=\"text-danger\" href=\"")
		buf.WriteString(template.HTMLEscapeString(fmt.Sprintf("/releasestream/%s/release/%s", url.PathEscape(release.Config.Name), url.PathEscape(tag.Name))))
		buf.WriteString("\">")
		buf.WriteString(template.HTMLEscapeString(fmt.Sprintf("%d failed", len(failing))))
		buf.WriteString("</a>")
	}

	return buf.String()
}

func renderVerifyLinks(w io.Writer, tag imagev1.TagReference, release *Release) {
	links := tag.Annotations[releaseAnnotationVerify]
	if len(links) == 0 {
		fmt.Fprintf(w, `<p><em>No tests for this release</em>`)
		return
	}
	var status VerificationStatusMap
	if err := json.Unmarshal([]byte(links), &status); err != nil {
		fmt.Fprintf(w, `<p><em class="text-danger">Unable to load test info</em>`)
		return
	}

	keys := make([]string, 0, len(release.Config.Verify))
	for k := range release.Config.Verify {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf := &bytes.Buffer{}
	for _, key := range keys {
		if s, ok := status[key]; ok {
			if len(s.URL) > 0 {
				switch s.State {
				case releaseVerificationStateFailed:
					buf.WriteString("<li><a class=\"text-danger\" href=\"")
				case releaseVerificationStateSucceeded:
					buf.WriteString("<li><a class=\"text-success\" href=\"")
				default:
					buf.WriteString("<li><a class=\"\" href=\"")
				}
				buf.WriteString(template.HTMLEscapeString(s.URL))
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
				if s.Retries > 0 {
					buf.WriteString(fmt.Sprintf(" <span class=\"text-warning\">(%d retries)</span>", s.Retries))
				}
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

	if out := buf.String(); len(out) > 0 {
		fmt.Fprintf(w, `<p>Tests:</p><ul>%s</ul>`, out)
	} else {
		fmt.Fprintf(w, `<p><em>No tests for this release</em>`)
	}
}

func renderCandidateLinks(w io.Writer, tag imagev1.TagReference, release *Release) {
	if _, ok := tag.Annotations[releaseAnnotationKeep]; !ok {
		return
	}
	links := tag.Annotations[releaseAnnotationCandidateTests]
	if len(links) == 0 {
		fmt.Fprintf(w, `<p><em>No candidate tests for this release</em>`)
		return
	}
	var status VerificationStatusList
	if err := json.Unmarshal([]byte(links), &status); err != nil {
		fmt.Fprintf(w, `<p><em class="text-danger">Unable to load candidate test info</em>`)
		return
	}
	keys := make([]string, 0, len(release.Config.CandidateTests))
	for k := range release.Config.CandidateTests {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf := &bytes.Buffer{}
	for _, key := range keys {
		if s, ok := status[key]; ok {
			switch release.Config.CandidateTests[key].state(s.Status...) {
			case releaseVerificationStateFailed:
				buf.WriteString("<li><span class=\"text-danger\">" + template.HTMLEscapeString(key) + " -")
			case releaseVerificationStateSucceeded:
				buf.WriteString("<li><span class=\"text-success\">" + template.HTMLEscapeString(key) + " -")
			default:
				buf.WriteString("<li><span class=\"\">" + template.HTMLEscapeString(key) + " -")
			}
			if len(s.Status) > 2 {
				for _, status := range s.Status {
					if len(status.URL) > 0 {
						buf.WriteString(" <a href=\"" + template.HTMLEscapeString(status.URL) + "\" ")
					} else {
						buf.WriteString(" <span ")
					}
					switch status.State {
					case releaseVerificationStateFailed:
						buf.WriteString("class=\"text-danger\">F</a>")
					case releaseVerificationStateSucceeded:
						buf.WriteString("class=\"text-success\">S</a>")
					case releaseVerificationStatePending:
						buf.WriteString("class=\"text-warning\">P</a>")
					}
					if len(status.URL) > 0 {
						buf.WriteString("</a>")
					} else {
						buf.WriteString("</span>")
					}
				}
			} else {
				for _, status := range s.Status {
					if len(status.URL) > 0 {
						buf.WriteString(" <a href=\"" + template.HTMLEscapeString(status.URL) + "\" ")
					} else {
						buf.WriteString(" <span ")
					}
					switch status.State {
					case releaseVerificationStateFailed:
						buf.WriteString("class=\"text-danger\">Failed</a>")
					case releaseVerificationStateSucceeded:
						buf.WriteString("class=\"text-success\">Success</a>")
					case releaseVerificationStatePending:
						buf.WriteString("class=\"text-warning\">Pending</a>")
					}
					if len(status.URL) > 0 {
						buf.WriteString("</a>")
					} else {
						buf.WriteString("</span>")
					}
				}
			}
			if pj := release.Config.CandidateTests[key].ProwJob; pj != nil {
				buf.WriteString(" ")
				buf.WriteString(pj.Name)
			}
		} else {
			// Multi-target tests: upgrade-rally. Multiple runs per target, display result for each target.
			testKeys := make([]string, 0)
			for test := range status {
				if !strings.HasPrefix(test, key+"-") {
					continue
				}
				testKeys = append(testKeys, test)
			}
			if len(testKeys) == 0 {
				continue
			}
			sort.Strings(testKeys)
			buf.WriteString("<li><span>" + template.HTMLEscapeString(key) + " -")
			if len(testKeys) > 2 {
				for _, test := range testKeys {
					target := template.HTMLEscapeString(test[len(key)+1:])
					switch state := release.Config.CandidateTests[key].state(status[test].Status...); {
					case state == releaseVerificationStateFailed:
						buf.WriteString(" <a class=\"text-danger\" href=\"#" + target + "\" title=\"" + target + "\">F</a>")
					case state == releaseVerificationStateSucceeded:
						buf.WriteString(" <a class=\"text-success\" href=\"#" + target + "\" title=\"" + target + "\">S</a>")
					case state == releaseVerificationStatePending:
						buf.WriteString(" <a class=\"text-warning\" href=\"#" + target + "\" title=\"" + target + "\">P</a>")
					}
				}
			} else {
				for _, test := range testKeys {
					target := template.HTMLEscapeString(test[len(key)+1:])
					switch state := release.Config.CandidateTests[key].state(status[test].Status...); {
					case state == releaseVerificationStateFailed:
						buf.WriteString(" <a class=\"text-danger\" href=\"#" + target + "\" title=\"" + target + "\">Failed</a>")
					case state == releaseVerificationStateSucceeded:
						buf.WriteString(" <a class=\"text-success\" href=\"#" + target + "\" title=\"" + target + "\">Success</a>")
					case state == releaseVerificationStatePending:
						buf.WriteString(" <a class=\"text-warning\" href=\"#" + target + "\" title=\"" + target + "\">Pending</a>")
					}
				}
			}
			buf.WriteString("</span>")
			if pj := release.Config.CandidateTests[key].ProwJob; pj != nil {
				buf.WriteString(" ")
				buf.WriteString(pj.Name)
			}
		}
	}
	if out := buf.String(); len(out) > 0 {
		fmt.Fprintf(w, `<p>Candidate Tests:</p><ul>%s</ul>`, out)
	} else {
		fmt.Fprintf(w, `<p><em>No candidate tests for this release</em>`)
	}
}

func renderAlerts(release ReleaseStream) string {
	var msgs []string
	for _, check := range release.Checks {
		if len(check.Errors) > 0 {
			sort.Strings(check.Errors)
			msgs = append(msgs, fmt.Sprintf("<div class=\"alert alert-danger\">%s failures:\n<ul><li>%s</ul></div>", check.Name, strings.Join(check.Errors, "<li>")))
		}
		if len(check.Warnings) > 0 {
			sort.Strings(check.Warnings)
			msgs = append(msgs, fmt.Sprintf("<div class=\"alert alert-warning\">%s warnings:\n<ul><li>%s</ul></div>", check.Name, strings.Join(check.Warnings, "<li>")))
		}
	}
	sort.Strings(msgs)
	return strings.Join(msgs, "\n")
}

func releaseJoin(streams []ReleaseStream) string {
	releases := []string{}
	for _, s := range streams {
		releases = append(releases, fmt.Sprintf("<a href=\"#%s\">%s</a>", s.Release.Config.Name, s.Release.Config.Name))
	}
	return strings.Join(releases, " | ")
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

func upgradeSummaryToString(history []UpgradeHistory) string {
	var out []string
	for _, h := range history {
		out = append(out, fmt.Sprintf("%s->%s", h.From, h.To))
	}
	return strings.Join(out, ",")
}

func calculateReleaseUpgrades(release *Release, tags []*imagev1.TagReference, graph *UpgradeGraph, fullHistory bool) *ReleaseUpgrades {
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
	var summaries []UpgradeHistory
	if fullHistory {
		summaries = graph.UpgradesTo(tagNames...)
	} else {
		summaries = graph.SummarizeUpgradesTo(tagNames...)
	}
	for _, name := range tagNames {
		var internal, external []UpgradeHistory
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
			// ensure that any older tags owned by this stream that may have been pruned
			// are not displayed
			tagUpgrade.External = filterWithPrefix(external, release.Config.Name+"-")
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

func upgradeSummaryState(summary *UpgradeHistory) string {
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
func takeUpgradesTo(summaries []UpgradeHistory, tag string) ([]UpgradeHistory, []UpgradeHistory) {
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
func takeUpgradesFromNames(summaries []UpgradeHistory, names map[string]int) (withNames []UpgradeHistory, withoutNames []UpgradeHistory) {
	for i := range summaries {
		if _, ok := names[summaries[i].From]; ok {
			continue
		}
		left := make([]UpgradeHistory, i, len(summaries))
		copy(left, summaries[:i])
		var right []UpgradeHistory
		for j, summary := range summaries[i:] {
			if _, ok := names[summaries[i+j].From]; ok {
				left = append(left, summary)
			} else {
				right = append(right, summary)
			}
		}
		return left, right
	}
	return summaries, nil
}

// filterWithPrefix removes any summary from summaries that has a From that starts with
// prefix.
func filterWithPrefix(summaries []UpgradeHistory, prefix string) []UpgradeHistory {
	if len(prefix) == 0 {
		return summaries
	}
	for i := range summaries {
		if !strings.HasPrefix(summaries[i].From, prefix) {
			continue
		}
		valid := make([]UpgradeHistory, 0, len(summaries)-i)
		for _, summary := range summaries {
			if !strings.HasPrefix(summary.From, prefix) {
				valid = append(valid, summary)
			}
		}
		return valid
	}
	return summaries
}

type preferredReleases []ReleaseStream

func (r preferredReleases) Less(i, j int) bool {
	a, b := r[i], r[j]
	if !a.Release.Config.Hide && b.Release.Config.Hide {
		return true
	}
	if a.Release.Config.Hide && !b.Release.Config.Hide {
		return false
	}
	aStable, bStable := a.Release.Config.As == releaseConfigModeStable, b.Release.Config.As == releaseConfigModeStable
	if aStable && !bStable {
		return true
	}
	if !aStable && bStable {
		return false
	}
	aV, _ := semver.ParseTolerant(a.Release.Config.Name)
	bV, _ := semver.ParseTolerant(b.Release.Config.Name)
	aV.Pre = nil
	bV.Pre = nil
	switch aV.Compare(bV) {
	case 1:
		return true
	case -1:
		return false
	}
	return a.Release.Config.Name <= b.Release.Config.Name
}

func (r preferredReleases) Swap(i, j int) { r[i], r[j] = r[j], r[i] }
func (r preferredReleases) Len() int      { return len(r) }

type newestSemVerFromSummaries struct {
	versions  []semver.Version
	summaries []UpgradeHistory
}

func newNewestSemVerFromSummaries(summaries []UpgradeHistory) newestSemVerFromSummaries {
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
	if s.summaries[i].From > s.summaries[j].From {
		return true
	}
	if s.summaries[i].From < s.summaries[j].From {
		return false
	}
	return s.summaries[i].Total >= s.summaries[j].Total
}
func (s newestSemVerFromSummaries) Swap(i, j int) {
	s.summaries[i], s.summaries[j] = s.summaries[j], s.summaries[i]
	s.versions[i], s.versions[j] = s.versions[j], s.versions[i]
}
func (s newestSemVerFromSummaries) Len() int { return len(s.summaries) }

type newestSemVerToSummaries struct {
	versions  []semver.Version
	summaries []UpgradeHistory
}

func newNewestSemVerToSummaries(summaries []UpgradeHistory) newestSemVerToSummaries {
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

func renderInstallInstructions(w io.Writer, mirror *imagev1.ImageStream, tag *imagev1.TagReference, tagPull, artifactsHost string) {
	if len(tagPull) == 0 {
		fmt.Fprintf(w, `<p class="alert alert-warning">No public location to pull this image from</p>`)
		return
	}
	if len(artifactsHost) == 0 {
		fmt.Fprintf(w, `<p>Download installer and client with:<pre class="ml-4">oc adm release extract --tools %s</pre>`, template.HTMLEscapeString(tagPull))
		return
	}
	fmt.Fprintf(w, `<p><a href="%s">Download the installer</a> for your operating system or run <pre class="ml-4">oc adm release extract --tools %s</pre>`, template.HTMLEscapeString(fmt.Sprintf("https://%s/%s", artifactsHost, tag.Name)), template.HTMLEscapeString(tagPull))
}

func checkReleasePage(page *ReleasePage) {
	for i := range page.Streams {
		stream := &page.Streams[i]
		for name, check := range stream.Release.Config.Check {
			switch {
			case check.ConsistentImages != nil:
				parent := findReleaseStream(page, check.ConsistentImages.Parent)
				if parent == nil {
					stream.Checks = append(stream.Checks, ReleaseCheckResult{
						Name:   name,
						Errors: []string{fmt.Sprintf("The parent stream %s could not be found.", check.ConsistentImages.Parent)},
					})
					continue
				}
				result := checkConsistentImages(stream.Release, parent.Release)
				result.Name = name
				stream.Checks = append(stream.Checks, result)
			}
		}
	}
}
