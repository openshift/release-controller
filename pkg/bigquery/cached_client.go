package bigquery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s.io/klog/v2"
)

var _ ClientInterface = (*CachedClient)(nil)

type cachedEntry struct {
	results   []ReleaseQualifiersProwjobSummaryResult
	expiresAt time.Time
}

type CachedClient struct {
	delegate ClientInterface
	ttl      time.Duration
	mu       sync.RWMutex
	cache    map[string]cachedEntry
}

func NewCachedClient(delegate ClientInterface, ttl time.Duration) *CachedClient {
	return &CachedClient{
		delegate: delegate,
		ttl:      ttl,
		cache:    make(map[string]cachedEntry),
	}
}

func (c *CachedClient) Query(ctx context.Context, query string) (RowIteratorInterface, error) {
	return c.delegate.Query(ctx, query)
}

func (c *CachedClient) GetReleaseQualifiersProwjobSummary(ctx context.Context, prowjobs []string) ([]ReleaseQualifiersProwjobSummaryResult, error) {
	return c.GetReleaseQualifiersProwjobSummaryWithFilters(ctx, prowjobs, nil)
}

func (c *CachedClient) GetReleaseQualifiersProwjobSummaryWithFilters(ctx context.Context, defaultJobs []string, filteredJobs []ProwjobQueryFilter) ([]ReleaseQualifiersProwjobSummaryResult, error) {
	key := buildCacheKey(defaultJobs, filteredJobs)

	c.mu.RLock()
	if entry, ok := c.cache[key]; ok && time.Now().Before(entry.expiresAt) {
		klog.V(5).Infof("BigQuery cache hit for key: %s", key)
		results := make([]ReleaseQualifiersProwjobSummaryResult, len(entry.results))
		copy(results, entry.results)
		c.mu.RUnlock()
		return results, nil
	}
	c.mu.RUnlock()

	results, err := c.delegate.GetReleaseQualifiersProwjobSummaryWithFilters(ctx, defaultJobs, filteredJobs)
	if err != nil {
		return nil, err
	}

	cached := make([]ReleaseQualifiersProwjobSummaryResult, len(results))
	copy(cached, results)

	c.mu.Lock()
	c.cache[key] = cachedEntry{
		results:   cached,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()

	klog.V(5).Infof("BigQuery cache miss, stored %d results for key: %s", len(results), key)
	return results, nil
}

func buildCacheKey(defaultJobs []string, filteredJobs []ProwjobQueryFilter) string {
	sorted := make([]string, len(defaultJobs))
	copy(sorted, defaultJobs)
	sort.Strings(sorted)

	var parts []string
	for _, f := range filteredJobs {
		parts = append(parts, fmt.Sprintf("%s=%s", f.Name, f.Interval))
	}
	sort.Strings(parts)

	return strings.Join(sorted, ",") + "|" + strings.Join(parts, ",")
}
