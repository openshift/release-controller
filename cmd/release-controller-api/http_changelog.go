package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/openshift/release-controller/pkg/rhcos"
	"github.com/russross/blackfriday"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
)

var (
	reInternalLink = regexp.MustCompile(`<a href="[^"]+">`)
)

type renderResult struct {
	out string
	err error
}

func (c *Controller) getChangeLog(ch chan renderResult, chRpmDiff chan renderResult, fromPull string, fromTag string, toPull string, toTag string, format string) {
	fromImage, err := releasecontroller.GetImageInfo(c.releaseInfo, c.architecture, fromPull)
	if err != nil {
		ch <- renderResult{err: err}
		return
	}

	toImage, err := releasecontroller.GetImageInfo(c.releaseInfo, c.architecture, toPull)
	if err != nil {
		ch <- renderResult{err: err}
		return
	}

	isJson := false
	switch format {
	case "json":
		isJson = true
	}

	// Generate the change log from image digests
	out, err := c.releaseInfo.ChangeLog(fromImage.GenerateDigestPullSpec(), toImage.GenerateDigestPullSpec(), isJson)
	if err != nil {
		ch <- renderResult{err: err}
		return
	}

	// There is an inconsistency with what is returned from ReleaseInfo (amd64) and what
	// needs to be passed into the RHCOS diff engine (x86_64).
	var architecture, archExtension string

	if toImage.Config.Architecture == "amd64" {
		architecture = "x86_64"
	} else if toImage.Config.Architecture == "arm64" {
		architecture = "aarch64"
		archExtension = fmt.Sprintf("-%s", architecture)
	} else {
		architecture = toImage.Config.Architecture
		archExtension = fmt.Sprintf("-%s", architecture)
	}

	if isJson {
		out, err = rhcos.TransformJsonOutput(out, architecture, archExtension)
		if err != nil {
			ch <- renderResult{err: err}
			return
		}
		ch <- renderResult{out: out}
	}

	out, err = rhcos.TransformMarkDownOutput(out, fromTag, toTag, architecture, archExtension)
	if err != nil {
		ch <- renderResult{err: err}
		return
	}
	ch <- renderResult{out: out}

	// We returned a result already, so we're no longer blocking rendering. So now also fetch the CoreOS RPM diff if requested.
	if chRpmDiff == nil {
		return
	}

	// Only request a CoreOS diff if it'll be rendered. Use the exact
	// check that renderChangelog does to know if to consume from us.
	if !strings.Contains(out, "#coreos-package-diff") {
		chRpmDiff <- renderResult{}
		return
	}

	rpmdiff, err := c.releaseInfo.RpmDiff(fromImage.GenerateDigestPullSpec(), toImage.GenerateDigestPullSpec())
	if err != nil {
		chRpmDiff <- renderResult{err: err}
	}

	chRpmDiff <- renderResult{out: rhcos.RenderRpmDiff(out, rpmdiff)}
}

func (c *Controller) renderChangeLog(w http.ResponseWriter, fromPull string, fromTag string, toPull string, toTag string, format string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		flusher = nopFlusher{}
	}

	flusher.Flush()

	ch := make(chan renderResult)
	chRpmDiff := make(chan renderResult)

	// run the changelog in a goroutine because it may take significant time
	go c.getChangeLog(ch, chRpmDiff, fromPull, fromTag, toPull, toTag, format)

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
		switch format {
		case "json":
			var changeLog releasecontroller.ChangeLog
			err := json.Unmarshal([]byte(render.out), &changeLog)
			if err != nil {
				fmt.Fprintf(w, `<p class="alert alert-danger">%s</p>`, fmt.Sprintf("Unable to show full changelog: %s", err))
				return
			}
			data, err := json.MarshalIndent(&changeLog, "", "  ")
			if err != nil {
				fmt.Fprintf(w, `<p class="alert alert-danger">%s</p>`, fmt.Sprintf("Unable to show full changelog: %s", err))
				return
			}
			fmt.Fprintf(w, "<pre><code>")
			if _, err := w.Write(data); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			fmt.Fprintf(w, "</pre></code>")
		default:
			result := blackfriday.Run([]byte(render.out))
			// make our links targets
			result = reInternalLink.ReplaceAllFunc(result, func(s []byte) []byte {
				return []byte(`<a target="_blank" ` + string(bytes.TrimPrefix(s, []byte("<a "))))
			})
			if _, err := w.Write(result); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}
		fmt.Fprintln(w, "<hr>")
	} else {
		// if we don't get a valid result within limits, just show the simpler informational view
		fmt.Fprintf(w, `<p class="alert alert-danger">%s</p>`, fmt.Sprintf("Unable to show full changelog: %s", render.err))
	}

	// only render a CoreOS diff if we need to; we can know this by
	// checking if it links to the diff section we create here
	if !strings.Contains(render.out, "#coreos-package-diff") {
		return
	}

	fmt.Fprintf(w, "<h3 id=\"coreos-package-diff\">CoreOS Package Diff</h3>")

	// only render the RPM diff if it's cached; judged by it taking less than 500ms
	select {
	case <-time.After(500 * time.Millisecond):
		fmt.Fprintf(w, `<p class="alert alert-danger">Package diff still loading; check again later...</p>`)
	case render = <-chRpmDiff:
		if render.err != nil {
			fmt.Fprintf(w, `<p class="alert alert-danger">%s</p>`, fmt.Sprintf("Unable to show package diff: %s", render.err))
		} else {
			result := blackfriday.Run([]byte(render.out))
			if _, err := w.Write(result); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}
	}
}
