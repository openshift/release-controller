package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"text/template"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/golang/glog"

	imagev1 "github.com/openshift/api/image/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const releasePageHtml = `
<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8"><title>Release Status</title>
<link rel="stylesheet" href="https://stackpath.bootstrapcdn.com/bootstrap/4.1.3/css/bootstrap.min.css" integrity="sha384-MCw98/SFnGE8fJT3GXwEOngsV7Zt27NXFoaoApmYm81iuXoPkFOJwJ8ERdknLPMO" crossorigin="anonymous">
<meta name="viewport" content="width=device-width, initial-scale=1, shrink-to-fit=no">
</head>
<body>
<div class="container">
<h1>Release Status</h1>
<div class="row">
<div class="col">
{{ range .Streams }}
		<h2 title="From image stream {{ .Release.Source.Namespace }}/{{ .Release.Source.Name }}">{{ .Release.Config.Name }}</h2>
		{{ $publishSpec := publishSpec . }}
		{{ if $publishSpec }}
		<p>pull-spec: <span>{{  $publishSpec }}</span></p>
		{{ end }}
		<table class="table">
			<thead>
				<tr><th>Tag</th><th>Phase</th><th>Started</th><th>Links</th></tr>
			</thead>
			<tbody>
		{{ $release := .Release }}
		{{ range .Tags }}
			{{ $created := index .Annotations "release.openshift.io/creationTimestamp" }}
			<tr class="{{ phaseAlert . }}">
				<td>{{ .Name }}</td>
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
</div>
</body>
</html>
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

type ReleasePage struct {
	Streams []ReleaseStream
}

type ReleaseStream struct {
	Release *Release
	Tags    []*imagev1.TagReference
}

func (c *Controller) userInterfaceHandler(w http.ResponseWriter, req *http.Request) {
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

	if err := releasePage.Execute(w, page); err != nil {
		glog.Errorf("Unable to render page: %v", err)
	}
}
