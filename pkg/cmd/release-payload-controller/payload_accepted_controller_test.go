package release_payload_controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/client/clientset/versioned/fake"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/clock"
)

func TestComputeReleasePayloadAcceptedCondition(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		payload  *v1alpha1.ReleasePayload
		expected metav1.Condition
	}{
		// === Empty / default state ===
		{
			name:    "EmptyPayload",
			payload: &v1alpha1.ReleasePayload{},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionUnknown,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		// === Release creation job failure (highest precedence) ===
		{
			name: "CreationJobFailed",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Status:  v1alpha1.ReleaseCreationJobFailed,
						Message: "BackoffLimitExceeded",
					},
				},
			},
			expected: metav1.Condition{
				Type:    v1alpha1.ConditionPayloadAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  ReleasePayloadCreationJobFailedReason,
				Message: "BackoffLimitExceeded",
			},
		},
		{
			name: "CreationJobFailedWithEmptyMessage",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Status: v1alpha1.ReleaseCreationJobFailed,
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionFalse,
				Reason: ReleasePayloadCreationJobFailedReason,
			},
		},
		{
			name: "CreationJobFailedTakesPrecedenceOverAcceptedOverride",
			payload: &v1alpha1.ReleasePayload{
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideAccepted,
						Reason:   "Manually accepted per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Status:  v1alpha1.ReleaseCreationJobFailed,
						Message: "BackoffLimitExceeded",
					},
				},
			},
			expected: metav1.Condition{
				Type:    v1alpha1.ConditionPayloadAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  ReleasePayloadCreationJobFailedReason,
				Message: "BackoffLimitExceeded",
			},
		},
		{
			name: "CreationJobFailedTakesPrecedenceOverRejectedOverride",
			payload: &v1alpha1.ReleasePayload{
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "Manually rejected per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Status:  v1alpha1.ReleaseCreationJobFailed,
						Message: "BackoffLimitExceeded",
					},
				},
			},
			expected: metav1.Condition{
				Type:    v1alpha1.ConditionPayloadAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  ReleasePayloadCreationJobFailedReason,
				Message: "BackoffLimitExceeded",
			},
		},
		{
			name: "CreationJobFailedTakesPrecedenceOverSuccessfulBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Status:  v1alpha1.ReleaseCreationJobFailed,
						Message: "BackoffLimitExceeded",
					},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateSuccess},
					},
				},
			},
			expected: metav1.Condition{
				Type:    v1alpha1.ConditionPayloadAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  ReleasePayloadCreationJobFailedReason,
				Message: "BackoffLimitExceeded",
			},
		},
		{
			name: "CreationJobSuccessDoesNotShortCircuit",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Status: v1alpha1.ReleaseCreationJobSuccess,
					},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateSuccess},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionTrue,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "CreationJobUnknownBlocksAcceptance",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Status: v1alpha1.ReleaseCreationJobUnknown,
					},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateSuccess},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionUnknown,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		// === PayloadOverride (second precedence) ===
		{
			name: "OverrideAccepted",
			payload: &v1alpha1.ReleasePayload{
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideAccepted,
						Reason:   "Manually accepted per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
				},
			},
			expected: metav1.Condition{
				Type:    v1alpha1.ConditionPayloadAccepted,
				Status:  metav1.ConditionTrue,
				Reason:  ReleasePayloadManuallyAcceptedReason,
				Message: "Manually accepted per TRT",
			},
		},
		{
			name: "OverrideRejected",
			payload: &v1alpha1.ReleasePayload{
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "Manually rejected per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
				},
			},
			expected: metav1.Condition{
				Type:    v1alpha1.ConditionPayloadAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  ReleasePayloadManuallyRejectedReason,
				Message: "Manually rejected per TRT",
			},
		},
		{
			name: "OverrideAcceptedWithEmptyReason",
			payload: &v1alpha1.ReleasePayload{
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideAccepted,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionTrue,
				Reason: ReleasePayloadManuallyAcceptedReason,
			},
		},
		{
			name: "OverrideRejectedWithEmptyReason",
			payload: &v1alpha1.ReleasePayload{
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionFalse,
				Reason: ReleasePayloadManuallyRejectedReason,
			},
		},
		{
			name: "OverrideAcceptedTakesPrecedenceOverFailedBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideAccepted,
						Reason:   "Force accept despite failures",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateFailure},
						{AggregateState: v1alpha1.JobStateFailure},
					},
				},
			},
			expected: metav1.Condition{
				Type:    v1alpha1.ConditionPayloadAccepted,
				Status:  metav1.ConditionTrue,
				Reason:  ReleasePayloadManuallyAcceptedReason,
				Message: "Force accept despite failures",
			},
		},
		{
			name: "OverrideRejectedTakesPrecedenceOverSuccessfulBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "Force reject despite success",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateSuccess},
						{AggregateState: v1alpha1.JobStateSuccess},
					},
				},
			},
			expected: metav1.Condition{
				Type:    v1alpha1.ConditionPayloadAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  ReleasePayloadManuallyRejectedReason,
				Message: "Force reject despite success",
			},
		},
		// === Blocking job results (lowest precedence) ===
		{
			name: "AllBlockingJobsSuccessful",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{CIConfigurationName: "job-a", AggregateState: v1alpha1.JobStateSuccess},
						{CIConfigurationName: "job-b", AggregateState: v1alpha1.JobStateSuccess},
						{CIConfigurationName: "job-c", AggregateState: v1alpha1.JobStateSuccess},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionTrue,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "SingleBlockingJobSuccessful",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateSuccess},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionTrue,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "SingleBlockingJobFailed",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateFailure},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionFalse,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "SingleBlockingJobPending",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStatePending},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionFalse,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "MixOfSuccessAndPendingBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateSuccess},
						{AggregateState: v1alpha1.JobStatePending},
						{AggregateState: v1alpha1.JobStateSuccess},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionFalse,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "MixOfSuccessAndFailedBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateSuccess},
						{AggregateState: v1alpha1.JobStateSuccess},
						{AggregateState: v1alpha1.JobStateFailure},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionFalse,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "FailureTakesPrecedenceOverPendingInBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateSuccess},
						{AggregateState: v1alpha1.JobStateFailure},
						{AggregateState: v1alpha1.JobStatePending},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionFalse,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "AllBlockingJobsPending",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStatePending},
						{AggregateState: v1alpha1.JobStatePending},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionFalse,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "AllBlockingJobsFailed",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateFailure},
						{AggregateState: v1alpha1.JobStateFailure},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionFalse,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "BlockingJobWithUnknownState",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateSuccess},
						{AggregateState: v1alpha1.JobStateUnknown},
						{AggregateState: v1alpha1.JobStateSuccess},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionUnknown,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "BlockingJobWithEmptyAggregateState",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateSuccess},
						{AggregateState: ""},
						{AggregateState: v1alpha1.JobStateSuccess},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionUnknown,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "NoBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionTrue,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		// === Informing/upgrade jobs should not affect acceptance ===
		{
			name: "InformingJobFailureDoesNotAffectAcceptance",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateSuccess},
					},
					InformingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateFailure},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionTrue,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "UpgradeJobFailureDoesNotAffectAcceptance",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateSuccess},
					},
					UpgradeJobResults: []v1alpha1.JobStatus{
						{AggregateState: v1alpha1.JobStateFailure},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionTrue,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		// === No blocking jobs: wait for informing/upgrade jobs to complete, then accept ===
		{
			name: "OnlyInformingJobsSuccessfulNoBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					InformingJobResults: []v1alpha1.JobStatus{
						{CIConfigurationName: "informing-a", AggregateState: v1alpha1.JobStateSuccess},
						{CIConfigurationName: "informing-b", AggregateState: v1alpha1.JobStateSuccess},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionTrue,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "OnlyInformingJobsWithFailuresNoBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					InformingJobResults: []v1alpha1.JobStatus{
						{CIConfigurationName: "informing-a", AggregateState: v1alpha1.JobStateSuccess},
						{CIConfigurationName: "informing-b", AggregateState: v1alpha1.JobStateFailure},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionTrue,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "OnlyInformingJobsPendingNoBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					InformingJobResults: []v1alpha1.JobStatus{
						{CIConfigurationName: "informing-a", AggregateState: v1alpha1.JobStatePending},
						{CIConfigurationName: "informing-b", AggregateState: v1alpha1.JobStateSuccess},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionUnknown,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "InformingJobsWithUnsetStateNoBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					InformingJobResults: []v1alpha1.JobStatus{
						{CIConfigurationName: "informing-a"},
						{CIConfigurationName: "informing-b"},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionUnknown,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "OnlyUpgradeJobsSuccessfulNoBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					UpgradeJobResults: []v1alpha1.JobStatus{
						{CIConfigurationName: "upgrade-a", AggregateState: v1alpha1.JobStateSuccess},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionTrue,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "OnlyUpgradeJobsPendingNoBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					UpgradeJobResults: []v1alpha1.JobStatus{
						{CIConfigurationName: "upgrade-a", AggregateState: v1alpha1.JobStatePending},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionUnknown,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "MixedInformingAndUpgradeJobsPendingNoBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					InformingJobResults: []v1alpha1.JobStatus{
						{CIConfigurationName: "informing-a", AggregateState: v1alpha1.JobStateSuccess},
					},
					UpgradeJobResults: []v1alpha1.JobStatus{
						{CIConfigurationName: "upgrade-a", AggregateState: v1alpha1.JobStatePending},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionUnknown,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			// Verifies that a pending job is not masked by a failure in a sibling
			// job. Acceptance must wait for all jobs to complete, regardless of
			// whether other jobs have already failed.
			name: "MixedInformingAndUpgradeJobsFailureAndPendingNoBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					InformingJobResults: []v1alpha1.JobStatus{
						{CIConfigurationName: "informing-a", AggregateState: v1alpha1.JobStateFailure},
					},
					UpgradeJobResults: []v1alpha1.JobStatus{
						{CIConfigurationName: "upgrade-a", AggregateState: v1alpha1.JobStatePending},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionUnknown,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
		{
			name: "MixedInformingAndUpgradeJobsCompleteNoBlockingJobs",
			payload: &v1alpha1.ReleasePayload{
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					InformingJobResults: []v1alpha1.JobStatus{
						{CIConfigurationName: "informing-a", AggregateState: v1alpha1.JobStateSuccess},
						{CIConfigurationName: "informing-b", AggregateState: v1alpha1.JobStateFailure},
					},
					UpgradeJobResults: []v1alpha1.JobStatus{
						{CIConfigurationName: "upgrade-a", AggregateState: v1alpha1.JobStateSuccess},
					},
				},
			},
			expected: metav1.Condition{
				Type:   v1alpha1.ConditionPayloadAccepted,
				Status: metav1.ConditionTrue,
				Reason: ReleasePayloadAcceptedReason,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := computeReleasePayloadAcceptedCondition(tc.payload)
			if diff := cmp.Diff(tc.expected, result, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")); diff != "" {
				t.Errorf("unexpected condition (-expected +got):\n%s", diff)
			}
		})
	}
}


func TestPayloadAcceptedSync(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    *v1alpha1.ReleasePayload
		expected *v1alpha1.ReleasePayload
	}{
		{
			name: "TestPayloadOverrideAccepted",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideAccepted,
						Reason:   "Manually accepted per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideAccepted,
						Reason:   "Manually accepted per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadAccepted,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadManuallyAcceptedReason,
							Message: "Manually accepted per TRT",
						},
					},
				},
			},
		},
		{
			name: "TestPayloadOverrideAcceptedAfterRejected",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideAccepted,
						Reason:   "Manually accepted per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadRejected,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadManuallyRejectedReason,
							Message: "Manually rejected per TRT",
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideAccepted,
						Reason:   "Manually accepted per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadAccepted,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadManuallyAcceptedReason,
							Message: "Manually accepted per TRT",
						},
						{
							Type:    v1alpha1.ConditionPayloadRejected,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadManuallyRejectedReason,
							Message: "Manually rejected per TRT",
						},
					},
				},
			},
		},
		{
			name: "TestPayloadOverrideRejected",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "Manually rejected per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "Manually rejected per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadAccepted,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadManuallyRejectedReason,
							Message: "Manually rejected per TRT",
						},
					},
				},
			},
		},
		{
			name: "TestPayloadOverrideRejectedAfterAccepted",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "Manually rejected per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadRejected,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadManuallyRejectedReason,
							Message: "Manually rejected per TRT",
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadOverride: v1alpha1.ReleasePayloadOverride{
						Override: v1alpha1.ReleasePayloadOverrideRejected,
						Reason:   "Manually rejected per TRT",
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadAccepted,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadManuallyRejectedReason,
							Message: "Manually rejected per TRT",
						},
						{
							Type:    v1alpha1.ConditionPayloadRejected,
							Status:  metav1.ConditionTrue,
							Reason:  ReleasePayloadManuallyRejectedReason,
							Message: "Manually rejected per TRT",
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithSuccessfulBlockingJobs",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha1.ConditionPayloadAccepted,
							Status: metav1.ConditionTrue,
							Reason: ReleasePayloadAcceptedReason,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithFailedBlockingJob",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateFailure,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateFailure,
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha1.ConditionPayloadAccepted,
							Status: metav1.ConditionFalse,
							Reason: ReleasePayloadAcceptedReason,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithPendingBlockingJob",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateFailure,
						},
						{
							AggregateState: v1alpha1.JobStatePending,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateFailure,
						},
						{
							AggregateState: v1alpha1.JobStatePending,
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha1.ConditionPayloadAccepted,
							Status: metav1.ConditionFalse,
							Reason: ReleasePayloadAcceptedReason,
						},
					},
				},
			},
		},
		{
			name: "ReleasePayloadWithUnknownBlockingJob",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateUnknown,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{Status: v1alpha1.ReleaseCreationJobSuccess},
					BlockingJobResults: []v1alpha1.JobStatus{
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
						{
							AggregateState: v1alpha1.JobStateUnknown,
						},
						{
							AggregateState: v1alpha1.JobStateSuccess,
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha1.ConditionPayloadAccepted,
							Status: metav1.ConditionUnknown,
							Reason: ReleasePayloadAcceptedReason,
						},
					},
				},
			},
		},
		{
			name: "ReleaseCreationJobFailed",
			input: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
						Status:  v1alpha1.ReleaseCreationJobFailed,
						Message: "BackoffLimitExceeded: Job has reached the specified backoff limit",
					},
				},
			},
			expected: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Status: v1alpha1.ReleasePayloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:    v1alpha1.ConditionPayloadAccepted,
							Status:  metav1.ConditionFalse,
							Reason:  ReleasePayloadCreationJobFailedReason,
							Message: "BackoffLimitExceeded: Job has reached the specified backoff limit",
						},
					},
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
						Status:  v1alpha1.ReleaseCreationJobFailed,
						Message: "BackoffLimitExceeded: Job has reached the specified backoff limit",
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			releasePayloadClient := fake.NewSimpleClientset(testCase.input)

			releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactory(releasePayloadClient, controllerDefaultResyncDuration)
			releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()

			c, err := NewPayloadAcceptedController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), events.NewInMemoryRecorder("payload-accepted-controller-test", clock.RealClock{}))
			if err != nil {
				t.Fatalf("Failed to create Payload Accepted Controller: %v", err)
			}

			releasePayloadInformerFactory.Start(context.Background().Done())

			if !cache.WaitForNamedCacheSync("PayloadAcceptedController", context.Background().Done(), c.cachesToSync...) {
				t.Errorf("%s: error waiting for caches to sync", testCase.name)
				return
			}

			if err := c.sync(context.TODO(), fmt.Sprintf("%s/%s", testCase.input.Namespace, testCase.input.Name)); err != nil {
				t.Errorf("%s: unexpected err: %v", testCase.name, err)
			}

			// Performing a live lookup instead of having to wait for the cache to sink (again)...
			output, _ := c.releasePayloadClient.ReleasePayloads(testCase.input.Namespace).Get(context.TODO(), testCase.input.Name, metav1.GetOptions{})
			if !cmp.Equal(output, testCase.expected, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, output)
			}
		})
	}
}
