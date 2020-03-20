package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

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
}

func NewUpgradeGraph() *UpgradeGraph {
	return &UpgradeGraph{
		to:   make(map[string]map[string]*UpgradeHistory),
		from: make(map[string]sets.String),
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

	data, err := json.Marshal(records)
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

func syncGraphToSecret(graph *UpgradeGraph, update bool, secretClient kv1core.SecretInterface, ns, name string, stopCh <-chan struct{}, c *Controller) {
	// read initial state
	wait.PollImmediateUntil(5*time.Second, func() (bool, error) {
		secret, err := secretClient.Get(name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				glog.Errorf("No secret %s/%s exists to store upgrade state into", ns, name)
				return false, nil
			}
			if errors.IsForbidden(err) {
				glog.Errorf("Release controller doesn't have permission to get secret %s/%s to store upgrade state into", ns, name)
				return false, nil
			}
			glog.Errorf("Can't load initial state from secret %s/%s: %v", ns, name, err)
			return false, nil
		}
		if data := secret.Data["latest"]; len(data) > 0 {
			if err := graph.Load(bytes.NewReader(data)); err != nil {
				glog.Errorf("Can't load initial state from secret %s/%s: %v", ns, name, err)
			}
		}
		if c != nil {
			c.PruneGraph()
		}
		return true, nil
	}, stopCh)

	if !update {
		return
	}

	// wait a bit of time to let any other loops load what they can
	time.Sleep(15 * time.Second)

	// keep the secret up to date
	buf := &bytes.Buffer{}
	wait.Until(func() {
		buf.Reset()
		if err := graph.Save(buf); err != nil {
			glog.Errorf("Unable to calculate graph state: %v", err)
			return
		}
		secret, err := secretClient.Get(name, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("Can't read latest secret %s/%s: %v", ns, name, err)
			return
		}
		if secret.Data == nil {
			secret.Data = make(map[string][]byte)
		}
		secret.Data["latest"] = buf.Bytes()
		if _, err := secretClient.Update(secret); err != nil {
			glog.Errorf("Can't save state to secret %s/%s: %v", ns, name, err)
		}
		glog.V(2).Infof("Saved upgrade graph state to %s/%s", ns, name)
	}, 5*time.Minute, stopCh)
}

const releaseTagTimestampFormat = "2006-01-02-150405"

func releaseTagTimestamp(v string) (time.Time, error) {
	var t time.Time
	version, err := semverParseTolerant(v)
	if err != nil {
		return t, err
	}
	if len(version.Pre) == 0 {
		return t, fmt.Errorf("tag %s has no timestamp", v)
	}
	// release of format x.y.z-prefix-timestamp gets preifx-timetamp parsed into PreRelease array
	// last entry of this has timestamp string, possibly with a prefix
	prereleaseLast := version.Pre[len(version.Pre) - 1].VersionStr
	// format string expects a timestamp of equal length
	if len(prereleaseLast) < len(releaseTagTimestampFormat) {
		return t, fmt.Errorf("tag %s has no timestamp", v)
	}
	// Remove any prefixes from the timestamp and parse
	return time.Parse(releaseTagTimestampFormat, prereleaseLast[len(prereleaseLast) - len(releaseTagTimestampFormat):])
}

func (g *UpgradeGraph) Remove(fromTag, toTag string) {
	if len(fromTag) == 0 || len(toTag) == 0 {
		return
	}

	g.lock.Lock()
	defer g.lock.Unlock()
	g.removeWithLock(fromTag, toTag)
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

func (c *Controller) PruneGraph() {
	if len(c.graph.to) == 0 {
		return
	}
	now := time.Now()
	pruneThreshold := time.Hour * 730 // roughly a month
	tagList := make([]string, 0, len(c.graph.to))
	for tag := range c.graph.to {
		releaseTimestamp, err := releaseTagTimestamp(tag)
		if err != nil {
			// Release tag does not have an embedded date, skip
			continue
		}
		if now.Sub(releaseTimestamp) <= pruneThreshold {
			// Check only edges pointing to releases older than a month
			continue
		}
		tagList = append(tagList, tag)
	}
	if len(tagList) == 0 {
		return
	}
	tags, _ := c.findReleaseStreamTags(false, tagList...)
	var prune []string
	if len(tags) == 0 {
		prune = tagList
	} else {
		prune = make([]string, 0, len(tagList))
		for _, tag := range tagList {
			// Remove edges that point to releases that don't exist
			if tags[tag] == nil {
				prune = append(prune, tag)
				continue
			}

			// Remove tags with invalid pullspec
			tagPullSpec := findPublicImagePullSpec(tags[tag].Release.Target, tag)
			if len(tagPullSpec) == 0 {
				prune = append(prune, tag)
				continue
			}
			if _, err := c.releaseInfo.ReleaseInfo(tagPullSpec); err != nil {
				prune = append(prune, tag)
				continue
			}
		}
	}
	glog.V(1).Infof("Pruning %d/%d tags from release controller graph: %v", len(prune), len(c.graph.to), prune)
	for _, toTag := range prune {
		for fromTag := range c.graph.to[toTag] {
			c.graph.Remove(fromTag, toTag)
		}
	}
}