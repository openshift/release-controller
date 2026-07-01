package qualifiers

import (
	"fmt"

	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/releasepayload/jobstatus"
	"github.com/openshift/release-controller/pkg/releasequalifiers"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	qualifierStateLabelPrefix = "release.openshift.io/"
	qualifierStateLabelSuffix = "_state"
	qualifierStateAccepted    = "Accepted"
	qualifierStateRejected    = "Rejected"
)

type ByQualifierJobReferenceName []v1alpha1.ReleaseQualifierJobReference

func (in ByQualifierJobReferenceName) Less(i, j int) bool {
	return in[i].CIConfigurationName < in[j].CIConfigurationName
}

func (in ByQualifierJobReferenceName) Len() int {
	return len(in)
}

func (in ByQualifierJobReferenceName) Swap(i, j int) {
	in[i], in[j] = in[j], in[i]
}

func GenerateQualifiersSummary(
	payload *v1alpha1.ReleasePayload,
	configAccessor releasequalifiers.ConfigAccessor,
) *v1alpha1.QualifiersSummary {
	// Build job status index for fast lookup
	jobStatusIndex := buildJobStatusIndex(&payload.Status)

	// Compute the complete summary directly
	result := computeQualifierStates(payload, jobStatusIndex, configAccessor)

	// Preserve JiraNotifications from existing summary (written by jira_escalations_controller)
	if payload.Status.QualifiersSummary != nil {
		for qualifierID, summary := range result.Qualifiers {
			if existing, ok := payload.Status.QualifiersSummary.Qualifiers[qualifierID]; ok && len(existing.JiraNotifications) > 0 {
				summary.JiraNotifications = existing.JiraNotifications
				result.Qualifiers[qualifierID] = summary
			}
		}
	}

	return result
}

// ComputeQualifierState aggregates the already-computed AggregateState values
// from JobStatus entries for a qualifier. This reuses the same aggregation logic
// as ComputeJobState but operates on pre-computed states.
// Designed to be extensible for future count-based thresholds.
func ComputeQualifierState(jobStatuses []v1alpha1.JobStatus) v1alpha1.JobState {
	// JobStatuses already have their AggregateState computed by JobStateController
	// We just need to aggregate those states using the same logic
	return jobstatus.ComputeJobState(jobStatuses)
}

// ComputeBadgeEarned determines if a qualifier has earned its badge.
// A badge is earned when BOTH conditions are met:
// 1. The qualifier is enabled in the merged config
// 2. All jobs for that qualifier have succeeded
func ComputeBadgeEarned(enabled *bool, aggregateState v1alpha1.JobState) bool {
	// Check if enabled (nil means not set, treat as disabled)
	if enabled == nil || !*enabled {
		return false
	}
	// Badge earned only if all jobs succeeded
	return aggregateState == v1alpha1.JobStateSuccess
}

// ComputeBadgePropagated determines if a badge should be propagated to the
// payload level based on PayloadBadgeStatus configuration.
// This is independent of payload state and purely instructs the UI about display.
func ComputeBadgePropagated(
	enabled *bool,
	badgeStatus releasequalifiers.BadgeStatus,
	aggregateState v1alpha1.JobState,
) bool {
	// Check if enabled
	if enabled == nil || !*enabled {
		return false
	}

	// Apply PayloadBadgeStatus rules
	switch badgeStatus {
	case releasequalifiers.BadgeStatusYes:
		// Always propagate (no conditions)
		return true
	case releasequalifiers.BadgeStatusNo:
		// Never propagate
		return false
	case releasequalifiers.BadgeStatusOnSuccess:
		// Propagate when earned (enabled + all jobs succeeded)
		return aggregateState == v1alpha1.JobStateSuccess
	case releasequalifiers.BadgeStatusOnFailure:
		// Propagate when enabled BUT jobs failed
		return aggregateState == v1alpha1.JobStateFailure
	default:
		// Unknown or empty badge status - don't propagate
		return false
	}
}

// buildJobStatusIndex creates a lookup map for fast job status retrieval
func buildJobStatusIndex(status *v1alpha1.ReleasePayloadStatus) map[string]v1alpha1.JobStatus {
	index := make(map[string]v1alpha1.JobStatus)

	for _, jobStatus := range status.BlockingJobResults {
		key := jobStatus.CIConfigurationName + "/" + jobStatus.CIConfigurationJobName
		index[key] = jobStatus
	}
	for _, jobStatus := range status.InformingJobResults {
		key := jobStatus.CIConfigurationName + "/" + jobStatus.CIConfigurationJobName
		index[key] = jobStatus
	}

	return index
}

// findJobStatusesForQualifier extracts job statuses for a specific qualifier
func findJobStatusesForQualifier(
	jobRefs []v1alpha1.ReleaseQualifierJobReference,
	jobStatusIndex map[string]v1alpha1.JobStatus,
) []v1alpha1.JobStatus {
	result := make([]v1alpha1.JobStatus, 0, len(jobRefs))

	for _, jobRef := range jobRefs {
		key := jobRef.CIConfigurationName + "/" + jobRef.CIConfigurationJobName
		if jobStatus, exists := jobStatusIndex[key]; exists {
			result = append(result, jobStatus)
		}
	}

	return result
}

// computeQualifierStates computes the aggregate state and badge info for all qualifiers
// This is used to determine badge earned/propagated status for each qualifier
func computeQualifierStates(
	payload *v1alpha1.ReleasePayload,
	jobStatusIndex map[string]v1alpha1.JobStatus,
	configAccessor releasequalifiers.ConfigAccessor,
) *v1alpha1.QualifiersSummary {
	// Collect all job references per qualifier AND their job-level overrides
	qualifierJobs := map[releasequalifiers.QualifierId][]v1alpha1.ReleaseQualifierJobReference{}
	jobLevelOverrides := map[releasequalifiers.QualifierId]releasequalifiers.ReleaseQualifier{}

	for _, jobs := range [][]v1alpha1.CIConfiguration{
		payload.Spec.PayloadVerificationConfig.BlockingJobs,
		payload.Spec.PayloadVerificationConfig.InformingJobs,
	} {
		for _, job := range jobs {
			if len(job.Qualifiers) > 0 {
				for key, qualifier := range job.Qualifiers {
					qualifierJobs[key] = append(qualifierJobs[key], v1alpha1.ReleaseQualifierJobReference{
						CIConfigurationName:    job.CIConfigurationName,
						CIConfigurationJobName: job.CIConfigurationJobName,
					})
					// Store job-level override (last one wins if multiple jobs define same qualifier)
					jobLevelOverrides[key] = qualifier
				}
			}
		}
	}

	// Load global config (nil-safe)
	var globalConfig releasequalifiers.ReleaseQualifiers
	if configAccessor != nil {
		globalConfig = configAccessor.Get()
	}

	// Compute state for each qualifier, collecting failure labels at the top level
	qualifiers := make(map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary)
	var allFailureLabels []string

	for qualifierID, jobs := range qualifierJobs {
		// Job-level overrides only apply when a matching global config entry exists
		globalQualifier, exists := globalConfig[qualifierID]
		if !exists {
			continue
		}

		// Find all JobStatus entries for jobs belonging to this qualifier
		jobStatuses := findJobStatusesForQualifier(jobs, jobStatusIndex)

		// Aggregate pre-computed JobStatus.AggregateState values
		aggregateState := ComputeQualifierState(jobStatuses)

		// Merge qualifier config: global config + job-level overrides
		mergedConfig := globalQualifier
		if jobOverride, exists := jobLevelOverrides[qualifierID]; exists {
			mergedConfig = mergedConfig.Merge(jobOverride)
		}

		// Compute badge earned and propagated
		badgeEarned := ComputeBadgeEarned(mergedConfig.Enabled, aggregateState)
		badgePropagated := ComputeBadgePropagated(mergedConfig.Enabled, mergedConfig.PayloadBadgeStatus, aggregateState)

		if aggregateState == v1alpha1.JobStateFailure {
			allFailureLabels = append(allFailureLabels, mergedConfig.FailureLabels...)
		}

		qualifiers[qualifierID] = v1alpha1.ReleaseQualifierSummary{
			Jobs:            jobs,
			AggregateState:  aggregateState,
			BadgeName:       mergedConfig.BadgeName,
			BadgeEarned:     badgeEarned,
			BadgePropagated: badgePropagated,
			FailureLabels:   mergedConfig.FailureLabels,
		}
	}

	// Process approval-based qualifiers from global config
	processApprovalQualifiers(payload, globalConfig, qualifiers, &allFailureLabels)

	return &v1alpha1.QualifiersSummary{
		FailureLabels: deduplicateAndSortStrings(allFailureLabels),
		Qualifiers:    qualifiers,
	}
}

// QualifierStateLabelKey returns the label key for a qualifier's approval state
func QualifierStateLabelKey(qualifierID releasequalifiers.QualifierId) string {
	return fmt.Sprintf("%s%s%s", qualifierStateLabelPrefix, qualifierID, qualifierStateLabelSuffix)
}

// processApprovalQualifiers checks global config for qualifiers with Approval=true
// and adds them to the summary based on the ReleasePayload's state label
// (release.openshift.io/{qualifier_id}_state). Recognized values:
//   - "Accepted" → JobStateSuccess (badge earned)
//   - "Rejected" → JobStateFailure (badge not earned)
//
// Other label values and missing labels are ignored.
// Approval-based qualifiers never have associated jobs — they are purely label-driven.
// If a qualifier ID already exists from job processing, it is skipped because approval
// qualifiers and job-based qualifiers are mutually exclusive.
func processApprovalQualifiers(
	payload *v1alpha1.ReleasePayload,
	globalConfig releasequalifiers.ReleaseQualifiers,
	result map[releasequalifiers.QualifierId]v1alpha1.ReleaseQualifierSummary,
	allFailureLabels *[]string,
) {
	if globalConfig == nil {
		return
	}

	for qualifierID, qualifier := range globalConfig {
		if qualifier.Approval == nil || !*qualifier.Approval {
			continue
		}

		// Approval qualifiers must not collide with job-based qualifiers
		if _, exists := result[qualifierID]; exists {
			continue
		}

		labelKey := QualifierStateLabelKey(qualifierID)
		labelValue, exists := payload.Labels[labelKey]
		if !exists {
			continue
		}

		var aggregateState v1alpha1.JobState
		var badgeEarned bool

		switch labelValue {
		case qualifierStateAccepted:
			aggregateState = v1alpha1.JobStateSuccess
			badgeEarned = true
		case qualifierStateRejected:
			aggregateState = v1alpha1.JobStateFailure
			badgeEarned = false
		default:
			continue
		}

		if aggregateState == v1alpha1.JobStateFailure {
			*allFailureLabels = append(*allFailureLabels, qualifier.FailureLabels...)
		}

		result[qualifierID] = v1alpha1.ReleaseQualifierSummary{
			AggregateState: aggregateState,
			BadgeName:      qualifier.BadgeName,
			BadgeEarned:    badgeEarned,
			Approval:       true,
			FailureLabels:  qualifier.FailureLabels,
			BadgePropagated: ComputeBadgePropagated(
				qualifier.Enabled, qualifier.PayloadBadgeStatus, aggregateState,
			),
		}
	}
}

func deduplicateAndSortStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return sets.List(sets.New(s...))
}
