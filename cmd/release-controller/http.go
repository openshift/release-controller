package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

	imagev1 "github.com/openshift/api/image/v1"
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
<div class="row">
<div class="col">
{{ range .Streams }}
		<h2 title="From image stream {{ .Release.Source.Namespace }}/{{ .Release.Source.Name }}">{{ .Release.Config.Name }}</h2>
		{{ publishDescription . }}
		<table class="table">
			<thead>
				<tr><th title="The name and version of the release image (as well as the tag it is published under)">Name</th><th title="The release moves through these stages:&#10;&#10;Pending - still creating release image&#10;Ready - release image created&#10;Accepted - all tests pass&#10;Rejected - some tests failed&#10;Failed - Could not create release image">Phase</th><th>Started</th><th title="All tests must pass for a candidate to be marked accepted">Tests</th></tr>
			</thead>
			<tbody>
		{{ $release := .Release }}
		{{ range .Tags }}
			{{ $created := index .Annotations "release.openshift.io/creationTimestamp" }}
			<tr class="{{ phaseAlert . }}">
				<td><a href="/releasestream/{{ $release.Config.Name }}/release/{{ .Name }}">{{ .Name }}</a></td>
				{{ phaseCell . }}
				<td title="{{ $created }}">{{ since $created }}</td>
				<td>{{ links . $release }}</td>
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
<p>Pull spec: <code>{{ publishSpec }}</code></p>
{{ $created := index .Tag.Annotations "release.openshift.io/creationTimestamp" }}
<p>Created: <span>{{ since $created }}</span></p>
`

func phaseCell(tag imagev1.TagReference) string {
	phase := tag.Annotations[releaseAnnotationPhase]
	switch phase {
	case releasePhaseRejected:
		return fmt.Sprintf("<td title=\"%s\">%s (%s)</td>",
			template.HTMLEscapeString(tag.Annotations[releaseAnnotationMessage]),
			template.HTMLEscapeString(phase),
			template.HTMLEscapeString(tag.Annotations[releaseAnnotationReason]),
		)
	}
	return "<td>" + template.HTMLEscapeString(phase) + "</td>"
}

func phaseAlert(tag imagev1.TagReference) string {
	phase := tag.Annotations[releaseAnnotationPhase]
	switch phase {
	case releasePhasePending:
		return ""
	case releasePhaseReady:
		return ""
	case releasePhaseAccepted:
		return "alert-success"
	case releasePhaseFailed:
		return "alert-danger"
	case releasePhaseRejected:
		return "alert-danger"
	default:
		return "alert-danger"
	}
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
		buf.WriteString(" <span title=\"Pending\">")
		buf.WriteString(template.HTMLEscapeString(key))
		buf.WriteString("</span>")
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
		buf.WriteString("<li><span title=\"Pending\">")
		buf.WriteString(template.HTMLEscapeString(key))
		buf.WriteString("</span>")
	}
	return buf.String()
}

type ReleasePage struct {
	Streams []ReleaseStream
}

type ReleaseStream struct {
	Release *Release
	Tags    []*imagev1.TagReference
}

type ReleaseStreamTag struct {
	Release  *Release
	Tag      *imagev1.TagReference
	Previous *imagev1.TagReference
	Older    []*imagev1.TagReference
}

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

func (c *Controller) userInterfaceHandler() http.Handler {
	mux := mux.NewRouter()
	mux.HandleFunc("/changelog", c.httpReleaseChangelog)
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

type nopFlusher struct{}

func (_ nopFlusher) Flush() {}

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
	if info.Release.Config.Name != release {
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

	w.Header().Set("Content-Type", "text/html;charset=UTF-8")
	fmt.Fprintf(w, htmlPageStart, fmt.Sprintf("Release %s", tag))
	defer func() { fmt.Fprintln(w, htmlPageEnd) }()

	if pull := info.Release.Target.Status.PublicDockerImageRepository; info.Previous != nil && len(pull) > 0 {
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
			out = rePrevious.ReplaceAllString(out, fmt.Sprintf("$1[%s](/releasestream/%s/release/%s)$2", info.Previous.Name, info.Release.Config.Name, info.Previous.Name))
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
			case <-time.After(10 * time.Second):
				render.err = fmt.Errorf("the changelog is still loading, if this is the first access it may take several minutes to clone all repositories")
			}
			fmt.Fprintf(w, `<style>#loading{display: none;}</style>`)
			flusher.Flush()
		}
		if render.err == nil {
			renderChangelog(w, render.out, pull, tag, info)
			return
		}

		// if we don't get a valid result within limits, just show the simpler informational view
		fmt.Fprintf(w, `<p class="alert alert-danger">%s</p>`, fmt.Sprintf("Unable to show full changelog: %s", render.err))
	}

	now := time.Now()
	var releasePage = template.Must(template.New("releaseInfoPage").Funcs(
		template.FuncMap{
			"publishSpec": func() string {
				if len(info.Release.Target.Status.PublicDockerImageRepository) > 0 {
					return info.Release.Target.Status.PublicDockerImageRepository + ":" + tag
				}
				return ""
			},
			"phaseCell":  phaseCell,
			"phaseAlert": phaseAlert,
			"links":      links,
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

	fmt.Fprintln(w, "<hr>")
	fmt.Fprintf(w, `<p>Pull spec: <code>%s:%s</code></p>`, pull, tag)

	fmt.Fprintf(w, `<p>Tests:</p><ul>%s</ul>`, extendedLinks(*info.Tag, info.Release))

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
			"phaseCell":  phaseCell,
			"phaseAlert": phaseAlert,
			"links":      links,
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
