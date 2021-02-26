package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/labels"
	"sort"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"

	"k8s.io/apimachinery/pkg/util/sets"
	kv1core "k8s.io/client-go/kubernetes/typed/core/v1"
)

type UpgradeResult struct {
	State string `json:"state"`
	URL   string `json:"url"`
}

type UpgradeRecord struct {
	From    string          `json:"from"`
	To      string          `json:"to"`
	Results []UpgradeResult `json:"results"`
}

type UpgradeGraph struct {
	lock sync.Mutex
	to   map[string]map[string]*UpgradeHistory
	from map[string]sets.String
	architecture string
}

func NewUpgradeGraph(architecture string) *UpgradeGraph {
	return &UpgradeGraph{
		to:   make(map[string]map[string]*UpgradeHistory),
		from: make(map[string]sets.String),
		architecture: architecture,
	}
}

type upgradeEdge struct {
	From string
	To   string
}

type UpgradeHistory struct {
	From string
	To   string

	Success int
	Failure int
	Total   int

	History map[string]UpgradeResult
}

func (g *UpgradeGraph) SummarizeUpgradesTo(toNames ...string) []UpgradeHistory {
	g.lock.Lock()
	defer g.lock.Unlock()
	summaries := make([]UpgradeHistory, 0, len(toNames)*2)
	for _, to := range toNames {
		for _, h := range g.to[to] {
			summaries = append(summaries, UpgradeHistory{
				From:    h.From,
				To:      to,
				Success: h.Success,
				Failure: h.Failure,
				Total:   len(h.History),
			})
		}
	}
	return summaries
}

func (g *UpgradeGraph) SummarizeUpgradesFrom(fromNames ...string) []UpgradeHistory {
	g.lock.Lock()
	defer g.lock.Unlock()
	summaries := make([]UpgradeHistory, 0, len(fromNames)*2)
	for _, from := range fromNames {
		for to := range g.from[from] {
			for _, h := range g.to[to] {
				summaries = append(summaries, UpgradeHistory{
					From:    from,
					To:      to,
					Success: h.Success,
					Failure: h.Failure,
					Total:   len(h.History),
				})
			}
		}
	}
	return summaries
}

func (g *UpgradeGraph) UpgradesTo(toNames ...string) []UpgradeHistory {
	g.lock.Lock()
	defer g.lock.Unlock()
	summaries := make([]UpgradeHistory, 0, len(toNames)*2)
	for _, to := range toNames {
		for _, h := range g.to[to] {
			summaries = append(summaries, UpgradeHistory{
				From:    h.From,
				To:      to,
				Success: h.Success,
				Failure: h.Failure,
				Total:   len(h.History),
				History: copyHistory(h.History),
			})
		}
	}
	return summaries
}

type historyEdgeReference struct {
	from string
	to   string
}

func (g *UpgradeGraph) UpgradesFrom(fromNames ...string) []UpgradeHistory {
	g.lock.Lock()
	defer g.lock.Unlock()
	summaries := make([]UpgradeHistory, 0, len(fromNames)*2)
	refs := make(map[historyEdgeReference]*UpgradeHistory)
	for _, from := range fromNames {
		for to := range g.from[from] {
			history := g.to[to][from]
			if history == nil {
				continue
			}
			key := historyEdgeReference{from, to}
			ref, ok := refs[key]
			if !ok {
				summaries = append(summaries, UpgradeHistory{
					From:    from,
					To:      to,
					History: make(map[string]UpgradeResult),
				})
				ref = &summaries[len(summaries)-1]
				refs[key] = ref
			}

			ref.Success += history.Success
			ref.Failure += history.Failure
			ref.Total += len(history.History)
			for k, v := range history.History {
				ref.History[k] = v
			}
		}
	}
	return summaries
}

func copyHistory(h map[string]UpgradeResult) map[string]UpgradeResult {
	copied := make(map[string]UpgradeResult, len(h))
	for k, v := range h {
		copied[k] = v
	}
	return copied
}

func (g *UpgradeGraph) Add(fromTag, toTag string, results ...UpgradeResult) {
	if len(results) == 0 || len(fromTag) == 0 || len(toTag) == 0 {
		return
	}

	g.lock.Lock()
	defer g.lock.Unlock()
	g.addWithLock(fromTag, toTag, results...)
}

func (g *UpgradeGraph) addWithLock(fromTag, toTag string, results ...UpgradeResult) {
	to, ok := g.to[toTag]
	if !ok {
		to = make(map[string]*UpgradeHistory)
		g.to[toTag] = to
	}
	from, ok := to[fromTag]
	if !ok {
		from = &UpgradeHistory{
			From: fromTag,
			To:   toTag,
		}
		to[fromTag] = from
		set, ok := g.from[fromTag]
		if !ok {
			set = sets.NewString()
			g.from[fromTag] = set
		}
		set.Insert(toTag)
	}
	if from.History == nil {
		from.History = make(map[string]UpgradeResult)
	}
	for _, result := range results {
		if len(result.URL) == 0 {
			continue
		}
		existing, ok := from.History[result.URL]
		if !ok || existing.State == releaseVerificationStatePending && result.State != releaseVerificationStatePending {
			from.History[result.URL] = result
			switch result.State {
			case releaseVerificationStateFailed:
				from.Failure++
			case releaseVerificationStateSucceeded:
				from.Success++
			}
		}
	}
}

func (g *UpgradeGraph) Histories() []UpgradeHistory {
	g.lock.Lock()
	defer g.lock.Unlock()

	results := make([]UpgradeHistory, 0, len(g.to)*5)
	for _, targets := range g.to {
		for _, history := range targets {
			copied := *history
			copied.History = nil
			results = append(results, copied)
		}
	}
	return results
}

func (g *UpgradeGraph) Records() []UpgradeRecord {
	g.lock.Lock()
	defer g.lock.Unlock()

	records := make([]UpgradeRecord, 0, len(g.to)*5)
	for to, targets := range g.to {
		for from, history := range targets {
			record := UpgradeRecord{From: from, To: to, Results: make([]UpgradeResult, 0, len(history.History))}
			for _, result := range history.History {
				record.Results = append(record.Results, result)
			}
			records = append(records, record)
		}
	}
	return records
}

func (g *UpgradeGraph) Save(w io.Writer) error {
	data, err := json.Marshal(g.orderedRecords())
	if err != nil {
		return err
	}
	gw := gzip.NewWriter(w)
	if _, err := gw.Write(data); err != nil {
		return err
	}
	return gw.Close()
}

func (g *UpgradeGraph) Load(r io.Reader) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	var records []UpgradeRecord
	if err := json.NewDecoder(gr).Decode(&records); err != nil {
		return err
	}

	g.lock.Lock()
	defer g.lock.Unlock()

	for _, record := range records {
		g.addWithLock(record.From, record.To, record.Results...)
	}
	return err
}

func syncGraphToSecret(graph *UpgradeGraph, update bool, secretClient kv1core.SecretInterface, ns, name string, stopCh <-chan struct{}) {
	loadUpgradeGraph(graph, secretClient, ns, name, stopCh)

	if !update {
		return
	}

	// wait a bit of time to let any other loops load what they can
	time.Sleep(15 * time.Second)

	// keep the secret up to date
	buf := &bytes.Buffer{}
	wait.Until(func() {
		buf.Reset()
		saveUpgradeGraph(buf, graph, secretClient, ns, name)
	}, 5*time.Minute, stopCh)
}

func loadUpgradeGraph(graph *UpgradeGraph, secretClient kv1core.SecretInterface, ns, name string, stopCh <-chan struct{}) {
	// read initial state
	wait.PollImmediateUntil(5*time.Second, func() (bool, error) {
		secret, err := secretClient.Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				klog.Errorf("No secret %s/%s exists to store upgrade state into", ns, name)
				return false, nil
			}
			if errors.IsForbidden(err) {
				klog.Errorf("Release controller doesn't have permission to get secret %s/%s to store upgrade state into", ns, name)
				return false, nil
			}
			klog.Errorf("Can't load initial state from secret %s/%s: %v", ns, name, err)
			return false, nil
		}
		if data := secret.Data["latest"]; len(data) > 0 {
			if err := graph.Load(bytes.NewReader(data)); err != nil {
				klog.Errorf("Can't load initial state from secret %s/%s: %v", ns, name, err)
			}
		}
		return true, nil
	}, stopCh)
}

func saveUpgradeGraph(buf *bytes.Buffer, graph *UpgradeGraph, secretClient kv1core.SecretInterface, ns, name string) {
	if err := graph.Save(buf); err != nil {
		klog.Errorf("Unable to calculate graph state: %v", err)
		return
	}
	secret, err := secretClient.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Can't read latest secret %s/%s: %v", ns, name, err)
		return
	}
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data["latest"] = buf.Bytes()
	if _, err := secretClient.Update(context.TODO(), secret, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("Can't save state to secret %s/%s: %v", ns, name, err)
	}
	klog.V(2).Infof("Saved upgrade graph state to %s/%s", ns, name)
}

func (g *UpgradeGraph) orderedRecords() []UpgradeRecord {
	records := g.Records()
	// put the records into a stable order
	sort.Slice(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if a.To == b.To {
			return a.From < b.From
		}
		return a.To < b.To
	})
	for _, record := range records {
		sort.Slice(record.Results, func(i, j int) bool {
			return record.Results[i].URL < record.Results[j].URL
		})
	}
	return records
}

const (
	PruneGraphPrintSecret = "secret"
	PruneGraphPrintDebug = "debug"
)

func (c *Controller) pruneGraph(secretClient kv1core.SecretInterface, ns, name string, printOption string, confirm bool) error {
	stopCh := wait.NeverStop

	loadUpgradeGraph(c.graph, secretClient, ns, name, stopCh)

	imageStreams, err := c.releaseLister.ImageStreams(ns).List(labels.Everything())
	if err != nil {
		return err
	}

	var stableTagList []string

	for _, imageStream := range imageStreams {
		r, ok, err := c.releaseDefinition(imageStream)
		if err != nil || !ok {
			continue
		}
		if r.Config.As == releaseConfigModeStable {
			for _, tag := range imageStream.Spec.Tags {
				stableTagList = append(stableTagList, tag.Name)
			}
		}
	}

	// To tags that are not present in the stable tags
	toMissingList := make([]string, 0, len(c.graph.to))
	for tag := range c.graph.to {
		if ! stringInSlice(tag, stableTagList) {
			toMissingList = append(toMissingList, tag)
		}
	}

	// From tags that are not present in the stable tags
	fromMissingList := make([]string, 0, len(c.graph.from))
	for tag := range c.graph.from {
		if ! stringInSlice(tag, stableTagList) {
			fromMissingList = append(fromMissingList, tag)
		}
	}

	// To tags that are not in the From tags
	pruneTagList := make([]string, 0, len(c.graph.to))
	for _, tag := range toMissingList {
		if ! stringInSlice(tag, fromMissingList) {
			pruneTagList = append(pruneTagList, tag)
		}
	}

	// From tags that are not in the To tags
	for _, tag := range fromMissingList {
		if ! stringInSlice(tag, toMissingList) {
			pruneTagList = append(pruneTagList, tag)
		}
	}

	// Tags that are in both the From and To lists
	for _, tag := range toMissingList {
		if stringInSlice(tag, fromMissingList) {
			pruneTagList = append(pruneTagList, tag)
		}
	}

	klog.V(2).Infof("Pruning %d/%d tags from release controller graph\n", len(pruneTagList), len(c.graph.to))

	// Prune graph
	c.graph.lock.Lock()
	for _, toTag := range pruneTagList {
		for fromTag := range c.graph.to[toTag] {
			c.graph.removeWithLock(fromTag, toTag)
		}
	}
	c.graph.lock.Unlock()

	if confirm {
		buf := &bytes.Buffer{}
		saveUpgradeGraph(buf, c.graph, secretClient, ns, name)
	} else {
		switch printOption {
		case PruneGraphPrintDebug:
			c.graph.prettyPrint()
		case PruneGraphPrintSecret:
			c.graph.printSecretPayload()
		}
	}

	return nil
}

func (g *UpgradeGraph) removeWithLock(fromTag, toTag string) {
	delete(g.to[toTag], fromTag)
	if len(g.to[toTag]) == 0 {
		delete(g.to, toTag)
	}
	g.from[fromTag].Delete(toTag)
	if g.from[fromTag].Len() == 0 {
		delete(g.from, fromTag)
	}
}

func (g *UpgradeGraph) prettyPrint() {
	json, err := json.MarshalIndent(g.orderedRecords(), "", "    ")
	if err != nil {
		klog.V(1).Infof("Unable to marshal graph: %v", err)
	}
	fmt.Printf("%s\n", json)
}

func (g *UpgradeGraph) printSecretPayload() {
	buf := &bytes.Buffer{}
	if err := g.Save(buf); err != nil {
		klog.Errorf("Unable to calculate graph state: %v", err)
		return
	}
	str := base64.StdEncoding.EncodeToString(buf.Bytes())
	fmt.Println(str)
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}
