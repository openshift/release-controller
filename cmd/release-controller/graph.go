package main

import (
	"encoding/json"
	"net/http"
	"sort"

	"k8s.io/apimachinery/pkg/labels"
)

type ReleaseNode struct {
	Version string `json:"version"`
	Image   string `json:"image"`
}

type ReleaseEdge []int

type ReleaseGraph struct {
	Nodes []ReleaseNode `json:"nodes"`
	Edges []ReleaseEdge `json:"edges"`
}

func (c *Controller) graphHandler(w http.ResponseWriter, req *http.Request) {
	imageStreams, err := c.imageStreamLister.ImageStreams(c.releaseNamespace).List(labels.Everything())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var streams []ReleaseStream
	for _, stream := range imageStreams {
		r, ok, err := c.releaseDefinition(stream)
		if err != nil || !ok {
			continue
		}
		s := ReleaseStream{
			Release: r,
			Tags:    tagsForRelease(r),
		}
		streams = append(streams, s)
	}

	graph := &ReleaseGraph{}
	for _, s := range streams {
		var nodes []ReleaseNode
		for _, tag := range s.Tags {
			nodes = append(nodes, ReleaseNode{
				Version: tag.Name,
				Image:   s.Release.Target.Status.PublicDockerImageRepository + ":" + tag.Name,
			})
		}
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].Version < nodes[j].Version
		})
		for i, node := range nodes {
			if i > 0 {
				graph.Edges = append(graph.Edges,
					ReleaseEdge{i - 1, i},
				)
			}
			if i > 1 {
				graph.Edges = append(graph.Edges,
					ReleaseEdge{i - 2, i},
				)
			}
			graph.Nodes = append(graph.Nodes, node)
		}
	}

	data, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.redhat.cincinnati.graph+json; version=1.0")
	w.Write(data)
}
