package bigquery

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloud.google.com/go/civil"
)

func TestCachedClient_InterfaceCompliance(t *testing.T) {
	var _ ClientInterface = (*CachedClient)(nil)
}

func TestCachedClient_CacheMiss(t *testing.T) {
	fake := NewFakeClient()
	fake.DefaultResult = []interface{}{
		ReleaseQualifiersProwjobSummaryResult{
			Release: "4.22.0-0.nightly-2026-04-06-112110",
			Name:    "job-a",
			State:   "success",
		},
	}

	cached := NewCachedClient(fake, 5*time.Minute)
	results, err := cached.GetReleaseQualifiersProwjobSummary(context.Background(), []string{"job-a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Release != "4.22.0-0.nightly-2026-04-06-112110" {
		t.Errorf("unexpected release: %s", results[0].Release)
	}
}

func TestCachedClient_CacheHit(t *testing.T) {
	fake := NewFakeClient()
	fake.DefaultResult = []interface{}{
		ReleaseQualifiersProwjobSummaryResult{
			Release: "4.22.0-0.nightly-2026-04-06-112110",
			Name:    "job-a",
			State:   "success",
		},
	}

	cached := NewCachedClient(fake, 5*time.Minute)
	ctx := context.Background()

	// First call — cache miss
	_, err := cached.GetReleaseQualifiersProwjobSummary(ctx, []string{"job-a"})
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}

	// Change the fake's results to verify the cache is used
	fake.DefaultResult = []interface{}{
		ReleaseQualifiersProwjobSummaryResult{
			Release: "different-release",
			Name:    "job-a",
			State:   "failure",
		},
	}

	// Second call — should return cached results
	results, err := cached.GetReleaseQualifiersProwjobSummary(ctx, []string{"job-a"})
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if results[0].Release != "4.22.0-0.nightly-2026-04-06-112110" {
		t.Errorf("expected cached release, got: %s", results[0].Release)
	}
}

func TestCachedClient_CacheExpiry(t *testing.T) {
	fake := NewFakeClient()
	fake.DefaultResult = []interface{}{
		ReleaseQualifiersProwjobSummaryResult{
			Release: "original",
			Name:    "job-a",
			State:   "success",
		},
	}

	cached := NewCachedClient(fake, 1*time.Millisecond)
	ctx := context.Background()

	// First call
	_, err := cached.GetReleaseQualifiersProwjobSummary(ctx, []string{"job-a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for expiry
	time.Sleep(5 * time.Millisecond)

	// Update fake results
	fake.DefaultResult = []interface{}{
		ReleaseQualifiersProwjobSummaryResult{
			Release: "refreshed",
			Name:    "job-a",
			State:   "success",
		},
	}

	// Should get fresh results
	results, err := cached.GetReleaseQualifiersProwjobSummary(ctx, []string{"job-a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Release != "refreshed" {
		t.Errorf("expected refreshed results after expiry, got: %s", results[0].Release)
	}
}

func TestCachedClient_ErrorNotCached(t *testing.T) {
	fake := NewFakeClient()
	fake.SummaryError = errors.New("bigquery unavailable")

	cached := NewCachedClient(fake, 5*time.Minute)
	ctx := context.Background()

	// First call — error
	_, err := cached.GetReleaseQualifiersProwjobSummary(ctx, []string{"job-a"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Clear the error and set results
	fake.SummaryError = nil
	fake.DefaultResult = []interface{}{
		ReleaseQualifiersProwjobSummaryResult{
			Release: "recovered",
			Name:    "job-a",
			State:   "success",
		},
	}

	// Should succeed now (error was not cached)
	results, err := cached.GetReleaseQualifiersProwjobSummary(ctx, []string{"job-a"})
	if err != nil {
		t.Fatalf("unexpected error after recovery: %v", err)
	}
	if results[0].Release != "recovered" {
		t.Errorf("expected recovered results, got: %s", results[0].Release)
	}
}

func TestCachedClient_DifferentParams(t *testing.T) {
	fake := NewFakeClient()
	fake.DefaultResult = []interface{}{
		ReleaseQualifiersProwjobSummaryResult{
			Release: "shared",
			Name:    "job-a",
			State:   "success",
		},
	}

	cached := NewCachedClient(fake, 5*time.Minute)
	ctx := context.Background()

	// Call with params A
	results1, err := cached.GetReleaseQualifiersProwjobSummary(ctx, []string{"job-a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Change fake results
	fake.DefaultResult = []interface{}{
		ReleaseQualifiersProwjobSummaryResult{
			Release: "different",
			Name:    "job-b",
			State:   "failure",
		},
	}

	// Call with params B — different cache key, should get fresh results
	results2, err := cached.GetReleaseQualifiersProwjobSummary(ctx, []string{"job-b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results1[0].Release == results2[0].Release {
		t.Error("different params should produce independent cache entries")
	}
	if results2[0].Release != "different" {
		t.Errorf("expected fresh results for new params, got: %s", results2[0].Release)
	}
}

func TestCachedClient_WithFilters(t *testing.T) {
	fake := NewFakeClient()
	fake.DefaultResult = []interface{}{
		ReleaseQualifiersProwjobSummaryResult{
			Release: "filtered-result",
			Name:    "job-c",
			State:   "success",
			StartTime: civil.DateTime{
				Date: civil.Date{Year: 2026, Month: 4, Day: 6},
				Time: civil.Time{Hour: 11, Minute: 21, Second: 10},
			},
		},
	}

	cached := NewCachedClient(fake, 5*time.Minute)
	ctx := context.Background()

	filters := []ProwjobQueryFilter{
		{Name: "job-c", Interval: "2 DAY"},
	}

	results, err := cached.GetReleaseQualifiersProwjobSummaryWithFilters(ctx, []string{"job-a"}, filters)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Release != "filtered-result" {
		t.Errorf("unexpected results: %+v", results)
	}

	// Second call with same filters should be cached
	fake.DefaultResult = []interface{}{
		ReleaseQualifiersProwjobSummaryResult{
			Release: "should-not-see-this",
			Name:    "job-c",
			State:   "failure",
		},
	}

	results2, err := cached.GetReleaseQualifiersProwjobSummaryWithFilters(ctx, []string{"job-a"}, filters)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results2[0].Release != "filtered-result" {
		t.Errorf("expected cached result, got: %s", results2[0].Release)
	}
}

func TestCachedClient_QueryPassthrough(t *testing.T) {
	fake := NewFakeClient()
	fake.SetQueryResult("SELECT 1", []interface{}{
		map[string]interface{}{"col": 1},
	})

	cached := NewCachedClient(fake, 5*time.Minute)
	iter, err := cached.Query(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iter == nil {
		t.Fatal("expected non-nil iterator")
	}

	queries := fake.GetQueries()
	if len(queries) != 1 || queries[0] != "SELECT 1" {
		t.Errorf("expected query passthrough, got: %v", queries)
	}
}

func TestBuildCacheKey(t *testing.T) {
	// Same jobs in different order should produce the same key
	key1 := buildCacheKey([]string{"job-b", "job-a"}, nil)
	key2 := buildCacheKey([]string{"job-a", "job-b"}, nil)
	if key1 != key2 {
		t.Errorf("expected same key for reordered jobs, got %q vs %q", key1, key2)
	}

	// Different filters should produce different keys
	key3 := buildCacheKey([]string{"job-a"}, []ProwjobQueryFilter{{Name: "job-c", Interval: "2 DAY"}})
	key4 := buildCacheKey([]string{"job-a"}, []ProwjobQueryFilter{{Name: "job-c", Interval: "7 DAY"}})
	if key3 == key4 {
		t.Error("different filters should produce different keys")
	}

	// Nil vs empty filters should produce the same key
	key5 := buildCacheKey([]string{"job-a"}, nil)
	key6 := buildCacheKey([]string{"job-a"}, []ProwjobQueryFilter{})
	if key5 != key6 {
		t.Errorf("nil and empty filters should produce same key, got %q vs %q", key5, key6)
	}
}
