package bigquery

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"google.golang.org/api/iterator"
)

// FakeClient is a fake implementation of ClientInterface for testing
type FakeClient struct {
	mu            sync.Mutex
	Queries       []string
	Results       map[string][]any
	QueryErrors   map[string]error
	DefaultResult []any
	SummaryError  error
}

// fakeRowIterator implements RowIteratorInterface for testing
type fakeRowIterator struct {
	results []any
	index   int
}

func (f *fakeRowIterator) Next(dst any) error {
	if f.index >= len(f.results) {
		return iterator.Done
	}

	result := f.results[f.index]
	f.index++

	// Handle different destination types
	switch d := dst.(type) {
	case *map[string]any:
		if m, ok := result.(map[string]any); ok {
			*d = m
		}
	default:
		// For structs, use reflection to copy data
		if err := copyToStruct(result, dst); err != nil {
			return err
		}
	}

	return nil
}

// copyToStruct copies data from source to destination struct using reflection
func copyToStruct(src, dst any) error {
	// If source is already the same type as destination, just copy it
	srcVal := reflect.ValueOf(src)
	dstVal := reflect.ValueOf(dst)

	if dstVal.Kind() != reflect.Pointer {
		return fmt.Errorf("destination must be a pointer")
	}

	dstElem := dstVal.Elem()

	// If source is the same type, just assign it
	if srcVal.Type() == dstElem.Type() {
		dstElem.Set(srcVal)
		return nil
	}

	// If source is a map, copy fields by name
	if srcVal.Kind() == reflect.Map {
		srcMap, ok := src.(map[string]any)
		if !ok {
			return fmt.Errorf("source map is not map[string]interface{}")
		}

		for i := 0; i < dstElem.NumField(); i++ {
			field := dstElem.Type().Field(i)
			fieldName := field.Tag.Get("bigquery")
			if fieldName == "" {
				fieldName = field.Name
			}

			if val, ok := srcMap[fieldName]; ok {
				fieldVal := dstElem.Field(i)
				if fieldVal.CanSet() {
					valReflect := reflect.ValueOf(val)
					if valReflect.Type().AssignableTo(fieldVal.Type()) {
						fieldVal.Set(valReflect)
					}
				}
			}
		}
		return nil
	}

	return fmt.Errorf("unsupported source type: %T", src)
}

// NewFakeClient creates a new fake BigQuery client
func NewFakeClient() *FakeClient {
	return &FakeClient{
		Queries:     []string{},
		Results:     make(map[string][]any),
		QueryErrors: make(map[string]error),
	}
}

// Query implements ClientInterface for the fake client
func (f *FakeClient) Query(ctx context.Context, query string) (RowIteratorInterface, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Record the query
	f.Queries = append(f.Queries, query)

	// Check if there's an error configured for this query
	if err, ok := f.QueryErrors[query]; ok {
		return nil, err
	}

	// Return configured results for this query, or default results
	var results []any
	if queryResults, ok := f.Results[query]; ok {
		results = queryResults
	} else {
		results = f.DefaultResult
	}

	return &fakeRowIterator{results: results, index: 0}, nil
}

// SetQueryResult configures the fake to return specific results for a query
func (f *FakeClient) SetQueryResult(query string, results []any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Results[query] = results
}

// SetQueryError configures the fake to return an error for a query
func (f *FakeClient) SetQueryError(query string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.QueryErrors[query] = err
}

// GetQueries returns all queries that were executed
func (f *FakeClient) GetQueries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.Queries...)
}

// Reset clears all recorded queries and configured results
func (f *FakeClient) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Queries = []string{}
	f.Results = make(map[string][]any)
	f.QueryErrors = make(map[string]error)
	f.DefaultResult = nil
	f.SummaryError = nil
}

// GetReleaseQualifiersProwjobSummary implements the ClientInterface method for testing.
// It returns the results configured in DefaultResult.
func (f *FakeClient) GetReleaseQualifiersProwjobSummary(ctx context.Context, prowjobs []string) ([]ReleaseQualifiersProwjobSummaryResult, error) {
	return f.GetReleaseQualifiersProwjobSummaryWithFilters(ctx, prowjobs, nil)
}

// GetReleaseQualifiersProwjobSummaryWithFilters implements the ClientInterface method for testing.
// It returns the results configured in DefaultResult, ignoring the filter parameters.
func (f *FakeClient) GetReleaseQualifiersProwjobSummaryWithFilters(ctx context.Context, defaultJobs []string, filteredJobs []ProwjobQueryFilter) ([]ReleaseQualifiersProwjobSummaryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.SummaryError != nil {
		return nil, f.SummaryError
	}

	var results []ReleaseQualifiersProwjobSummaryResult
	for _, result := range f.DefaultResult {
		if r, ok := result.(ReleaseQualifiersProwjobSummaryResult); ok {
			results = append(results, r)
		}
	}

	return results, nil
}
