package rhcos

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
)

const (
	rhelCoreOs         = "Red Hat Enterprise Linux CoreOS"
	centosStreamCoreOs = "CentOS Stream CoreOS"
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

	reMdRHCoSDiff    = regexp.MustCompile(`\* Red Hat Enterprise Linux CoreOS upgraded from ((\d+)\.[\w\.\-]+) to ((\d+)\.[\w\.\-]+)\n`)
	reMdRHCoSVersion = regexp.MustCompile(`\* Red Hat Enterprise Linux CoreOS ((\d+)\.[\w\.\-]+)\n`)

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

	// TODO: As we get more comfortable with these sorts of transformations, we could make them more generic.
	//       For now, this will have to do.
	if m := reMdRHCoSDiff.FindStringSubmatch(markdown); m != nil {
		markdown = transformCoreOSUpgradeLinks(rhelCoreOs, architecture, architectureExtension, markdown, m)
	} else if m = reMdCentOSCoSDiff.FindStringSubmatch(markdown); m != nil {
		markdown = transformCoreOSUpgradeLinks(centosStreamCoreOs, architecture, architectureExtension, markdown, m)
	}
	if m := reMdRHCoSVersion.FindStringSubmatch(markdown); m != nil {
		markdown = transformCoreOSLinks(rhelCoreOs, architecture, architectureExtension, markdown, m)
	} else if m = reMdCentOSCoSVersion.FindStringSubmatch(markdown); m != nil {
		markdown = transformCoreOSLinks(centosStreamCoreOs, architecture, architectureExtension, markdown, m)
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
		case rhelCoreOs, centosStreamCoreOs:
			var ok bool
			var fromStream, toStream string
			if len(component.Version) == 0 {
				continue
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
			changeLogJson.Components[i] = component
		}
	}

	updated, err := json.MarshalIndent(&changeLogJson, "", "  ")
	if err != nil {
		return "", err
	}

	return string(updated), nil
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

func RenderNodeImageInfo(markdown string, rpmDiff releasecontroller.RpmDiff) string {
	output := new(strings.Builder)

	writeList := func(header string, elements []string) {
		fmt.Fprintf(output, "#### %s:\n\n", header)
		sort.Strings(elements)
		for _, elem := range elements {
			fmt.Fprintf(output, "* %s\n", elem)
		}
		fmt.Fprint(output, "\n")
	}

	if len(rpmDiff.Changed) > 0 {
		elements := []string{}
		for pkg, v := range rpmDiff.Changed {
			elements = append(elements, fmt.Sprintf("%s %s → %s", pkg, v.Old, v.New))
		}
		writeList("Changed", elements)
	}
	if len(rpmDiff.Removed) > 0 {
		elements := []string{}
		for pkg, v := range rpmDiff.Removed {
			elements = append(elements, fmt.Sprintf("%s %s", pkg, v))
		}
		writeList("Removed", elements)
	}
	if len(rpmDiff.Added) > 0 {
		elements := []string{}
		for pkg, v := range rpmDiff.Added {
			elements = append(elements, fmt.Sprintf("%s %s", pkg, v))
		}
		writeList("Added", elements)
	}

	if output.Len() == 0 {
		return "No package diff"
	}

	// Reprint the version/diff line, with the browser links for the build itself.
	// But put it last to de-emphasize it. Most people don't need to click on this.
	if m := reMdCoSPost.FindStringSubmatch(markdown); m != nil {
		fmt.Fprintf(output, "<br/>%s **base layer**: %s\n\n", m[1], m[3])
	}

	return output.String()
}
