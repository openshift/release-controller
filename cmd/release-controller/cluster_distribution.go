package main

import (
	"errors"
	"math/rand"
	"slices"
	"sync"
	"time"
)

type ClusterDistribution interface {
	Get() string
	Contains(str string) bool
}

var ErrEmptyItems = errors.New("cannot create cluster distribution with no items")

type RandomClusterDistribution struct {
	random *rand.Rand
	pool   []string
}

func NewRandomClusterDistribution(items ...string) (ClusterDistribution, error) {
	if len(items) == 0 {
		return nil, ErrEmptyItems
	}
	cd := &RandomClusterDistribution{
		random: rand.New(rand.NewSource(time.Now().UnixNano())),
		pool:   items,
	}
	return cd, nil
}

func (r *RandomClusterDistribution) Get() string {
	return r.pool[r.random.Intn(len(r.pool))]
}

func (r *RandomClusterDistribution) Contains(str string) bool {
	return slices.Contains(r.pool, str)
}

type RoundRobinClusterDistribution struct {
	lock    sync.Mutex
	current int
	pool    []string
}

func NewRoundRobinClusterDistribution(items ...string) (ClusterDistribution, error) {
	if len(items) == 0 {
		return nil, ErrEmptyItems
	}
	cd := &RoundRobinClusterDistribution{
		pool: items,
	}
	return cd, nil
}

func (r *RoundRobinClusterDistribution) Get() string {
	r.lock.Lock()
	defer r.lock.Unlock()

	for ok := r.current >= len(r.pool); ok; ok = r.current >= len(r.pool) {
		r.current = r.current - len(r.pool)
	}

	result := r.pool[r.current]
	r.current++
	return result
}

func (r *RoundRobinClusterDistribution) Contains(str string) bool {
	return slices.Contains(r.pool, str)
}
