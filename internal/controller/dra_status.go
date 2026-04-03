package controller

import (
	"context"
	"fmt"

	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/dra"
)

// DRAClaimStatusResult captures the result of observing DRA claim
// allocation state. Used to update RTJ status.devices fields.
type DRAClaimStatusResult struct {
	// StatusChanged indicates whether any status.devices field was modified.
	StatusChanged bool

	// Summary is the computed claim allocation summary.
	Summary dra.ClaimAllocationSummary
}

// observeDRAClaimStatus queries ResourceClaims in the namespace that are
// associated with the RTJ's ResourceClaimTemplates and updates the
// status.devices claim allocation fields. This is a read-observe-update
// pattern that does not modify Kubernetes objects directly (the caller
// is responsible for persisting status changes).
//
// When DRA is not enabled, this is a no-op returning an unchanged result.
func (r *ResumableTrainingJobReconciler) observeDRAClaimStatus(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	now metav1.Time,
) (DRAClaimStatusResult, error) {
	result := DRAClaimStatusResult{}

	if !job.IsDevicesEnabled() || job.Status.Devices == nil {
		return result, nil
	}

	logger := log.FromContext(ctx)

	// Build the set of known template names for filtering.
	templateNames := make(map[string]bool, len(job.Status.Devices.ResourceClaimTemplateRefs))
	for _, ref := range job.Status.Devices.ResourceClaimTemplateRefs {
		templateNames[ref.Name] = true
	}

	// List all ResourceClaims in the namespace.
	var claimList resourcev1beta1.ResourceClaimList
	if err := r.List(ctx, &claimList, client.InNamespace(job.Namespace)); err != nil {
		return result, fmt.Errorf("list ResourceClaims: %w", err)
	}

	// Filter to claims associated with this RTJ.
	rtjClaims := dra.FilterClaimsForRTJ(
		claimList.Items,
		job.Name,
		string(job.UID),
		templateNames,
	)

	logger.V(1).Info("observed DRA claims",
		"totalInNamespace", len(claimList.Items),
		"matchingRTJ", len(rtjClaims),
		"templateCount", len(templateNames),
	)

	// If no claims are found but templates exist, the claims have not been
	// created yet (pods not scheduled). Keep the current state.
	if len(rtjClaims) == 0 && len(templateNames) > 0 {
		// Only change state if currently Unknown (initial state).
		if job.Status.Devices.ClaimAllocationState == trainingv1alpha1.ClaimAllocationUnknown {
			job.Status.Devices.ClaimAllocationState = trainingv1alpha1.ClaimAllocationPending
			result.StatusChanged = true
		}
		return result, nil
	}

	// Summarize the claim allocation state.
	summary := dra.SummarizeClaimAllocations(rtjClaims)
	result.Summary = summary

	// Update status fields.
	result.StatusChanged = syncClaimAllocationFields(job, summary, now)

	return result, nil
}

// syncClaimAllocationFields updates the RTJ's status.devices claim
// allocation fields from a ClaimAllocationSummary. Returns true when
// any field changed.
func syncClaimAllocationFields(
	job *trainingv1alpha1.ResumableTrainingJob,
	summary dra.ClaimAllocationSummary,
	now metav1.Time,
) bool {
	if job.Status.Devices == nil {
		return false
	}

	changed := false
	ds := job.Status.Devices

	if ds.ClaimAllocationState != summary.State {
		ds.ClaimAllocationState = summary.State
		changed = true
	}

	if ds.AllocatedClaimCount != summary.AllocatedCount {
		ds.AllocatedClaimCount = summary.AllocatedCount
		changed = true
	}

	// Update failure fields.
	if summary.State == trainingv1alpha1.ClaimAllocationFailed {
		if ds.LastClaimFailureReason != summary.LastFailureReason {
			ds.LastClaimFailureReason = summary.LastFailureReason
			changed = true
		}
		if summary.LastFailureTime != nil {
			if ds.LastClaimFailureTime == nil || !ds.LastClaimFailureTime.Equal(summary.LastFailureTime) {
				ds.LastClaimFailureTime = summary.LastFailureTime.DeepCopy()
				changed = true
			}
		}
	}

	return changed
}

// conditionTypeDRAClaimFailure is the condition type set on the RTJ when
// DRA claim allocation fails.
const conditionTypeDRAClaimFailure = "DRAClaimAllocationFailed"

// syncDRAClaimConditions updates RTJ conditions based on the DRA claim
// allocation state. Returns true if any condition changed.
func syncDRAClaimConditions(
	job *trainingv1alpha1.ResumableTrainingJob,
	summary dra.ClaimAllocationSummary,
	now metav1.Time,
) bool {
	if summary.State == trainingv1alpha1.ClaimAllocationFailed {
		message := fmt.Sprintf(
			"DRA claim allocation failed: %d/%d claims failed. Last failure: %s",
			summary.FailedCount,
			summary.TotalClaims,
			summary.LastFailureReason,
		)
		return setCondition(job, conditionTypeDRAClaimFailure,
			metav1.ConditionTrue, "ClaimAllocationFailed", message, now)
	}

	return clearCondition(job, conditionTypeDRAClaimFailure)
}
