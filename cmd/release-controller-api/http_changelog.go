package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/russross/blackfriday"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	reInternalLink = regexp.MustCompile(`<a href="[^"]+">`)
	rePromotedFrom = regexp.MustCompile("Promoted from (.*):(.*)")
	reRHCoSDiff    = regexp.MustCompile(`\* Red Hat Enterprise Linux CoreOS upgraded from ((\d)(\d+)\.[\w\.\-]+) to ((\d)(\d+)\.[\w\.\-]+)\n`)
	reRHCoSVersion = regexp.MustCompile(`\* Red Hat Enterprise Linux CoreOS ((\d)(\d+)\.[\w\.\-]+)\n`)
)

type dockerImageConfig struct {
	Architecture string `json:"architecture"`
	Os           string `json:"os"`
}

type imageInfoConfig struct {
	Config *dockerImageConfig `json:"config"`
	Digest string             `json:"digest"`
	Name   string             `json:"name"`
}

func (c imageInfoConfig) generateDigestPullSpec() string {
	if strings.Contains(c.Name, "@sha256:") {
		return fmt.Sprintf("%s@%s", strings.Split(c.Name, "@sha256:")[0], c.Digest)
	}
	return fmt.Sprintf("%s@%s", strings.Split(c.Name, ":")[0], c.Digest)
}

type renderResult struct {
	out string
	err error
}

func (c *Controller) getImageInfo(pullSpec string) (*imageInfoConfig, error) {
	// Get the ImageInfo
	imageInfo, err := c.releaseInfo.ImageInfo(pullSpec, c.architecture)
	if err != nil {
		return nil, fmt.Errorf("could not get image info for from pullSpec %s: %v", pullSpec, err)
	}
	config := imageInfoConfig{}
	if err := json.Unmarshal([]byte(imageInfo), &config); err != nil {
		return nil, fmt.Errorf("could not unmarshal image info for from pullSpec %s: %v", pullSpec, err)
	}
	return &config, nil
}

func (c *Controller) getChangeLog(ch chan renderResult, fromPull string, fromTag string, toPull string, toTag string) {
	fromImage, err := c.getImageInfo(fromPull)
	if err != nil {
		ch <- renderResult{err: err}
		return
	}

	toImage, err := c.getImageInfo(toPull)
	if err != nil {
		ch <- renderResult{err: err}
		return
	}

	// Generate the change log from image digests
	out, err := c.releaseInfo.ChangeLog(fromImage.generateDigestPullSpec(), toImage.generateDigestPullSpec())
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

	// replace references to the previous version with links
	rePrevious, err := regexp.Compile(fmt.Sprintf(`([^\w:])%s(\W)`, regexp.QuoteMeta(fromTag)))
	if err != nil {
		ch <- renderResult{err: err}
		return
	}
	// do a best effort replacement to change out the headers
	out = strings.Replace(out, fmt.Sprintf(`# %s`, toTag), "", -1)
	if changed := strings.Replace(out, fmt.Sprintf(`## Changes from %s`, fromTag), "", -1); len(changed) != len(out) {
		out = fmt.Sprintf("## Changes from %s\n%s", fromTag, changed)
	}
	out = rePrevious.ReplaceAllString(out, fmt.Sprintf("$1[%s](/releasetag/%s)$2", fromTag, fromTag))

	// add link to tag from which current version promoted from
	out = rePromotedFrom.ReplaceAllString(out, fmt.Sprintf("Release %s was created from [$1:$2](/releasetag/$2)", toTag))

	// TODO: As we get more comfortable with these sorts of transformations, we could make them more generic.
	//       For now, this will have to do.
	if m := reRHCoSDiff.FindStringSubmatch(out); m != nil {
		fromRelease := m[1]
		fromStream := fmt.Sprintf("releases/rhcos-%s.%s%s", m[2], m[3], archExtension)
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
		toStream := fmt.Sprintf("releases/rhcos-%s.%s%s", m[5], m[6], archExtension)
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
				"arch":           []string{architecture},
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
		fromStream := fmt.Sprintf("releases/rhcos-%s.%s%s", m[2], m[3], archExtension)
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
}

func (c *Controller) renderChangeLog(w http.ResponseWriter, fromPull string, fromTag string, toPull string, toTag string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		flusher = nopFlusher{}
	}

	flusher.Flush()

	ch := make(chan renderResult)

	// run the changelog in a goroutine because it may take significant time
	go c.getChangeLog(ch, fromPull, fromTag, toPull, toTag)

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
