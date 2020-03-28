package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"text/template"

	viz "github.com/awalterschulze/gographviz"
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
	channel := req.URL.Query().Get("channel")

	var nodeCount int
	var streams []ReleaseStream
	for _, stream := range imageStreams {
		r, ok, err := c.releaseDefinition(stream)
		if err != nil || !ok {
			continue
		}
		s := ReleaseStream{
			Release: r,
			Tags:    sortedReleaseTags(r),
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
				if id := findImageIDForTag(s.Release.Target, tag.Name); len(id) > 0 {
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
			case channel == "", channel == "stable":
				if history.Success == 0 {
					continue
				}
			case channel == "prerelease", channel == "nightly":
			case strings.HasPrefix(channel, "stable-"):
				if history.Success == 0 {
					continue
				}
				branch := channel[len("stable-"):] + "."
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
		w.Write(data)

	case "dot", "svg", "png":
		g := viz.NewGraph()
		g.SetDir(true)
		g.SetName("Upgrades")
		g.AddAttr("Upgrades", string(viz.RankDir), "BT")
		g.AddAttr("Upgrades", string(viz.LabelLOC), "t")
		nodeLabels := make(map[string]int, nodeCount)
		index := 0
		for j, s := range streams {
			subgraphName := fmt.Sprintf("cluster_%d", j)
			subgraphLabel := fmt.Sprintf("%q", "Stream "+s.Release.Config.Name)
			if err := g.AddSubGraph("Upgrades", subgraphName, map[string]string{
				string(viz.Label): subgraphLabel,
			}); err != nil {
				http.Error(w, fmt.Sprintf("Unable to add subgraph: %v", err), http.StatusBadRequest)
				return
			}
			for i, tag := range s.Tags {
				nodeLabels[tag.Name] = index
				attrs := map[string]string{
					string(viz.Label): fmt.Sprintf(`%q`, tag.Name),
					string(viz.Shape): "record",
					string(viz.HREF):  fmt.Sprintf(`"/releasetag/%s"`, template.HTMLEscapeString(tag.Name)),
				}
				if phase := tag.Annotations[releaseAnnotationPhase]; phase == releasePhaseRejected {
					attrs[string(viz.Color)] = "red"
				}
				if err := g.AddNode(subgraphName, dotNodeName(index), attrs); err != nil {
					http.Error(w, fmt.Sprintf("Unable to add node to graph: %v", err), http.StatusBadRequest)
					return
				}
				if i > 0 {
					if err := g.AddEdge(dotNodeName(index), dotNodeName(index-1), true, map[string]string{
						string(viz.Style):  "invis",
						string(viz.Weight): "5",
					}); err != nil {
						http.Error(w, fmt.Sprintf("Unable to add edge to graph: %v", err), http.StatusBadRequest)
						return
					}
				}
				index++
			}
		}
	Edges:
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
				attrs[string(viz.Style)] = "dashed"
				attrs[string(viz.Color)] = "red"
				attrs[string(viz.Weight)] = "1"
			} else {
				attrs[string(viz.Weight)] = "2"
			}

			if dsts := g.Edges.SrcToDsts[dotNodeName(from)]; dsts != nil {
				if edges := dsts[dotNodeName(to)]; len(edges) > 0 {
					for _, edge := range edges {
						if edge.Attrs[viz.Style] == "invis" {
							for k := range edge.Attrs {
								delete(edge.Attrs, k)
							}
							for k, v := range attrs {
								edge.Attrs[viz.Attr(k)] = v
							}
							edge.Attrs[viz.Weight] = "5"
							continue Edges
						}
					}
				}
			}

			if err := g.AddEdge(dotNodeName(from), dotNodeName(to), true, attrs); err != nil {
				http.Error(w, fmt.Sprintf("Unable to add edge to graph: %v", err), http.StatusBadRequest)
				return
			}
		}

		switch format {
		case "dot":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(g.String()))
			return
		case "svg", "png":
			cmd := exec.Command("dot", fmt.Sprintf("-T%s", format))
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
			switch format {
			case "svg":
				w.Header().Set("Content-Type", "image/svg+xml")
			case "png":
				w.Header().Set("Content-Type", "image/png")
			}
			w.Write(out)
			return
		}

	default:
		http.Error(w, "Unsupported ?format, must be 'dot', 'cincinnati' (default)", http.StatusBadRequest)
	}
}

func dotNodeName(index int) string {
	return strconv.Itoa(index)
}
