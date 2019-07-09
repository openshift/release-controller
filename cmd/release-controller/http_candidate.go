package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/blang/semver"
	"github.com/golang/glog"
	"github.com/gorilla/mux"

	"k8s.io/apimachinery/pkg/labels"

	imagev1 "github.com/openshift/api/image/v1"
)

const candidatePageHtml = `
{{ range $stream, $list := . }}
<h1>Release Candidates for {{ nextReleaseName $list }}</h1>
<hr>
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
<div class="row">
<div class="col">
	<table class="table text-nowrap">
		<thead>
			<tr>
				<th title="Candidate tag for next release">Name</th>
				<th title="Tag(s) of release this can upgrade FROM">Upgrades</th>
				<th title="Creation time">Creation time</th>
			</tr>
		</thead>
		<tbody>
		{{ range $candidate := $list.Items }}
			<tr>
				<td> <a href="/releasestream/releasetag/{{ $candidate.FromTag }}" >{{ $candidate.FromTag }} </a></td>
				<td>{{ range $prev := $candidate.UpgradeFrom }}
					<a href="/releasestream/{{ $prev }}/release/{{ $prev }}"> {{ $prev }} </a>, 
					{{ end }}
				</td>
				<td>{{ $candidate.CreationTime }}</td>
			</tr>
		{{ end }}
		</tbody>
	</table>
</div>
</div>
{{ end }}
`

func (c *Controller) httpReleaseCandidateList(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { glog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()
	vars := mux.Vars(req)
	releaseStreamName := vars["release"]
	successPercent := 80.0
	releaseCandidateList, err := c.findReleaseCandidates(successPercent, releaseStreamName)
	if err != nil {
		if err == errStreamNotFound {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if releaseCandidateList[releaseStreamName] == nil {
		releaseCandidateList[releaseStreamName] = &ReleaseCandidateList{}
	}

	switch req.URL.Query().Get("format") {
	case "json":
		data, err := json.MarshalIndent(&releaseCandidateList, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, string(data))
	default:
		fmt.Fprintf(w, htmlPageStart, "Release Status")
		page := template.Must(template.New("candidatePage").Funcs(
			template.FuncMap{
				"nextReleaseName": func(list *ReleaseCandidateList) string {
					if list == nil || list.Items == nil || len(list.Items) == 0 {
						return "next release"
					}
					return list.Items[0].Name
				},
			},
		).Parse(candidatePageHtml))

		if err := page.Execute(w, releaseCandidateList); err != nil {
			glog.Errorf("Unable to render page: %v", err)
		}
		fmt.Fprintln(w, htmlPageEnd)
	}
}

func (c *Controller) apiReleaseCandidate(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() { glog.V(4).Infof("rendered in %s", time.Now().Sub(start)) }()
	vars := mux.Vars(req)
	releaseStreamName := vars["release"]
	successPercent := 80.0
	releaseCandidateList, err := c.findReleaseCandidates(successPercent, releaseStreamName)
	if err != nil {
		if err == errStreamNotFound {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var candidate *ReleasePromoteJobParameters
	if releaseCandidateList[releaseStreamName] != nil && len(releaseCandidateList[releaseStreamName].Items) != 0 {
		candidate = &(releaseCandidateList[releaseStreamName].Items[0].ReleasePromoteJobParameters)
	}

	data, err := json.MarshalIndent(map[string]*ReleasePromoteJobParameters{"candidate": candidate}, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
	fmt.Fprintln(w)
}

type releaseInfoShort struct {
	Image      string               `json:"image"`
	References *imagev1.ImageStream `json:"references"`
}

func (c *Controller) findReleaseCandidates(upgradeSuccessPercent float64, releaseStreams ...string) (map[string]*ReleaseCandidateList, error) {
	releaseCandidates := make(map[string]*ReleaseCandidateList)
	if len(releaseStreams) == 0 {
		return releaseCandidates, nil
	}

	releaseStreamTagMap, ok := c.findReleaseByName(true, releaseStreams...)
	if !ok || len(releaseStreamTagMap) == 0 {
		return releaseCandidates, errStreamNotFound
	}

	stableReleases := make([]imagev1.TagReference, 0)

	stable, err := c.stableReleases()
	if err != nil {
		return releaseCandidates, err
	}
	for _, r := range stable.Releases {
		for _, tag := range r.Release.Source.Spec.Tags {
			if tag.Annotations[releaseAnnotationSource] != fmt.Sprintf("%s/%s", r.Release.Source.Namespace, r.Release.Source.Name) {
				continue
			}
			// Only consider stable versions with a parseable version
			if _, err := semverParseTolerant(tag.Name); err == nil {
				stableReleases = append(stableReleases, tag)
			}
		}
	}
	sort.Slice(stableReleases, func(i, j int) bool {
		vi, _ := semverParseTolerant(stableReleases[i].Name)
		vj, _ := semverParseTolerant(stableReleases[j].Name)
		return vi.GT(vj)
	})

	for _, stream := range releaseStreams {
		nextReleaseName := ""
		var latestPromotedTime int64 = 0
		nextVersion, promotedTime, err := c.nextVersionDetails(stream, stableReleases)
		if err != nil || nextVersion == nil {
			glog.Errorf("Unable to find next candidate for %s: %v", stream, err)
			continue
		}
		nextReleaseName = nextVersion.String()
		latestPromotedTime = promotedTime.Unix()

		candidates := make([]*ReleaseCandidate, 0)
		releaseTags := tagsForRelease(releaseStreamTagMap[stream].Release)
		for _, tag := range releaseTags {
			if tag.Annotations != nil && tag.Annotations[releaseAnnotationPhase] == releasePhaseAccepted &&
				tag.Annotations[releaseAnnotationCreationTimestamp] != "" {
				t, _ := time.Parse(time.RFC3339, tag.Annotations[releaseAnnotationCreationTimestamp])
				ts := t.Unix()
				if ts > latestPromotedTime {

					upgradeSuccess := make([]string, 0)
					upgrades := c.graph.UpgradesTo(tag.Name)
					for _, u := range upgrades {
						if u.Total == 0 {
							continue
						}
						if float64(100*u.Success)/float64(u.Total) > upgradeSuccessPercent {
							upgradeSuccess = append(upgradeSuccess, u.From)
						}
					}
					sort.Strings(upgradeSuccess)

					candidates = append(candidates, &ReleaseCandidate{
						ReleasePromoteJobParameters: ReleasePromoteJobParameters{
							FromTag:     tag.Name,
							Name:        nextReleaseName,
							UpgradeFrom: upgradeSuccess,
						},
						CreationTime: time.Unix(ts, 0).Format(time.RFC3339),
						Tag:          tag,
					})
				}
			}
		}
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].CreationTime > candidates[j].CreationTime
		})
		releaseCandidates[stream] = &ReleaseCandidateList{Items: candidates}
	}
	return releaseCandidates, nil
}

func (c *Controller) findReleaseByName(includeStableTags bool, names ...string) (map[string]*ReleaseStreamTag, bool) {
	needed := make(map[string]*ReleaseStreamTag)
	for _, name := range names {
		if len(name) == 0 {
			continue
		}
		needed[name] = nil
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

		if includeStableTags {
			if version, err := semverParseTolerant(r.Config.Name); err == nil || r.Config.As == releaseConfigModeStable {
				stable.Releases = append(stable.Releases, StableRelease{
					Release: r,
					Version: version,
				})
			}
		}
		if includeStableTags && remaining == 0 {
			continue
		}

		matched := false
		for _, name := range names {
			if r.Config.Name == name {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		needed[r.Config.Name] = &ReleaseStreamTag{
			Release: r,
			Stable:  stable,
		}
		remaining--
		if !includeStableTags && remaining == 0 {
			return needed, true
		}
	}
	if includeStableTags {
		sort.Sort(stable.Releases)
	}
	return needed, remaining == 0
}

// TODO: Add support for returning stable releases after rally point
func (c *Controller) stableReleases() (*StableReferences, error) {
	imageStreams, err := c.imageStreamLister.ImageStreams(c.releaseNamespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	stable := &StableReferences{}

	for _, stream := range imageStreams {
		r, ok, err := c.releaseDefinition(stream)
		if err != nil || !ok {
			continue
		}

		if r.Config.As == releaseConfigModeStable {
			version, _ := semverParseTolerant(r.Source.Name)
			stable.Releases = append(stable.Releases, StableRelease{
				Release: r,
				Version: version,
			})
		}
	}

	sort.Sort(stable.Releases)
	return stable, nil
}

func (c *Controller) tagPromotedFrom(tag *imagev1.TagReference) (*imagev1.TagReference, error) {
	// Call oc adm release info to get previous nightly info for the stable release
	op, err := c.releaseInfo.ReleaseInfo(tag.From.Name)
	if err != nil {
		// releaseinfo not found, old tag
		return nil, fmt.Errorf("Could not get release info for tag %s: %v", tag.From.Name, err)
	}

	releaseInfo := releaseInfoShort{}
	if err := json.Unmarshal([]byte(op), &releaseInfo); err != nil {
		return nil, fmt.Errorf("Could not unmarshal release info for tag %s: %v", tag.From.Name, err)
	}

	latestPromotedFrom := releaseInfo.References.Annotations[releaseAnnotationFromImageStream]
	// latestPromotedFrom has the format <namespace>/<imagestream name>
	isTokens := strings.Split(latestPromotedFrom, "/")
	if len(isTokens) != 2 {
		// not of the format <namespace>/<imagestream name>
		return nil, fmt.Errorf("Unrecognized imagestream format %s", latestPromotedFrom)
	}

	is, err := c.imageStreamLister.ImageStreams(isTokens[0]).Get(isTokens[1])
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, fmt.Errorf("No such imagestream %s", isTokens[1])
	}

	if len(is.Annotations) == 0 || len(is.Annotations[releaseAnnotationReleaseTag]) == 0 || len(is.Annotations[releaseAnnotationTarget]) == 0 {
		return nil, fmt.Errorf("Required annotations missing from imagestream %s", isTokens[1])
	}

	fromIsTokens := strings.Split(is.Annotations[releaseAnnotationTarget], "/")
	if len(fromIsTokens) != 2 {
		// not of the format <namespace>/<imagestream name>
		return nil, fmt.Errorf("Unrecognized imagestream format %s", latestPromotedFrom)
	}

	fromStream, err := c.imageStreamLister.ImageStreams(fromIsTokens[0]).Get(fromIsTokens[1])
	if err != nil {
		return nil, err
	}

	fromTag := findTagReference(fromStream, is.Annotations[releaseAnnotationReleaseTag])

	if fromTag != nil {
		return fromTag, nil
	}
	return nil, errStreamTagNotFound
}

func (c *Controller) nextVersionDetails(stream string, stable []imagev1.TagReference) (*semver.Version, *time.Time, error) {
	for _, tag := range stable {
		// Check if the stable version's <MAJOR>.<MINOR> matches any release stream that we are processing
		streamVersion, err := semverParseTolerant(stream)
		if err != nil {
			return nil, nil, err
		}

		stableVersion, err := semverParseTolerant(tag.Name)
		if err != nil || streamVersion.Major != stableVersion.Major && streamVersion.Minor != stableVersion.Minor {
			continue
		}

		fromTag, err := c.tagPromotedFrom(&tag)
		if err != nil {
			// Cannot get promoted tag
			return nil, nil, err
		}

		if fromTag.Annotations[releaseAnnotationName] != stream {
			continue
		}

		pt, err := time.Parse(time.RFC3339, fromTag.Annotations[releaseAnnotationCreationTimestamp])
		if err != nil {
			glog.Errorf("Unable to parse timestamp %s: %v", fromTag.Annotations[releaseAnnotationCreationTimestamp], err)
			continue
		}

		nextVersion, _ := incrementSemanticVersion(stableVersion)
		return &nextVersion, &pt, nil
	}
	// no stable releases matching version
	return nil, nil, nil
}
