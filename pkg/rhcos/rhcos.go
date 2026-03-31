package rhcos

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
)

const (
	rhelCoreOs           = "Red Hat Enterprise Linux CoreOS"
	rhelCoreOs10         = "Red Hat Enterprise Linux CoreOS 10"
	centosStreamCoreOs   = "CentOS Stream CoreOS"
	baseLayerAlertBox  = `<div class="alert alert-info" id="coreos-base-alert">
		<p>The CoreOS links above are for <em>the base CoreOS layer</em> used to build
		the OpenShift node image and do not contain OpenShift components. This is
		normally only useful to devs working closely with the CoreOS team. For info
		about the node image, see the <a href="#node-image-info">Node Image Info</a>
		section.</p></div>`
)

var (
	// when CoreOS switched to a new bucket prefix
	changeoverTimestamp = 202212000000

	serviceScheme = "https"
	serviceUrl    = "releases-rhcos--prod-pipeline.apps.int.prod-stable-spoke1-dc-iad2.itup.redhat.com"

	reMdPromotedFrom = regexp.MustCompile("Promoted from (.*):(.*)")

	// Handle both old format (no RHEL version) and new format (with RHEL version like "9.8")
	// Old: "* Red Hat Enterprise Linux CoreOS upgraded from 9.8.20260312-0 to 9.8.20260227-0"
	// New: "* Red Hat Enterprise Linux CoreOS 9.8 upgraded from 9.8.20260305-0 to 9.8.20260312-0"
	reMdRHCoSDiff    = regexp.MustCompile(`\* Red Hat Enterprise Linux CoreOS(?: \d+\.\d+)? upgraded from ((\d+)\.[\w\.\-]+) to ((\d+)\.[\w\.\-]+)\n`)
	reMdRHCoSVersion = regexp.MustCompile(`\* Red Hat Enterprise Linux CoreOS(?: \d+\.\d+)? ((\d+)\.[\w\.\-]+)\n`)

	// RHEL 10 node image (rhel-coreos-10); match before generic RHCOS regex (longer prefix first).
	reMdRHCoS10Diff    = regexp.MustCompile(`\* Red Hat Enterprise Linux CoreOS 10(?: \d+\.\d+)? upgraded from ((\d+)\.[\w\.\-]+) to ((\d+)\.[\w\.\-]+)\n`)
	reMdRHCoS10Version = regexp.MustCompile(`\* Red Hat Enterprise Linux CoreOS 10(?: \d+\.\d+)? ((\d+)\.[\w\.\-]+)\n`)

	reMdCentOSCoSDiff    = regexp.MustCompile(`\* CentOS Stream CoreOS upgraded from ((\d+)\.[\w\.\-]+) to ((\d+)\.[\w\.\-]+)\n`)
	reMdCentOSCoSVersion = regexp.MustCompile(`\* CentOS Stream CoreOS ((\d+)\.[\w\.\-]+)\n`)

	// the postprocessed markdown version/diff line
	reMdCoSPost = regexp.MustCompile(`\* (.*? CoreOS) (upgraded from .+ to )?(.+)`)

	// regex for RHCOS in < 4.19, where the version string was e.g. 418.94.202410090804-0
	reOcpCoreOsVersion = regexp.MustCompile(`((\d)(\d+))\.(\d+)\.(\d+)-(\d+)`)

	// regex for RHCOS in >= 4.19, where the version string was e.g. 9.6.20250121-0
	reRhelCoreOsVersion = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)-(\d+)`)
)

func TransformMarkDownOutput(markdown, fromTag, toTag, architecture, architectureExtension string) (string, error) {
	// replace references to the previous version with links
	rePrevious, err := regexp.Compile(fmt.Sprintf(`([^\w:])%s(\W)`, regexp.QuoteMeta(fromTag)))
	if err != nil {
		return "", err
	}

	// do a best effort replacement to change out the headers
	markdown = strings.ReplaceAll(markdown, fmt.Sprintf(`# %s`, toTag), "")
	if changed := strings.ReplaceAll(markdown, fmt.Sprintf(`## Changes from %s`, fromTag), ""); len(changed) != len(markdown) {
		markdown = fmt.Sprintf("## Changes from %s\n%s", fromTag, changed)
	}
	markdown = rePrevious.ReplaceAllString(markdown, fmt.Sprintf("$1[%s](/releasetag/%s)$2", fromTag, fromTag))

	// add link to tag from which current version promoted from
	markdown = reMdPromotedFrom.ReplaceAllString(markdown, fmt.Sprintf("Release %s was created from [$1:$2](/releasetag/$2)", toTag))

	// Apply CoreOS link transforms for every matching line (OpenShift 4.21+ may list RHCOS 9 and 10 separately).
	for {
		var m []string
		var name string
		switch {
		case reMdRHCoS10Diff.MatchString(markdown):
			m = reMdRHCoS10Diff.FindStringSubmatch(markdown)
			name = rhelCoreOs10
		case reMdRHCoSDiff.MatchString(markdown):
			m = reMdRHCoSDiff.FindStringSubmatch(markdown)
			name = rhelCoreOs
		case reMdCentOSCoSDiff.MatchString(markdown):
			m = reMdCentOSCoSDiff.FindStringSubmatch(markdown)
			name = centosStreamCoreOs
		default:
			m = nil
		}
		if m == nil {
			break
		}
		markdown = transformCoreOSUpgradeLinks(name, architecture, architectureExtension, markdown, m)
	}
	for {
		var m []string
		var name string
		switch {
		case reMdRHCoS10Version.MatchString(markdown):
			m = reMdRHCoS10Version.FindStringSubmatch(markdown)
			name = rhelCoreOs10
		case reMdRHCoSVersion.MatchString(markdown):
			m = reMdRHCoSVersion.FindStringSubmatch(markdown)
			name = rhelCoreOs
		case reMdCentOSCoSVersion.MatchString(markdown):
			m = reMdCentOSCoSVersion.FindStringSubmatch(markdown)
			name = centosStreamCoreOs
		default:
			m = nil
		}
		if m == nil {
			break
		}
		markdown = transformCoreOSLinks(name, architecture, architectureExtension, markdown, m)
	}
	return markdown, nil
}

func TransformJsonOutput(output, architecture, architectureExtension string) (string, error) {
	var changeLogJson releasecontroller.ChangeLog
	err := json.Unmarshal([]byte(output), &changeLogJson)
	if err != nil {
		return "", err
	}

	for i, component := range changeLogJson.Components {
		switch component.Name {
		case rhelCoreOs, rhelCoreOs10, centosStreamCoreOs:
			changeLogJson.Components[i] = enrichCoreOSComponentJSON(component, architecture, architectureExtension)
		}
	}

	updated, err := json.MarshalIndent(&changeLogJson, "", "  ")
	if err != nil {
		return "", err
	}

	return string(updated), nil
}

func enrichCoreOSComponentJSON(component releasecontroller.ChangeLogComponentInfo, architecture, architectureExtension string) releasecontroller.ChangeLogComponentInfo {
	var ok bool
	var fromStream, toStream string
	if len(component.Version) == 0 {
		return component
	}
	if toStream, ok = getRHCoSReleaseStream(component.Version, architectureExtension); ok {
		toURL := url.URL{
			Scheme:   serviceScheme,
			Host:     serviceUrl,
			Path:     "/",
			Fragment: component.Version,
			RawQuery: (url.Values{
				"stream":  []string{toStream},
				"arch":    []string{architecture},
				"release": []string{component.Version},
			}).Encode(),
		}
		component.VersionUrl = toURL.String()
	}

	if len(component.From) > 0 {
		if fromStream, ok = getRHCoSReleaseStream(component.From, architectureExtension); ok {
			fromUrl := url.URL{
				Scheme:   serviceScheme,
				Host:     serviceUrl,
				Path:     "/",
				Fragment: component.From,
				RawQuery: (url.Values{
					"stream":  []string{fromStream},
					"arch":    []string{architecture},
					"release": []string{component.From},
				}).Encode(),
			}
			component.FromUrl = fromUrl.String()

			diffURL := url.URL{
				Scheme: serviceScheme,
				Host:   serviceUrl,
				Path:   "/diff.html",
				RawQuery: (url.Values{
					"first_stream":   []string{fromStream},
					"first_release":  []string{component.From},
					"second_stream":  []string{toStream},
					"second_release": []string{component.Version},
					"arch":           []string{architecture},
				}).Encode(),
			}
			component.DiffUrl = diffURL.String()
		}
	}
	return component
}

func getRHCoSReleaseStream(version, architectureExtension string) (string, bool) {
	if strings.HasPrefix(version, "4") {
		if m := reOcpCoreOsVersion.FindStringSubmatch(version); m != nil {
			ts, err := strconv.Atoi(m[5])
			if err != nil {
				return "", false
			}
			minor, err := strconv.Atoi(m[3])
			if err != nil {
				return "", false
			}
			switch {
			case ts > changeoverTimestamp && minor >= 9:

				// TODO: This should hopefully only be temporary...
				versionMap := map[string]string{
					"92": "9.2",
					"94": "9.4",
					"96": "9.6",
				}
				if version, ok := versionMap[m[4]]; ok {
					return fmt.Sprintf("prod/streams/%s.%s-%s", m[2], m[3], version), true
				}

				return fmt.Sprintf("prod/streams/%s.%s", m[2], m[3]), true
			default:
				return fmt.Sprintf("releases/rhcos-%s.%s%s", m[2], m[3], architectureExtension), true
			}
		}
	} else {
		if m := reRhelCoreOsVersion.FindStringSubmatch(version); m != nil {
			return fmt.Sprintf("prod/streams/rhel-%s.%s", m[1], m[2]), true
		}
	}
	return "", false
}

func transformCoreOSUpgradeLinks(name, architecture, architectureExtension, input string, matches []string) string {
	var ok bool
	var fromURL, toURL url.URL
	var fromStream, toStream string

	fromRelease := matches[1]
	if fromStream, ok = getRHCoSReleaseStream(fromRelease, architectureExtension); ok {
		fromURL = url.URL{
			Scheme:   serviceScheme,
			Host:     serviceUrl,
			Path:     "/",
			Fragment: fromRelease,
			RawQuery: (url.Values{
				"stream":  []string{fromStream},
				"arch":    []string{architecture},
				"release": []string{fromRelease},
			}).Encode(),
		}
	}

	toRelease := matches[3]
	if toStream, ok = getRHCoSReleaseStream(toRelease, architectureExtension); ok {
		toURL = url.URL{
			Scheme:   serviceScheme,
			Host:     serviceUrl,
			Path:     "/",
			Fragment: toRelease,
			RawQuery: (url.Values{
				"stream":  []string{toStream},
				"arch":    []string{architecture},
				"release": []string{toRelease},
			}).Encode(),
		}
	}

	// anything not starting with "4" means it's the new node images so we will
	// render a package diff ourselves, see also:
	// https://github.com/openshift/enhancements/blob/master/enhancements/rhcos/split-rhcos-into-layers.md#etcos-release
	hasCoreosLayered := !strings.HasPrefix(fromRelease, "4") || !strings.HasPrefix(toRelease, "4")

	// even for the few 4.19 releases we've had, we render the package diff so that we can test it out
	hasCoreos419 := strings.HasPrefix(fromRelease, "419") || strings.HasPrefix(toRelease, "419")

	diffInfo := ""
	if hasCoreos419 || hasCoreosLayered {
		// for newer styles, we print an infobox that links to the node image info section
		diffInfo = baseLayerAlertBox
	} else {
		// keep legacy behaviour of linking to internal RHCOS release browser for the diff
		diffURL := (&url.URL{
			Scheme: serviceScheme,
			Host:   serviceUrl,
			Path:   "/diff.html",
			RawQuery: (url.Values{
				"first_stream":   []string{fromStream},
				"first_release":  []string{fromRelease},
				"second_stream":  []string{toStream},
				"second_release": []string{toRelease},
				"arch":           []string{architecture},
			}).Encode(),
		}).String()
		diffInfo = fmt.Sprintf(`([diff](%s))`, diffURL)
	}

	replace := fmt.Sprintf(
		`* %s upgraded from [%s](%s) to [%s](%s) %s`,
		name,
		fromRelease,
		fromURL.String(),
		toRelease,
		toURL.String(),
		diffInfo,
	)
	return strings.ReplaceAll(input, matches[0], replace)
}

func transformCoreOSLinks(name, architecture, architectureExtension, input string, matches []string) string {
	var ok bool
	var fromURL url.URL
	var fromStream string

	fromRelease := matches[1]
	if fromStream, ok = getRHCoSReleaseStream(fromRelease, architectureExtension); ok {
		fromURL = url.URL{
			Scheme:   serviceScheme,
			Host:     serviceUrl,
			Path:     "/",
			Fragment: fromRelease,
			RawQuery: (url.Values{
				"stream":  []string{fromStream},
				"arch":    []string{architecture},
				"release": []string{fromRelease},
			}).Encode(),
		}
	}

	// these conditions are similar to those in transformCoreOSUpgradeLinks() above
	hasCoreosLayered := !strings.HasPrefix(fromRelease, "4")
	hasCoreos419 := strings.HasPrefix(fromRelease, "419")

	diffInfo := ""
	if hasCoreos419 || hasCoreosLayered {
		diffInfo = baseLayerAlertBox
	}
	replace := fmt.Sprintf(
		`* %s [%s](%s) %s`+"\n",
		name,
		fromRelease,
		fromURL.String(),
		diffInfo,
	)
	return strings.ReplaceAll(input, matches[0], replace)
}

// CoreOSNodeStream holds RPM package lists and diffs for one rhel-coreos* or stream-coreos image.
type CoreOSNodeStream struct {
	Title   string
	RpmList releasecontroller.RpmList
	RpmDiff releasecontroller.RpmDiff
}

func RenderNodeImageInfo(markdown string, rpmList releasecontroller.RpmList, rpmDiff releasecontroller.RpmDiff) string {
	return RenderDualNodeImageInfo(markdown, []CoreOSNodeStream{{RpmList: rpmList, RpmDiff: rpmDiff}})
}

// RenderDualNodeImageInfo renders one or more Node Image Info sections (e.g. multiple machine-OS
// streams in OpenShift 4.21+).
func RenderDualNodeImageInfo(markdown string, streams []CoreOSNodeStream) string {
	if len(streams) == 0 {
		return ""
	}
	var out strings.Builder
	dual := len(streams) > 1
	for i, s := range streams {
		if i > 0 {
			out.WriteString("\n\n")
		}
		renderOneNodeStream(&out, s, dual)
	}
	out.WriteString(baseLayerFooter(markdown))
	return out.String()
}

func renderOneNodeStream(out *strings.Builder, stream CoreOSNodeStream, dual bool) {
	h := "###"
	if stream.Title != "" {
		fmt.Fprintf(out, "### %s\n\n", stream.Title)
		h = "####"
	} else if dual {
		h = "####"
	}

	fmt.Fprintf(out, "%s Package List\n\n", h)

	importantPkgs := []string{"cri-o", "kernel", "openshift-kubelet", "systemd"}
	for _, pkg := range importantPkgs {
		fmt.Fprintf(out, "* %s-%s\n", pkg, stream.RpmList.Packages[pkg])
	}

	fmt.Fprintf(out, "\n<details><summary>Full list (%d packages)</summary>\n\n", len(stream.RpmList.Packages))
	sortedPkgs := slices.Sorted(maps.Keys(stream.RpmList.Packages))
	for _, pkg := range sortedPkgs {
		fmt.Fprintf(out, "* %s-%s\n", pkg, stream.RpmList.Packages[pkg])
	}
	fmt.Fprintf(out, "</details>\n\n")

	writeList := func(header string, elements []string) {
		fmt.Fprintf(out, "%s %s:\n\n", h, header)
		sort.Strings(elements)
		for _, elem := range elements {
			fmt.Fprintf(out, "* %s\n", elem)
		}
		fmt.Fprint(out, "\n")
	}

	if len(stream.RpmDiff.Changed) > 0 {
		elements := []string{}
		for pkg, v := range stream.RpmDiff.Changed {
			elements = append(elements, fmt.Sprintf("%s %s → %s", pkg, v.Old, v.New))
		}
		writeList("Changed", elements)
	}
	if len(stream.RpmDiff.Removed) > 0 {
		elements := []string{}
		for pkg, v := range stream.RpmDiff.Removed {
			elements = append(elements, fmt.Sprintf("%s %s", pkg, v))
		}
		writeList("Removed", elements)
	}
	if len(stream.RpmDiff.Added) > 0 {
		elements := []string{}
		for pkg, v := range stream.RpmDiff.Added {
			elements = append(elements, fmt.Sprintf("%s %s", pkg, v))
		}
		writeList("Added", elements)
	}

	fmt.Fprintf(out, "\n\n%s Extensions\n\n", h)

	fmt.Fprintf(out, "\n<details><summary>Full list (%d packages)</summary>\n\n", len(stream.RpmList.Extensions))
	sortedPkgs = slices.Sorted(maps.Keys(stream.RpmList.Extensions))
	for _, pkg := range sortedPkgs {
		fmt.Fprintf(out, "* %s-%s\n", pkg, stream.RpmList.Extensions[pkg])
	}
	fmt.Fprintf(out, "</details>\n\n")
}

func baseLayerFooter(markdown string) string {
	matches := reMdCoSPost.FindAllStringSubmatch(markdown, -1)
	if len(matches) == 0 {
		return ""
	}
	var b strings.Builder
	for _, m := range matches {
		fmt.Fprintf(&b, "<br/>%s **base layer**: %s\n\n", m[1], m[3])
	}
	return b.String()
}
