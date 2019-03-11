package main

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	blackfriday "gopkg.in/russross/blackfriday.v2"

	"k8s.io/apimachinery/pkg/labels"
)

const htmlPageStart = `
<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8"><title>%s</title>
<link rel="stylesheet" href="https://stackpath.bootstrapcdn.com/bootstrap/4.1.3/css/bootstrap.min.css" integrity="sha384-MCw98/SFnGE8fJT3GXwEOngsV7Zt27NXFoaoApmYm81iuXoPkFOJwJ8ERdknLPMO" crossorigin="anonymous">
<meta name="viewport" content="width=device-width, initial-scale=1, shrink-to-fit=no">
</head>
<body>
<div class="container">
`

const htmlPageEnd = `
</div>
</body>
</html>
`

const releasePageHtml = `
<h1>Release Status</h1>
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
<div class="row">
<div class="col">
{{ range .Streams }}
		<h2 title="From image stream {{ .Release.Source.Namespace }}/{{ .Release.Source.Name }}">{{ .Release.Config.Name }}</h2>
		{{ publishDescription . }}
		{{ $upgrades := .Upgrades }}
		<table class="table">
			<thead>
				<tr>
					<th title="The name and version of the release image (as well as the tag it is published under)">Name</th>
					<th title="The release moves through these stages:&#10;&#10;Pending - still creating release image&#10;Ready - release image created&#10;Accepted - all tests pass&#10;Rejected - some tests failed&#10;Failed - Could not create release image">Phase</th>
					<th>Started</th>
					<th title="All tests must pass for a candidate to be marked accepted">Tests</th>
					<th colspan="{{ inc $upgrades.Width }}">Upgrades</th>
				</tr>
			</thead>
			<tbody>
		{{ $release := .Release }}
		{{ range $index, $tag := .Tags }}
			{{ $created := index .Annotations "release.openshift.io/creationTimestamp" }}
			<tr>
				{{ if canLink . }}
				<td class="text-monospace"><a class="{{ phaseAlert . }}" href="/releasestream/{{ $release.Config.Name }}/release/{{ .Name }}">{{ .Name }}</a></td>
				{{ else }}
				<td class="{{ phaseAlert . }}">{{ .Name }}</td>
				{{ end }}
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

func (c *Controller) findReleaseStreamTags(tags ...string) (map[string]*ReleaseStreamTag, bool) {
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
	for _, stream := range imageStreams {
		r, ok, err := c.releaseDefinition(stream)
		if err != nil || !ok {
			continue
		}
		releaseTags := tagsForRelease(r)
		for i, tag := range releaseTags {
			if needs, ok := needed[tag.Name]; ok && needs == nil {
				needed[tag.Name] = &ReleaseStreamTag{
					Release:  r,
					Tag:      tag,
					Previous: findPreviousRelease(tag, releaseTags[i+1:], r),
					Older:    releaseTags[i+1:],
				}
				remaining--
				if remaining == 0 {
					return needed, true
				}
			}
		}
	}
	return needed, remaining == 0
}

func (c *Controller) userInterfaceHandler() http.Handler {
	mux := mux.NewRouter()
	mux.HandleFunc("/changelog", c.httpReleaseChangelog)
	mux.HandleFunc("/releasetag/{tag}", c.httpReleaseInfo)
	mux.HandleFunc("/releasestream/{release}/release/{tag}", c.httpReleaseInfo)
	mux.HandleFunc("/", c.httpReleases)
	return mux
}

func (c *Controller) httpReleaseChangelog(w http.ResponseWriter, req *http.Request) {
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

	tags, ok := c.findReleaseStreamTags(from, to)
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
		fmt.Fprintf(w, htmlPageStart, fmt.Sprintf("Change log for %s", to))
		w.Write(result)
		fmt.Fprintln(w, htmlPageEnd)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintln(w, out)
}

func (c *Controller) httpReleaseInfo(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)

	release := vars["release"]
	tag := vars["tag"]
	from := req.URL.Query().Get("from")

	tags, ok := c.findReleaseStreamTags(tag, from)
	if !ok {
		http.Error(w, fmt.Sprintf("Unable to find release tag %s, it may have been deleted", tag), http.StatusNotFound)
		return
	}

	info := tags[tag]
	if len(release) > 0 && info.Release.Config.Name != release {
		http.Error(w, fmt.Sprintf("Release tag %s does not belong to release %s", tag, release), http.StatusNotFound)
		return
	}

	if len(from) > 0 {
		info.Previous = tags[from].Tag
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		flusher = nopFlusher{}
	}
	var skipHeader bool

	w.Header().Set("Content-Type", "text/html;charset=UTF-8")
	fmt.Fprintf(w, htmlPageStart, fmt.Sprintf("Release %s", tag))
	defer func() { fmt.Fprintln(w, htmlPageEnd) }()

	pull := info.Release.Target.Status.PublicDockerImageRepository
	if info.Previous != nil && len(pull) > 0 {
		type renderResult struct {
			out string
			err error
		}
		ch := make(chan renderResult)

		// run the changelog in a goroutine because it may take significant time
		go func() {
			out, err := c.releaseInfo.ChangeLog(pull+":"+info.Previous.Name, pull+":"+info.Tag.Name)
			if err != nil {
				ch <- renderResult{err: err}
				return
			}

			// replace references to the previous version with links
			rePrevious, err := regexp.Compile(fmt.Sprintf(`(\W)%s(\W)`, regexp.QuoteMeta(info.Previous.Name)))
			if err != nil {
				ch <- renderResult{err: err}
				return
			}
			out = rePrevious.ReplaceAllString(out, fmt.Sprintf("$1[%s](/releasetag/%s)$2", info.Previous.Name, info.Release.Config.Name, info.Previous.Name))
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
			skipHeader = true
			renderChangelog(w, render.out, pull, tag, info)
		} else {
			// if we don't get a valid result within limits, just show the simpler informational view
			fmt.Fprintf(w, `<p class="alert alert-danger">%s</p>`, fmt.Sprintf("Unable to show full changelog: %s", render.err))
		}
	}

	if !skipHeader {
		now := time.Now()
		var releasePage = template.Must(template.New("releaseInfoPage").Funcs(
			template.FuncMap{
				"publishSpec": func() string {
					if len(info.Release.Target.Status.PublicDockerImageRepository) > 0 {
						return info.Release.Target.Status.PublicDockerImageRepository + ":" + tag
					}
					return ""
				},
				"phaseCell":    phaseCell,
				"phaseAlert":   phaseAlert,
				"canLink":      canLink,
				"links":        links,
				"inc":          func(i int) int { return i + 1 },
				"upgradeCells": upgradeCells,
				"since": func(utcDate string) string {
					t, err := time.Parse(time.RFC3339, utcDate)
					if err != nil {
						return ""
					}
					return humanize.RelTime(t, now, "ago", "from now")
				},
			},
		).Parse(releaseInfoPageHtml))

		fmt.Fprintf(w, "<p><a href=\"/\">Back to index</a></p>\n")
		if err := releasePage.Execute(w, info); err != nil {
			glog.Errorf("Unable to render page: %v", err)
		}
	}

	fmt.Fprintln(w, "<hr>")
	if len(pull) > 0 {
		fmt.Fprintf(w, `<p>Pull spec: <code>%s:%s</code></p>`, pull, tag)
	}

	fmt.Fprintf(w, `<p>Tests:</p><ul>%s</ul>`, extendedLinks(*info.Tag, info.Release))

	if upgradesTo := c.graph.SummarizeUpgradesTo(tag); len(upgradesTo) > 0 {
		sort.Sort(newNewestSemVerFromSummaries(upgradesTo))
		fmt.Fprintf(w, `<p>Upgrades from:</p><ul>`)
		for _, upgrade := range upgradesTo {
			var style string
			if upgrade.Success == 0 && upgrade.Failure > 0 {
				style = "text-danger"
			}
			if upgrade.From == from {
				fmt.Fprintf(w, `<li><a class="text-monospace %s" href="/releasetag/%s">%s</a> - %d success, %d failures`, style, upgrade.From, upgrade.From, upgrade.Success, upgrade.Failure)
			} else {
				fmt.Fprintf(w, `<li><a class="text-monospace %s" href="/releasetag/%s">%s</a> (<a href="?from=%s">changes</a>) - %d success, %d failures`, style, upgrade.From, upgrade.From, upgrade.From, upgrade.Success, upgrade.Failure)
			}
		}
		fmt.Fprintf(w, `</ul>`)
	}

	if upgradesFrom := c.graph.SummarizeUpgradesFrom(tag); len(upgradesFrom) > 0 {
		sort.Sort(newNewestSemVerToSummaries(upgradesFrom))
		fmt.Fprintf(w, `<p>Upgrades to:</p><ul>`)
		for _, upgrade := range upgradesFrom {
			var style string
			if upgrade.Success == 0 && upgrade.Failure > 0 {
				style = "text-danger"
			}
			fmt.Fprintf(w, `<li><a class="text-monospace %s" href="/releasetag/%s">%s</a> - %d success, %d failures`, style, upgrade.To, upgrade.To, upgrade.Success, upgrade.Failure)
		}
		fmt.Fprintf(w, `</ul>`)
	}

	if len(info.Older) > 0 {
		var options []string
		for _, tag := range info.Older {
			var selected string
			if tag.Name == info.Previous.Name {
				selected = `selected="true"`
			}
			options = append(options, fmt.Sprintf(`<option %s>%s</option>`, selected, tag.Name))
		}
		fmt.Fprintf(w, `<p><form class="form-inline" method="GET"><a href="/changelog?from=%s&to=%s">View changelog in Markdown</a><span>&nbsp;or&nbsp;</span><label for="from">change previous release:&nbsp;</label><select id="from" class="form-control" name="from">%s</select> <input class="btn btn-link" type="submit" value="Compare"></form></p>`, info.Previous.Name, info.Tag.Name, strings.Join(options, ""))
	}
}

func (c *Controller) httpReleases(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html;charset=UTF-8")

	page := &ReleasePage{}

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
				var out []string
				out = append(out, fmt.Sprintf(`<span>updated when <code>%s/%s</code> changes</span>`, r.Release.Source.Namespace, r.Release.Source.Name))

				if len(r.Release.Target.Status.PublicDockerImageRepository) > 0 {
					for _, target := range r.Release.Config.Publish {
						if target.TagRef != nil && len(target.TagRef.Name) > 0 {
							out = append(out, fmt.Sprintf(`<span>promote to pull spec: <code>%s:%s</code></span>`, r.Release.Target.Status.PublicDockerImageRepository, target.TagRef.Name))
						}
					}
				}
				for _, target := range r.Release.Config.Publish {
					if target.ImageStreamRef != nil {
						out = append(out, fmt.Sprintf(`<span>promote to image stream: <code>%s/%s</code></span>`, target.ImageStreamRef.Namespace, target.ImageStreamRef.Name))
					}
				}
				if len(out) == 0 {
					return ""
				}
				sort.Strings(out)
				return fmt.Sprintf("<p>%s</p>\n", strings.Join(out, ", "))
			},
			"phaseCell":    phaseCell,
			"phaseAlert":   phaseAlert,
			"canLink":      canLink,
			"links":        links,
			"inc":          func(i int) int { return i + 1 },
			"upgradeCells": upgradeCells,
			"since": func(utcDate string) string {
				t, err := time.Parse(time.RFC3339, utcDate)
				if err != nil {
					return ""
				}
				return humanize.RelTime(t, now, "ago", "from now")
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
			Tags:    tagsForRelease(r),
		}
		s.Upgrades = calculateReleaseUpgrades(r, s.Tags, c.graph)
		page.Streams = append(page.Streams, s)
	}

	sort.Slice(page.Streams, func(i, j int) bool {
		return page.Streams[i].Release.Config.Name < page.Streams[j].Release.Config.Name
	})

	fmt.Fprintf(w, htmlPageStart, "Release Status")
	if err := releasePage.Execute(w, page); err != nil {
		glog.Errorf("Unable to render page: %v", err)
	}
	fmt.Fprintln(w, htmlPageEnd)
}
