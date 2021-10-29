package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"github.com/openshift/release-controller/pkg/release-controller"
	"io"
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

type UpgradeGraph struct {
	lock         sync.Mutex
	to           map[string]map[string]*release_controller.UpgradeHistory
	from         map[string]sets.String
	architecture string
}

func NewUpgradeGraph(architecture string) *UpgradeGraph {
	return &UpgradeGraph{
		to:           make(map[string]map[string]*release_controller.UpgradeHistory),
		from:         make(map[string]sets.String),
		architecture: architecture,
	}
}

type upgradeEdge struct {
	From string
	To   string
}

func (g *UpgradeGraph) SummarizeUpgradesTo(toNames ...string) []release_controller.UpgradeHistory {
	g.lock.Lock()
	defer g.lock.Unlock()
	summaries := make([]release_controller.UpgradeHistory, 0, len(toNames)*2)
	for _, to := range toNames {
		for _, h := range g.to[to] {
			summaries = append(summaries, release_controller.UpgradeHistory{
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

func (g *UpgradeGraph) SummarizeUpgradesFrom(fromNames ...string) []release_controller.UpgradeHistory {
	g.lock.Lock()
	defer g.lock.Unlock()
	summaries := make([]release_controller.UpgradeHistory, 0, len(fromNames)*2)
	for _, from := range fromNames {
		for to := range g.from[from] {
			for _, h := range g.to[to] {
				summaries = append(summaries, release_controller.UpgradeHistory{
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

func (g *UpgradeGraph) UpgradesTo(toNames ...string) []release_controller.UpgradeHistory {
	g.lock.Lock()
	defer g.lock.Unlock()
	summaries := make([]release_controller.UpgradeHistory, 0, len(toNames)*2)
	for _, to := range toNames {
		for _, h := range g.to[to] {
			summaries = append(summaries, release_controller.UpgradeHistory{
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

func (g *UpgradeGraph) UpgradesFrom(fromNames ...string) []release_controller.UpgradeHistory {
	g.lock.Lock()
	defer g.lock.Unlock()
	summaries := make([]release_controller.UpgradeHistory, 0, len(fromNames)*2)
	refs := make(map[historyEdgeReference]*release_controller.UpgradeHistory)
	for _, from := range fromNames {
		for to := range g.from[from] {
			history := g.to[to][from]
			if history == nil {
				continue
			}
			key := historyEdgeReference{from, to}
			ref, ok := refs[key]
			if !ok {
				summaries = append(summaries, release_controller.UpgradeHistory{
					From:    from,
					To:      to,
					History: make(map[string]release_controller.UpgradeResult),
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

func copyHistory(h map[string]release_controller.UpgradeResult) map[string]release_controller.UpgradeResult {
	copied := make(map[string]release_controller.UpgradeResult, len(h))
	for k, v := range h {
		copied[k] = v
	}
	return copied
}

func (g *UpgradeGraph) Add(fromTag, toTag string, results ...release_controller.UpgradeResult) {
	if len(results) == 0 || len(fromTag) == 0 || len(toTag) == 0 {
		return
	}

	g.lock.Lock()
	defer g.lock.Unlock()
	g.addWithLock(fromTag, toTag, results...)
}

func (g *UpgradeGraph) addWithLock(fromTag, toTag string, results ...release_controller.UpgradeResult) {
	to, ok := g.to[toTag]
	if !ok {
		to = make(map[string]*release_controller.UpgradeHistory)
		g.to[toTag] = to
	}
	from, ok := to[fromTag]
	if !ok {
		from = &release_controller.UpgradeHistory{
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
		from.History = make(map[string]release_controller.UpgradeResult)
	}
	for _, result := range results {
		if len(result.URL) == 0 {
			continue
		}
		existing, ok := from.History[result.URL]
		if !ok || existing.State == release_controller.ReleaseVerificationStatePending && result.State != release_controller.ReleaseVerificationStatePending {
			from.History[result.URL] = result
			switch result.State {
			case release_controller.ReleaseVerificationStateFailed:
				from.Failure++
			case release_controller.ReleaseVerificationStateSucceeded:
				from.Success++
			}
		}
	}
}

func (g *UpgradeGraph) Histories() []release_controller.UpgradeHistory {
	g.lock.Lock()
	defer g.lock.Unlock()

	results := make([]release_controller.UpgradeHistory, 0, len(g.to)*5)
	for _, targets := range g.to {
		for _, history := range targets {
			copied := *history
			copied.History = nil
			results = append(results, copied)
		}
	}
	return results
}

func (g *UpgradeGraph) Records() []release_controller.UpgradeRecord {
	g.lock.Lock()
	defer g.lock.Unlock()

	records := make([]release_controller.UpgradeRecord, 0, len(g.to)*5)
	for to, targets := range g.to {
		for from, history := range targets {
			record := release_controller.UpgradeRecord{From: from, To: to, Results: make([]release_controller.UpgradeResult, 0, len(history.History))}
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
	var records []release_controller.UpgradeRecord
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
	}, 5*time.Minute, stopCh)
}
