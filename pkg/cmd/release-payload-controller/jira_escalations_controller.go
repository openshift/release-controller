package release_payload_controller

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	gojira "github.com/andygrunwald/go-jira"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/bigquery"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	releasequalifierslib "github.com/openshift/release-controller/pkg/releasequalifiers"
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/jira"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	jiraclient "sigs.k8s.io/prow/pkg/jira"
)

// JiraEscalationsController monitors ReleasePayload resources and evaluates Jira escalation
// rules based on job results from BigQuery. When escalation conditions are met, it triggers
// appropriate Jira notifications.
//
// The controller:
//  1. Watches ReleasePayload resources for changes
//  2. Identifies jobs with Jira escalation configurations
//  3. Queries BigQuery for historical job run results
//  4. Evaluates escalation rules against job history
//  5. Triggers Jira notifications when thresholds are reached
type JiraEscalationsController struct {
	*ReleasePayloadController
	configAccessor releasequalifierslib.ConfigAccessor
	bigQueryClient bigquery.ClientInterface
	jiraClient     jiraclient.Client
	addWatcherFn   func(issueKey, username string) error
}

func NewJiraEscalationsController(
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	eventRecorder events.Recorder,
	configAccessor releasequalifierslib.ConfigAccessor,
	bigQueryClient bigquery.ClientInterface,
	jiraClient jiraclient.Client,
) (*JiraEscalationsController, error) {
	c := &JiraEscalationsController{
		ReleasePayloadController: NewReleasePayloadController(
			"Jira Escalations Controller",
			releasePayloadInformer,
			releasePayloadClient,
			eventRecorder.WithComponentSuffix("jira-escalations-controller"),
			workqueue.NewTypedRateLimitingQueueWithConfig(
				workqueue.DefaultTypedControllerRateLimiter[string](),
				workqueue.TypedRateLimitingQueueConfig[string]{Name: "JiraEscalationsController"},
			),
		),
		configAccessor: configAccessor,
		bigQueryClient: bigQueryClient,
		jiraClient:     jiraClient,
	}

	c.syncFn = c.sync

	if _, err := releasePayloadInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: c.Enqueue,
		UpdateFunc: func(oldObj, newObj any) {
			c.Enqueue(newObj)
		},
		DeleteFunc: c.Enqueue,
	}); err != nil {
		return nil, fmt.Errorf("failed to add release payload event handler: %v", err)
	}

	return c, nil
}

func (c *JiraEscalationsController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting JiraEscalationsController sync")
	defer klog.V(4).Infof("JiraEscalationsController sync done")

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	originalReleasePayload, err := c.releasePayloadLister.ReleasePayloads(namespace).Get(name)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// Skip if release qualifier config is not available
	if c.configAccessor == nil {
		klog.V(5).Infof("Skipping Jira escalations check - no release qualifiers config")
		return nil
	}

	qualifiersConfig := c.configAccessor.Get()
	if len(qualifiersConfig) == 0 {
		klog.V(5).Infof("Skipping Jira escalations check - empty qualifiers config")
		return nil
	}

	// Collect all job names that need to be queried, split by whether they have custom OverPeriod
	defaultJobs, filteredJobs := c.collectJobNamesWithEscalations(originalReleasePayload, qualifiersConfig)
	if len(defaultJobs) == 0 && len(filteredJobs) == 0 {
		klog.V(5).Infof("No jobs with Jira escalations found")
		return nil
	}

	// Query BigQuery once for all jobs
	allJobHistory, err := c.getJobHistory(ctx, defaultJobs, filteredJobs)
	if err != nil {
		klog.Errorf("Failed to get job history: %v", err)
		return err
	}

	// TODO: Should we be running this everytime the controller is invoked?  Maybe only when a corresponding job transitions or the aggregate state changes?

	// Group results by job name for efficient lookup
	historyByJob := c.groupHistoryByJob(allJobHistory)
	if len(historyByJob) == 0 {
		return nil
	}

	// Work on a deep copy so we can track notification state changes
	releasePayload := originalReleasePayload.DeepCopy()

	// Process all job types
	if err := c.processJobsForEscalations(ctx, releasePayload, releasePayload.Status.BlockingJobResults, qualifiersConfig, historyByJob); err != nil {
		return err
	}
	if err := c.processJobsForEscalations(ctx, releasePayload, releasePayload.Status.InformingJobResults, qualifiersConfig, historyByJob); err != nil {
		return err
	}
	if err := c.processJobsForEscalations(ctx, releasePayload, releasePayload.Status.UpgradeJobResults, qualifiersConfig, historyByJob); err != nil {
		return err
	}

	// Persist notification state changes
	if !reflect.DeepEqual(originalReleasePayload.Status.QualifiersSummary, releasePayload.Status.QualifiersSummary) {
		klog.V(4).Infof("Updating notification state for ReleasePayload: %s/%s", releasePayload.Namespace, releasePayload.Name)
		if _, err := c.releasePayloadClient.ReleasePayloads(releasePayload.Namespace).UpdateStatus(ctx, releasePayload, metav1.UpdateOptions{}); err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("failed to update notification state: %w", err)
		}
	}

	return nil
}

// collectJobNamesWithEscalations collects all job names that have Jira escalations configured.
// It returns two slices: defaultJobs (using the 14-day default) and
// filteredJobs (with custom OverPeriod intervals). For a job with multiple escalations, the
// last escalation's OverPeriod determines whether the job uses a custom interval.
func (c *JiraEscalationsController) collectJobNamesWithEscalations(
	releasePayload *v1alpha1.ReleasePayload,
	qualifiersConfig releasequalifierslib.ReleaseQualifiers,
) (defaultJobs []string, filteredJobs []bigquery.ProwjobQueryFilter) {
	// Track jobs we've already processed to avoid duplicates
	seen := make(map[string]bool)

	collectFromJobs := func(jobs []v1alpha1.CIConfiguration) {
		for _, job := range jobs {
			if len(job.Qualifiers) == 0 || seen[job.CIConfigurationJobName] {
				continue
			}

			for qualifierID := range job.Qualifiers {
				globalConfig, exists := qualifiersConfig[qualifierID]
				if !exists {
					continue
				}

				jobQualifierConfig := job.Qualifiers[qualifierID]
				merged := globalConfig.Merge(jobQualifierConfig)

				if merged.Notifications == nil || merged.Notifications.Jira == nil || len(merged.Notifications.Jira.Escalations) == 0 {
					continue
				}

				seen[job.CIConfigurationJobName] = true

				// Use the last escalation's OverPeriod to determine the query interval
				lastEscalation := merged.Notifications.Jira.Escalations[len(merged.Notifications.Jira.Escalations)-1]
				if lastEscalation.OverPeriod != "" {
					interval, err := parsePeriodToSQLInterval(lastEscalation.OverPeriod)
					if err != nil {
						klog.Warningf("Invalid OverPeriod %q for job %s, using default 14-day window: %v",
							lastEscalation.OverPeriod, job.CIConfigurationJobName, err)
						defaultJobs = append(defaultJobs, job.CIConfigurationJobName)
					} else {
						filteredJobs = append(filteredJobs, bigquery.ProwjobQueryFilter{
							Name:     job.CIConfigurationJobName,
							Interval: interval,
						})
					}
				} else {
					defaultJobs = append(defaultJobs, job.CIConfigurationJobName)
				}
				break
			}
		}
	}

	collectFromJobs(releasePayload.Spec.PayloadVerificationConfig.BlockingJobs)
	collectFromJobs(releasePayload.Spec.PayloadVerificationConfig.InformingJobs)
	collectFromJobs(releasePayload.Spec.PayloadVerificationConfig.UpgradeJobs)

	return defaultJobs, filteredJobs
}

// groupHistoryByJob groups job history results by job name for efficient lookup
func (c *JiraEscalationsController) groupHistoryByJob(
	allHistory []bigquery.ReleaseQualifiersProwjobSummaryResult,
) map[string][]bigquery.ReleaseQualifiersProwjobSummaryResult {
	historyByJob := make(map[string][]bigquery.ReleaseQualifiersProwjobSummaryResult)

	for _, result := range allHistory {
		historyByJob[result.Name] = append(historyByJob[result.Name], result)
	}

	return historyByJob
}

// processJobsForEscalations processes a list of jobs and evaluates their Jira escalation rules.
// It handles duplicate prevention (skipping already-notified escalations) and abatement
// detection (commenting when conditions improve). Notification state is tracked in
// releasePayload.Status.QualifiersSummary[qualifierID].JiraNotifications[threadID].
func (c *JiraEscalationsController) processJobsForEscalations(
	ctx context.Context,
	releasePayload *v1alpha1.ReleasePayload,
	jobs []v1alpha1.JobStatus,
	qualifiersConfig releasequalifierslib.ReleaseQualifiers,
	historyByJob map[string][]bigquery.ReleaseQualifiersProwjobSummaryResult,
) error {
	for _, job := range jobs {
		jobQualifiers := c.getJobQualifiers(releasePayload, job.CIConfigurationName)
		if len(jobQualifiers) == 0 {
			continue
		}

		jobHistory, exists := historyByJob[job.CIConfigurationJobName]
		if !exists || len(jobHistory) == 0 {
			klog.V(5).Infof("No job history found for %s", job.CIConfigurationJobName)
			continue
		}

		for qualifierID, jobQualifierConfig := range jobQualifiers {
			globalConfig, exists := qualifiersConfig[qualifierID]
			if !exists {
				klog.V(5).Infof("Qualifier %s not found in global config, skipping", qualifierID)
				continue
			}

			merged := globalConfig.Merge(jobQualifierConfig)
			if merged.Notifications == nil || merged.Notifications.Jira == nil || len(merged.Notifications.Jira.Escalations) == 0 {
				continue
			}
			jiraConfig := merged.Notifications.Jira

			threadID := c.buildThreadID(releasePayload, qualifierID, jiraConfig)
			notifState := c.getNotificationState(releasePayload, qualifierID, threadID)

			// Find the highest-priority escalation that should fire
			var highestEscalation *jira.Escalation
			for i := range jiraConfig.Escalations {
				escalation := &jiraConfig.Escalations[i]
				if c.shouldTriggerEscalation(*escalation, jobHistory) {
					if highestEscalation == nil || isHigherPriority(escalation.Priority, highestEscalation.Priority) {
						highestEscalation = escalation
					}
				}
			}

			if highestEscalation != nil {
				// Duplicate prevention: skip if same escalation already active and not abated
				if notifState.ActiveEscalation == highestEscalation.Name &&
					notifState.ActivePriority == highestEscalation.Priority &&
					!notifState.Abated {
					klog.V(4).Infof("Skipping duplicate escalation '%s' for thread %s", highestEscalation.Name, threadID)
					continue
				}

				issueKey := c.triggerJiraEscalation(ctx, releasePayload, job, qualifierID, jiraConfig, *highestEscalation)

				// Update notification state
				notifState.ActiveEscalation = highestEscalation.Name
				notifState.ActivePriority = highestEscalation.Priority
				notifState.Abated = false
				notifState.LastTransitionTime = metav1.NewTime(time.Now())
				if issueKey != "" {
					notifState.IssueKey = issueKey
				}
				c.setNotificationState(releasePayload, qualifierID, threadID, notifState)
			} else if notifState.ActiveEscalation != "" && !notifState.Abated {
				// Abatement: conditions improved, leave comment on open Jira ticket
				notifState = c.handleAbatement(ctx, releasePayload, qualifierID, jiraConfig, threadID, notifState)
				c.setNotificationState(releasePayload, qualifierID, threadID, notifState)
			}
		}
	}

	return nil
}

// getNotificationState retrieves the JiraNotificationState for a specific qualifier and thread
func (c *JiraEscalationsController) getNotificationState(
	releasePayload *v1alpha1.ReleasePayload,
	qualifierID releasequalifierslib.QualifierId,
	threadID string,
) v1alpha1.JiraNotificationState {
	if releasePayload.Status.QualifiersSummary != nil {
		if summary, ok := releasePayload.Status.QualifiersSummary.Qualifiers[qualifierID]; ok {
			if state, ok := summary.JiraNotifications[threadID]; ok {
				return state
			}
		}
	}
	return v1alpha1.JiraNotificationState{}
}

// setNotificationState persists the JiraNotificationState for a specific qualifier and thread
func (c *JiraEscalationsController) setNotificationState(
	releasePayload *v1alpha1.ReleasePayload,
	qualifierID releasequalifierslib.QualifierId,
	threadID string,
	state v1alpha1.JiraNotificationState,
) {
	if releasePayload.Status.QualifiersSummary == nil {
		releasePayload.Status.QualifiersSummary = &v1alpha1.QualifiersSummary{
			Qualifiers: make(map[releasequalifierslib.QualifierId]v1alpha1.ReleaseQualifierSummary),
		}
	}
	if releasePayload.Status.QualifiersSummary.Qualifiers == nil {
		releasePayload.Status.QualifiersSummary.Qualifiers = make(map[releasequalifierslib.QualifierId]v1alpha1.ReleaseQualifierSummary)
	}
	summary := releasePayload.Status.QualifiersSummary.Qualifiers[qualifierID]
	if summary.JiraNotifications == nil {
		summary.JiraNotifications = make(map[string]v1alpha1.JiraNotificationState)
	}
	summary.JiraNotifications[threadID] = state
	releasePayload.Status.QualifiersSummary.Qualifiers[qualifierID] = summary
}

// handleAbatement detects when escalation conditions have improved and leaves
// an abatement comment on the existing Jira ticket. Per the design doc, tickets
// are never closed programmatically.
func (c *JiraEscalationsController) handleAbatement(
	ctx context.Context,
	releasePayload *v1alpha1.ReleasePayload,
	qualifierID releasequalifierslib.QualifierId,
	jiraConfig *jira.Notification,
	threadID string,
	state v1alpha1.JiraNotificationState,
) v1alpha1.JiraNotificationState {
	klog.Infof("Conditions abated for thread %s (was: escalation '%s')", threadID, state.ActiveEscalation)

	if c.jiraClient == nil {
		klog.V(4).Infof("Jira client not configured, skipping abatement comment for thread %s", threadID)
		state.Abated = true
		state.LastTransitionTime = metav1.NewTime(time.Now())
		return state
	}

	existingIssue, err := c.findExistingEscalationIssue(ctx, jiraConfig.Project, threadID)
	if err != nil {
		klog.Errorf("Failed to search for existing Jira issue during abatement (thread: %s): %v", threadID, err)
		return state
	}

	if existingIssue != nil {
		comment := &gojira.Comment{
			Body: fmt.Sprintf(
				"Conditions have improved. Escalation '%s' is no longer active for qualifier '%s'.\n\n"+
					"The ticket remains open for review — escalation conditions may recur.",
				state.ActiveEscalation,
				qualifierID,
			),
		}

		if _, err := c.jiraClient.AddComment(existingIssue.Key, comment); err != nil {
			klog.Errorf("Failed to add abatement comment to %s: %v", existingIssue.Key, err)
			return state
		}

		klog.Infof("Added abatement comment to Jira issue %s for thread %s", existingIssue.Key, threadID)
	}

	state.Abated = true
	state.LastTransitionTime = metav1.NewTime(time.Now())
	return state
}

// getJobQualifiers extracts qualifiers for a specific job from the payload spec
func (c *JiraEscalationsController) getJobQualifiers(
	releasePayload *v1alpha1.ReleasePayload,
	ciConfigurationName string,
) releasequalifierslib.ReleaseQualifiers {
	// Check blocking jobs
	for _, job := range releasePayload.Spec.PayloadVerificationConfig.BlockingJobs {
		if job.CIConfigurationName == ciConfigurationName {
			return job.Qualifiers
		}
	}

	// Check informing jobs
	for _, job := range releasePayload.Spec.PayloadVerificationConfig.InformingJobs {
		if job.CIConfigurationName == ciConfigurationName {
			return job.Qualifiers
		}
	}

	// Check upgrade jobs
	for _, job := range releasePayload.Spec.PayloadVerificationConfig.UpgradeJobs {
		if job.CIConfigurationName == ciConfigurationName {
			return job.Qualifiers
		}
	}

	return nil
}

// getJobHistory queries BigQuery for historical job results using per-job time intervals
func (c *JiraEscalationsController) getJobHistory(
	ctx context.Context,
	defaultJobs []string,
	filteredJobs []bigquery.ProwjobQueryFilter,
) ([]bigquery.ReleaseQualifiersProwjobSummaryResult, error) {
	if c.bigQueryClient == nil {
		klog.V(4).Infof("BigQuery client not configured, unable to get job history")
		return nil, nil
	}

	results, err := c.bigQueryClient.GetReleaseQualifiersProwjobSummaryWithFilters(ctx, defaultJobs, filteredJobs)
	if err != nil {
		return nil, fmt.Errorf("failed to query BigQuery for jobs: %w", err)
	}

	return results, nil
}

// shouldTriggerEscalation evaluates an escalation rule against job history
func (c *JiraEscalationsController) shouldTriggerEscalation(
	escalation jira.Escalation,
	jobHistory []bigquery.ReleaseQualifiersProwjobSummaryResult,
) bool {
	if len(jobHistory) == 0 {
		return false
	}

	// Determine the window size
	windowSize := c.getWindowSize(escalation)

	// Get the relevant job results based on window
	relevantResults := c.getRelevantResults(jobHistory, windowSize, escalation.OverPeriod)

	if len(relevantResults) == 0 {
		return false
	}

	// Evaluate based on escalation type
	if escalation.PassPercentage != nil {
		return c.evaluatePassPercentage(relevantResults, *escalation.PassPercentage)
	}

	if escalation.Failures > 0 {
		return c.evaluateFailures(relevantResults, escalation.Failures, windowSize)
	}

	return false
}

// getWindowSize determines the window size for evaluation
func (c *JiraEscalationsController) getWindowSize(escalation jira.Escalation) int {
	if escalation.OverLastRuns != nil {
		return *escalation.OverLastRuns
	}
	// If OverLastRuns is not specified, default to Failures for consecutive mode
	if escalation.Failures > 0 {
		return escalation.Failures
	}
	return 10 // Default window size
}

// getRelevantResults filters job history based on window size and time period.
// When overPeriod is specified, it computes both the count-based window (OverLastRuns)
// and the time-based window (OverPeriod), then returns whichever provides more samples.
// Results must be ordered by prowjob_completion DESC (most recent first).
func (c *JiraEscalationsController) getRelevantResults(
	jobHistory []bigquery.ReleaseQualifiersProwjobSummaryResult,
	windowSize int,
	overPeriod string,
) []bigquery.ReleaseQualifiersProwjobSummaryResult {
	if len(jobHistory) == 0 {
		return jobHistory
	}

	// Count-based window
	countWindow := windowSize
	if countWindow > len(jobHistory) {
		countWindow = len(jobHistory)
	}

	if overPeriod == "" {
		return jobHistory[:countWindow]
	}

	// Parse the time period
	period, err := parsePeriod(overPeriod)
	if err != nil {
		klog.Warningf("Failed to parse overPeriod %q, falling back to count-based window: %v", overPeriod, err)
		return jobHistory[:countWindow]
	}

	// Time-based window: count results within the period.
	// Results are ordered by prowjob_completion DESC, so we can break early.
	cutoff := time.Now().Add(-period)
	timeWindowCount := 0
	for _, result := range jobHistory {
		completionTime := result.CompletionTime.In(time.UTC)
		if completionTime.After(cutoff) {
			timeWindowCount++
		} else {
			break
		}
	}

	// Use whichever provides more samples
	effectiveWindow := countWindow
	if timeWindowCount > effectiveWindow {
		effectiveWindow = timeWindowCount
	}
	if effectiveWindow > len(jobHistory) {
		effectiveWindow = len(jobHistory)
	}

	return jobHistory[:effectiveWindow]
}

// parsePeriod converts an OverPeriod string (e.g., "2d", "1w", "24h") to a time.Duration.
func parsePeriod(period string) (time.Duration, error) {
	if period == "" {
		return 0, fmt.Errorf("empty period")
	}

	// Try Go's built-in parser first (handles "h", "m", "s")
	if d, err := time.ParseDuration(period); err == nil {
		if d <= 0 {
			return 0, fmt.Errorf("period must be positive: %s", period)
		}
		return d, nil
	}

	if len(period) < 2 {
		return 0, fmt.Errorf("invalid period format: %s", period)
	}

	suffix := period[len(period)-1]
	num, err := strconv.Atoi(period[:len(period)-1])
	if err != nil {
		return 0, fmt.Errorf("invalid period format %q: %w", period, err)
	}
	if num <= 0 {
		return 0, fmt.Errorf("period must be positive: %s", period)
	}

	switch suffix {
	case 'd':
		return time.Duration(num) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(num) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported period suffix %q in %q", string(suffix), period)
	}
}

// parsePeriodToSQLInterval converts an OverPeriod string to a BigQuery SQL INTERVAL value.
// Examples: "2d" -> "2 DAY", "1w" -> "7 DAY", "24h" -> "24 HOUR".
func parsePeriodToSQLInterval(period string) (string, error) {
	if period == "" {
		return "", fmt.Errorf("empty period")
	}

	if len(period) < 2 {
		return "", fmt.Errorf("invalid period format: %s", period)
	}

	suffix := period[len(period)-1]
	num, err := strconv.Atoi(period[:len(period)-1])
	if err != nil {
		return "", fmt.Errorf("invalid period format %q: %w", period, err)
	}
	if num <= 0 {
		return "", fmt.Errorf("period must be positive: %s", period)
	}

	switch suffix {
	case 'h':
		return fmt.Sprintf("%d HOUR", num), nil
	case 'd':
		return fmt.Sprintf("%d DAY", num), nil
	case 'w':
		return fmt.Sprintf("%d DAY", num*7), nil
	default:
		return "", fmt.Errorf("unsupported period suffix %q in %q", string(suffix), period)
	}
}

// evaluatePassPercentage checks if the pass percentage threshold is met
func (c *JiraEscalationsController) evaluatePassPercentage(
	results []bigquery.ReleaseQualifiersProwjobSummaryResult,
	threshold int,
) bool {
	if len(results) == 0 {
		return false
	}

	passCount := 0
	for _, result := range results {
		if result.State == "success" {
			passCount++
		}
	}

	passPercentage := (passCount * 100) / len(results)
	return passPercentage < threshold
}

// evaluateFailures checks if the failure threshold is met
func (c *JiraEscalationsController) evaluateFailures(
	results []bigquery.ReleaseQualifiersProwjobSummaryResult,
	threshold int,
	windowSize int,
) bool {
	failureCount := 0

	// If window size equals threshold, check for consecutive failures
	if windowSize == threshold {
		for i := 0; i < threshold && i < len(results); i++ {
			if results[i].State != "success" {
				failureCount++
			} else {
				// Not consecutive
				return false
			}
		}
		return failureCount >= threshold
	}

	// Otherwise, count total failures in window
	for _, result := range results {
		if result.State != "success" {
			failureCount++
		}
	}

	return failureCount >= threshold
}

// jiraPriorityRank maps Jira priority names to a numeric rank.
// Lower number = higher priority.
var jiraPriorityRank = map[string]int{
	"Blocker":  1,
	"Critical": 2,
	"Major":    3,
	"Normal":   4,
	"Minor":    5,
	"Trivial":  6,
}

func isHigherPriority(newPriority, existingPriority string) bool {
	newRank, newOk := jiraPriorityRank[newPriority]
	existingRank, existingOk := jiraPriorityRank[existingPriority]
	if !newOk || !existingOk {
		return false
	}
	return newRank < existingRank
}

// triggerJiraEscalation creates or updates a Jira issue based on the escalation.
// Returns the Jira issue key if an issue was created or updated, or empty string on failure/skip.
func (c *JiraEscalationsController) triggerJiraEscalation(
	ctx context.Context,
	releasePayload *v1alpha1.ReleasePayload,
	job v1alpha1.JobStatus,
	qualifierID releasequalifierslib.QualifierId,
	jiraConfig *jira.Notification,
	escalation jira.Escalation,
) string {
	threadID := c.buildThreadID(releasePayload, qualifierID, jiraConfig)

	klog.Infof("Triggering Jira escalation '%s' for job %s in payload %s/%s",
		escalation.Name,
		job.CIConfigurationJobName,
		releasePayload.Namespace,
		releasePayload.Name,
	)

	if c.jiraClient == nil {
		klog.V(4).Infof("Jira client not configured, skipping ticket creation for thread %s", threadID)
		return ""
	}

	existingIssue, err := c.findExistingEscalationIssue(ctx, jiraConfig.Project, threadID)
	if err != nil {
		klog.Errorf("Failed to search for existing Jira issue (thread: %s): %v", threadID, err)
		return ""
	}

	if existingIssue != nil {
		c.updateExistingEscalation(existingIssue, releasePayload, job, qualifierID, jiraConfig, escalation, threadID)
		return existingIssue.Key
	}

	return c.createNewEscalation(releasePayload, job, qualifierID, jiraConfig, escalation, threadID)
}

func (c *JiraEscalationsController) findExistingEscalationIssue(
	ctx context.Context,
	project string,
	threadID string,
) (*gojira.Issue, error) {
	jql := fmt.Sprintf(
		`project = "%s" AND labels = "%s" AND status not in (Closed, Resolved, Verified)`,
		project,
		threadID,
	)

	issues, _, err := c.jiraClient.SearchV2JqlWithContext(ctx, jql, &gojira.SearchOptionsV2{
		MaxResults: 1,
		Fields:     []string{"summary", "priority", "status", "labels"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search Jira for thread %s: %w", threadID, err)
	}

	if len(issues) == 0 {
		return nil, nil
	}

	return &issues[0], nil
}

func (c *JiraEscalationsController) buildEscalationSummary(
	jiraConfig *jira.Notification,
	escalation jira.Escalation,
	job v1alpha1.JobStatus,
) string {
	summary := jiraConfig.Summary
	if summary == "" {
		summary = fmt.Sprintf("[%s] %s escalation for %s",
			escalation.Priority,
			escalation.Name,
			job.CIConfigurationJobName,
		)
	}

	if len(escalation.Mentions) > 0 {
		mentions := make([]string, len(escalation.Mentions))
		for i, user := range escalation.Mentions {
			mentions[i] = fmt.Sprintf("[~%s]", user)
		}
		summary += " | CC: " + strings.Join(mentions, ", ")
	}

	if len(escalation.NeedsInfo) > 0 {
		needsInfo := make([]string, len(escalation.NeedsInfo))
		for i, user := range escalation.NeedsInfo {
			needsInfo[i] = fmt.Sprintf("[~%s]", user)
		}
		summary += " | Needs Info: " + strings.Join(needsInfo, ", ")
	}

	return summary
}

func (c *JiraEscalationsController) buildEscalationDescription(
	releasePayload *v1alpha1.ReleasePayload,
	job v1alpha1.JobStatus,
	qualifierID releasequalifierslib.QualifierId,
	jiraConfig *jira.Notification,
	escalation jira.Escalation,
) string {
	if jiraConfig.Description != "" {
		return jiraConfig.Description
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "*Escalation:* %s\n", escalation.Name)
	fmt.Fprintf(&sb, "*Job:* %s\n", job.CIConfigurationJobName)
	fmt.Fprintf(&sb, "*Payload:* %s/%s\n", releasePayload.Namespace, releasePayload.Name)
	fmt.Fprintf(&sb, "*Qualifier:* %s\n", qualifierID)
	fmt.Fprintf(&sb, "*Priority:* %s\n", escalation.Priority)

	if escalation.Failures > 0 {
		if escalation.OverLastRuns != nil {
			fmt.Fprintf(&sb, "*Condition:* %d failures over last %d runs\n", escalation.Failures, *escalation.OverLastRuns)
		} else {
			fmt.Fprintf(&sb, "*Condition:* %d consecutive failures\n", escalation.Failures)
		}
	}
	if escalation.PassPercentage != nil {
		fmt.Fprintf(&sb, "*Condition:* Pass percentage below %d%%", *escalation.PassPercentage)
		if escalation.OverLastRuns != nil {
			fmt.Fprintf(&sb, " over last %d runs", *escalation.OverLastRuns)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func (c *JiraEscalationsController) createNewEscalation(
	releasePayload *v1alpha1.ReleasePayload,
	job v1alpha1.JobStatus,
	qualifierID releasequalifierslib.QualifierId,
	jiraConfig *jira.Notification,
	escalation jira.Escalation,
	threadID string,
) string {
	summary := c.buildEscalationSummary(jiraConfig, escalation, job)
	description := c.buildEscalationDescription(releasePayload, job, qualifierID, jiraConfig, escalation)

	issue := &gojira.Issue{
		Fields: &gojira.IssueFields{
			Project:     gojira.Project{Key: jiraConfig.Project},
			Summary:     summary,
			Description: description,
			Type:        gojira.IssueType{Name: "Bug"},
			Priority:    &gojira.Priority{Name: escalation.Priority},
			Labels:      []string{threadID},
		},
	}

	if jiraConfig.Component != "" {
		issue.Fields.Components = []*gojira.Component{
			{Name: jiraConfig.Component},
		}
	}

	if jiraConfig.Assignee != "" {
		issue.Fields.Assignee = &gojira.User{Name: jiraConfig.Assignee}
	}

	createdIssue, err := c.jiraClient.CreateIssue(issue)
	if err != nil {
		klog.Errorf("Failed to create Jira issue for thread %s: %v", threadID, err)
		return ""
	}

	klog.Infof("Created Jira issue %s for thread %s (project: %s, priority: %s)",
		createdIssue.Key, threadID, jiraConfig.Project, escalation.Priority)

	c.addWatchers(createdIssue.Key, escalation.NeedsInfo)

	return createdIssue.Key
}

func (c *JiraEscalationsController) updateExistingEscalation(
	existingIssue *gojira.Issue,
	releasePayload *v1alpha1.ReleasePayload,
	job v1alpha1.JobStatus,
	qualifierID releasequalifierslib.QualifierId,
	jiraConfig *jira.Notification,
	escalation jira.Escalation,
	threadID string,
) {
	issueKey := existingIssue.Key

	if existingIssue.Fields != nil && existingIssue.Fields.Priority != nil {
		if isHigherPriority(escalation.Priority, existingIssue.Fields.Priority.Name) {
			updateIssue := &gojira.Issue{
				Key: issueKey,
				Fields: &gojira.IssueFields{
					Priority: &gojira.Priority{Name: escalation.Priority},
				},
			}
			if _, err := c.jiraClient.UpdateIssue(updateIssue); err != nil {
				klog.Errorf("Failed to update priority on %s: %v", issueKey, err)
			} else {
				klog.Infof("Updated priority on %s from %s to %s",
					issueKey, existingIssue.Fields.Priority.Name, escalation.Priority)
			}
		}
	}

	commentBody := c.buildEscalationDescription(releasePayload, job, qualifierID, jiraConfig, escalation)
	comment := &gojira.Comment{
		Body: fmt.Sprintf("Escalation '%s' re-triggered:\n\n%s", escalation.Name, commentBody),
	}

	if _, err := c.jiraClient.AddComment(issueKey, comment); err != nil {
		klog.Errorf("Failed to add comment to %s: %v", issueKey, err)
		return
	}

	c.addWatchers(issueKey, escalation.NeedsInfo)

	klog.Infof("Updated existing Jira issue %s for thread %s", issueKey, threadID)
}

func (c *JiraEscalationsController) addWatchers(issueKey string, users []string) {
	if len(users) == 0 || c.jiraClient == nil {
		return
	}
	for _, username := range users {
		var err error
		if c.addWatcherFn != nil {
			err = c.addWatcherFn(issueKey, username)
		} else {
			_, err = c.jiraClient.JiraClient().Issue.AddWatcher(issueKey, username)
		}
		if err != nil {
			klog.Warningf("Failed to add watcher %s to %s: %v", username, issueKey, err)
		} else {
			klog.V(4).Infof("Added watcher %s to %s", username, issueKey)
		}
	}
}

// buildThreadID constructs a unique thread identifier for Jira notifications
// Format: <stream>-<qualifierID>-<project>-<component>-<thread>
func (c *JiraEscalationsController) buildThreadID(
	releasePayload *v1alpha1.ReleasePayload,
	qualifierID releasequalifierslib.QualifierId,
	jiraConfig *jira.Notification,
) string {
	stream := releasePayload.Spec.PayloadCoordinates.StreamName
	if stream == "" {
		stream = "unknown"
	}
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		stream,
		qualifierID,
		jiraConfig.Project,
		jiraConfig.Component,
		jiraConfig.Thread,
	)
}

