package dra

import (
	"fmt"
	"sort"

	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// ClaimAllocationSummary summarizes the allocation state of all
// ResourceClaims observed for an RTJ. It is computed from the live
// ResourceClaim objects in the namespace and used to update
// status.devices allocation fields.
type ClaimAllocationSummary struct {
	// State is the aggregate allocation state.
	State trainingv1alpha1.ClaimAllocationState

	// TotalClaims is the total number of ResourceClaims observed.
	TotalClaims int32

	// AllocatedCount is the number of claims with a non-empty
	// allocation result.
	AllocatedCount int32

	// PendingCount is the number of claims without an allocation
	// result.
	PendingCount int32

	// FailedCount is the number of claims with a recognized
	// allocation failure condition (per-device conditions with
	// failure indicators).
	FailedCount int32

	// LastFailureReason is the most recent failure reason string
	// from a failed claim's per-device status conditions.
	LastFailureReason string

	// LastFailureTime is the timestamp of the most recent failure.
	LastFailureTime *metav1.Time
}

// SummarizeClaimAllocations computes the aggregate allocation state
// from a list of ResourceClaim objects. The claims should be filtered
// to those owned by / associated with a single RTJ before calling.
//
// Allocation detection:
//   - A claim is "allocated" when claim.Status.Allocation is non-nil.
//     An allocated claim may still have per-device failure conditions
//     in claim.Status.Devices[].Conditions; these are tracked separately.
//   - Otherwise the claim is "pending" (waiting for allocation).
//
// Failure detection:
//   - Allocated claims with per-device conditions indicating failure
//     (Ready=False, or failure-indicating reasons) are counted as failed.
//
// Aggregate state:
//   - If no claims exist: Unknown
//   - If any claim has a device-level failure: Failed
//   - If all claims are allocated (without failures): Allocated
//   - Otherwise: Pending
func SummarizeClaimAllocations(claims []resourcev1beta1.ResourceClaim) ClaimAllocationSummary {
	if len(claims) == 0 {
		return ClaimAllocationSummary{
			State: trainingv1alpha1.ClaimAllocationUnknown,
		}
	}

	summary := ClaimAllocationSummary{
		TotalClaims: int32(len(claims)),
	}

	for i := range claims {
		claim := &claims[i]

		if isClaimAllocated(claim) {
			// Check for per-device failure conditions in allocated claims.
			if reason, failTime, failed := checkDeviceFailures(claim); failed {
				summary.FailedCount++
				if summary.LastFailureTime == nil || (failTime != nil && failTime.After(summary.LastFailureTime.Time)) {
					summary.LastFailureReason = reason
					summary.LastFailureTime = failTime
				}
			} else {
				summary.AllocatedCount++
			}
			continue
		}

		summary.PendingCount++
	}

	switch {
	case summary.FailedCount > 0:
		summary.State = trainingv1alpha1.ClaimAllocationFailed
	case summary.AllocatedCount == summary.TotalClaims:
		summary.State = trainingv1alpha1.ClaimAllocationAllocated
	default:
		summary.State = trainingv1alpha1.ClaimAllocationPending
	}

	return summary
}

// isClaimAllocated returns true when the ResourceClaim has a non-nil
// allocation result, indicating the DRA scheduler has processed the claim.
func isClaimAllocated(claim *resourcev1beta1.ResourceClaim) bool {
	return claim.Status.Allocation != nil
}

// checkDeviceFailures inspects per-device conditions in an allocated
// claim for failure signals. In DRA v1beta1, per-device status conditions
// (claim.Status.Devices[].Conditions) are set by DRA drivers to report
// device health. A "Ready" condition with status False indicates the
// device is not usable.
//
// Returns the failure reason, time, and whether a failure was detected.
func checkDeviceFailures(claim *resourcev1beta1.ResourceClaim) (string, *metav1.Time, bool) {
	for _, device := range claim.Status.Devices {
		for _, cond := range device.Conditions {
			if isDeviceFailureCondition(cond) {
				t := cond.LastTransitionTime.DeepCopy()
				reason := fmt.Sprintf("device %s/%s/%s: %s",
					device.Driver, device.Pool, device.Device,
					conditionFailureReason(cond))
				return reason, t, true
			}
		}
	}
	return "", nil, false
}

// isDeviceFailureCondition returns true when a per-device condition
// represents a failure.
func isDeviceFailureCondition(cond metav1.Condition) bool {
	// Ready=False means the device is not usable.
	if cond.Type == "Ready" && cond.Status == metav1.ConditionFalse {
		return true
	}
	// Explicit failure conditions.
	switch cond.Type {
	case "AllocationFailed", "Failed":
		return cond.Status == metav1.ConditionTrue
	}
	// Failure-indicating reasons regardless of condition type.
	switch cond.Reason {
	case "AllocationFailed", "DeviceAllocationFailed", "Unschedulable",
		"UnsatisfiedConstraints", "DriverError":
		return true
	}
	return false
}

// conditionFailureReason extracts a human-readable failure reason
// from a condition.
func conditionFailureReason(cond metav1.Condition) string {
	if cond.Message != "" {
		return fmt.Sprintf("%s: %s", cond.Reason, cond.Message)
	}
	if cond.Reason != "" {
		return cond.Reason
	}
	return cond.Type
}

// FilterClaimsForRTJ filters a list of ResourceClaims to only those
// that were generated from an RTJ's ResourceClaimTemplates. It matches
// by the RTJ-name label or by the claim-template-name annotation.
func FilterClaimsForRTJ(
	claims []resourcev1beta1.ResourceClaim,
	rtjName string,
	rtjUID string,
	templateNames map[string]bool,
) []resourcev1beta1.ResourceClaim {
	if len(claims) == 0 {
		return nil
	}

	var filtered []resourcev1beta1.ResourceClaim
	for i := range claims {
		claim := &claims[i]
		if matchesRTJ(claim, rtjName, rtjUID, templateNames) {
			filtered = append(filtered, *claim)
		}
	}

	// Sort by name for deterministic ordering.
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Name < filtered[j].Name
	})

	return filtered
}

// matchesRTJ returns true when a ResourceClaim is associated with the
// given RTJ. Matching is done by:
//  1. Checking the RTJ-name label (set on the ResourceClaimTemplate;
//     claims generated from templates inherit template labels).
//  2. Checking if the claim was created from a known template via
//     the claim-template-name annotation.
func matchesRTJ(
	claim *resourcev1beta1.ResourceClaim,
	rtjName string,
	rtjUID string,
	templateNames map[string]bool,
) bool {
	// Check labels set by the RTJ operator on ResourceClaimTemplates.
	if claim.Labels != nil {
		if claim.Labels["training.checkpoint.example.io/rtj-name"] == rtjName {
			return true
		}
	}

	// Check if the claim was created from a known template by inspecting
	// the annotation that Kubernetes sets when creating claims from templates.
	if claim.Annotations != nil {
		if templateName, ok := claim.Annotations["resource.kubernetes.io/claim-template-name"]; ok {
			if templateNames[templateName] {
				return true
			}
		}
	}

	return false
}
