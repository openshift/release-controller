package main

import (
	"bytes"
	"context"
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

func (c *Controller) getChangeLog(ctx context.Context, ch chan renderResult, chNodeInfo chan renderResult, fromPull string, fromTag string, toPull string, toTag string, format string) {
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

	switch toImage.Config.Architecture {
	case "amd64":
		architecture = "x86_64"
	case "arm64":
		architecture = "aarch64"
		archExtension = fmt.Sprintf("-%s", architecture)
	default:
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
		return
	}

	out, err = rhcos.TransformMarkDownOutput(out, fromTag, toTag, architecture, archExtension)
	if err != nil {
		ch <- renderResult{err: err}
		return
	}
	ch <- renderResult{out: out}

	// We returned a result already, so we're no longer blocking rendering. So now also fetch the CoreOS RPM diff if requested.
	if chNodeInfo == nil {
		return
	}

	toImagePullspec := toImage.GenerateDigestPullSpec()
	fromImagePullspec := fromImage.GenerateDigestPullSpec()

	// Request node image info when the changelog links to #node-image-info (CoreOS infobox) or when
	// the target payload has discoverable machine-os streams (newer oc may omit RHCOS summary lines).
	fetchNode := strings.Contains(out, "#node-image-info")
	if !fetchNode {
		streams, err := c.releaseInfo.ListMachineOSStreams(toImagePullspec)
		if err != nil {
			chNodeInfo <- renderResult{err: err}
			return
		}
		fetchNode = len(streams) > 0
	}
	if !fetchNode {
		chNodeInfo <- renderResult{}
		return
	}

	nodeMD, err := rhcos.NodeImageSectionMarkdown(ctx, c.releaseInfo, fromImagePullspec, toImagePullspec, out)
	if err != nil {
		chNodeInfo <- renderResult{err: err}
		return
	}
	chNodeInfo <- renderResult{out: nodeMD}
}

func (c *Controller) renderChangeLog(w http.ResponseWriter, fromPull string, fromTag string, toPull string, toTag string, format string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		flusher = nopFlusher{}
	}

	flusher.Flush()

	// Cancel ctx when the handler returns so orphaned goroutines release
	// semaphore slots promptly instead of processing remaining streams.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan renderResult)
	chNodeInfo := make(chan renderResult, 1)

	// run the changelog in a goroutine because it may take significant time
	go c.getChangeLog(ctx, ch, chNodeInfo, fromPull, fromTag, toPull, toTag, format)

	var render renderResult
	select {
	case render = <-ch:
	case <-time.After(500 * time.Millisecond):
		fmt.Fprintf(w, `<p id="loading" class="alert alert-info">Loading changelog, this may take a while ...</p>`)
		flusher.Flush()
		select {
		case render = <-ch:
		case <-time.After(60 * time.Second):
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
			// make our links to other pages targets
			result = reInternalLink.ReplaceAllFunc(result, func(s []byte) []byte {
				if bytes.HasPrefix(s, []byte(`<a href="#`)) {
					return s
				}
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

	needsNode := strings.Contains(render.out, "#node-image-info")
	if !needsNode && render.err == nil && format != "json" {
		toImage, err := releasecontroller.GetImageInfo(c.releaseInfo, c.architecture, toPull)
		if err == nil {
			streams, err2 := c.releaseInfo.ListMachineOSStreams(toImage.GenerateDigestPullSpec())
			if err2 == nil && len(streams) > 0 {
				needsNode = true
			}
		}
	}
	if !needsNode {
		return
	}

	fmt.Fprintf(w, "<h2 id=\"node-image-info\">Node Image Info</h2>")
	fmt.Fprintf(w, `<p id="node_loading" class="alert alert-info">Loading node image info, this may take a while ...</p>`)
	flusher.Flush()

	select {
	case render = <-chNodeInfo:
	case <-time.After(60 * time.Second):
		render.err = fmt.Errorf("node image info is still loading, check back later")
	}
	fmt.Fprintf(w, `<style>#node_loading{display: none;}</style>`)
	if render.err != nil {
		fmt.Fprintf(w, `<p class="alert alert-danger">%s</p>`, fmt.Sprintf("Unable to show node image info: %s", render.err))
	} else {
		result := blackfriday.Run([]byte(render.out))
		if _, err := w.Write(result); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
