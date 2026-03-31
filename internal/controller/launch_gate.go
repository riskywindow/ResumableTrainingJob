package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	resumeac "github.com/example/checkpoint-native-preemption-controller/internal/admissionchecks/resume"
	"github.com/example/checkpoint-native-preemption-controller/internal/provisioning"
	"github.com/example/checkpoint-native-preemption-controller/internal/topology"
)

// LaunchGateResult captures the outcome of pre-launch gate evaluation.
type LaunchGateResult struct {
	// Ready is true when all gates have passed and the RTJ can launch.
	Ready bool

	// Reason is a machine-readable reason when not ready.
	Reason string

	// Message is a human-readable explanation when not ready.
	Message string

	// TopologyResult holds parsed topology assignments when topology is
	// enabled and the assignment is available. Nil when topology is disabled
	// or not yet assigned.
	TopologyResult *topology.ParseResult

	// Workload is the Kueue Workload object (when found). Retained so the
	// caller can extract admission info without a second fetch.
	Workload *kueuev1beta2.Workload

	// LaunchView is the Phase 7 provisioning/topology observation layer view.
	// Populated when a Workload is found. Nil when no Workload exists
	// (Phase 3/6 backward-compatible path).
	LaunchView *provisioning.LaunchReadinessView
}

const (
	reasonWaitingForReadinessGate  = "WaitingForReadinessGate"
	reasonReadinessGateRejected    = "ReadinessGateRejected"
	reasonWaitingForTopology       = "WaitingForTopologyAssignment"
	reasonTopologyNotRepresentable = "TopologyNotRepresentable"

	// Phase 7: provisioning-aware gate reasons.
	reasonCapacityPending                   = "CapacityPending"
	reasonProvisioningInProgress            = "ProvisioningInProgress"
	reasonProvisioningFailed                = "ProvisioningFailed"
	reasonTopologyPendingSecondPass         = "TopologyPendingSecondPass"
	reasonLaunchReady                       = "LaunchReady"
	reasonLaunchBlockedByConflictingUpdate  = "LaunchBlockedByConflictingPodSetUpdate"
	reasonAdmissionCheckPending             = "AdmissionCheckPending"
	reasonQuotaNotReserved                  = "QuotaNotReserved"
)

// evaluateLaunchGates checks all pre-launch prerequisites:
//  1. RTJ is admitted (not suspended) — already checked by caller.
//  2. Phase 7: All AdmissionChecks on the Workload must be Ready.
//  3. Phase 7: If provisioning is configured, the ProvisioningRequest AC must be Ready.
//  4. Phase 7: If topology is configured and delayedTopologyRequest is true,
//     the topology assignment must be present.
//  5. Phase 4: ResumeReadiness AdmissionCheck must be Ready (if configured).
//  6. Phase 4: Topology assignment must be present when topology is enabled.
//
// When any gate is not satisfied, the result indicates not-ready with a reason.
// When all gates pass, Ready is true and TopologyResult is populated (if applicable).
//
// Phase 6 backward compatibility: when no Workload exists or no AdmissionChecks
// are configured, the gates pass through (fail-open).
func (r *ResumableTrainingJobReconciler) evaluateLaunchGates(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
) (*LaunchGateResult, error) {
	logger := log.FromContext(ctx)

	// Find the Kueue Workload for this RTJ.
	workload, err := r.findWorkloadForRTJ(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("find workload: %w", err)
	}

	result := &LaunchGateResult{
		Workload: workload,
	}

	// Phase 7: Build the launch readiness view from the Workload.
	viewOpts := provisioning.ViewOptions{
		ProvisioningACNames: r.ProvisioningACNames,
		TopologyEnabled:     job.IsTopologyEnabled(),
	}
	if workload != nil {
		viewOpts.WorkloadName = workload.Name
		viewOpts.WorkloadNamespace = workload.Namespace
	}
	view := provisioning.BuildView(workload, viewOpts)
	result.LaunchView = view

	// Phase 7 Gate 1: Check quota reservation.
	if workload != nil && !view.QuotaReserved {
		result.Ready = false
		result.Reason = reasonQuotaNotReserved
		result.Message = "Workload exists but quota has not been reserved yet."
		logger.Info("launch gated: quota not reserved")
		return result, nil
	}

	// Phase 7 Gate 2: Check all AdmissionChecks are Ready.
	// This generalizes the Phase 4 ResumeReadiness check to cover ALL ACs
	// (ProvisioningRequest, ResumeReadiness, and any future ACs).
	if workload != nil && !view.AllChecksReady {
		// Determine the most specific reason based on provisioning state.
		switch view.Provisioning {
		case provisioning.ProvisioningPending, provisioning.ProvisioningRetry:
			result.Ready = false
			result.Reason = reasonProvisioningInProgress
			result.Message = "ProvisioningRequest AdmissionCheck is pending; waiting for backend to confirm capacity."
			logger.Info("launch gated: provisioning in progress",
				"provisioningState", view.Provisioning)
			return result, nil
		case provisioning.ProvisioningFailed:
			result.Ready = false
			result.Reason = reasonProvisioningFailed
			result.Message = "ProvisioningRequest AdmissionCheck was rejected by the provisioning backend."
			logger.Info("launch gated: provisioning failed")
			return result, nil
		default:
			// Check for ResumeReadiness-specific state (Phase 4 compat).
			readinessState := evaluateReadinessCheck(workload)
			switch readinessState {
			case trainingv1alpha1.ReadinessGatePending:
				result.Ready = false
				result.Reason = reasonWaitingForReadinessGate
				result.Message = "Waiting for ResumeReadiness AdmissionCheck to complete."
				logger.Info("launch gated: waiting for readiness check")
				return result, nil
			case trainingv1alpha1.ReadinessGateRejected:
				result.Ready = false
				result.Reason = reasonReadinessGateRejected
				result.Message = "ResumeReadiness AdmissionCheck was rejected."
				logger.Info("launch gated: readiness check rejected")
				return result, nil
			default:
				// Generic AC pending.
				result.Ready = false
				result.Reason = reasonAdmissionCheckPending
				result.Message = "One or more AdmissionChecks are not yet Ready."
				logger.Info("launch gated: admission check pending")
				return result, nil
			}
		}
	}

	// Phase 7 Gate 3: Check topology second-pass when topology is configured.
	// Even if all ACs are Ready, the topology assignment might not be present
	// yet (delayed topology second-pass scenario).
	if job.IsTopologyEnabled() && view.TopologyState.SecondPassPending {
		result.Ready = false
		result.Reason = reasonTopologyPendingSecondPass
		result.Message = "Topology is configured but the topology assignment is not yet available (second-pass pending)."
		logger.Info("launch gated: topology second pass pending")
		return result, nil
	}

	// Phase 4 Gate: Check topology assignment (if topology is enabled).
	// This handles the case where topology is enabled but the Workload
	// might not have the assignment yet (non-delayed scenario).
	if job.IsTopologyEnabled() {
		if workload == nil || workload.Status.Admission == nil {
			result.Ready = false
			result.Reason = reasonWaitingForTopology
			result.Message = "Topology is enabled but Workload admission data is not yet available."
			logger.Info("launch gated: waiting for workload admission")
			return result, nil
		}

		topoResult, err := topology.ParseFromAdmission(workload.Status.Admission)
		if err != nil {
			result.Ready = false
			result.Reason = reasonTopologyNotRepresentable
			result.Message = fmt.Sprintf("Failed to parse topology assignment: %v", err)
			return result, nil
		}

		if topoResult == nil {
			// Topology is enabled but no assignment yet — wait.
			result.Ready = false
			result.Reason = reasonWaitingForTopology
			result.Message = "Topology is enabled but topology assignment is not yet present on the Workload."
			logger.Info("launch gated: waiting for topology assignment")
			return result, nil
		}

		result.TopologyResult = topoResult
	}

	result.Ready = true
	result.Reason = reasonLaunchReady
	result.Message = "All launch gates satisfied."
	return result, nil
}

// evaluateReadinessCheck examines the Workload's AdmissionCheckStates for a
// ResumeReadiness check managed by this controller. Returns the gate state:
//   - ReadinessGateReady: the check passed
//   - ReadinessGatePending: the check is in progress
//   - ReadinessGateRejected: the check was rejected
//   - "" (empty): no readiness check is configured
func evaluateReadinessCheck(workload *kueuev1beta2.Workload) trainingv1alpha1.ReadinessGateState {
	if workload == nil {
		return ""
	}

	for _, acs := range workload.Status.AdmissionChecks {
		if isResumeReadinessCheckState(&acs) {
			switch acs.State {
			case kueuev1beta2.CheckStateReady:
				return trainingv1alpha1.ReadinessGateReady
			case kueuev1beta2.CheckStateRejected:
				return trainingv1alpha1.ReadinessGateRejected
			case kueuev1beta2.CheckStatePending, kueuev1beta2.CheckStateRetry:
				return trainingv1alpha1.ReadinessGatePending
			}
		}
	}

	// No resume-readiness check found — not configured, pass through.
	return ""
}

// isResumeReadinessCheckState checks if an AdmissionCheckState belongs to the
// resume-readiness controller by matching known message patterns.
func isResumeReadinessCheckState(acs *kueuev1beta2.AdmissionCheckState) bool {
	knownReasons := []string{
		resumeac.ReasonInitialLaunchReady,
		resumeac.ReasonCheckpointReady,
		resumeac.ReasonNoCheckpointAvailable,
		resumeac.ReasonCheckpointTooOld,
		resumeac.ReasonCheckpointIncomplete,
		resumeac.ReasonCheckpointIncompatible,
		resumeac.ReasonStorageUnavailable,
		resumeac.ReasonInitialLaunchBlocked,
		resumeac.ReasonPolicyResolutionFailed,
		resumeac.ReasonOwnerNotFound,
	}

	for _, reason := range knownReasons {
		if acs.Message != "" && containsReason(acs.Message, reason) {
			return true
		}
	}

	return false
}

// containsReason checks if a message string contains a specific reason identifier.
func containsReason(message, reason string) bool {
	return len(message) > 0 && len(reason) > 0 &&
		(message == reason ||
			len(message) > len(reason) &&
				(message[:len(reason)] == reason ||
					containsSubstring(message, reason)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// findWorkloadForRTJ locates the Kueue Workload owned by this RTJ.
// Returns nil (without error) if no Workload reference is available.
func (r *ResumableTrainingJobReconciler) findWorkloadForRTJ(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
) (*kueuev1beta2.Workload, error) {
	if job.Status.WorkloadReference == nil || job.Status.WorkloadReference.Name == "" {
		return nil, nil
	}

	workload := &kueuev1beta2.Workload{}
	key := types.NamespacedName{
		Name:      job.Status.WorkloadReference.Name,
		Namespace: job.Namespace,
	}
	if err := r.Get(ctx, key, workload); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	return workload, nil
}
