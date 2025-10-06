package main

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	v1 "github.com/openshift/api/image/v1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
)

type ComparisonType int

const (
	From ComparisonType = 0
	To   ComparisonType = 1
)

type Comparison struct {
	Type     ComparisonType
	PullSpec string
	Tag      *v1.TagReference
}

type ComparisonPage struct {
	BaseURL    string
	Streams    []ReleaseStream
	Tags       []*v1.TagReference
	Dashboards []Dashboard
}

const comparisonDashboardPageHtml = `
<h1>Release Comparison Dashboard</h1>
<p class="small mb-3">
	Quick links: {{ dashboardsJoin .Dashboards }}
</p>
<div class="alert alert-primary">This site is part of OpenShift's continuous delivery pipeline. Neither the builds linked here nor the upgrade paths tested here are officially supported. For information about the available builds, please reference the <a href="https://mirror.openshift.com/pub/openshift-v4/OpenShift_Release_Types.pdf" target="_blank">OpenShift Release Types documentation</a>.</br>Please visit the Red Hat Customer Portal for the latest supported product details.</div>
`

func (c *Controller) httpDashboardCompare(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { klog.V(4).Infof("rendered in %s", time.Since(start)) }()

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
	page := &ComparisonPage{
		BaseURL:    base.String(),
		Dashboards: c.dashboards,
	}

	fromRelease := req.URL.Query().Get("from")
	toRelease := req.URL.Query().Get("to")
	format := req.URL.Query().Get("format")

	fromComparison := &Comparison{
		Type:     From,
		Tag:      nil,
		PullSpec: "",
	}
	toComparison := &Comparison{
		Type:     To,
		Tag:      nil,
		PullSpec: "",
	}

	var releasePage = template.Must(template.New("releaseDashboardPage").Funcs(template.FuncMap{
		"dashboardsJoin": dashboardsJoin,
	}).Parse(comparisonDashboardPageHtml))

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
		if r.Config.Name == "4-stable" {
			s := ReleaseStream{
				Release: r,
				Tags:    releasecontroller.SortedReleaseTags(r),
			}
			page.Streams = append(page.Streams, s)
		}
	}

	for _, stream := range page.Streams {
		for _, tag := range stream.Tags {
			if len(fromRelease) > 0 && len(toRelease) > 0 {
				pullSpec := releasecontroller.FindPublicImagePullSpec(stream.Release.Target, tag.Name)
				switch tag.Name {
				case fromRelease:
					fromComparison.PullSpec = pullSpec
					fromComparison.Tag = tag
				case toRelease:
					toComparison.PullSpec = pullSpec
					toComparison.Tag = tag
				}
			}
			page.Tags = append(page.Tags, tag)
		}
	}

	fmt.Fprintf(w, htmlPageStart, "Release Comparison Dashboard")
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

	if err := releasePage.Execute(w, page); err != nil {
		klog.Errorf("Unable to render page: %v", err)
	}

	if len(page.Tags) > 0 {
		fmt.Fprint(w, `<p><form class="form-inline" method="GET">`)
		fmt.Fprint(w, `<label for="from">From release:&nbsp;</label>`)
		fmt.Fprintf(w, `<select id="from" class="form-control" name="from"><option value="" disabled selected>Select</option>%s</select>`, generateSelectOptions(page.Tags, fromComparison))
		fmt.Fprint(w, `&nbsp;&nbsp;<label for="to">To release:&nbsp;</label>`)
		fmt.Fprintf(w, `<select id="to" class="form-control" name="to"><option value="" disabled selected>Select</option>%s</select>&nbsp;&nbsp;`, generateSelectOptions(page.Tags, toComparison))
		fmt.Fprint(w, `&nbsp;&nbsp;<label for="format">Format:&nbsp;</label>`)
		fmt.Fprintf(w, `<select id="format" class="form-control" name="format">Select</option>%s</select>&nbsp;&nbsp;`, generateFormatOptions(format))
		fmt.Fprintf(w, `<input class="btn btn-link" type="submit" value="Compare">`)
		fmt.Fprint(w, `</form></p>`)
	}

	fmt.Fprintln(w, "<hr>")

	if fromComparison.Tag != nil && toComparison.Tag != nil {
		c.renderChangeLog(w, fromComparison.PullSpec, fromComparison.Tag.Name, toComparison.PullSpec, toComparison.Tag.Name, format)
	} else {
		var unsupported []string
		if fromComparison.Tag == nil && len(fromRelease) > 0 {
			unsupported = append(unsupported, fromRelease)
		}
		if toComparison.Tag == nil && len(toRelease) > 0 {
			unsupported = append(unsupported, toRelease)
		}
		if len(unsupported) > 0 {
			fmt.Fprintf(w, `<p class="alert alert-danger">%s</p>`, fmt.Sprintf("Unable to locate release(s): %s", template.HTMLEscapeString(strings.Join(unsupported, ", "))))
		}
	}
}

func generateSelectOptions(tags []*v1.TagReference, comp *Comparison) string {
	var options []string
	for _, tag := range tags {
		selected := ""
		if comp.Tag != nil && comp.Tag.Name == tag.Name {
			selected = "selected"
		}
		options = append(options, fmt.Sprintf(`<option value="%s" %s>%s</option>`, tag.Name, selected, tag.Name))
	}
	return strings.Join(options, "")
}

func generateFormatOptions(format string) string {
	var options []string
	for _, f := range []string{"html", "json"} {
		selected := ""
		if format == f {
			selected = "selected"
		}
		options = append(options, fmt.Sprintf(`<option value="%s" %s>%s</option>`, f, selected, f))
	}
	return strings.Join(options, "")
}
