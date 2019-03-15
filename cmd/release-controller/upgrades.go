package main

import (
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
)

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

type UpgradeResult struct {
	State string
	Url   string
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

func (g *UpgradeGraph) UpgradesFrom(fromNames ...string) []UpgradeHistory {
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
					History: copyHistory(h.History),
				})
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
		if len(result.Url) == 0 {
			continue
		}
		existing, ok := from.History[result.Url]
		if !ok || existing.State == releaseVerificationStatePending && result.State != releaseVerificationStatePending {
			from.History[result.Url] = result
			switch result.State {
			case releaseVerificationStateFailed:
				from.Failure++
			case releaseVerificationStateSucceeded:
				from.Success++
			}
		}
	}
}
