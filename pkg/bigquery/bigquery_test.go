package bigquery

import (
	"context"
	"errors"
	"fmt"
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
				fc.SetQueryResult("SELECT * FROM table", []interface{}{
					map[string]interface{}{"col1": "value1", "col2": 123},
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
				fc.DefaultResult = []interface{}{
					map[string]interface{}{"count": 42},
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
	fc.SetQueryResult("SELECT * FROM table", []interface{}{
		map[string]interface{}{"id": 1, "name": "first"},
		map[string]interface{}{"id": 2, "name": "second"},
	})

	ctx := context.Background()
	iter, err := fc.Query(ctx, "SELECT * FROM table")
	if err != nil {
		t.Fatalf("Query() failed: %v", err)
	}

	// Read first row
	var row1 map[string]interface{}
	err = iter.Next(&row1)
	if err != nil {
		t.Fatalf("Next() failed on first row: %v", err)
	}
	if row1["id"] != 1 {
		t.Errorf("expected id=1, got %v", row1["id"])
	}

	// Read second row
	var row2 map[string]interface{}
	err = iter.Next(&row2)
	if err != nil {
		t.Fatalf("Next() failed on second row: %v", err)
	}
	if row2["id"] != 2 {
		t.Errorf("expected id=2, got %v", row2["id"])
	}

	// Reading beyond results should return iterator.Done
	var row3 map[string]interface{}
	err = iter.Next(&row3)
	if err != iterator.Done {
		t.Errorf("expected iterator.Done, got %v", err)
	}
}

func TestFakeClient_MultipleQueries(t *testing.T) {
	fc := NewFakeClient()
	fc.SetQueryResult("SELECT * FROM table1", []interface{}{
		map[string]interface{}{"table": "table1"},
	})
	fc.SetQueryResult("SELECT * FROM table2", []interface{}{
		map[string]interface{}{"table": "table2"},
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
	fc.SetQueryResult("SELECT * FROM table", []interface{}{
		map[string]interface{}{"col": "value"},
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

func TestClientInterface(t *testing.T) {
	// Verify that both Client and FakeClient implement ClientInterface
	var _ ClientInterface = (*Client)(nil)
	var _ ClientInterface = (*FakeClient)(nil)
}

func TestQueryInto(t *testing.T) {
	type TestResult struct {
		ID   int    `bigquery:"id"`
		Name string `bigquery:"name"`
	}

	tests := []struct {
		name        string
		query       string
		setupFake   func(*FakeClient, string)
		wantCount   int
		wantErr     bool
		validateRes func(*testing.T, []TestResult)
	}{
		{
			name:  "successful query with results",
			query: "SELECT id, name FROM table",
			setupFake: func(fc *FakeClient, query string) {
				fc.SetQueryResult(query, []interface{}{
					map[string]interface{}{"id": 1, "name": "first"},
					map[string]interface{}{"id": 2, "name": "second"},
					map[string]interface{}{"id": 3, "name": "third"},
				})
			},
			wantCount: 3,
			wantErr:   false,
			validateRes: func(t *testing.T, results []TestResult) {
				if results[0].ID != 1 || results[0].Name != "first" {
					t.Errorf("unexpected first result: %+v", results[0])
				}
				if results[2].ID != 3 || results[2].Name != "third" {
					t.Errorf("unexpected third result: %+v", results[2])
				}
			},
		},
		{
			name:  "empty results",
			query: "SELECT id, name FROM empty_table",
			setupFake: func(fc *FakeClient, query string) {
				fc.SetQueryResult(query, []interface{}{})
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:  "query error",
			query: "SELECT * FROM nonexistent",
			setupFake: func(fc *FakeClient, query string) {
				fc.SetQueryError(query, fmt.Errorf("table not found"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := NewFakeClient()
			if tt.setupFake != nil {
				tt.setupFake(fc, tt.query)
			}

			var results []TestResult
			iter, err := fc.Query(context.Background(), tt.query)
			if err != nil && !tt.wantErr {
				t.Fatalf("unexpected query error: %v", err)
			}
			if err != nil {
				return
			}

			// Iterate through results
			for {
				var result TestResult
				err := iter.Next(&result)
				if err == iterator.Done {
					break
				}
				if err != nil {
					if !tt.wantErr {
						t.Fatalf("unexpected iteration error: %v", err)
					}
					return
				}
				results = append(results, result)
			}

			if len(results) != tt.wantCount {
				t.Errorf("expected %d results, got %d", tt.wantCount, len(results))
			}

			if tt.validateRes != nil {
				tt.validateRes(t, results)
			}
		})
	}
}
