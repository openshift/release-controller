package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/blang/semver"
	imagev1 "github.com/openshift/api/image/v1"

	humanize "github.com/dustin/go-humanize"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"github.com/russross/blackfriday/v2"

	"k8s.io/apimachinery/pkg/labels"
)

const htmlPageStart = `
<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8"><title>%s</title>
<link rel="stylesheet" href="https://stackpath.bootstrapcdn.com/bootstrap/4.1.3/css/bootstrap.min.css" integrity="sha384-MCw98/SFnGE8fJT3GXwEOngsV7Zt27NXFoaoApmYm81iuXoPkFOJwJ8ERdknLPMO" crossorigin="anonymous">
<meta name="viewport" content="width=device-width, initial-scale=1, shrink-to-fit=no">
<style>
@media (max-width: 992px) {
  .container {
    width: 100%%;
    max-width: none;
  }
}
</style>
</head>
<body>
<div class="container">
`

const htmlPageEnd = `
<p class="small">Source code for this page located on <a href="https://github.com/openshift/release-controller">github</a></p>
</div>
</body>
</html>
`

const releasePageHtml = `
<h1>Release Status</h1>
<p>Visualize upgrades in <a href="/graph">Cincinnati</a> | <a href="/graph?format=dot">dot</a> | <a href="/graph?format=svg">SVG</a> | <a href="/graph?format=png">PNG</a> format. Run the following command to make this your update server:</p>
<pre class="ml-4">
oc patch clusterversion/version --patch '{"spec":{"upstream":"{{ .BaseURL }}graph"}}' --type=merge
</pre>
<style>
.upgrade-track-line {
	position: absolute;
	top: 0;
	bottom: -1px;
	left: 7px;
	width: 0;
	display: inline-block;
	border-left: 2px solid #000;
	display: none;
	z-index: 200;
}
.upgrade-track-dot {
	display: inline-block;
	position: absolute;
	top: 15px;
	left: 2px;
	width: 12px;
	height: 12px;
	background: #fff;
	z-index: 300;
	cursor: pointer;
}
.upgrade-track-dot {
	border: 2px solid #000;
	border-radius: 50%;
}
.upgrade-track-dot:hover {
	border-width: 6px;
}
.upgrade-track-line.start {
	top: 18px;
	height: 31px;
	display: block;
}
.upgrade-track-line.middle {
	display: block;
}
.upgrade-track-line.end {
	top: -1px;
	height: 16px;
	display: block;
}
td.upgrade-track {
	width: 16px;
	position: relative;
	padding-left: 2px;
	padding-right: 2px;
}
</style>

<p class="small mb-3">
	Jump to: {{ releaseJoin .Streams }}
</p>

<div class="row">
<div class="col">
{{ range .Streams }}
		<h2 title="From image stream {{ .Release.Source.Namespace }}/{{ .Release.Source.Name }}"><a id="{{ .Release.Config.Name }}" href="#{{ .Release.Config.Name }}" class="text-dark">{{ .Release.Config.Name }}</a></h2>
		{{ publishDescription . }}
		{{ alerts . }}
		{{ $upgrades := .Upgrades }}
		<table class="table text-nowrap">
			<thead>
				<tr>
					<th title="The name and version of the release image (as well as the tag it is published under)">Name</th>
					<th title="The release moves through these stages:&#10;&#10;Pending - still creating release image&#10;Ready - release image created&#10;Accepted - all tests pass&#10;Rejected - some tests failed&#10;Failed - Could not create release image">Phase</th>
					<th>Started</th>
					<th title="Tests that failed or are still pending on releases. See release page for more.">Failures</th>
					<th colspan="{{ inc $upgrades.Width }}">Upgrades</th>
				</tr>
			</thead>
			<tbody>
		{{ $release := .Release }}
		{{ if .Delayed }}
			<tr>
				<td colspan="4"><em>{{ .Delayed.Message }}</em></td>
				<td colspan="{{ inc $upgrades.Width }}"></td>
			</tr>
		{{ end }}
		{{ range $index, $tag := .Tags }}
			{{ $created := index .Annotations "release.openshift.io/creationTimestamp" }}
			<tr>
				{{ tableLink $release.Config . }}
				{{ phaseCell . }}
				<td title="{{ $created }}">{{ since $created }}</td>
				<td>{{ links . $release }}</td>
				{{ upgradeCells $upgrades $index }}
			</tr>
		{{ end }}
			</tbody>
		</table>
{{ end }}
</div>
</div>
`

const releaseInfoPageHtml = `
<h1>{{ .Tag.Name }}</h1>
{{ $created := index .Tag.Annotations "release.openshift.io/creationTimestamp" }}
<p>Created: <span>{{ since $created }}</span></p>
`

const releaseDashboardPageHtml = `
<h1>Release Dashboard</h1>
<p><a href=https://bugzilla.redhat.com/buglist.cgi?bug_status=NEW&bug_status=ASSIGNED&bug_status=POST&f1=cf_internal_whiteboard&f2=status_whiteboard&j_top=OR&known_name=BuildCop&list_id=10913331&o1=substring&o2=substring&query_format=advanced&v1=buildcop&v2=buildcop>Open Build Cop Bugs</a></p>
<p class="small mb-3">
	Jump to: {{ releaseJoin .Streams }}
</p>
<div class="row">
<div class="col">
{{ range .Streams }}
		{{ if ne .Release.Config.Name "4-stable" }}
			<h2 title="From image stream {{ .Release.Source.Namespace }}/{{ .Release.Source.Name }}"><a id="{{ .Release.Config.Name }}" href="#{{ .Release.Config.Name }}" class="text-dark">{{ .Release.Config.Name }}</a></h2>
			{{ publishDescription . }}
			{{ $upgrades := .Upgrades }}
			<table class="table text-nowrap">
				<thead>
					<tr>
						<th title="The name and version of the release image (as well as the tag it is published under)">Name</th>
						<th title="The release moves through these stages:&#10;&#10;Pending - still creating release image&#10;Ready - release image created&#10;Accepted - all tests pass&#10;Rejected - some tests failed&#10;Failed - Could not create release image">Phase</th>
						<th>Started</th>
						<th colspan="1">Successful<br>Upgrades</th>
						<th colspan="1">Running<br>Upgrades</th>
						<th colspan="1">Failed<br>Upgrade From</th>
					</tr>
				</thead>
				<tbody>
			{{ $release := .Release }}
			{{ if .Delayed }}
				<tr>
					<td colspan="4"><em>{{ .Delayed.Message }}</em></td>
					<td colspan="{{ inc $upgrades.Width }}"></td>
				</tr>
			{{ end }}
			{{ if .Failing }}
				<div class="alert alert-danger">This release has no recently accepted payloads, investigation required.</div>
			{{ end }}
			{{ range $index, $tag := .Tags }}
				{{ if lt $index 10 }}
						{{ $created := index .Annotations "release.openshift.io/creationTimestamp" }}
						<tr>
							{{ tableLink $release.Config . }}
							{{ phaseCell . }}
							<td title="{{ $created }}">{{ since $created }}</td>
							{{ upgradeJobs $upgrades $index $created }}  				    
						</tr>
				{{end}}
			{{ end }}
				</tbody>
			</table>
		{{ end }}
{{ end }}
</div>
</div>
`

var (
	reInternalLink = regexp.MustCompile(`<a href="[^"]+">`)
	rePromotedFrom = regexp.MustCompile("Promoted from (.*):(.*)")
	reRHCoSDiff    = regexp.MustCompile(`\* Red Hat Enterprise Linux CoreOS upgraded from ((\d)(\d+)\.[\w\.\-]+) to ((\d)(\d+)\.[\w\.\-]+)\n`)
	reRHCoSVersion = regexp.MustCompile(`\* Red Hat Enterprise Linux CoreOS ((\d)(\d+)\.[\w\.\-]+)\n`)
)

func (c *Controller) findReleaseStreamTags(includeStableTags bool, tags ...string) (map[string]*ReleaseStreamTag, bool) {
	needed := make(map[string]*ReleaseStreamTag)
	for _, tag := range tags {
		if len(tag) == 0 {
			continue
		}
		needed[tag] = nil
	}
	remaining := len(needed)

	imageStreams, err := c.imageStreamLister.ImageStreams(c.releaseNamespace).List(labels.Everything())
	if err != nil {
		return nil, false
	}

	var stable *StableReferences
	if includeStableTags {
		stable = &StableReferences{}
	}

	for _, stream := range imageStreams {
		r, ok, err := c.releaseDefinition(stream)
		if err != nil || !ok {
			continue
		}
		// TODO: should be refactored to be unsortedSemanticReleaseTags
		releaseTags := sortedReleaseTags(r)
		if includeStableTags {
			if version, err := semverParseTolerant(r.Config.Name); err == nil || r.Config.As == releaseConfigModeStable {
				stable.Releases = append(stable.Releases, StableRelease{
					Release:  r,
					Version:  version,
					Versions: NewSemanticVersions(releaseTags),
				})
			}
		}
		if includeStableTags && remaining == 0 {
			continue
		}
		for i, tag := range releaseTags {
			if needs, ok := needed[tag.Name]; ok && needs == nil {
				needed[tag.Name] = &ReleaseStreamTag{
					Release:         r,
					Tag:             tag,
					Previous:        findPreviousRelease(tag, releaseTags[i+1:], r),
					PreviousRelease: r,
					Older:           releaseTags[i+1:],
					Stable:          stable,
				}
				remaining--
				if !includeStableTags && remaining == 0 {
					return needed, true
				}
			}
		}
	}
	if includeStableTags {
		sort.Sort(stable.Releases)
	}
	return needed, remaining == 0
}

// semverParseTolerant works around https://github.com/blang/semver/issues/55 until
// it is resolved.
func semverParseTolerant(v string) (semver.Version, error) {
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

func (c *Controller) userInterfaceHandler() http.Handler {
	mux := mux.NewRouter()
	mux.HandleFunc("/graph", c.graphHandler)
	mux.HandleFunc("/changelog", c.httpReleaseChangelog)
	mux.HandleFunc("/releasetag/{tag}/json", c.httpReleaseInfoJson)
	mux.HandleFunc("/archive/graph", c.httpGraphSave)
	mux.HandleFunc("/api/v1/releasestream/{release}/tags", c.apiReleaseTags)
	mux.HandleFunc("/api/v1/releasestream/{release}/latest", c.apiReleaseLatest)
	mux.HandleFunc("/releasetag/{tag}", c.httpReleaseInfo)
	mux.HandleFunc("/releasestream/{release}/release/{tag}", c.httpReleaseInfo)
	mux.HandleFunc("/releasestream/{release}/release/{tag}/download", c.httpReleaseInfoDownload)
	mux.HandleFunc("/releasestream/{release}/latest", c.httpReleaseLatest)
	mux.HandleFunc("/releasestream/{release}/latest/download", c.httpReleaseLatestDownload)
	mux.HandleFunc("/api/v1/releasestream/{release}/candidate", c.apiReleaseCandidate)
	mux.HandleFunc("/releasestream/{release}/candidates", c.httpReleaseCandidateList)
	mux.HandleFunc("/", c.httpReleases)
	mux.HandleFunc("/dashboards/overview", c.httpDashboardOverview)
	return mux
}

func (c *Controller) urlForArtifacts(tagName string) (string, bool) {
	if len(c.artifactsHost) == 0 {
		return "", false
	}
	return fmt.Sprintf("https://%s/%s", c.artifactsHost, url.PathEscape(tagName)), true
}

func (c *Controller) locateLatest(w http.ResponseWriter, req *http.Request) (*Release, *imagev1.TagReference, bool) {
	vars := mux.Vars(req)
	streamName := vars["release"]
	var constraint semver.Range
	if inString := req.URL.Query().Get("in"); len(inString) > 0 {
		r, err := semver.ParseRange(inString)
		if err != nil {
			http.Error(w, fmt.Sprintf("error: ?in must be a valid semantic version range: %v", err), http.StatusBadRequest)
			return nil, nil, false
		}
		constraint = r
	}
	var relativeIndex int
	if relativeIndexString := req.URL.Query().Get("rel"); len(relativeIndexString) > 0 {
		i, err := strconv.Atoi(relativeIndexString)
		if err != nil {
			http.Error(w, fmt.Sprintf("error: ?rel must be non-negative integer: %v", err), http.StatusBadRequest)
			return nil, nil, false
		}
		if i < 0 {
			http.Error(w, "error: ?rel must be non-negative integer", http.StatusBadRequest)
			return nil, nil, false
		}
		relativeIndex = i
	}

	r, latest, err := c.latestForStream(streamName, constraint, relativeIndex)
	if err != nil {
		code := http.StatusInternalServerError
		if err == errStreamNotFound || err == errStreamTagNotFound {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return nil, nil, false
	}
	return r, latest, true
}

func (c *Controller) apiReleaseLatest(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { glog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	r, latest, ok := c.locateLatest(w, req)
	if !ok {
		return
	}

	downloadURL, _ := c.urlForArtifacts(latest.Name)
	resp := APITag{
		Name:        latest.Name,
		PullSpec:    findPublicImagePullSpec(r.Target, latest.Name),
		DownloadURL: downloadURL,
		Phase:       latest.Annotations[releaseAnnotationPhase],
	}

	switch req.URL.Query().Get("format") {
	case "pullSpec":
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, resp.PullSpec)
	case "downloadURL":
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, resp.DownloadURL)
	case "name":
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, resp.Name)
	case "", "json":
		data, err := json.MarshalIndent(&resp, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		fmt.Fprintln(w)
	default:
		http.Error(w, fmt.Sprintf(("error: Must specify one of '', 'json', 'pullSpec', 'name', or 'downloadURL")), http.StatusBadRequest)
	}
}

func (c *Controller) locateStream(streamName string, phases ...string) (*ReleaseStream, error) {
	imageStreams, err := c.imageStreamLister.ImageStreams(c.releaseNamespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	for _, stream := range imageStreams {
		r, ok, err := c.releaseDefinition(stream)
		if err != nil || !ok {
			continue
		}
		if r.Config.Name != streamName {
			continue
		}
		// find all accepted tags, then sort by semantic version
		tags := unsortedSemanticReleaseTags(r, phases...)
		sort.Sort(tags)
		return &ReleaseStream{
			Release: r,
			Tags:    tags.Tags(),
		}, nil
	}
	return nil, errStreamNotFound

}

func (c *Controller) apiReleaseTags(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { glog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	vars := mux.Vars(req)
	streamName := vars["release"]

	filterPhase := req.URL.Query()["phase"]

	r, err := c.locateStream(streamName)
	if err != nil {
		if err == errStreamNotFound {
			http.Error(w, fmt.Sprintf("Unable to find release %s", streamName), http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf("Unable to find release %s: %v", streamName, err), http.StatusInternalServerError)
		}
		return
	}

	var tags []APITag
	for _, tag := range r.Tags {
		downloadURL, _ := c.urlForArtifacts(tag.Name)
		phase := tag.Annotations[releaseAnnotationPhase]
		if len(filterPhase) > 0 && !containsString(filterPhase, phase) {
			continue
		}
		tags = append(tags, APITag{
			Name:        tag.Name,
			PullSpec:    findPublicImagePullSpec(r.Release.Target, tag.Name),
			DownloadURL: downloadURL,
			Phase:       phase,
		})
	}

	resp := APIRelease{
		Name: r.Release.Config.Name,
		Tags: tags,
	}

	switch req.URL.Query().Get("format") {
	case "", "json":
		data, err := json.MarshalIndent(&resp, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		fmt.Fprintln(w)
	default:
		http.Error(w, fmt.Sprintf(("error: Must specify one of '', 'json', 'pullSpec', 'name', or 'downloadURL")), http.StatusBadRequest)
	}
}

func (c *Controller) httpGraphSave(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { glog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
	if err := c.graph.Save(w); err != nil {
		http.Error(w, fmt.Sprintf("unable to save graph: %v", err), http.StatusInternalServerError)
	}
}

func (c *Controller) httpReleaseChangelog(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { glog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	var isHtml bool
	switch req.URL.Query().Get("format") {
	case "html":
		isHtml = true
	case "markdown", "":
	default:
		http.Error(w, fmt.Sprintf("unrecognized format= string: html, markdown, empty accepted"), http.StatusBadRequest)
		return
	}

	from := req.URL.Query().Get("from")
	if len(from) == 0 {
		http.Error(w, fmt.Sprintf("from must be set to a valid tag"), http.StatusBadRequest)
		return
	}
	to := req.URL.Query().Get("to")
	if len(to) == 0 {
		http.Error(w, fmt.Sprintf("to must be set to a valid tag"), http.StatusBadRequest)
		return
	}

	tags, ok := c.findReleaseStreamTags(false, from, to)
	if !ok {
		for k, v := range tags {
			if v == nil {
				http.Error(w, fmt.Sprintf("could not find tag: %s", k), http.StatusBadRequest)
				return
			}
		}
	}

	fromBase := tags[from].Release.Target.Status.PublicDockerImageRepository
	if len(fromBase) == 0 {
		http.Error(w, fmt.Sprintf("release target %s does not have a configured registry", tags[from].Release.Target.Name), http.StatusBadRequest)
		return
	}
	toBase := tags[to].Release.Target.Status.PublicDockerImageRepository
	if len(toBase) == 0 {
		http.Error(w, fmt.Sprintf("release target %s does not have a configured registry", tags[to].Release.Target.Name), http.StatusBadRequest)
		return
	}

	out, err := c.releaseInfo.ChangeLog(fromBase+":"+from, toBase+":"+to)
	if err != nil {
		http.Error(w, fmt.Sprintf("Internal error\n%v", err), http.StatusInternalServerError)
		return
	}

	if isHtml {
		result := blackfriday.Run([]byte(out))
		w.Header().Set("Content-Type", "text/html;charset=UTF-8")
		fmt.Fprintf(w, htmlPageStart, template.HTMLEscapeString(fmt.Sprintf("Change log for %s", to)))
		w.Write(result)
		fmt.Fprintln(w, htmlPageEnd)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintln(w, out)
}

func (c *Controller) httpReleaseInfoJson(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { glog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	vars := mux.Vars(req)
	tag := vars["tag"]
	if len(tag) == 0 {
		http.Error(w, fmt.Sprintf("tag must be specified"), http.StatusBadRequest)
		return
	}

	tags, ok := c.findReleaseStreamTags(false, tag)
	if !ok {
		http.Error(w, fmt.Sprintf("could not find tag: %s", tag), http.StatusBadRequest)
		return
	}

	tagPullSpec := findPublicImagePullSpec(tags[tag].Release.Target, tag)
	if len(tagPullSpec) == 0 {
		http.Error(w, fmt.Sprintf("could not find pull spec for tag %s in image stream %s", tag, tags[tag].Release.Target.Name), http.StatusBadRequest)
		return
	}

	out, err := c.releaseInfo.ReleaseInfo(tagPullSpec)
	if err != nil {
		http.Error(w, fmt.Sprintf("Internal error: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, out)
}

func (c *Controller) httpReleaseInfoDownload(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { glog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	vars := mux.Vars(req)
	release := vars["release"]
	tag := vars["tag"]

	tags, ok := c.findReleaseStreamTags(true, tag)
	if !ok {
		http.Error(w, fmt.Sprintf("Unable to find release tag %s, it may have been deleted", tag), http.StatusNotFound)
		return
	}

	info := tags[tag]
	if len(release) > 0 && info.Release.Config.Name != release {
		http.Error(w, fmt.Sprintf("Release tag %s does not belong to release %s", tag, release), http.StatusNotFound)
		return
	}

	u, ok := c.urlForArtifacts(tag)
	if !ok {
		http.Error(w, "No artifacts download URL is configured, cannot show download link", http.StatusNotFound)
		return
	}
	http.Redirect(w, req, u, http.StatusFound)
}

func (c *Controller) httpReleaseInfo(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { glog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	vars := mux.Vars(req)
	release := vars["release"]
	tag := vars["tag"]
	from := req.URL.Query().Get("from")

	tags, ok := c.findReleaseStreamTags(true, tag, from)
	if !ok {
		http.Error(w, fmt.Sprintf("Unable to find release tag %s, it may have been deleted", tag), http.StatusNotFound)
		return
	}

	info := tags[tag]
	if len(release) > 0 && info.Release.Config.Name != release {
		http.Error(w, fmt.Sprintf("Release tag %s does not belong to release %s", tag, release), http.StatusNotFound)
		return
	}

	if previous := tags[from]; previous != nil {
		info.Previous = previous.Tag
		info.PreviousRelease = previous.Release
	}
	if info.Previous == nil && len(info.Older) > 0 {
		info.Previous = info.Older[0]
		info.PreviousRelease = info.Release
	}
	if info.Previous == nil {
		if version, err := semver.Parse(info.Tag.Name); err == nil {
			for _, release := range info.Stable.Releases {
				if release.Version.Major == version.Major && release.Version.Minor == version.Minor && len(release.Versions) > 0 {
					info.Previous = release.Versions[0].Tag
					info.PreviousRelease = release.Release
					break
				}
			}
		}
	}

	// require public pull specs because we can't get the x509 cert for the internal registry without service-ca.crt
	tagPull := findPublicImagePullSpec(info.Release.Target, info.Tag.Name)
	var previousTagPull string
	if info.Previous != nil {
		previousTagPull = findPublicImagePullSpec(info.PreviousRelease.Target, info.Previous.Name)
	}
	mirror, _ := c.getMirror(info.Release, info.Tag.Name)

	flusher, ok := w.(http.Flusher)
	if !ok {
		flusher = nopFlusher{}
	}

	w.Header().Set("Content-Type", "text/html;charset=UTF-8")
	fmt.Fprintf(w, htmlPageStart, template.HTMLEscapeString(fmt.Sprintf("Release %s", tag)))
	defer func() { fmt.Fprintln(w, htmlPageEnd) }()

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
	fmt.Fprintf(w, "<h1>%s</h1>\n", template.HTMLEscapeString(tag))

	switch info.Tag.Annotations[releaseAnnotationPhase] {
	case releasePhaseFailed:
		fmt.Fprintf(w, `<div class="alert alert-danger"><p>%s</p>`, template.HTMLEscapeString(info.Tag.Annotations[releaseAnnotationMessage]))
		if log := info.Tag.Annotations[releaseAnnotationLog]; len(log) > 0 {
			fmt.Fprintf(w, `<pre class="small">%s</pre>`, template.HTMLEscapeString(log))
		} else {
			fmt.Fprintf(w, `<div><em>No failure log was captured</em></div>`)
		}
		fmt.Fprintf(w, `</div>`)
		return
	}

	renderInstallInstructions(w, mirror, info.Tag, tagPull, c.artifactsHost)

	renderVerifyLinks(w, *info.Tag, info.Release)

	if upgradesTo := c.graph.UpgradesTo(tag); len(upgradesTo) > 0 {
		sort.Sort(newNewestSemVerFromSummaries(upgradesTo))
		fmt.Fprintf(w, `<p id="upgrades-from">Upgrades from:</p><ul>`)
		for _, upgrade := range upgradesTo {
			var style string
			switch {
			case upgrade.Success == 0 && upgrade.Failure > 0:
				style = "text-danger"
			case upgrade.Success > 0:
				style = "text-success"
			}

			fmt.Fprintf(w, `<li><a class="text-monospace %s" href="/releasetag/%s">%s</a>`, style, upgrade.From, upgrade.From)
			if info.Previous == nil || upgrade.From != info.Previous.Name {
				fmt.Fprintf(w, ` (<a href="?from=%s">changes</a>)`, upgrade.From)
			}
			if upgrade.Total > 0 {
				fmt.Fprintf(w, ` - `)
				urls := make([]string, 0, len(upgrade.History))
				for url := range upgrade.History {
					urls = append(urls, url)
				}
				sort.Strings(urls)
				if len(urls) > 2 {
					for _, url := range urls {
						switch upgrade.History[url].State {
						case releaseVerificationStateSucceeded:
							fmt.Fprintf(w, ` <a class="text-success" href="%s">S</a>`, template.HTMLEscapeString(url))
						case releaseVerificationStateFailed:
							fmt.Fprintf(w, ` <a class="text-danger" href="%s">F</a>`, template.HTMLEscapeString(url))
						default:
							fmt.Fprintf(w, ` <a class="" href="%s">P</a>`, template.HTMLEscapeString(url))
						}
					}
				} else {
					for _, url := range urls {
						switch upgrade.History[url].State {
						case releaseVerificationStateSucceeded:
							fmt.Fprintf(w, ` <a class="text-success" href="%s">Success</a>`, template.HTMLEscapeString(url))
						case releaseVerificationStateFailed:
							fmt.Fprintf(w, ` <a class="text-danger" href="%s">Failed</a>`, template.HTMLEscapeString(url))
						default:
							fmt.Fprintf(w, ` <a class="" href="%s">Pending</a>`, template.HTMLEscapeString(url))
						}
					}
				}
			}
		}
		fmt.Fprintf(w, `</ul>`)
	}

	if upgradesFrom := c.graph.UpgradesFrom(tag); len(upgradesFrom) > 0 {
		sort.Sort(newNewestSemVerToSummaries(upgradesFrom))
		fmt.Fprintf(w, `<p id="upgrades-to">Upgrades to:</p><ul>`)
		for _, upgrade := range upgradesFrom {
			var style string
			switch {
			case upgrade.Success == 0 && upgrade.Failure > 0:
				style = "text-danger"
			case upgrade.Success > 0:
				style = "text-success"
			}

			fmt.Fprintf(w, `<li><a class="text-monospace %s" href="/releasetag/%s">%s</a>`, style, template.HTMLEscapeString(upgrade.To), upgrade.To)
			fmt.Fprintf(w, ` (<a href="/releasetag/%s">changes</a>)`, template.HTMLEscapeString((&url.URL{Path: upgrade.To, RawQuery: url.Values{"from": []string{upgrade.From}}.Encode()}).String()))
			if upgrade.Total > 0 {
				fmt.Fprintf(w, ` - `)
				urls := make([]string, 0, len(upgrade.History))
				for url := range upgrade.History {
					urls = append(urls, url)
				}
				sort.Strings(urls)
				if len(urls) > 2 {
					for _, url := range urls {
						switch upgrade.History[url].State {
						case releaseVerificationStateSucceeded:
							fmt.Fprintf(w, ` <a class="text-success" href="%s">S</a>`, template.HTMLEscapeString(url))
						case releaseVerificationStateFailed:
							fmt.Fprintf(w, ` <a class="text-danger" href="%s">F</a>`, template.HTMLEscapeString(url))
						default:
							fmt.Fprintf(w, ` <a class="" href="%s">P</a>`, template.HTMLEscapeString(url))
						}
					}
				} else {
					for _, url := range urls {
						switch upgrade.History[url].State {
						case releaseVerificationStateSucceeded:
							fmt.Fprintf(w, ` <a class="text-success" href="%s">Success</a>`, template.HTMLEscapeString(url))
						case releaseVerificationStateFailed:
							fmt.Fprintf(w, ` <a class="text-danger" href="%s">Failed</a>`, template.HTMLEscapeString(url))
						default:
							fmt.Fprintf(w, ` <a class="" href="%s">Pending</a>`, template.HTMLEscapeString(url))
						}
					}
				}
			}
		}
		fmt.Fprintf(w, `</ul>`)
	}

	if info.Previous != nil && len(previousTagPull) > 0 && len(tagPull) > 0 {
		fmt.Fprintln(w, "<hr>")
		flusher.Flush()

		type renderResult struct {
			out string
			err error
		}
		ch := make(chan renderResult)

		// run the changelog in a goroutine because it may take significant time
		go func() {
			out, err := c.releaseInfo.ChangeLog(previousTagPull, tagPull)
			if err != nil {
				ch <- renderResult{err: err}
				return
			}

			// replace references to the previous version with links
			rePrevious, err := regexp.Compile(fmt.Sprintf(`([^\w:])%s(\W)`, regexp.QuoteMeta(info.Previous.Name)))
			if err != nil {
				ch <- renderResult{err: err}
				return
			}
			// do a best effort replacement to change out the headers
			out = strings.Replace(out, fmt.Sprintf(`# %s`, info.Tag.Name), "", -1)
			if changed := strings.Replace(out, fmt.Sprintf(`## Changes from %s`, info.Previous.Name), "", -1); len(changed) != len(out) {
				out = fmt.Sprintf("## Changes from %s\n%s", info.Previous.Name, changed)
			}
			out = rePrevious.ReplaceAllString(out, fmt.Sprintf("$1[%s](/releasetag/%s)$2", info.Previous.Name, info.Previous.Name))

			// add link to tag from which current version promoted from
			out = rePromotedFrom.ReplaceAllString(out, fmt.Sprintf("Release %s was created from [$1:$2](/releasetag/$2)", info.Tag.Name))

			// TODO: As we get more comfortable with these sorts of transformations, we could make them more generic.
			//       For now, this will have to do.
			if m := reRHCoSDiff.FindStringSubmatch(out); m != nil {
				fromRelease := m[1]
				fromStream := fmt.Sprintf("releases/rhcos-%s.%s", m[2], m[3])
				fromURL := url.URL{
					Scheme: "https",
					Host:   "releases-rhcos-art.cloud.privileged.psi.redhat.com",
					Path:   "/",
					RawQuery: (url.Values{
						"stream":  []string{fromStream},
						"release": []string{fromRelease},
					}).Encode(),
				}
				toRelease := m[4]
				toStream := fmt.Sprintf("releases/rhcos-%s.%s", m[5], m[6])
				toURL := url.URL{
					Scheme: "https",
					Host:   "releases-rhcos-art.cloud.privileged.psi.redhat.com",
					Path:   "/",
					RawQuery: (url.Values{
						"stream":  []string{toStream},
						"release": []string{toRelease},
					}).Encode(),
				}
				diffURL := url.URL{
					Scheme: "https",
					Host:   "releases-rhcos-art.cloud.privileged.psi.redhat.com",
					Path:   "/diff.html",
					RawQuery: (url.Values{
						"first_stream":   []string{fromStream},
						"first_release":  []string{fromRelease},
						"second_stream":  []string{toStream},
						"second_release": []string{toRelease},
					}).Encode(),
				}
				replace := fmt.Sprintf(
					`* Red Hat Enterprise Linux CoreOS upgraded from [%s](%s) to [%s](%s) ([diff](%s))`+"\n",
					fromRelease,
					fromURL.String(),
					toRelease,
					toURL.String(),
					diffURL.String(),
				)
				out = strings.ReplaceAll(out, m[0], replace)
			}
			if m := reRHCoSVersion.FindStringSubmatch(out); m != nil {
				fromRelease := m[1]
				fromStream := fmt.Sprintf("releases/rhcos-%s.%s", m[2], m[3])
				fromURL := url.URL{
					Scheme: "https",
					Host:   "releases-rhcos-art.cloud.privileged.psi.redhat.com",
					Path:   "/",
					RawQuery: (url.Values{
						"stream":  []string{fromStream},
						"release": []string{fromRelease},
					}).Encode(),
				}
				replace := fmt.Sprintf(
					`* Red Hat Enterprise Linux CoreOS [%s](%s)`+"\n",
					fromRelease,
					fromURL.String(),
				)
				out = strings.ReplaceAll(out, m[0], replace)
			}
			ch <- renderResult{out: out}
		}()

		var render renderResult
		select {
		case render = <-ch:
		case <-time.After(500 * time.Millisecond):
			fmt.Fprintf(w, `<p id="loading" class="alert alert-info">Loading changelog, this may take a while ...</p>`)
			flusher.Flush()
			select {
			case render = <-ch:
			case <-time.After(15 * time.Second):
				render.err = fmt.Errorf("the changelog is still loading, if this is the first access it may take several minutes to clone all repositories")
			}
			fmt.Fprintf(w, `<style>#loading{display: none;}</style>`)
			flusher.Flush()
		}
		if render.err == nil {
			result := blackfriday.Run([]byte(render.out))
			// make our links targets
			result = reInternalLink.ReplaceAllFunc(result, func(s []byte) []byte {
				return []byte(`<a target="_blank" ` + string(bytes.TrimPrefix(s, []byte("<a "))))
			})
			w.Write(result)
			fmt.Fprintln(w, "<hr>")
		} else {
			// if we don't get a valid result within limits, just show the simpler informational view
			fmt.Fprintf(w, `<p class="alert alert-danger">%s</p>`, fmt.Sprintf("Unable to show full changelog: %s", render.err))
		}
	}

	var options []string
	for _, tag := range info.Older {
		var selected string
		if tag.Name == info.Previous.Name {
			selected = `selected="true"`
		}
		options = append(options, fmt.Sprintf(`<option %s>%s</option>`, selected, tag.Name))
	}
	for _, release := range info.Stable.Releases {
		if release.Release == info.Release {
			continue
		}
		for j, version := range release.Versions {
			if j == 0 && len(options) > 0 {
				options = append(options, `<option disabled>───</option>`)
			}
			var selected string
			if info.Previous != nil && version.Tag.Name == info.Previous.Name {
				selected = `selected="true"`
			}
			options = append(options, fmt.Sprintf(`<option %s>%s</option>`, selected, version.Tag.Name))
		}
	}
	if len(options) > 0 {
		fmt.Fprint(w, `<p><form class="form-inline" method="GET">`)
		if info.Previous != nil {
			fmt.Fprintf(w, `<a href="/changelog?from=%s&to=%s">View changelog in Markdown</a><span>&nbsp;or&nbsp;</span><label for="from">change previous release:&nbsp;</label>`, info.Previous.Name, info.Tag.Name)
		} else {
			fmt.Fprint(w, `<label for="from">change previous release:&nbsp;</label>`)
		}
		fmt.Fprintf(w, `<select onchange="this.form.submit()" id="from" class="form-control" name="from">%s</select> <input class="btn btn-link" type="submit" value="Compare">`, strings.Join(options, ""))
		fmt.Fprint(w, `</form></p>`)
	}
}

var (
	errStreamNotFound    = fmt.Errorf("no release configuration exists with the requested name")
	errStreamTagNotFound = fmt.Errorf("no tags exist within the release that satisfy the request")
)

func (c *Controller) latestForStream(streamName string, constraint semver.Range, relativeIndex int) (*Release, *imagev1.TagReference, error) {
	imageStreams, err := c.imageStreamLister.ImageStreams(c.releaseNamespace).List(labels.Everything())
	if err != nil {
		return nil, nil, err
	}
	for _, stream := range imageStreams {
		r, ok, err := c.releaseDefinition(stream)
		if err != nil || !ok {
			continue
		}
		if r.Config.Name != streamName {
			continue
		}
		// find all accepted tags, then sort by semantic version
		tags := unsortedSemanticReleaseTags(r, releasePhaseAccepted)
		sort.Sort(tags)
		for _, ver := range tags {
			if constraint != nil && (ver.Version == nil || !constraint(*ver.Version)) {
				continue
			}
			if relativeIndex > 0 {
				relativeIndex--
				continue
			}
			return r, ver.Tag, nil
		}
		return nil, nil, errStreamTagNotFound
	}
	return nil, nil, errStreamNotFound
}

func (c *Controller) httpReleaseLatest(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { glog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	r, latest, ok := c.locateLatest(w, req)
	if !ok {
		return
	}

	http.Redirect(w, req, fmt.Sprintf("/releasestream/%s/release/%s", url.PathEscape(r.Config.Name), url.PathEscape(latest.Name)), http.StatusFound)
}

func (c *Controller) httpReleaseLatestDownload(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { glog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	_, latest, ok := c.locateLatest(w, req)
	if !ok {
		return
	}

	u, ok := c.urlForArtifacts(latest.Name)
	if !ok {
		http.Error(w, "No artifacts download URL is configured, cannot show download link", http.StatusNotFound)
		return
	}
	http.Redirect(w, req, u, http.StatusFound)
}

func (c *Controller) httpReleases(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { glog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	w.Header().Set("Content-Type", "text/html;charset=UTF-8")

	base := *req.URL
	base.Scheme = "http"
	if p := req.Header.Get("X-Forwarded-Proto"); len(p) > 0 {
		base.Scheme = p
	}
	base.Host = req.Host
	base.Path = "/"
	base.RawQuery = ""
	base.Fragment = ""
	page := &ReleasePage{
		BaseURL: base.String(),
	}

	now := time.Now()
	var releasePage = template.Must(template.New("releasePage").Funcs(
		template.FuncMap{
			"publishSpec": func(r *ReleaseStream) string {
				if len(r.Release.Target.Status.PublicDockerImageRepository) > 0 {
					for _, target := range r.Release.Config.Publish {
						if target.TagRef != nil && len(target.TagRef.Name) > 0 {
							return r.Release.Target.Status.PublicDockerImageRepository + ":" + target.TagRef.Name
						}
					}
				}
				return ""
			},
			"publishDescription": func(r *ReleaseStream) string {
				if len(r.Release.Config.Message) > 0 {
					return fmt.Sprintf("<p>%s</p>\n", r.Release.Config.Message)
				}
				var out []string
				switch r.Release.Config.As {
				case releaseConfigModeStable:
					if len(r.Release.Config.Message) == 0 {
						out = append(out, fmt.Sprintf(`<span>stable tags</span>`))
					}
				default:
					out = append(out, fmt.Sprintf(`<span>updated when <code>%s/%s</code> changes</span>`, r.Release.Source.Namespace, r.Release.Source.Name))
				}

				if len(r.Release.Target.Status.PublicDockerImageRepository) > 0 {
					for _, target := range r.Release.Config.Publish {
						if target.Disabled {
							continue
						}
						if target.TagRef != nil && len(target.TagRef.Name) > 0 {
							out = append(out, fmt.Sprintf(`<span>promote to pull spec <code>%s:%s</code></span>`, r.Release.Target.Status.PublicDockerImageRepository, target.TagRef.Name))
						}
					}
				}
				for _, target := range r.Release.Config.Publish {
					if target.Disabled {
						continue
					}
					if target.ImageStreamRef != nil {
						ns := target.ImageStreamRef.Namespace
						if len(ns) > 0 {
							ns += "/"
						}
						if len(target.ImageStreamRef.Tags) == 0 {
							out = append(out, fmt.Sprintf(`<span>promote to image stream <code>%s%s</code></span>`, ns, target.ImageStreamRef.Name))
						} else {
							var tagNames []string
							for _, tag := range target.ImageStreamRef.Tags {
								tagNames = append(tagNames, fmt.Sprintf("<code>%s</code>", template.HTMLEscapeString(tag)))
							}
							out = append(out, fmt.Sprintf(`<span>promote %s to image stream <code>%s%s</code></span>`, strings.Join(tagNames, "/"), ns, target.ImageStreamRef.Name))
						}
					}
				}
				if len(out) > 0 {
					sort.Strings(out)
					return fmt.Sprintf("<p>%s</p>\n", strings.Join(out, ", "))
				}
				return ""
			},
			"tableLink":    tableLink,
			"phaseCell":    phaseCell,
			"phaseAlert":   phaseAlert,
			"alerts":       renderAlerts,
			"links":        links,
			"releaseJoin":  releaseJoin,
			"inc":          func(i int) int { return i + 1 },
			"upgradeCells": upgradeCells,
			"since": func(utcDate string) string {
				t, err := time.Parse(time.RFC3339, utcDate)
				if err != nil {
					return ""
				}
				return relTime(t, now, "ago", "from now")
			},
		},
	).Parse(releasePageHtml))

	imageStreams, err := c.imageStreamLister.ImageStreams(c.releaseNamespace).List(labels.Everything())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, stream := range imageStreams {
		r, ok, err := c.releaseDefinition(stream)
		if err != nil || !ok {
			continue
		}
		s := ReleaseStream{
			Release: r,
			Tags:    sortedReleaseTags(r),
		}
		var delays []string
		if r.Config.As != releaseConfigModeStable && len(s.Tags) > 0 {
			if ok, _, queueAfter := isReleaseDelayedForInterval(r, s.Tags[0]); ok {
				delays = append(delays, fmt.Sprintf("waiting for %s", queueAfter.Truncate(time.Second)))
			}
			if r.Config.MaxUnreadyReleases > 0 && countUnreadyReleases(r, s.Tags) > r.Config.MaxUnreadyReleases {
				delays = append(delays, fmt.Sprintf("no more than %d pending", r.Config.MaxUnreadyReleases))
			}
		}
		if len(delays) > 0 {
			s.Delayed = &ReleaseDelay{Message: fmt.Sprintf("Next release may not start: %s", strings.Join(delays, ", "))}
		}
		s.Upgrades = calculateReleaseUpgrades(r, s.Tags, c.graph, false)
		if r.Config.As == releaseConfigModeStable {
			for i := range s.Upgrades.Tags {
				s.Upgrades.Tags[i].External = nil
			}
		}
		page.Streams = append(page.Streams, s)
	}

	sort.Sort(preferredReleases(page.Streams))
	checkReleasePage(page)

	fmt.Fprintf(w, htmlPageStart, "Release Status")
	if err := releasePage.Execute(w, page); err != nil {
		glog.Errorf("Unable to render page: %v", err)
	}
	fmt.Fprintln(w, htmlPageEnd)
}

func (c *Controller) httpDashboardOverview(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { glog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	w.Header().Set("Content-Type", "text/html;charset=UTF-8")

	base := *req.URL
	base.Scheme = "http"
	if p := req.Header.Get("X-Forwarded-Proto"); len(p) > 0 {
		base.Scheme = p
	}
	base.Host = req.Host
	base.Path = "/"
	base.RawQuery = ""
	base.Fragment = ""
	page := &ReleasePage{
		BaseURL: base.String(),
	}

	now := time.Now()
	var releasePage = template.Must(template.New("releaseDashboardPage").Funcs(
		template.FuncMap{
			"publishSpec": func(r *ReleaseStream) string {
				if len(r.Release.Target.Status.PublicDockerImageRepository) > 0 {
					for _, target := range r.Release.Config.Publish {
						if target.TagRef != nil && len(target.TagRef.Name) > 0 {
							return r.Release.Target.Status.PublicDockerImageRepository + ":" + target.TagRef.Name
						}
					}
				}
				return ""
			},
			"publishDescription": func(r *ReleaseStream) string {
				if len(r.Release.Config.Message) > 0 {
					return fmt.Sprintf("<p>%s</p>\n", r.Release.Config.Message)
				}
				var out []string
				switch r.Release.Config.As {
				case releaseConfigModeStable:
					if len(r.Release.Config.Message) == 0 {
						out = append(out, fmt.Sprintf(`<span>stable tags</span>`))
					}
				default:
					out = append(out, fmt.Sprintf(`<span>updated when <code>%s/%s</code> changes</span>`, r.Release.Source.Namespace, r.Release.Source.Name))
				}

				if len(r.Release.Target.Status.PublicDockerImageRepository) > 0 {
					for _, target := range r.Release.Config.Publish {
						if target.Disabled {
							continue
						}
						if target.TagRef != nil && len(target.TagRef.Name) > 0 {
							out = append(out, fmt.Sprintf(`<span>promote to pull spec <code>%s:%s</code></span>`, r.Release.Target.Status.PublicDockerImageRepository, target.TagRef.Name))
						}
					}
				}
				for _, target := range r.Release.Config.Publish {
					if target.Disabled {
						continue
					}
					if target.ImageStreamRef != nil {
						ns := target.ImageStreamRef.Namespace
						if len(ns) > 0 {
							ns += "/"
						}
						if len(target.ImageStreamRef.Tags) == 0 {
							out = append(out, fmt.Sprintf(`<span>promote to image stream <code>%s%s</code></span>`, ns, target.ImageStreamRef.Name))
						} else {
							var tagNames []string
							for _, tag := range target.ImageStreamRef.Tags {
								tagNames = append(tagNames, fmt.Sprintf("<code>%s</code>", template.HTMLEscapeString(tag)))
							}
							out = append(out, fmt.Sprintf(`<span>promote %s to image stream <code>%s%s</code></span>`, strings.Join(tagNames, "/"), ns, target.ImageStreamRef.Name))
						}
					}
				}
				if len(out) > 0 {
					sort.Strings(out)
					return fmt.Sprintf("<p>%s</p>\n", strings.Join(out, ", "))
				}
				return ""
			},
			"tableLink":   tableLink,
			"phaseCell":   phaseCell,
			"phaseAlert":  phaseAlert,
			"inc":         func(i int) int { return i + 1 },
			"upgradeJobs": upgradeJobs,
			"releaseJoin": releaseJoin,
			"since": func(utcDate string) string {
				t, err := time.Parse(time.RFC3339, utcDate)
				if err != nil {
					return ""
				}
				return relTime(t, now, "ago", "from now")
			},
		},
	).Parse(releaseDashboardPageHtml))

	imageStreams, err := c.imageStreamLister.ImageStreams(c.releaseNamespace).List(labels.Everything())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, stream := range imageStreams {
		r, ok, err := c.releaseDefinition(stream)
		if err != nil || !ok {
			continue
		}
		s := ReleaseStream{
			Release: r,
			Tags:    sortedReleaseTags(r),
		}
		var delays []string
		if r.Config.As != releaseConfigModeStable && len(s.Tags) > 0 {
			if ok, _, queueAfter := isReleaseDelayedForInterval(r, s.Tags[0]); ok {
				delays = append(delays, fmt.Sprintf("waiting for %s", queueAfter.Truncate(time.Second)))
			}
			if r.Config.MaxUnreadyReleases > 0 && countUnreadyReleases(r, s.Tags) > r.Config.MaxUnreadyReleases {
				delays = append(delays, fmt.Sprintf("no more than %d pending", r.Config.MaxUnreadyReleases))
			}
		}
		if isReleaseFailing(s.Tags, r.Config.MaxUnreadyReleases) {
			s.Failing = true
		}

		if len(delays) > 0 {
			s.Delayed = &ReleaseDelay{Message: fmt.Sprintf("Next release may not start: %s", strings.Join(delays, ", "))}
		}
		s.Upgrades = calculateReleaseUpgrades(r, s.Tags, c.graph, true)
		page.Streams = append(page.Streams, s)
	}

	sort.Sort(preferredReleases(page.Streams))
	checkReleasePage(page)

	fmt.Fprintf(w, htmlPageStart, "Release Status")
	if err := releasePage.Execute(w, page); err != nil {
		glog.Errorf("Unable to render page: %v", err)
	}
	fmt.Fprintln(w, htmlPageEnd)
}

func isReleaseFailing(tags []*imagev1.TagReference, maxUnready int) bool {
	for i := 0; i < maxUnready && i < len(tags); i++ {
		if tags[i].Annotations[releaseAnnotationPhase] == releasePhaseAccepted {
			return false
		}
	}
	return true
}

var extendedRelTime = []humanize.RelTimeMagnitude{
	{time.Second, "now", time.Second},
	{2 * time.Minute, "%d seconds %s", time.Second},
	{2 * time.Hour, "%d minutes %s", time.Minute},
	{2 * humanize.Day, "%d hours %s", time.Hour},
	{3 * humanize.Week, "%d days %s", humanize.Day},
	{3 * humanize.Month, "%d weeks %s", humanize.Week},
	{3 * humanize.Year, "%d months %s", humanize.Month},
	{math.MaxInt64, "a long while %s", 1},
}

func relTime(a, b time.Time, albl, blbl string) string {
	return humanize.CustomRelTime(a, b, albl, blbl, extendedRelTime)
}
