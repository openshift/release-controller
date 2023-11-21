package main

import (
	"bytes"
	"fmt"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/releasepayload"
	"io"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"net/url"
	"sort"
	"strings"
	"text/template"
	"time"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	"github.com/blang/semver"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imagev1 "github.com/openshift/api/image/v1"
)

type ReleasePage struct {
	BaseURL    string
	Streams    []ReleaseStream
	Dashboards []Dashboard
}

type ReleaseStream struct {
	Release *releasecontroller.Release
	Tags    []*imagev1.TagReference

	// Delayed is set if there is a pending release
	Delayed *ReleaseDelay

	Upgrades *ReleaseUpgrades
	Checks   []ReleaseCheckResult

	// Failing is set if none of the most recent 5 payloads were accepted
	Failing bool
}

type Dashboard struct {
	Name string
	Path string
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
	Release *releasecontroller.Release
	Tag     *imagev1.TagReference

	PreviousRelease *releasecontroller.Release
	Previous        *imagev1.TagReference

	Older  []*imagev1.TagReference
	Stable *releasecontroller.StableReferences
}

type ReleaseUpgrades struct {
	Width int
	Tags  []ReleaseTagUpgrade
}

type ReleaseTagUpgrade struct {
	Internal []releasecontroller.UpgradeHistory
	External []releasecontroller.UpgradeHistory
	Visual   []ReleaseTagUpgradeVisual
}

type ReleaseTagUpgradeVisual struct {
	Begin, Current, End *releasecontroller.UpgradeHistory
}

func phaseCell(tag imagev1.TagReference) string {
	phase := tag.Annotations[releasecontroller.ReleaseAnnotationPhase]
	switch phase {
	case releasecontroller.ReleasePhaseRejected:
		return fmt.Sprintf("<td class=\"%s\" title=\"%s\">%s</td>",
			phaseAlert(tag),
			template.HTMLEscapeString(tag.Annotations[releasecontroller.ReleaseAnnotationMessage]),
			template.HTMLEscapeString(phase),
		)
	}
	return fmt.Sprintf("<td class=\"%s\">", phaseAlert(tag)) + template.HTMLEscapeString(phase) + "</td>"
}

func phaseAlert(tag imagev1.TagReference) string {
	phase := tag.Annotations[releasecontroller.ReleaseAnnotationPhase]
	switch phase {
	case releasecontroller.ReleasePhasePending:
		return ""
	case releasecontroller.ReleasePhaseReady:
		return ""
	case releasecontroller.ReleasePhaseAccepted:
		return "text-success"
	case releasecontroller.ReleasePhaseFailed:
		return "text-danger"
	case releasecontroller.ReleasePhaseRejected:
		return "text-danger"
	default:
		return "text-danger"
	}
}

func styleForUpgrade(upgrade *releasecontroller.UpgradeHistory) string {
	switch upgradeSummaryState(upgrade) {
	case releasecontroller.ReleaseVerificationStateFailed:
		return "border-color: #dc3545"
	case releasecontroller.ReleaseVerificationStateSucceeded:
		return "border-color: #28a745"
	default:
		return "border-color: #007bff"
	}
}

func upgradeCells(upgrades *ReleaseUpgrades, index int) string {
	if upgrades == nil {
		return ""
	}
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
			case releasecontroller.ReleaseVerificationStateSucceeded:
				successCount++
			case releasecontroller.ReleaseVerificationStateFailed:
				buf.WriteString(fmt.Sprintf(`<a class="text-danger" href=%s>%s</a><br>`, url, visual.Begin.From))
			default:
				otherCount++
			}
		}
	}

	for _, external := range upgrades.Tags[index].External {
		for url, result := range external.History {
			switch result.State {
			case releasecontroller.ReleaseVerificationStateSucceeded:
				successCount++
			case releasecontroller.ReleaseVerificationStateFailed:
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
	switch tag.Annotations[releasecontroller.ReleaseAnnotationPhase] {
	case releasecontroller.ReleasePhasePending:
		return false
	default:
		return true
	}
}

func tableLink(config *releasecontroller.ReleaseConfig, tag imagev1.TagReference, inconsistencies map[string]bool) string {
	if canLink(tag) {
		if value, ok := tag.Annotations[releasecontroller.ReleaseAnnotationKeep]; ok {
			return fmt.Sprintf(`<td class="text-monospace"><a title="%s" class="%s" href="/releasestream/%s/release/%s">%s <span>*</span></a></td>`, template.HTMLEscapeString(value), phaseAlert(tag), template.HTMLEscapeString(config.Name), template.HTMLEscapeString(tag.Name), template.HTMLEscapeString(tag.Name))
		}
		if strings.Contains(tag.Name, "nightly") && inconsistencies[tag.Name] {
			return fmt.Sprintf(`<td class="text-monospace"><a class="%s" href="/releasestream/%s/release/%s">%s</a> <a href="/releasestream/%s/inconsistency/%s"><i title="Inconsistency detected! Click for more details" class="bi bi-exclamation-circle"></i></a></td>`, phaseAlert(tag), template.HTMLEscapeString(config.Name), template.HTMLEscapeString(tag.Name), template.HTMLEscapeString(tag.Name), template.HTMLEscapeString(config.Name), template.HTMLEscapeString(tag.Name))
		} else if config.As == releasecontroller.ReleaseConfigModeStable {
			return fmt.Sprintf(`<td class="text-monospace"><a class="%s" style="padding-left:15px" href="/releasestream/%s/release/%s">%s</a></td>`, phaseAlert(tag), template.HTMLEscapeString(config.Name), template.HTMLEscapeString(tag.Name), template.HTMLEscapeString(tag.Name))
		} else {
			return fmt.Sprintf(`<td class="text-monospace"><a class="%s" href="/releasestream/%s/release/%s">%s</a></td>`, phaseAlert(tag), template.HTMLEscapeString(config.Name), template.HTMLEscapeString(tag.Name), template.HTMLEscapeString(tag.Name))
		}
	}
	return fmt.Sprintf(`<td class="text-monospace %s">%s</td>`, phaseAlert(tag), template.HTMLEscapeString(tag.Name))
}

func (c *Controller) GetReleasePayload(name string) *v1alpha1.ReleasePayload {
	payload, err := c.releasePayloadLister.ReleasePayloads(c.releasePayloadNamespace).Get(name)
	if err != nil {
		klog.Warningf("Unable to locate releasepayload (%s): %v", name, err)
		return nil
	}
	return payload
}

func (c *Controller) links(tag imagev1.TagReference, release *releasecontroller.Release) string {
	payload := c.GetReleasePayload(tag.Name)
	if payload == nil {
		klog.Errorf("unable to find releasepayload: %q", tag.Name)
		return "error"
	}
	var status releasecontroller.VerificationStatusMap
	if ok := releasepayload.GenerateVerificationStatusMap(payload, &status); !ok {
		klog.Errorf("unable to generate VerificationStatusMap for: %q", tag.Name)
		return "error"
	}
	verificationJobs, err := releasecontroller.GetVerificationJobs(c.parsedReleaseConfigCache, c.eventRecorder, c.releaseLister, release, &tag, c.artSuffix)
	if err != nil {
		klog.Errorf("unable to retrieve VerificationJobs for: %q", tag.Name)
		return "error"
	}
	pending, failing := make([]string, 0, len(verificationJobs)), make([]string, 0, len(verificationJobs))
	for k := range verificationJobs {
		s, ok := status[k]
		if !ok {
			continue
		}
		switch s.State {
		case releasecontroller.ReleaseVerificationStateSucceeded:
			continue
		case releasecontroller.ReleaseVerificationStateFailed:
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
				case releasecontroller.ReleaseVerificationStateSucceeded:
					continue
				case releasecontroller.ReleaseVerificationStateFailed:
					buf.WriteString(" <a title=\"Failed\" class=\"text-danger\" href=\"")
				default:
					buf.WriteString(" <a title=\"Pending\" class=\"\" href=\"")
				}
				buf.WriteString(template.HTMLEscapeString(releasecontroller.GenerateProwJobResultsURL(s.URL)))
				buf.WriteString("\">")
				buf.WriteString(template.HTMLEscapeString(key))
				buf.WriteString("</a>")
				continue
			}
			switch s.State {
			case releasecontroller.ReleaseVerificationStateSucceeded:
				continue
			case releasecontroller.ReleaseVerificationStateFailed:
				buf.WriteString(" <span title=\"Failed\" class=\"text-danger\">")
			default:
				buf.WriteString(" <span title=\"Pending\" class=\"\">")
			}
			buf.WriteString(template.HTMLEscapeString(key))
			buf.WriteString("</span>")
			continue
		}
		final := tag.Annotations[releasecontroller.ReleaseAnnotationPhase] == releasecontroller.ReleasePhaseRejected || tag.Annotations[releasecontroller.ReleaseAnnotationPhase] == releasecontroller.ReleasePhaseAccepted
		if !verificationJobs[key].Disabled && !final {
			buf.WriteString(" <span title=\"Pending\">")
			buf.WriteString(template.HTMLEscapeString(key))
			buf.WriteString("</span>")
		}
	}
	if pendingCount > 3 {
		buf.WriteString(" <a title=\"See details page for more\" class=\"\" href=\"")
		buf.WriteString(template.HTMLEscapeString(fmt.Sprintf("/releasestream/%s/release/%s", url.PathEscape(release.Config.Name), url.PathEscape(tag.Name))))
		buf.WriteString("\">")
		buf.WriteString(template.HTMLEscapeString(fmt.Sprintf("%d pending", len(pending))))
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

func (c *Controller) getVerificationJobs(tag imagev1.TagReference, release *releasecontroller.Release) (*releasecontroller.VerificationJobsSummary, string) {
	payload := c.GetReleasePayload(tag.Name)
	if payload == nil {
		klog.Errorf("unable to find releasepayload: %q", tag.Name)
		return nil, fmt.Sprintf(`<p><em class="text-danger">Unable to load ReleasePayload info for: %s</em>`, tag.Name)
	}
	var status releasecontroller.VerificationStatusMap
	if ok := releasepayload.GenerateVerificationStatusMap(payload, &status); !ok {
		klog.Errorf("unable to generate VerificationStatusMap for: %q", tag.Name)
		return nil, fmt.Sprintf(`<p><em class="text-danger">Unable to load test info for: %s</em>`, tag.Name)
	}
	verificationJobs, err := releasecontroller.GetVerificationJobs(c.parsedReleaseConfigCache, c.eventRecorder, c.releaseLister, release, &tag, c.artSuffix)
	if err != nil {
		klog.Errorf("unable to retrieve VerificationJobs for: %q", tag.Name)
		return nil, fmt.Sprintf(`<p><em class="text-danger">Unable to load VerificationJobs for: %s</em>`, tag.Name)
	}
	blockingJobs := make(releasecontroller.VerificationStatusMap)
	informingJobs := make(releasecontroller.VerificationStatusMap)
	pendingJobs := make(releasecontroller.VerificationStatusMap)
	keys := make([]string, 0, len(verificationJobs))
	for k := range verificationJobs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if value, ok := status[key]; ok {
			if verificationJobs[key].Optional {
				informingJobs[key] = value
			} else {
				blockingJobs[key] = value
			}
		} else {
			delete(pendingJobs, key)
		}
	}
	return &releasecontroller.VerificationJobsSummary{
		BlockingJobs:  blockingJobs,
		InformingJobs: informingJobs,
		PendingJobs:   pendingJobs,
	}, ""
}

func (c *Controller) renderVerifyLinks(w io.Writer, tag imagev1.TagReference, release *releasecontroller.Release) {
	verificationJobs, msg := c.getVerificationJobs(tag, release)
	if len(msg) > 0 {
		fmt.Fprintf(w, msg)
		return
	}
	buf := &bytes.Buffer{}
	final := tag.Annotations[releasecontroller.ReleaseAnnotationPhase] == releasecontroller.ReleasePhaseRejected || tag.Annotations[releasecontroller.ReleaseAnnotationPhase] == releasecontroller.ReleasePhaseAccepted
	if len(verificationJobs.BlockingJobs) > 0 {
		buf.WriteString("<li>Blocking jobs<ul>")
		buf.WriteString(c.renderVerificationJobsList(verificationJobs.BlockingJobs, release, tag, final))
		buf.WriteString("</ul></li>")
	}
	if len(verificationJobs.InformingJobs) > 0 {
		buf.WriteString("<li>Informing jobs<ul>")
		buf.WriteString(c.renderVerificationJobsList(verificationJobs.InformingJobs, release, tag, final))
		buf.WriteString("</ul></li>")
	}
	if len(verificationJobs.PendingJobs) > 0 {
		buf.WriteString("<li>Pending jobs<ul>")
		buf.WriteString(c.renderVerificationJobsList(verificationJobs.PendingJobs, release, tag, final))
		buf.WriteString("</ul></li>")
	}
	if out := buf.String(); len(out) > 0 {
		fmt.Fprintf(w, `<p>Tests:</p><ul>%s</ul>`, out)
	} else {
		fmt.Fprintf(w, `<p><em>No tests for this release</em>`)
	}
}

func (c *Controller) renderVerificationJobsList(jobs releasecontroller.VerificationStatusMap, release *releasecontroller.Release, tag imagev1.TagReference, final bool) string {
	buf := &bytes.Buffer{}
	keys := make([]string, 0, len(jobs))
	for k := range jobs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	verificationJobs, err := releasecontroller.GetVerificationJobs(c.parsedReleaseConfigCache, c.eventRecorder, c.releaseLister, release, &tag, c.artSuffix)
	if err != nil {
		return "error"
	}
	for _, key := range keys {
		value := jobs[key]
		if value == nil {
			if !verificationJobs[key].Disabled && !final {
				buf.WriteString("<li><span title=\"Pending\">")
				buf.WriteString(template.HTMLEscapeString(key))
				buf.WriteString("</span></li>")
			}
			continue
		}
		if len(value.URL) > 0 {
			switch value.State {
			case releasecontroller.ReleaseVerificationStateFailed:
				buf.WriteString("<li><a class=\"text-danger\" href=\"")
			case releasecontroller.ReleaseVerificationStateSucceeded:
				buf.WriteString("<li><a class=\"text-success\" href=\"")
			default:
				buf.WriteString("<li><a class=\"\" href=\"")
			}
			buf.WriteString(template.HTMLEscapeString(releasecontroller.GenerateProwJobResultsURL(value.URL)))
			buf.WriteString("\">")
			buf.WriteString(template.HTMLEscapeString(key))
			switch value.State {
			case releasecontroller.ReleaseVerificationStateFailed:
				buf.WriteString(" Failed")
			case releasecontroller.ReleaseVerificationStateSucceeded:
				buf.WriteString(" Succeeded")
			default:
				buf.WriteString(" Pending")
			}
			buf.WriteString("</a>")
			if value.Retries > 0 {
				buf.WriteString(fmt.Sprintf(" <span class=\"text-warning\">(%d retries)</span>", value.Retries))
			}
			if pj := verificationJobs[key].ProwJob; pj != nil {
				buf.WriteString(" ")
				buf.WriteString(pj.Name)
			}
			continue
		}
		// Synthetic upgrade jobs are always successful and never have a URL.  This can cause confusion, so we're going
		// to render them in a different color...
		if verificationJobs[key].Upgrade && len(value.URL) == 0 && value.State == releasecontroller.ReleaseVerificationStateSucceeded {
			buf.WriteString("<li><span class=\"text-muted\">")
		} else {
			switch value.State {
			case releasecontroller.ReleaseVerificationStateFailed:
				buf.WriteString("<li><span class=\"text-danger\">")
			case releasecontroller.ReleaseVerificationStateSucceeded:
				buf.WriteString("<li><span class=\"text-success\">")
			default:
				buf.WriteString("<li><span class=\"\">")
			}
		}
		buf.WriteString(template.HTMLEscapeString(key))
		switch value.State {
		case releasecontroller.ReleaseVerificationStateFailed:
			buf.WriteString(" Failed")
		case releasecontroller.ReleaseVerificationStateSucceeded:
			buf.WriteString(" Succeeded")
		default:
			buf.WriteString(" Pending")
		}
		buf.WriteString("</span>")
		if pj := verificationJobs[key].ProwJob; pj != nil {
			buf.WriteString(" ")
			buf.WriteString(pj.Name)
		}
		continue
	}
	return buf.String()
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

func releaseJoin(streams []ReleaseStream, showStableReleases bool) string {
	releases := []string{}
	for _, s := range streams {
		if !showStableReleases && s.Release.Config.As == releasecontroller.ReleaseConfigModeStable {
			continue
		}
		releases = append(releases, fmt.Sprintf("<a href=\"#%s\">%s</a>", template.HTMLEscapeString(s.Release.Config.Name), template.HTMLEscapeString(s.Release.Config.Name)))
	}
	return strings.Join(releases, " | ")
}

func hasPublishTag(config *releasecontroller.ReleaseConfig) (string, bool) {
	for _, v := range config.Publish {
		if v.TagRef != nil {
			return v.TagRef.Name, true
		}
	}
	return "", false
}

func findPreviousRelease(tag *imagev1.TagReference, older []*imagev1.TagReference, release *releasecontroller.Release) *imagev1.TagReference {
	if len(older) == 0 {
		return nil
	}
	if name, ok := hasPublishTag(release.Config); ok {
		if published := releasecontroller.FindSpecTag(release.Target.Spec.Tags, name); published != nil && published.From != nil {
			target := published.From.Name
			for _, old := range older {
				if old.Name == target {
					return old
				}
			}
		}
	}
	for _, old := range older {
		if old.Annotations[releasecontroller.ReleaseAnnotationPhase] == releasecontroller.ReleasePhaseAccepted {
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

func upgradeSummaryToString(history []releasecontroller.UpgradeHistory) string {
	var out []string
	for _, h := range history {
		out = append(out, fmt.Sprintf("%s->%s", h.From, h.To))
	}
	return strings.Join(out, ",")
}

func calculateReleaseUpgrades(release *releasecontroller.Release, tags []*imagev1.TagReference, graph *releasecontroller.UpgradeGraph, fullHistory bool) *ReleaseUpgrades {
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
	var summaries []releasecontroller.UpgradeHistory
	if fullHistory {
		summaries = graph.UpgradesTo(tagNames...)
	} else {
		summaries = graph.SummarizeUpgradesTo(tagNames...)
	}
	for _, name := range tagNames {
		var internal, external []releasecontroller.UpgradeHistory
		internal, summaries = takeUpgradesTo(summaries, name)
		internal, external = takeUpgradesFromNames(internal, internalTags)
		sort.Slice(internal, func(i, j int) bool {
			return internalTags[internal[i].From] < internalTags[internal[j].From]
		})
		sort.Sort(releasecontroller.NewNewestSemVerFromSummaries(external))
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

func upgradeSummaryState(summary *releasecontroller.UpgradeHistory) string {
	if summary.Success > 0 {
		return releasecontroller.ReleaseVerificationStateSucceeded
	}
	if summary.Failure > 0 {
		return releasecontroller.ReleaseVerificationStateFailed
	}
	return releasecontroller.ReleaseVerificationStatePending
}

// takeUpgradesTo returns all leading summaries with To equal to tag, and the rest of the
// slice.
func takeUpgradesTo(summaries []releasecontroller.UpgradeHistory, tag string) ([]releasecontroller.UpgradeHistory, []releasecontroller.UpgradeHistory) {
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
func takeUpgradesFromNames(summaries []releasecontroller.UpgradeHistory, names map[string]int) (withNames []releasecontroller.UpgradeHistory, withoutNames []releasecontroller.UpgradeHistory) {
	for i := range summaries {
		if _, ok := names[summaries[i].From]; ok {
			continue
		}
		left := make([]releasecontroller.UpgradeHistory, i, len(summaries))
		copy(left, summaries[:i])
		var right []releasecontroller.UpgradeHistory
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
func filterWithPrefix(summaries []releasecontroller.UpgradeHistory, prefix string) []releasecontroller.UpgradeHistory {
	if len(prefix) == 0 {
		return summaries
	}
	for i := range summaries {
		if !strings.HasPrefix(summaries[i].From, prefix) {
			continue
		}
		valid := make([]releasecontroller.UpgradeHistory, 0, len(summaries)-i)
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
	aStable, bStable := a.Release.Config.As == releasecontroller.ReleaseConfigModeStable, b.Release.Config.As == releasecontroller.ReleaseConfigModeStable
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

func renderMultiArchPullSpec(w io.Writer, tagPull string) {
	if len(tagPull) == 0 {
		fmt.Fprintf(w, `<p class="alert alert-warning">No public location to pull this image from</p>`)
		return
	}
	fmt.Fprintf(w, `<p>PullSpec: <pre class="ml-4">%s</pre>`, template.HTMLEscapeString(tagPull))
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

func dashboardsJoin(dashboards []Dashboard) string {
	boards := []string{}
	for _, d := range dashboards {
		boards = append(boards, fmt.Sprintf("<a href=\"%s\">%s</a>", template.HTMLEscapeString(d.Path), template.HTMLEscapeString(d.Name)))
	}
	return strings.Join(boards, " | ")
}

func findReleaseStream(page *ReleasePage, name string) *ReleaseStream {
	for i := range page.Streams {
		if page.Streams[i].Release.Config.Name == name {
			return &page.Streams[i]
		}
	}
	return nil
}

func checkConsistentImages(release, parent *releasecontroller.Release) ReleaseCheckResult {
	var result ReleaseCheckResult
	source := parent.Source
	target := release.Source
	statusTags := make(map[string]*imagev1.NamedTagEventList)
	for i := range source.Status.Tags {
		statusTags[source.Status.Tags[i].Tag] = &source.Status.Tags[i]
	}
	var (
		errUnableToImport     map[string]string
		errDoesNotExist       []string
		errStaleBuild         []string
		warnOlderThanUpstream []string
		warnNoDownstream      []string
	)

	now := time.Now()

	for _, tag := range target.Status.Tags {
		if releasecontroller.IsTagEventConditionNotImported(&tag) {
			source := "image"
			if ref := releasecontroller.FindTagReference(target, tag.Tag); ref != nil {
				if ref.From != nil {
					source = ref.From.Name
				}
			}
			if errUnableToImport == nil {
				errUnableToImport = make(map[string]string)
			}
			errUnableToImport[tag.Tag] = source
			continue
		}
		if len(tag.Items) == 0 {
			// TODO: check something here?
			continue
		}
		if isStaleStatusTag(tag, target) {
			// TODO: These should be pruned, by whom is the question!?!?
			klog.Warningf("found stale status tag: %s/%s:%s", target.Namespace, target.Name, tag.Tag)
			continue
		}
		sourceTag := statusTags[tag.Tag]
		delete(statusTags, tag.Tag)

		if sourceTag == nil || len(sourceTag.Items) == 0 {
			if now.Sub(tag.Items[0].Created.Time) > 10*24*time.Hour {
				errStaleBuild = append(errStaleBuild, tag.Tag)
			} else {
				errDoesNotExist = append(errDoesNotExist, tag.Tag)
			}
			continue
		}
		delta := sourceTag.Items[0].Created.Sub(tag.Items[0].Created.Time)
		if delta > 0 {
			// source tag is newer than current tag
			if delta > 5*24*time.Hour {
				warnOlderThanUpstream = append(warnOlderThanUpstream, tag.Tag)
				continue
			}
		}
	}
	for _, tag := range statusTags {
		if len(tag.Items) == 0 {
			continue
		}
		if now.Sub(tag.Items[0].Created.Time) > 24*time.Hour {
			warnNoDownstream = append(warnNoDownstream, tag.Tag)
			continue
		}
	}

	if len(errUnableToImport) > 0 {
		var keys []string
		for k := range errUnableToImport {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		result.Errors = append(result.Errors, fmt.Sprintf("Unable to import the following tags: %s", strings.Join(keys, ", ")))
	}
	if len(errStaleBuild) > 0 {
		sort.Strings(errStaleBuild)
		result.Errors = append(result.Errors, fmt.Sprintf("Found old tags downstream that are not built upstream (not in %s/%s): %s", parent.Source.Namespace, parent.Source.Name, strings.Join(errStaleBuild, ", ")))
	}
	if len(errDoesNotExist) > 0 {
		sort.Strings(errDoesNotExist)
		result.Errors = append(result.Errors, fmt.Sprintf("Found recent tags downstream that are not built upstream (not in %s/%s): %s", parent.Source.Namespace, parent.Source.Name, strings.Join(errDoesNotExist, ", ")))
	}
	return result
}

func isStaleStatusTag(tag imagev1.NamedTagEventList, target *imagev1.ImageStream) bool {
	for _, specTag := range target.Spec.Tags {
		if specTag.Name == tag.Tag {
			return false
		}
	}
	return true
}

func pruneEndOfLifeTags(page *ReleasePage, endOfLifePrefixes sets.String) {
	for i := range page.Streams {
		stream := &page.Streams[i]
		if stream.Release.Config.As == releasecontroller.ReleaseConfigModeStable {
			var tags []*imagev1.TagReference
			for _, tag := range stream.Tags {
				if version, err := releasecontroller.SemverParseTolerant(tag.Name); err == nil {
					if !endOfLifePrefixes.Has(fmt.Sprintf("%d.%d", version.Major, version.Minor)) {
						tags = append(tags, tag)
					}
				}
			}
			stream.Tags = tags
		}
	}
}

func pruneTagInfo(tagInfo string) string {
	tagInfoParts := strings.Split(tagInfo, ".")
	return fmt.Sprintf("%s.%s", tagInfoParts[0], tagInfoParts[1])
}
