package bigquery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestClient_GetReleaseQualifiersProwjobSummary(t *testing.T) {
	tests := []struct {
		name          string
		project       string
		jobNames      []string
		setupFake     func(*FakeClient, string, []string)
		wantResults   int
		wantErr       bool
		validateQuery func(*testing.T, string)
	}{
		{
			name:     "successful query with multiple results",
			project:  "openshift-gce-devel",
			jobNames: []string{"periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance"},
			setupFake: func(fc *FakeClient, project string, jobNames []string) {
				query := buildExpectedQuery(project, jobNames)
				fc.SetQueryResult(query, []interface{}{
					ReleaseQualifiersProwjobSummaryResult{
						Release: "4.22.0-0.nightly-2026-04-06-112110",
						Name:    "periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance",
						State:   "failure",
						URL:     "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance/1",
					},
					ReleaseQualifiersProwjobSummaryResult{
						Release: "4.22.0-0.nightly-2026-04-06-051707",
						Name:    "periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance",
						State:   "failure",
						URL:     "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance/2",
					},
					ReleaseQualifiersProwjobSummaryResult{
						Release: "4.22.0-0.nightly-2026-04-05-230815",
						Name:    "periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance",
						State:   "success",
						URL:     "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance/3",
					},
				})
			},
			wantResults: 3,
			wantErr:     false,
			validateQuery: func(t *testing.T, query string) {
				if !strings.Contains(query, "manager = 'release-controller'") {
					t.Error("query should filter by manager = 'release-controller'")
				}
				if !strings.Contains(query, "is_release_verify = TRUE") {
					t.Error("query should filter by is_release_verify = TRUE")
				}
				if !strings.Contains(query, "ORDER BY prowjob_job_name, prowjob_completion DESC") {
					t.Error("query should order by prowjob_job_name, prowjob_completion DESC")
				}
				if !strings.Contains(query, "INTERVAL 14 DAY") {
					t.Error("query should filter by 14 day interval")
				}
				if !strings.Contains(query, Dataset) {
					t.Errorf("query should use dataset constant %s", Dataset)
				}
				if !strings.Contains(query, Table) {
					t.Errorf("query should use table constant %s", Table)
				}
			},
		},
		{
			name:     "empty results",
			project:  "test-project",
			jobNames: []string{"nonexistent-job"},
			setupFake: func(fc *FakeClient, project string, jobNames []string) {
				query := buildExpectedQuery(project, jobNames)
				fc.SetQueryResult(query, []interface{}{})
			},
			wantResults: 0,
			wantErr:     false,
		},
		{
			name:     "query execution error",
			project:  "test-project",
			jobNames: []string{"test-job"},
			setupFake: func(fc *FakeClient, project string, jobNames []string) {
				query := buildExpectedQuery(project, jobNames)
				fc.SetQueryError(query, errors.New("table not found"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := NewFakeClient()
			if tt.setupFake != nil {
				tt.setupFake(fc, tt.project, tt.jobNames)
			}

			// Create client wrapper with project
			client := &clientWrapper{
				ClientInterface: fc,
				project:         tt.project,
			}

			ctx := context.Background()
			results, err := client.GetReleaseQualifiersProwjobSummary(ctx, tt.jobNames)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetReleaseQualifiersProwjobSummary() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(results) != tt.wantResults {
					t.Errorf("expected %d results, got %d", tt.wantResults, len(results))
				}

				queries := fc.GetQueries()
				if len(queries) != 1 {
					t.Errorf("expected 1 query to be executed, got %d", len(queries))
					return
				}

				if tt.validateQuery != nil {
					tt.validateQuery(t, queries[0])
				}
			}
		})
	}
}

func TestGetReleaseQualifiersProwjobSummary_ResultStructure(t *testing.T) {
	fc := NewFakeClient()
	project := "openshift-gce-devel"
	jobNames := []string{"periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance"}

	query := buildExpectedQuery(project, jobNames)
	fc.SetQueryResult(query, []interface{}{
		ReleaseQualifiersProwjobSummaryResult{
			Release: "4.22.0-0.nightly-2026-04-06-112110",
			Name:    "periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance",
			State:   "failure",
			URL:     "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance/1",
		},
		ReleaseQualifiersProwjobSummaryResult{
			Release: "4.22.0-0.nightly-2026-04-05-230815",
			Name:    "periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance",
			State:   "success",
			URL:     "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance/2",
		},
	})

	client := &clientWrapper{
		ClientInterface: fc,
		project:         project,
	}
	ctx := context.Background()
	results, err := client.GetReleaseQualifiersProwjobSummary(ctx, jobNames)

	if err != nil {
		t.Fatalf("GetReleaseQualifiersProwjobSummary() failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Validate first result - all fields
	if results[0].Release != "4.22.0-0.nightly-2026-04-06-112110" {
		t.Errorf("unexpected release in first result: %s", results[0].Release)
	}
	if results[0].Name != "periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance" {
		t.Errorf("unexpected name in first result: %s", results[0].Name)
	}
	if results[0].State != "failure" {
		t.Errorf("unexpected state in first result: %s", results[0].State)
	}
	if results[0].URL != "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance/1" {
		t.Errorf("unexpected URL in first result: %s", results[0].URL)
	}

	// Validate second result - all fields
	if results[1].Release != "4.22.0-0.nightly-2026-04-05-230815" {
		t.Errorf("unexpected release in second result: %s", results[1].Release)
	}
	if results[1].Name != "periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance" {
		t.Errorf("unexpected name in second result: %s", results[1].Name)
	}
	if results[1].State != "success" {
		t.Errorf("unexpected state in second result: %s", results[1].State)
	}
	if results[1].URL != "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance/2" {
		t.Errorf("unexpected URL in second result: %s", results[1].URL)
	}
}

func TestGetReleaseQualifiersProwjobSummary_WithMapResults(t *testing.T) {
	fc := NewFakeClient()
	project := "test-project"
	jobNames := []string{"test-job"}

	query := buildExpectedQuery(project, jobNames)
	// Test with map results instead of struct results
	fc.SetQueryResult(query, []interface{}{
		map[string]interface{}{
			"release_verify_tag": "4.22.0-0.nightly-2026-04-06-112110",
			"prowjob_job_name":   "test-job",
			"prowjob_state":      "success",
			"prowjob_url":        "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/test-job/1",
		},
		map[string]interface{}{
			"release_verify_tag": "4.22.0-0.nightly-2026-04-05-230815",
			"prowjob_job_name":   "test-job",
			"prowjob_state":      "failure",
			"prowjob_url":        "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/test-job/2",
		},
	})

	client := &clientWrapper{
		ClientInterface: fc,
		project:         project,
	}
	ctx := context.Background()
	results, err := client.GetReleaseQualifiersProwjobSummary(ctx, jobNames)

	if err != nil {
		t.Fatalf("GetReleaseQualifiersProwjobSummary() failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Validate all fields in first result
	if results[0].Release != "4.22.0-0.nightly-2026-04-06-112110" {
		t.Errorf("unexpected release in first result: %s", results[0].Release)
	}
	if results[0].Name != "test-job" {
		t.Errorf("unexpected name in first result: %s", results[0].Name)
	}
	if results[0].State != "success" {
		t.Errorf("unexpected state in first result: %s", results[0].State)
	}
	if results[0].URL != "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/test-job/1" {
		t.Errorf("unexpected URL in first result: %s", results[0].URL)
	}
}

func TestBuildProwjobSummaryQuery(t *testing.T) {
	t.Parallel()

	t.Run("default jobs only - matches legacy format", func(t *testing.T) {
		query := BuildProwjobSummaryQuery("proj", []string{"'job-a'", "'job-b'"}, nil)

		if !strings.Contains(query, "prowjob_job_name IN ('job-a','job-b')") {
			t.Errorf("expected IN clause with both jobs, got:\n%s", query)
		}
		if !strings.Contains(query, "INTERVAL 14 DAY") {
			t.Error("expected 14 DAY default interval")
		}
		if !strings.Contains(query, "prowjob_completion") {
			t.Error("expected prowjob_completion in SELECT")
		}
	})

	t.Run("filtered jobs only", func(t *testing.T) {
		query := BuildProwjobSummaryQuery("proj", nil, []ProwjobQueryFilter{
			{Name: "job-c", Interval: "2 DAY"},
			{Name: "job-d", Interval: "7 DAY"},
		})

		if strings.Contains(query, "IN (") {
			t.Error("expected no IN clause when defaultJobs is empty")
		}
		if !strings.Contains(query, "prowjob_job_name = 'job-c' AND prowjob_start >= DATETIME_SUB(CURRENT_DATETIME(), INTERVAL 2 DAY)") {
			t.Errorf("expected job-c with 2 DAY interval, got:\n%s", query)
		}
		if !strings.Contains(query, "prowjob_job_name = 'job-d' AND prowjob_start >= DATETIME_SUB(CURRENT_DATETIME(), INTERVAL 7 DAY)") {
			t.Errorf("expected job-d with 7 DAY interval, got:\n%s", query)
		}
	})

	t.Run("hybrid - default and filtered jobs", func(t *testing.T) {
		query := BuildProwjobSummaryQuery("proj", []string{"'job-a'", "'job-b'"}, []ProwjobQueryFilter{
			{Name: "job-c", Interval: "2 DAY"},
		})

		if !strings.Contains(query, "prowjob_job_name IN ('job-a','job-b')") {
			t.Errorf("expected IN clause for default jobs, got:\n%s", query)
		}
		if !strings.Contains(query, "INTERVAL 14 DAY") {
			t.Error("expected 14 DAY default interval for default jobs")
		}
		if !strings.Contains(query, "prowjob_job_name = 'job-c' AND prowjob_start >= DATETIME_SUB(CURRENT_DATETIME(), INTERVAL 2 DAY)") {
			t.Errorf("expected job-c with custom interval, got:\n%s", query)
		}
		if !strings.Contains(query, " OR ") {
			t.Error("expected OR between default and filtered conditions")
		}
	})

	t.Run("query structure", func(t *testing.T) {
		query := BuildProwjobSummaryQuery("openshift-gce-devel", []string{"'job-a'"}, nil)

		if !strings.Contains(query, "manager = 'release-controller'") {
			t.Error("expected manager filter")
		}
		if !strings.Contains(query, "is_release_verify = TRUE") {
			t.Error("expected is_release_verify filter")
		}
		if !strings.Contains(query, "ORDER BY prowjob_job_name, prowjob_completion DESC") {
			t.Error("expected ORDER BY clause")
		}
		if !strings.Contains(query, "openshift-gce-devel.ci_analysis_us.jobs") {
			t.Error("expected project.dataset.table in FROM clause")
		}
		if !strings.Contains(query, "prowjob_completion") {
			t.Error("expected prowjob_completion in SELECT")
		}
	})
}

// buildExpectedQuery constructs the expected query string for testing using the production query builder.
func buildExpectedQuery(project string, prowjobs []string) string {
	return BuildProwjobSummaryQuery(project, prowjobs, nil)
}

// clientWrapper wraps FakeClient to provide the GetReleaseQualifiersProwjobSummary method
type clientWrapper struct {
	ClientInterface
	project string
}

func (c *clientWrapper) GetReleaseQualifiersProwjobSummary(ctx context.Context, prowjobs []string) ([]ReleaseQualifiersProwjobSummaryResult, error) {
	return c.GetReleaseQualifiersProwjobSummaryWithFilters(ctx, prowjobs, nil)
}

func (c *clientWrapper) GetReleaseQualifiersProwjobSummaryWithFilters(ctx context.Context, defaultJobs []string, filteredJobs []ProwjobQueryFilter) ([]ReleaseQualifiersProwjobSummaryResult, error) {
	query := BuildProwjobSummaryQuery(c.project, defaultJobs, filteredJobs)

	iter, err := c.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query release verify jobs: %w", err)
	}

	var results []ReleaseQualifiersProwjobSummaryResult
	for {
		var result ReleaseQualifiersProwjobSummaryResult
		err := iter.Next(&result)
		if err != nil {
			break
		}
		results = append(results, result)
	}

	return results, nil
}
