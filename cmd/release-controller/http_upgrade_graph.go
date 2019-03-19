package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"

	"github.com/awalterschulze/gographviz"
	"github.com/golang/glog"

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
	imageStreams, err := c.imageStreamLister.ImageStreams(c.releaseNamespace).List(labels.Everything())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	format := req.URL.Query().Get("format")
	mode := req.URL.Query().Get("mode")

	var nodeCount int
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
				nodes = append(nodes, ReleaseNode{
					Version: tag.Name,
					Payload: s.Release.Target.Status.PublicDockerImageRepository + ":" + tag.Name,
				})
			}
		}

		edges := make([]ReleaseEdge, 0, len(histories))
		for _, history := range histories {
			switch mode {
			case "", "passing":
				if history.Success == 0 {
					continue
				}
			case "failing":
				if history.Failure == 0 {
					continue
				}
			case "all":
			default:
				http.Error(w, "Unsupported ?mode, must be 'passing', 'all', 'failing'", http.StatusBadRequest)
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
		w.Write(data)

	case "dot", "svg":
		g := gographviz.NewGraph()
		g.SetDir(true)
		g.SetName("Upgrades")
		if err := g.AddAttr("Upgrades", string(gographviz.RankDir), "TB"); err != nil {
			http.Error(w, fmt.Sprintf("Unable to add attribute to graph: %v", err), http.StatusBadRequest)
			return
		}
		nodeLabels := make(map[string]int, nodeCount)
		index := 0
		for _, s := range streams {
			for _, tag := range s.Tags {
				nodeLabels[tag.Name] = index
				if err := g.AddNode("Upgrades", "tag"+strconv.Itoa(index), map[string]string{
					string(gographviz.Label): fmt.Sprintf(`%q`, tag.Name),
					string(gographviz.Shape): "record",
				}); err != nil {
					http.Error(w, fmt.Sprintf("Unable to add edge to graph: %v", err), http.StatusBadRequest)
					return
				}
				index++
			}
		}
		for _, history := range histories {
			from, ok := nodeLabels[history.From]
			if !ok {
				continue
			}
			to, ok := nodeLabels[history.To]
			if !ok {
				continue
			}
			attrs := map[string]string{}
			if history.Success == 0 && history.Failure > 0 {
				attrs[string(gographviz.Style)] = "dashed"
				attrs[string(gographviz.Color)] = "red"
			}
			if err := g.AddEdge("tag"+strconv.Itoa(from), "tag"+strconv.Itoa(to), true, attrs); err != nil {
				http.Error(w, fmt.Sprintf("Unable to add edge to graph: %v", err), http.StatusBadRequest)
				return
			}
		}

		if format == "svg" {
			cmd := exec.Command("dot", "-Tsvg")
			cmd.Stdin = bytes.NewBufferString(g.String())
			buf := &bytes.Buffer{}
			cmd.Stderr = buf
			out, err := cmd.Output()
			if execErr, ok := err.(*exec.Error); ok && execErr.Err == exec.ErrNotFound {
				http.Error(w, "The 'dot' binary is not installed", http.StatusBadRequest)
				return
			}
			if err != nil {
				glog.Errorf("dot failed:\n%s", buf.String())
				http.Error(w, fmt.Sprintf("Unable to render graph: %v", err), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Write(out)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(g.String()))

	default:
		http.Error(w, "Unsupported ?format, must be 'dot', 'cincinnati' (default)", http.StatusBadRequest)
	}
}
