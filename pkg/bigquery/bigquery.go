package bigquery

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// ClientInterface defines the interface for BigQuery client operations
type ClientInterface interface {
	Query(ctx context.Context, query string) (RowIteratorInterface, error)
	GetReleaseQualifiersProwjobSummary(ctx context.Context, prowjobs []string) ([]ReleaseQualifiersProwjobSummaryResult, error)
	GetReleaseQualifiersProwjobSummaryWithFilters(ctx context.Context, defaultJobs []string, filteredJobs []ProwjobQueryFilter) ([]ReleaseQualifiersProwjobSummaryResult, error)
}

// RowIteratorInterface defines the interface for iterating over query results
type RowIteratorInterface interface {
	Next(dst interface{}) error
}

type Client struct {
	*bigquery.Client
	project string
}

// rowIteratorWrapper wraps bigquery.RowIterator to implement RowIteratorInterface
type rowIteratorWrapper struct {
	*bigquery.RowIterator
}

func (w *rowIteratorWrapper) Next(dst interface{}) error {
	err := w.RowIterator.Next(dst)
	if errors.Is(err, iterator.Done) {
		return iterator.Done
	}
	return err
}

func NewBigQueryClient(project, credentialsFile string) (*Client, error) {
	bc, err := bigquery.NewClient(context.Background(), project, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("unable to create BigQuery client: %v", err)
	}
	return &Client{Client: bc, project: project}, nil
}

// Query executes a SQL query against BigQuery and returns a row iterator
func (c *Client) Query(ctx context.Context, query string) (RowIteratorInterface, error) {
	q := c.Client.Query(query)
	it, err := q.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %v", err)
	}
	return &rowIteratorWrapper{it}, nil
}

// QueryInto executes a SQL query and populates the provided slice with results.
// The results parameter must be a pointer to a slice of the desired type.
//
// Example:
//
//	var results []MyStruct
//	err := QueryInto(client, ctx, "SELECT * FROM table", &results)
func QueryInto[T any](c *Client, ctx context.Context, query string, results *[]T) error {
	iter, err := c.Query(ctx, query)
	if err != nil {
		return err
	}

	// Clear the slice
	*results = (*results)[:0]

	for {
		var result T
		err := iter.Next(&result)
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to iterate results: %w", err)
		}
		*results = append(*results, result)
	}

	return nil
}
