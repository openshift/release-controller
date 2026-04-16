package main

import (
	"encoding/json"
	"net/http"
	"strings"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	"k8s.io/apimachinery/pkg/labels"
)

type ReleaseNode struct {
	Version string `json:"version"`
	Payload string `json:"payload"`
}

type ReleaseEdge []int

type ReleaseGraph struct {
	Nodes []ReleaseNode `json:"nodes"`
	Edges []ReleaseEdge `json:"edges"`
}

func (c *Controller) graphHandler(w http.ResponseWriter, req *http.Request) {
	imageStreams, err := c.releaseLister.List(labels.Everything())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	format := req.URL.Query().Get("format")
	channel := req.URL.Query().Get("channel")

	var nodeCount int
	var streams []ReleaseStream
	for _, stream := range imageStreams {
		r, ok, err := releasecontroller.ReleaseDefinition(stream, c.parsedReleaseConfigCache, c.eventRecorder, *c.releaseLister)
		if err != nil || !ok {
			continue
		}
		s := ReleaseStream{
			Release: r,
			Tags:    releasecontroller.SortedReleaseTags(r),
		}
		nodeCount += len(s.Tags)
		streams = append(streams, s)
	}

	histories := c.graph.Histories()

	switch format {
	case "", "cincinnati":
		nodesByName := make(map[string]int, nodeCount)
		nodes := make([]ReleaseNode, 0, nodeCount)
		for _, s := range streams {
			for _, tag := range s.Tags {
				nodesByName[tag.Name] = len(nodes)
				if id := releasecontroller.FindImageIDForTag(s.Release.Target, tag.Name); len(id) > 0 {
					nodes = append(nodes, ReleaseNode{
						Version: tag.Name,
						Payload: s.Release.Target.Status.PublicDockerImageRepository + "@" + id,
					})
				} else {
					nodes = append(nodes, ReleaseNode{
						Version: tag.Name,
						Payload: s.Release.Target.Status.PublicDockerImageRepository + ":" + tag.Name,
					})
				}
			}
		}

		edges := make([]ReleaseEdge, 0, len(histories))
		for _, history := range histories {
			switch {
			case channel == "", channel == "stable", channel == "stable-scos", channel == "next", channel == "next-scos":
				if history.Success == 0 {
					continue
				}
			case channel == "prerelease", channel == "nightly":
			case strings.HasPrefix(channel, "stable-scos-"):
				if history.Success == 0 {
					continue
				}
				branch := channel[len("stable-scos-"):] + "."
				if !strings.HasPrefix(history.To, branch) {
					continue
				}
			case strings.HasPrefix(channel, "stable-"):
				if history.Success == 0 {
					continue
				}
				branch := channel[len("stable-"):] + "."
				if !strings.HasPrefix(history.To, branch) {
					continue
				}
			case strings.HasPrefix(channel, "next-scos-"):
				if history.Success == 0 {
					continue
				}
				branch := channel[len("next-scos-"):] + "."
				if !strings.HasPrefix(history.To, branch) {
					continue
				}
			case strings.HasPrefix(channel, "next-"):
				if history.Success == 0 {
					continue
				}
				branch := channel[len("next-"):] + "."
				if !strings.HasPrefix(history.To, branch) {
					continue
				}
			case strings.HasPrefix(channel, "prerelease-"):
				branch := channel[len("prerelease-"):] + "."
				if !strings.HasPrefix(history.To, branch) {
					continue
				}
			case strings.HasPrefix(channel, "nightly-"):
				branch := channel[len("nightly-"):] + ".0-0.nightly-"
				if !strings.HasPrefix(history.To, branch) {
					continue
				}
			default:
				http.Error(w, "Unsupported ?channel, must be '', 'prerelease', 'prerelease-*', 'nightly', 'nightly-*', 'stable', or 'stable-*", http.StatusBadRequest)
				return
			}
			to, ok := nodesByName[history.To]
			if !ok {
				continue
			}
			from, ok := nodesByName[history.From]
			if !ok {
				continue
			}
			edges = append(edges, ReleaseEdge{from, to})
		}

		graph := &ReleaseGraph{
			Nodes: nodes,
			Edges: edges,
		}

		data, err := json.MarshalIndent(graph, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/vnd.redhat.cincinnati.graph+json; version=1.0")
		if _, err := w.Write(data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	default:
		http.Error(w, "Unsupported ?format, must be 'cincinnati' (default)", http.StatusBadRequest)
	}
}
