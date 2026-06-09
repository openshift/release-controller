package bigquery

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/civil"
	"k8s.io/klog/v2"
)

const (
	// Dataset is the BigQuery dataset for CI analysis
	Dataset = "ci_analysis_us"
	// Table is the BigQuery table for prowjob data
	Table = "jobs"
)

// ReleaseQualifiersProwjobSummaryResult represents a single result from the release qualifier prowjob summary query
type ReleaseQualifiersProwjobSummaryResult struct {
	Release        string         `bigquery:"release_verify_tag"`
	Name           string         `bigquery:"prowjob_job_name"`
	State          string         `bigquery:"prowjob_state"`
	URL            string         `bigquery:"prowjob_url"`
	StartTime      civil.DateTime `bigquery:"prowjob_start"`
	CompletionTime civil.DateTime `bigquery:"prowjob_completion"`
}

// ProwjobQueryFilter represents a per-job query filter with a custom time interval.
// Jobs with a specific OverPeriod escalation setting use this to specify their own
// lookback interval in the BigQuery query rather than the default 14-day window.
type ProwjobQueryFilter struct {
	Name     string
	Interval string // SQL interval value, e.g. "2 DAY", "7 DAY", "24 HOUR"
}

var validSQLInterval = regexp.MustCompile(`^\d+ (DAY|HOUR|MINUTE)$`)

// GetReleaseQualifiersProwjobSummary queries BigQuery for prowjob summaries across all jobs defined as release qualifiers.
// All jobs use the default 14-day lookback window.
func (c *Client) GetReleaseQualifiersProwjobSummary(ctx context.Context, prowjobs []string) ([]ReleaseQualifiersProwjobSummaryResult, error) {
	return c.GetReleaseQualifiersProwjobSummaryWithFilters(ctx, prowjobs, nil)
}

// GetReleaseQualifiersProwjobSummaryWithFilters queries BigQuery for prowjob summaries with support
// for per-job time intervals. Jobs in defaultJobs use the standard 14-day lookback. Jobs in
// filteredJobs each specify their own interval (derived from escalation OverPeriod settings).
// The resulting query uses an OR structure to combine both groups efficiently.
func (c *Client) GetReleaseQualifiersProwjobSummaryWithFilters(ctx context.Context, defaultJobs []string, filteredJobs []ProwjobQueryFilter) ([]ReleaseQualifiersProwjobSummaryResult, error) {
	query, params, err := BuildProwjobSummaryQuery(c.project, defaultJobs, filteredJobs)
	if err != nil {
		return nil, fmt.Errorf("failed to build prowjob summary query: %w", err)
	}

	var results []ReleaseQualifiersProwjobSummaryResult
	if err := QueryParamsInto(c, ctx, query, params, &results); err != nil {
		return nil, fmt.Errorf("failed to query release verify jobs: %w", err)
	}

	return results, nil
}

// BuildProwjobSummaryQuery constructs a parameterized SQL query for prowjob summaries.
// It builds a hybrid WHERE clause: default jobs share a single IN UNNEST clause with a 14-day window,
// while filtered jobs each get their own condition with a custom interval.
// Job names are passed as BigQuery query parameters to prevent SQL injection.
// Intervals are validated against a strict allowlist of safe patterns.
func BuildProwjobSummaryQuery(project string, defaultJobs []string, filteredJobs []ProwjobQueryFilter) (string, []bigquery.QueryParameter, error) {
	var conditions []string
	var params []bigquery.QueryParameter

	if len(defaultJobs) > 0 {
		conditions = append(conditions,
			"(prowjob_job_name IN UNNEST(@default_jobs) AND prowjob_start >= DATETIME_SUB(CURRENT_DATETIME(), INTERVAL 14 DAY))")
		params = append(params, bigquery.QueryParameter{
			Name:  "default_jobs",
			Value: defaultJobs,
		})
	}

	for i, f := range filteredJobs {
		if !validSQLInterval.MatchString(f.Interval) {
			return "", nil, fmt.Errorf("invalid SQL interval %q for job %q", f.Interval, f.Name)
		}
		paramName := fmt.Sprintf("filtered_name_%d", i)
		conditions = append(conditions, fmt.Sprintf(
			"(prowjob_job_name = @%s AND prowjob_start >= DATETIME_SUB(CURRENT_DATETIME(), INTERVAL %s))",
			paramName,
			f.Interval,
		))
		params = append(params, bigquery.QueryParameter{
			Name:  paramName,
			Value: f.Name,
		})
	}

	query := fmt.Sprintf(`
		SELECT release_verify_tag, prowjob_job_name, prowjob_state, prowjob_url, prowjob_start, prowjob_completion
		FROM `+"`%s.%s.%s`"+`
        WHERE manager = 'release-controller'
          AND is_release_verify = TRUE
          AND (%s)
        ORDER BY prowjob_job_name, prowjob_completion DESC`,
		project,
		Dataset,
		Table,
		strings.Join(conditions, " OR "),
	)

	klog.V(5).Infof("Prowjob Summary Query: %s", query)
	return query, params, nil
}

// SELECT release_verify_tag, prowjob_state, prowjob_url, prowjob_completion FROM `openshift-gce-devel.ci_analysis_us.jobs` WHERE manager = 'release-controller' AND is_release_verify = TRUE AND prowjob_job_name = 'periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance' AND prowjob_completion >= DATETIME_SUB(CURRENT_DATETIME(), INTERVAL 14 DAY) ORDER BY prowjob_completion DESC
// SELECT release_verify_tag, prowjob_job_name, prowjob_state, prowjob_url, prowjob_completion FROM `openshift-gce-devel.ci_analysis_us.jobs` WHERE manager = 'release-controller' AND is_release_verify = TRUE AND prowjob_completion >= DATETIME_SUB(CURRENT_DATETIME(), INTERVAL 14 DAY) AND prowjob_job_name IN ('periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-proxy', 'periodic-ci-openshift-release-main-nightly-4.22-e2e-aws-ovn-upgrade-fips-no-nat-instance', 'periodic-ci-openshift-release-main-nightly-4.22-console-aws', '') ORDER BY prowjob_job_name, prowjob_completion DESC
