package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"k8s.io/apimachinery/pkg/util/sets"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	"github.com/blang/semver"
	imagev1 "github.com/openshift/api/image/v1"

	humanize "github.com/dustin/go-humanize"
	"github.com/gorilla/mux"
	"github.com/russross/blackfriday"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
)

//go:embed static
var static embed.FS
var resources, _ = fs.Sub(static, "static")

var htmlPageStart = loadStaticHTML("htmlPageStart.html")
var htmlPageEnd = loadStaticHTML("htmlPageEnd.html")

func (c *Controller) findReleaseStreamTags(includeStableTags bool, tags ...string) (map[string]*ReleaseStreamTag, bool) {
	needed := make(map[string]*ReleaseStreamTag)
	for _, tag := range tags {
		if len(tag) == 0 {
			continue
		}
		needed[tag] = nil
	}
	remaining := len(needed)

	imageStreams, err := c.releaseLister.List(labels.Everything())
	if err != nil {
		return nil, false
	}

	var stable *releasecontroller.StableReferences
	if includeStableTags {
		stable = &releasecontroller.StableReferences{}
	}

	for _, stream := range imageStreams {
		r, ok, err := releasecontroller.ReleaseDefinition(stream, c.parsedReleaseConfigCache, c.eventRecorder, *c.releaseLister)
		if err != nil || !ok {
			continue
		}
		// TODO: should be refactored to be unsortedSemanticReleaseTags
		releaseTags := releasecontroller.SortedReleaseTags(r)
		if includeStableTags {
			if version, err := releasecontroller.SemverParseTolerant(r.Config.Name); err == nil || r.Config.As == releasecontroller.ReleaseConfigModeStable {
				stable.Releases = append(stable.Releases, releasecontroller.StableRelease{
					Release:  r,
					Version:  version,
					Versions: releasecontroller.NewSemanticVersions(releaseTags),
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

func (c *Controller) endOfLifePrefixes() sets.String {
	imageStreams, err := c.releaseLister.List(labels.Everything())
	if err != nil {
		return nil
	}
	endOfLifePrefixes := sets.NewString()
	for _, stream := range imageStreams {
		r, ok, err := releasecontroller.ReleaseDefinition(stream, c.parsedReleaseConfigCache, c.eventRecorder, *c.releaseLister)
		if err != nil || !ok {
			continue
		}
		if r.Config.EndOfLife {
			if version, err := releasecontroller.SemverParseTolerant(r.Config.Name); err == nil {
				endOfLifePrefixes.Insert(fmt.Sprintf("%d.%d", version.Major, version.Minor))
			}
			continue
		}
	}
	return endOfLifePrefixes
}

func (c *Controller) userInterfaceHandler() http.Handler {
	mux := mux.NewRouter()
	mux.HandleFunc("/", c.httpReleases)
	mux.HandleFunc("/graph", c.graphHandler)
	mux.HandleFunc("/changelog", c.httpReleaseChangelog)
	mux.HandleFunc("/archive/graph", c.httpGraphSave)

	mux.HandleFunc("/releasetag/{tag}/json", c.httpReleaseInfoJson)
	mux.HandleFunc("/releasetag/{tag}", c.httpReleaseInfo)

	mux.HandleFunc("/releasestream/{release}/release/{tag}", c.httpReleaseInfo)
	mux.HandleFunc("/releasestream/{release}/release/{tag}/download", c.httpReleaseInfoDownload)
	mux.HandleFunc("/releasestream/{release}/inconsistency/{tag}", c.httpInconsistencyInfo)
	mux.HandleFunc("/releasestream/{release}/latest", c.httpReleaseLatest)
	mux.HandleFunc("/releasestream/{release}/latest/download", c.httpReleaseLatestDownload)
	mux.HandleFunc("/releasestream/{release}/candidates", c.httpReleaseCandidateList)

	mux.HandleFunc("/dashboards/overview", c.httpDashboardOverview)
	mux.HandleFunc("/dashboards/compare", c.httpDashboardCompare)

	// APIs
	mux.HandleFunc("/api/v1/releasestream/{release}/tags", c.apiReleaseTags)
	mux.HandleFunc("/api/v1/releasestream/{release}/latest", c.apiReleaseLatest)
	mux.HandleFunc("/api/v1/releasestream/{release}/candidate", c.apiReleaseCandidate)
	mux.HandleFunc("/api/v1/releasestream/{release}/release/{tag}", c.apiReleaseInfo)
	mux.HandleFunc("/api/v1/releasestream/{release}/config", c.apiReleaseConfig)

	// static files
	mux.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.FS(resources))))

	return mux
}

func (c *Controller) urlForArtifacts(tagName string) (string, bool) {
	if len(c.artifactsHost) == 0 {
		return "", false
	}
	return fmt.Sprintf("https://%s/%s", c.artifactsHost, url.PathEscape(tagName)), true
}

func (c *Controller) locateLatest(w http.ResponseWriter, req *http.Request) (*releasecontroller.Release, *imagev1.TagReference, bool) {
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

	r, latest, err := releasecontroller.LatestForStream(c.parsedReleaseConfigCache, c.eventRecorder, c.releaseLister, streamName, constraint, relativeIndex)
	if err != nil {
		code := http.StatusInternalServerError
		if err == releasecontroller.ErrStreamNotFound || err == releasecontroller.ErrStreamTagNotFound {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return nil, nil, false
	}
	return r, latest, true
}

func (c *Controller) apiReleaseLatest(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { klog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	r, latest, ok := c.locateLatest(w, req)
	if !ok {
		return
	}

	downloadURL, _ := c.urlForArtifacts(latest.Name)
	resp := releasecontroller.APITag{
		Name:        latest.Name,
		PullSpec:    releasecontroller.FindPublicImagePullSpec(r.Target, latest.Name),
		DownloadURL: downloadURL,
		Phase:       latest.Annotations[releasecontroller.ReleaseAnnotationPhase],
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
		http.Error(w, fmt.Sprintf("error: Must specify one of '', 'json', 'pullSpec', 'name', or 'downloadURL"), http.StatusBadRequest)
	}
}

func (c *Controller) locateStream(streamName string, phases ...string) (*ReleaseStream, error) {
	imageStreams, err := c.releaseLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	for _, stream := range imageStreams {
		r, ok, err := releasecontroller.ReleaseDefinition(stream, c.parsedReleaseConfigCache, c.eventRecorder, *c.releaseLister)
		if err != nil || !ok {
			continue
		}
		if r.Config.Name != streamName {
			continue
		}
		// find all accepted tags, then sort by semantic version
		tags := releasecontroller.UnsortedSemanticReleaseTags(r, phases...)
		sort.Sort(tags)
		return &ReleaseStream{
			Release: r,
			Tags:    tags.Tags(),
		}, nil
	}
	return nil, releasecontroller.ErrStreamNotFound

}

func (c *Controller) apiReleaseTags(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { klog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	vars := mux.Vars(req)
	streamName := vars["release"]

	filterPhase := req.URL.Query()["phase"]

	r, err := c.locateStream(streamName)
	if err != nil {
		if err == releasecontroller.ErrStreamNotFound {
			http.Error(w, fmt.Sprintf("Unable to find release %s", streamName), http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf("Unable to find release %s: %v", streamName, err), http.StatusInternalServerError)
		}
		return
	}

	var tags []releasecontroller.APITag
	for _, tag := range r.Tags {
		downloadURL, _ := c.urlForArtifacts(tag.Name)
		phase := tag.Annotations[releasecontroller.ReleaseAnnotationPhase]
		if len(filterPhase) > 0 && !releasecontroller.ContainsString(filterPhase, phase) {
			continue
		}
		tags = append(tags, releasecontroller.APITag{
			Name:        tag.Name,
			PullSpec:    releasecontroller.FindPublicImagePullSpec(r.Release.Target, tag.Name),
			DownloadURL: downloadURL,
			Phase:       phase,
		})
	}

	resp := releasecontroller.APIRelease{
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
		http.Error(w, fmt.Sprintf("error: Must specify one of '', 'json', 'pullSpec', 'name', or 'downloadURL"), http.StatusBadRequest)
	}
}

func (c *Controller) apiReleaseInfo(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { klog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	tagInfo, err := c.getReleaseTagInfo(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	verificationJobs, msg := c.getVerificationJobs(*tagInfo.Info.Tag, tagInfo.Info.Release)
	if len(msg) > 0 {
		klog.V(4).Infof("Unable to retrieve verification job results for: %s", tagInfo.Tag)
	}

	var changeLog []byte

	if tagInfo.Info.Previous != nil && len(tagInfo.PreviousTagPullSpec) > 0 && len(tagInfo.TagPullSpec) > 0 {
		ch := make(chan renderResult)

		// run the changelog in a goroutine because it may take significant time
		go c.getChangeLog(ch, tagInfo.PreviousTagPullSpec, tagInfo.Info.Previous.Name, tagInfo.TagPullSpec, tagInfo.Info.Tag.Name)

		var render renderResult
		select {
		case render = <-ch:
		case <-time.After(500 * time.Millisecond):
			select {
			case render = <-ch:
			case <-time.After(15 * time.Second):
				render.err = fmt.Errorf("the changelog is still loading, if this is the first access it may take several minutes to clone all repositories")
			}
		}
		if render.err == nil {
			result := blackfriday.Run([]byte(render.out))
			// make our links targets
			result = reInternalLink.ReplaceAllFunc(result, func(s []byte) []byte {
				return []byte(`<a target="_blank" ` + string(bytes.TrimPrefix(s, []byte("<a "))))
			})
			changeLog = result
		}
	}
	summary := releasecontroller.APIReleaseInfo{
		Name:         tagInfo.Tag,
		Phase:        tagInfo.Info.Tag.Annotations[releasecontroller.ReleaseAnnotationPhase],
		Results:      verificationJobs,
		UpgradesTo:   c.graph.UpgradesTo(tagInfo.Tag),
		UpgradesFrom: c.graph.UpgradesFrom(tagInfo.Tag),
		ChangeLog:    changeLog,
	}

	data, err := json.MarshalIndent(&summary, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
	fmt.Fprintln(w)
}

func (c *Controller) httpGraphSave(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { klog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
	if err := c.graph.Save(w); err != nil {
		http.Error(w, fmt.Sprintf("unable to save graph: %v", err), http.StatusInternalServerError)
	}
}

func (c *Controller) httpReleaseChangelog(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { klog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

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
	defer func() { klog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

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

	tagPullSpec := releasecontroller.FindPublicImagePullSpec(tags[tag].Release.Target, tag)
	if len(tagPullSpec) == 0 {
		http.Error(w, fmt.Sprintf("could not find pull spec for tag %s in image stream %s", tag, tags[tag].Release.Target.Name), http.StatusBadRequest)
		return
	}

	imageInfo, err := releasecontroller.GetImageInfo(c.releaseInfo, c.architecture, tagPullSpec)
	if err != nil {
		http.Error(w, fmt.Sprintf("unable to determine image info for %s: %v", tagPullSpec, err), http.StatusBadRequest)
		return
	}

	out, err := c.releaseInfo.ReleaseInfo(imageInfo.GenerateDigestPullSpec())
	if err != nil {
		http.Error(w, fmt.Sprintf("Internal error: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, out)
}

func (c *Controller) httpReleaseInfoDownload(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { klog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

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

type releaseTagInfo struct {
	Tag                 string
	Info                *ReleaseStreamTag
	TagPullSpec         string
	PreviousTagPullSpec string
}

func (c *Controller) getReleaseTagInfo(req *http.Request) (*releaseTagInfo, error) {
	vars := mux.Vars(req)
	release := vars["release"]
	tag := vars["tag"]
	from := req.URL.Query().Get("from")

	tags, ok := c.findReleaseStreamTags(true, tag, from)
	if !ok {
		return nil, fmt.Errorf("unable to find release tag %s, it may have been deleted", tag)
	}

	info := tags[tag]
	if len(release) > 0 && info.Release.Config.Name != release {
		return nil, fmt.Errorf("release tag %s does not belong to release %s", tag, release)
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
	tagPull := releasecontroller.FindPublicImagePullSpec(info.Release.Target, info.Tag.Name)
	var previousTagPull string
	if info.Previous != nil {
		previousTagPull = releasecontroller.FindPublicImagePullSpec(info.PreviousRelease.Target, info.Previous.Name)
	}

	return &releaseTagInfo{
		Tag:                 tag,
		Info:                info,
		TagPullSpec:         tagPull,
		PreviousTagPullSpec: previousTagPull,
	}, nil
}

func (c *Controller) httpReleaseInfo(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { klog.V(4).Infof("rendered in %s", time.Since(start)) }()

	endOfLifePrefixes := c.endOfLifePrefixes()

	tagInfo, err := c.getReleaseTagInfo(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	mirror, _ := releasecontroller.GetMirror(tagInfo.Info.Release, tagInfo.Info.Tag.Name, c.releaseLister)

	w.Header().Set("Content-Type", "text/html;charset=UTF-8")
	fmt.Fprintf(w, htmlPageStart, template.HTMLEscapeString(fmt.Sprintf("Release %s", tagInfo.Tag)))
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
	fmt.Fprintf(w, "<h1>%s</h1>\n", template.HTMLEscapeString(tagInfo.Tag))

	switch tagInfo.Info.Tag.Annotations[releasecontroller.ReleaseAnnotationPhase] {
	case releasecontroller.ReleasePhaseFailed:
		fmt.Fprintf(w, `<div class="alert alert-danger"><p>%s</p>`, template.HTMLEscapeString(tagInfo.Info.Tag.Annotations[releasecontroller.ReleaseAnnotationMessage]))
		if log := tagInfo.Info.Tag.Annotations[releasecontroller.ReleaseAnnotationLog]; len(log) > 0 {
			fmt.Fprintf(w, `<pre class="small">%s</pre>`, template.HTMLEscapeString(log))
		} else {
			fmt.Fprintf(w, `<div><em>No failure log was captured</em></div>`)
		}
		fmt.Fprintf(w, `</div>`)
		return
	}

	// Disable the installation instructions for manifest list based releases
	switch c.architecture {
	case "multi":
		renderMultiArchPullSpec(w, tagInfo.TagPullSpec)
	default:
		renderInstallInstructions(w, mirror, tagInfo.Info.Tag, tagInfo.TagPullSpec, c.artifactsHost)
	}

	c.renderVerifyLinks(w, *tagInfo.Info.Tag, tagInfo.Info.Release)

	upgradesTo := c.graph.UpgradesTo(tagInfo.Tag)

	var missingUpgrades []string
	upgradeFound := make(map[string]bool)
	supportedUpgrades, _ := c.getSupportedUpgrades(tagInfo.TagPullSpec)
	if len(supportedUpgrades) > 0 {
		for _, u := range upgradesTo {
			upgradeFound[u.From] = true
		}
		for _, from := range supportedUpgrades {
			if !upgradeFound[from] {
				upgradesTo = append(upgradesTo, releasecontroller.UpgradeHistory{
					From:  from,
					To:    tagInfo.Tag,
					Total: -1,
				})
				missingUpgrades = append(missingUpgrades, fmt.Sprintf(`<a class="text-monospace" href="/releasetag/%s">%s</a>`, from, from))
			}
		}
	}
	if len(upgradesTo) > 0 {
		sort.Sort(releasecontroller.NewNewestSemVerFromSummaries(upgradesTo))
		fmt.Fprintf(w, `<p id="upgrades-from">Upgrades from:</p>`)
		if len(missingUpgrades) > 0 {
			fmt.Fprintf(w, "<div class=\"alert alert-warning\">Untested upgrades: %s</div>", strings.Join(missingUpgrades, ", "))
		}
		fmt.Fprintf(w, "<ul>")
		for _, upgrade := range upgradesTo {
			var style string
			switch {
			case upgrade.Success == 0 && upgrade.Failure > 0:
				style = "text-danger"
			case upgrade.Success > 0:
				style = "text-success"
			}
			if len(supportedUpgrades) > 0 && !upgradeFound[upgrade.From] {
				continue
			}
			fmt.Fprintf(w, `<li><a class="text-monospace %s" href="/releasetag/%s">%s</a>`, style, upgrade.From, upgrade.From)
			if tagInfo.Info.Previous == nil || upgrade.From != tagInfo.Info.Previous.Name {
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
						case releasecontroller.ReleaseVerificationStateSucceeded:
							fmt.Fprintf(w, ` <a class="text-success" href="%s">S</a>`, template.HTMLEscapeString(url))
						case releasecontroller.ReleaseVerificationStateFailed:
							fmt.Fprintf(w, ` <a class="text-danger" href="%s">F</a>`, template.HTMLEscapeString(url))
						default:
							fmt.Fprintf(w, ` <a class="" href="%s">P</a>`, template.HTMLEscapeString(url))
						}
					}
				} else {
					for _, url := range urls {
						switch upgrade.History[url].State {
						case releasecontroller.ReleaseVerificationStateSucceeded:
							fmt.Fprintf(w, ` <a class="text-success" href="%s">Success</a>`, template.HTMLEscapeString(url))
						case releasecontroller.ReleaseVerificationStateFailed:
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

	if upgradesFrom := c.graph.UpgradesFrom(tagInfo.Tag); len(upgradesFrom) > 0 {
		sort.Sort(releasecontroller.NewNewestSemVerToSummaries(upgradesFrom))
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
						case releasecontroller.ReleaseVerificationStateSucceeded:
							fmt.Fprintf(w, ` <a class="text-success" href="%s">S</a>`, template.HTMLEscapeString(url))
						case releasecontroller.ReleaseVerificationStateFailed:
							fmt.Fprintf(w, ` <a class="text-danger" href="%s">F</a>`, template.HTMLEscapeString(url))
						default:
							fmt.Fprintf(w, ` <a class="" href="%s">P</a>`, template.HTMLEscapeString(url))
						}
					}
				} else {
					for _, url := range urls {
						switch upgrade.History[url].State {
						case releasecontroller.ReleaseVerificationStateSucceeded:
							fmt.Fprintf(w, ` <a class="text-success" href="%s">Success</a>`, template.HTMLEscapeString(url))
						case releasecontroller.ReleaseVerificationStateFailed:
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

	if tagInfo.Info.Previous != nil && len(tagInfo.PreviousTagPullSpec) > 0 && len(tagInfo.TagPullSpec) > 0 {
		fmt.Fprintln(w, "<hr>")
		c.renderChangeLog(w, tagInfo.PreviousTagPullSpec, tagInfo.Info.Previous.Name, tagInfo.TagPullSpec, tagInfo.Info.Tag.Name)
	}

	var options []string
	for _, tag := range tagInfo.Info.Older {
		var selected string
		if tag.Name == tagInfo.Info.Previous.Name {
			selected = `selected="true"`
		}
		if !endOfLifePrefixes.Has(pruneTagInfo(tag.Name)) {
			options = append(options, fmt.Sprintf(`<option %s>%s</option>`, selected, tag.Name))
		}
	}
	for _, release := range tagInfo.Info.Stable.Releases {
		if release.Release == tagInfo.Info.Release {
			continue
		}
		for j, version := range release.Versions {
			if !endOfLifePrefixes.Has(pruneTagInfo(version.Tag.Name)) {
				if j == 0 && len(options) > 0 {
					options = append(options, `<option disabled>───</option>`)
				}
				var selected string
				if tagInfo.Info.Previous != nil && version.Tag.Name == tagInfo.Info.Previous.Name {
					selected = `selected="true"`
				}
				options = append(options, fmt.Sprintf(`<option %s>%s</option>`, selected, version.Tag.Name))
			}
		}
	}
	if len(options) > 0 {
		fmt.Fprint(w, `<p><form class="form-inline" method="GET">`)
		if tagInfo.Info.Previous != nil {
			fmt.Fprintf(w, `<a href="/changelog?from=%s&to=%s">View changelog in Markdown</a><span>&nbsp;or&nbsp;</span><label for="from">change previous release:&nbsp;</label>`, tagInfo.Info.Previous.Name, tagInfo.Info.Tag.Name)
		} else {
			fmt.Fprint(w, `<label for="from">change previous release:&nbsp;</label>`)
		}
		fmt.Fprintf(w, `<select onchange="this.form.submit()" id="from" class="form-control" name="from">%s</select> <input class="btn btn-link" type="submit" value="Compare">`, strings.Join(options, ""))
		fmt.Fprint(w, `</form></p>`)
	}
}

func (c *Controller) httpReleaseLatest(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { klog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	r, latest, ok := c.locateLatest(w, req)
	if !ok {
		return
	}

	http.Redirect(w, req, fmt.Sprintf("/releasestream/%s/release/%s", url.PathEscape(r.Config.Name), url.PathEscape(latest.Name)), http.StatusFound)
}

func (c *Controller) httpReleaseLatestDownload(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { klog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

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
	defer func() { klog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

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
		BaseURL:    base.String(),
		Dashboards: c.dashboards,
	}

	authMessage := ""
	if len(c.authenticationMessage) > 0 {
		authMessage = fmt.Sprintf("<p>%s</p>", c.authenticationMessage)
	}

	now := time.Now()
	var releasePage = template.Must(template.New("releasePageHtml.tmpl").Funcs(
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
					if r.Release.Config.As == releasecontroller.ReleaseConfigModeStable {
						searchFunctionPrefix := removeSpecialCharacters(r.Release.Config.Name)
						searchFunction := fmt.Sprintf("searchTable_%s('%s')", searchFunctionPrefix, searchFunctionPrefix)
						return fmt.Sprintf("<td class=\"text-center\"colspan=3>\n<div class=\"container\">\n<div class=\"row d-flex justify-content-between\">\n<div><p>%s</p></div>\n<div class=\"form-outline\"><input type=\"search\" class=\"form-control\" id=\"%s\" onkeyup=\"%s\"  placeholder=\"Search\" aria-label=\"Search\"></div>\n</div>\n</div>\n</td>", r.Release.Config.Message, searchFunctionPrefix, searchFunction)
					}
					return fmt.Sprintf("<p>%s</p>\n", r.Release.Config.Message)
				}
				var out []string
				switch r.Release.Config.As {
				case releasecontroller.ReleaseConfigModeStable:
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
			"tableLink":       tableLink,
			"versionGrouping": versionGrouping,
			"stableStream":    stableStream,
			"phaseCell":       phaseCell,
			"phaseAlert":      phaseAlert,
			"alerts":          renderAlerts,
			"links":           c.links,
			"releaseJoin":     releaseJoin,
			"dashboardsJoin":  dashboardsJoin,
			"inc":             func(i int) int { return i + 1 },
			"upgradeCells":    upgradeCells,
			"since": func(utcDate string) string {
				t, err := time.Parse(time.RFC3339, utcDate)
				if err != nil {
					return ""
				}
				return relTime(t, now, "ago", "from now")
			},
			"displayAuthMessage": func() string { return authMessage },
		},
	).ParseFS(resources, "releasePageHtml.tmpl"))

	var pageEnd = template.Must(template.New("htmlPageEndScripts.tmpl").Funcs(
		template.FuncMap{
			"stableStream":            stableStream,
			"removeSpecialCharacters": removeSpecialCharacters,
		},
	).ParseFS(resources, "htmlPageEndScripts.tmpl"))

	imageStreams, err := c.releaseLister.List(labels.Everything())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	endOfLifePrefixes := sets.NewString()

	for _, stream := range imageStreams {
		r, ok, err := releasecontroller.ReleaseDefinition(stream, c.parsedReleaseConfigCache, c.eventRecorder, *c.releaseLister)
		if err != nil || !ok {
			continue
		}
		if r.Config.EndOfLife {
			if version, err := releasecontroller.SemverParseTolerant(r.Config.Name); err == nil {
				endOfLifePrefixes.Insert(fmt.Sprintf("%d.%d", version.Major, version.Minor))
			}
			continue
		}
		s := ReleaseStream{
			Release: r,
			Tags:    releasecontroller.SortedReleaseTags(r),
		}
		var delays []string
		if r.Config.As != releasecontroller.ReleaseConfigModeStable && len(s.Tags) > 0 {
			if ok, _, queueAfter := releasecontroller.IsReleaseDelayedForInterval(r, s.Tags[0]); ok {
				delays = append(delays, fmt.Sprintf("waiting for %s", queueAfter.Truncate(time.Second)))
			}
			if r.Config.MaxUnreadyReleases > 0 && releasecontroller.CountUnreadyReleases(r, s.Tags) >= r.Config.MaxUnreadyReleases {
				delays = append(delays, fmt.Sprintf("no more than %d pending", r.Config.MaxUnreadyReleases))
			}
		}
		if len(delays) > 0 {
			s.Delayed = &ReleaseDelay{Message: fmt.Sprintf("Next release may not start: %s", strings.Join(delays, ", "))}
		}
		if r.Config.As != releasecontroller.ReleaseConfigModeStable {
			s.Upgrades = calculateReleaseUpgrades(r, s.Tags, c.graph, false)
		}
		page.Streams = append(page.Streams, s)
	}
	sort.Sort(preferredReleases(page.Streams))
	checkReleasePage(page)
	pruneEndOfLifeTags(page, endOfLifePrefixes)

	fmt.Fprintf(w, htmlPageStart, "Release Status")
	if err := releasePage.Execute(w, page); err != nil {
		klog.Errorf("Unable to render page: %v", err)
	}
	if err := pageEnd.Execute(w, page); err != nil {
		klog.Errorf("Unable to render page: %v", err)
	}
}

func (c *Controller) httpDashboardOverview(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { klog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

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
		BaseURL:    base.String(),
		Dashboards: c.dashboards,
	}

	now := time.Now()
	var releasePage = template.Must(template.New("releaseDashboardPage.tmpl").Funcs(
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
				case releasecontroller.ReleaseConfigModeStable:
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
			"tableLink":      tableLink,
			"phaseCell":      phaseCell,
			"phaseAlert":     phaseAlert,
			"inc":            func(i int) int { return i + 1 },
			"upgradeJobs":    upgradeJobs,
			"releaseJoin":    releaseJoin,
			"dashboardsJoin": dashboardsJoin,
			"since": func(utcDate string) string {
				t, err := time.Parse(time.RFC3339, utcDate)
				if err != nil {
					return ""
				}
				return relTime(t, now, "ago", "from now")
			},
		},
	).ParseFS(resources, "releaseDashboardPage.tmpl"))

	imageStreams, err := c.releaseLister.List(labels.Everything())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, stream := range imageStreams {
		r, ok, err := releasecontroller.ReleaseDefinition(stream, c.parsedReleaseConfigCache, c.eventRecorder, *c.releaseLister)
		if err != nil || !ok {
			continue
		}
		if r.Config.EndOfLife {
			continue
		}
		s := ReleaseStream{
			Release: r,
			Tags:    releasecontroller.SortedReleaseTags(r),
		}
		var delays []string
		if r.Config.As != releasecontroller.ReleaseConfigModeStable && len(s.Tags) > 0 {
			if ok, _, queueAfter := releasecontroller.IsReleaseDelayedForInterval(r, s.Tags[0]); ok {
				delays = append(delays, fmt.Sprintf("waiting for %s", queueAfter.Truncate(time.Second)))
			}
			if r.Config.MaxUnreadyReleases > 0 && releasecontroller.CountUnreadyReleases(r, s.Tags) >= r.Config.MaxUnreadyReleases {
				delays = append(delays, fmt.Sprintf("no more than %d pending", r.Config.MaxUnreadyReleases))
			}
		}
		if isReleaseFailing(s.Tags, r.Config.MaxUnreadyReleases) {
			s.Failing = true
		}

		if len(delays) > 0 {
			s.Delayed = &ReleaseDelay{Message: fmt.Sprintf("Next release may not start: %s", strings.Join(delays, ", "))}
		}
		if r.Config.As != releasecontroller.ReleaseConfigModeStable {
			s.Upgrades = calculateReleaseUpgrades(r, s.Tags, c.graph, true)
		}
		page.Streams = append(page.Streams, s)
	}

	sort.Sort(preferredReleases(page.Streams))
	checkReleasePage(page)

	fmt.Fprintf(w, htmlPageStart, "Release Status")
	if err := releasePage.Execute(w, page); err != nil {
		klog.Errorf("Unable to render page: %v", err)
	}
	fmt.Fprintln(w, htmlPageEnd)
}

func isReleaseFailing(tags []*imagev1.TagReference, maxUnready int) bool {
	unreadyCount := 0
	for i := 0; unreadyCount < maxUnready && i < len(tags); i++ {
		switch tags[i].Annotations[releasecontroller.ReleaseAnnotationPhase] {
		case releasecontroller.ReleasePhaseReady:
			continue
		case releasecontroller.ReleasePhaseAccepted:
			return false
		default:
			unreadyCount++
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

func (c *Controller) getSupportedUpgrades(tagPull string) ([]string, error) {
	imageInfo, err := releasecontroller.GetImageInfo(c.releaseInfo, c.architecture, tagPull)
	if err != nil {
		return nil, fmt.Errorf("unable to determine image info for %s: %v", tagPull, err)
	}
	tagUpgradeInfo, err := c.releaseInfo.UpgradeInfo(imageInfo.GenerateDigestPullSpec())
	if err != nil {
		return nil, fmt.Errorf("could not get release info for tag %s: %v", tagPull, err)
	}
	var supportedUpgrades []string
	if tagUpgradeInfo.Metadata != nil {
		supportedUpgrades = tagUpgradeInfo.Metadata.Previous
	}
	return supportedUpgrades, nil
}

func (c *Controller) apiReleaseConfig(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { klog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	vars := mux.Vars(req)
	streamName := vars["release"]
	jobType := req.URL.Query().Get("jobType")

	imageStreams, err := c.releaseLister.List(labels.Everything())
	if err != nil {
		code := http.StatusInternalServerError
		if err == releasecontroller.ErrStreamNotFound || err == releasecontroller.ErrStreamTagNotFound {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}

	var release *releasecontroller.Release

	for _, stream := range imageStreams {
		r, ok, err := releasecontroller.ReleaseDefinition(stream, c.parsedReleaseConfigCache, c.eventRecorder, *c.releaseLister)
		if err != nil || !ok {
			continue
		}
		if r.Config.Name != streamName {
			continue
		}
		release = r
		break
	}

	if release == nil {
		http.Error(w, fmt.Sprintf("error: unknown release stream specified: %s", streamName), http.StatusBadRequest)
		return
	}

	displayConfig := false
	periodicJobs := make(map[string]releasecontroller.ReleasePeriodic)
	verificationJobs := make(map[string]releasecontroller.ReleaseVerification)

	if jobType == "periodic" {
		for name, periodic := range release.Config.Periodic {
			periodicJobs[name] = periodic
		}
	} else {
	Loop:
		for name, verify := range release.Config.Verify {
			switch jobType {
			case "informing":
				if verify.Optional {
					verificationJobs[name] = verify
				}
			case "blocking":
				if !verify.Optional {
					verificationJobs[name] = verify
				}
			case "disabled":
				if verify.Disabled {
					verificationJobs[name] = verify
				}
			case "":
				displayConfig = true
				break Loop
			default:
				http.Error(w, fmt.Sprintf("error: jobType must be one of '', 'informing', 'blocking', 'disabled' or 'periodic'"), http.StatusBadRequest)
				return
			}
		}
	}

	var data []byte

	if displayConfig {
		data, err = json.MarshalIndent(&release.Config, "", "  ")
	} else {
		if jobType == "periodic" {
			data, err = json.MarshalIndent(&periodicJobs, "", "  ")
		} else {
			data, err = json.MarshalIndent(&verificationJobs, "", "  ")
		}
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
	fmt.Fprintln(w)
}

type Inconsistencies struct {
	PayloadInconsistencies      map[string]PayloadInconsistencyDetails
	AssemblyWideInconsistencies string
	Tag                         string
	Release                     string
}

type PayloadInconsistencyDetails struct {
	PullSpec string
	Message  string
}

func jsonArrayToString(messageArray string) (string, error) {
	var arr []string
	err := json.Unmarshal([]byte(messageArray), &arr)
	if err != nil {
		return "", err
	}
	return strings.Join(arr, "; "), nil
}

func (c *Controller) httpInconsistencyInfo(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { klog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()

	vars := mux.Vars(req)
	imageStreamInconsistencies := Inconsistencies{}
	type1Inconsistency := PayloadInconsistencyDetails{}
	m := make(map[string]PayloadInconsistencyDetails)

	tagInfo, err := c.getReleaseTagInfo(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if inconsistencyMessage, ok := tagInfo.Info.Release.Source.Annotations[releasecontroller.ReleaseAnnotationInconsistency]; ok {
		message, err := jsonArrayToString(inconsistencyMessage)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		imageStreamInconsistencies.AssemblyWideInconsistencies = message
	}
	for _, tag := range tagInfo.Info.Release.Source.Spec.Tags {
		if inconsistencyMessage, ok := tag.Annotations[releasecontroller.ReleaseAnnotationInconsistency]; ok {
			type1Inconsistency.PullSpec = tag.From.Name
			message, err := jsonArrayToString(inconsistencyMessage)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			type1Inconsistency.Message = message
			m[tag.Name] = type1Inconsistency
		}
	}
	imageStreamInconsistencies.PayloadInconsistencies = m
	imageStreamInconsistencies.Tag = vars["tag"]
	imageStreamInconsistencies.Release = vars["release"]

	w.Header().Set("Content-Type", "text/html;charset=UTF-8")
	tmpl := template.Must(template.ParseFS(resources, "imageStreamInconsistency.tmpl"))

	err = tmpl.Execute(w, imageStreamInconsistencies)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func versionGrouping(tag string) string {
	s := strings.Split(tag, ".")
	return fmt.Sprintf("%s.%s", s[0], s[1])
}

func stableStream(streams []ReleaseStream) []string {
	var stableList []string
	for _, stream := range streams {
		if stream.Release.Config.As == releasecontroller.ReleaseConfigModeStable {
			stableList = append(stableList, stream.Release.Config.Name)
		}

	}
	return stableList
}

func removeSpecialCharacters(str string) string {
	return regexp.MustCompile(`[^a-zA-Z0-9 ]+`).ReplaceAllString(str, "")
}

func loadStaticHTML(file string) string {
	readFile, err := fs.ReadFile(resources, file)
	if err != nil {
		klog.Errorf("Failed to load static files from the filesystem. Error: %s", err)
		return ""
	}
	return string(readFile)
}
