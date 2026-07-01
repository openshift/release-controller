package bigquery

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/api/iterator"
)

func TestFakeClient_Query(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		setupFake     func(*FakeClient)
		wantErr       bool
		wantQueries   int
		validateQuery func(*testing.T, *FakeClient)
	}{
		{
			name:  "successful query execution",
			query: "SELECT * FROM table",
			setupFake: func(fc *FakeClient) {
				fc.SetQueryResult("SELECT * FROM table", []any{
					map[string]any{"col1": "value1", "col2": 123},
				})
			},
			wantErr:     false,
			wantQueries: 1,
			validateQuery: func(t *testing.T, fc *FakeClient) {
				queries := fc.GetQueries()
				if len(queries) != 1 {
					t.Errorf("expected 1 query, got %d", len(queries))
				}
				if queries[0] != "SELECT * FROM table" {
					t.Errorf("expected query 'SELECT * FROM table', got '%s'", queries[0])
				}
			},
		},
		{
			name:  "query returns error",
			query: "SELECT * FROM invalid_table",
			setupFake: func(fc *FakeClient) {
				fc.SetQueryError("SELECT * FROM invalid_table", errors.New("table not found"))
			},
			wantErr:     true,
			wantQueries: 1,
		},
		{
			name:  "query with default results",
			query: "SELECT COUNT(*) FROM table",
			setupFake: func(fc *FakeClient) {
				fc.DefaultResult = []any{
					map[string]any{"count": 42},
				}
			},
			wantErr:     false,
			wantQueries: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := NewFakeClient()
			if tt.setupFake != nil {
				tt.setupFake(fc)
			}

			ctx := context.Background()
			iter, err := fc.Query(ctx, tt.query)

			if (err != nil) != tt.wantErr {
				t.Errorf("Query() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && iter == nil {
				t.Error("Query() returned nil iterator without error")
			}

			queries := fc.GetQueries()
			if len(queries) != tt.wantQueries {
				t.Errorf("expected %d queries, got %d", tt.wantQueries, len(queries))
			}

			if tt.validateQuery != nil {
				tt.validateQuery(t, fc)
			}
		})
	}
}

func TestFakeClient_IteratorNext(t *testing.T) {
	fc := NewFakeClient()
	fc.SetQueryResult("SELECT * FROM table", []any{
		map[string]any{"id": 1, "name": "first"},
		map[string]any{"id": 2, "name": "second"},
	})

	ctx := context.Background()
	iter, err := fc.Query(ctx, "SELECT * FROM table")
	if err != nil {
		t.Fatalf("Query() failed: %v", err)
	}

	// Read first row
	var row1 map[string]any
	err = iter.Next(&row1)
	if err != nil {
		t.Fatalf("Next() failed on first row: %v", err)
	}
	if row1["id"] != 1 {
		t.Errorf("expected id=1, got %v", row1["id"])
	}

	// Read second row
	var row2 map[string]any
	err = iter.Next(&row2)
	if err != nil {
		t.Fatalf("Next() failed on second row: %v", err)
	}
	if row2["id"] != 2 {
		t.Errorf("expected id=2, got %v", row2["id"])
	}

	// Reading beyond results should return iterator.Done
	var row3 map[string]any
	err = iter.Next(&row3)
	if err != iterator.Done {
		t.Errorf("expected iterator.Done, got %v", err)
	}
}

func TestFakeClient_MultipleQueries(t *testing.T) {
	fc := NewFakeClient()
	fc.SetQueryResult("SELECT * FROM table1", []any{
		map[string]any{"table": "table1"},
	})
	fc.SetQueryResult("SELECT * FROM table2", []any{
		map[string]any{"table": "table2"},
	})

	ctx := context.Background()

	// Execute first query
	_, err := fc.Query(ctx, "SELECT * FROM table1")
	if err != nil {
		t.Fatalf("First query failed: %v", err)
	}

	// Execute second query
	_, err = fc.Query(ctx, "SELECT * FROM table2")
	if err != nil {
		t.Fatalf("Second query failed: %v", err)
	}

	queries := fc.GetQueries()
	if len(queries) != 2 {
		t.Errorf("expected 2 queries, got %d", len(queries))
	}
}

func TestFakeClient_Reset(t *testing.T) {
	fc := NewFakeClient()
	fc.SetQueryResult("SELECT * FROM table", []any{
		map[string]any{"col": "value"},
	})

	ctx := context.Background()
	_, err := fc.Query(ctx, "SELECT * FROM table")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(fc.GetQueries()) != 1 {
		t.Error("expected 1 query before reset")
	}

	fc.Reset()

	if len(fc.GetQueries()) != 0 {
		t.Error("expected 0 queries after reset")
	}

	if len(fc.Results) != 0 {
		t.Error("expected no results after reset")
	}
}

func TestFakeClient_SummaryRecordsParameters(t *testing.T) {
	fc := NewFakeClient()
	fc.DefaultResult = []any{
		ReleaseQualifiersProwjobSummaryResult{
			Release: "4.22.0-0.nightly-2026-04-06-112110",
			Name:    "job-a",
			State:   "success",
			URL:     "https://prow.ci/1",
		},
	}

	defaultJobs := []string{"job-a", "job-b"}
	filteredJobs := []ProwjobQueryFilter{
		{Name: "job-c", Interval: "2 DAY"},
	}

	_, err := fc.GetReleaseQualifiersProwjobSummaryWithFilters(context.Background(), defaultJobs, filteredJobs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fc.LastDefaultJobs) != 2 || fc.LastDefaultJobs[0] != "job-a" || fc.LastDefaultJobs[1] != "job-b" {
		t.Errorf("expected LastDefaultJobs [job-a, job-b], got %v", fc.LastDefaultJobs)
	}
	if len(fc.LastFilteredJobs) != 1 || fc.LastFilteredJobs[0].Name != "job-c" || fc.LastFilteredJobs[0].Interval != "2 DAY" {
		t.Errorf("expected LastFilteredJobs [{job-c 2 DAY}], got %v", fc.LastFilteredJobs)
	}
}

func TestFakeClient_SummaryRejectsMistypedResult(t *testing.T) {
	fc := NewFakeClient()
	fc.DefaultResult = []any{
		ReleaseQualifiersProwjobSummaryResult{
			Release: "4.22.0-0.nightly-2026-04-06-112110",
			Name:    "job-a",
			State:   "success",
			URL:     "https://prow.ci/1",
		},
		"not a result struct",
	}

	_, err := fc.GetReleaseQualifiersProwjobSummaryWithFilters(context.Background(), []string{"job-a"}, nil)
	if err == nil {
		t.Fatal("expected error for mis-typed DefaultResult entry, got nil")
	}
	if err.Error() != "DefaultResult[1]: expected ReleaseQualifiersProwjobSummaryResult, got string" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestClientInterface(t *testing.T) {
	// Verify that both Client and FakeClient implement ClientInterface
	var _ ClientInterface = (*Client)(nil)
	var _ ClientInterface = (*FakeClient)(nil)
}

func TestCopyToStruct_MapToNonStruct(t *testing.T) {
	src := map[string]any{"key": "value"}
	dst := new(string)

	err := copyToStruct(src, dst)
	if err == nil {
		t.Fatal("expected error when destination is a non-struct pointer, got nil")
	}
	if err.Error() != "destination must be a struct, got string" {
		t.Errorf("unexpected error message: %v", err)
	}
}

