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
}

const (
	reasonWaitingForReadinessGate = "WaitingForReadinessGate"
	reasonReadinessGateRejected   = "ReadinessGateRejected"
	reasonWaitingForTopology      = "WaitingForTopologyAssignment"
	reasonTopologyNotRepresentable = "TopologyNotRepresentable"
)

// evaluateLaunchGates checks all pre-launch prerequisites:
//  1. RTJ is admitted (not suspended) — already checked by caller.
//  2. If a ResumeReadiness AdmissionCheck is configured, it must be Ready.
//  3. If topology is enabled, the topology assignment must be present on the Workload.
//
// When any gate is not satisfied, the result indicates not-ready with a reason.
// When all gates pass, Ready is true and TopologyResult is populated (if applicable).
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

	// Gate 1: Check ResumeReadiness AdmissionCheck (if configured).
	if workload != nil {
		gateState := evaluateReadinessCheck(workload)
		switch gateState {
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
		case trainingv1alpha1.ReadinessGateReady:
			// Pass through.
		default:
			// No readiness check configured — pass through (Phase 3 behavior).
		}
	}

	// Gate 2: Check topology assignment (if topology is enabled).
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
		// We need to determine if this check is managed by our controller.
		// The check name alone doesn't tell us; we need to match the state.
		// Since we can't look up the AdmissionCheck object here (we don't have
		// the client), we rely on a convention: any check that has been set to
		// Ready/Retry/Rejected by our controller will have a recognizable
		// reason string. However, the simplest approach is to check all
		// admission checks and look for the resume-readiness controller's
		// reason patterns.
		//
		// For a more precise check, we inspect the check name against known
		// reason patterns from the resume-readiness controller.
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
// resume-readiness controller by matching known message patterns. In a
// production system, this would look up the AdmissionCheck object to verify
// controllerName. For our controller, we check if the message contains
// known reason strings from the evaluator.
func isResumeReadinessCheckState(acs *kueuev1beta2.AdmissionCheckState) bool {
	// Check if the message contains any of our known reason strings.
	// This is a pragmatic approach that avoids an extra API call.
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

	// Also check if the state is Pending and we haven't touched it yet.
	// In that case, we can't tell if it's ours. We need to be conservative:
	// if ANY admission check is Pending and we don't know if it's ours,
	// we still should not block on it. So we return false for unknown checks.
	return false
}

// containsReason checks if a message string contains a specific reason identifier.
func containsReason(message, reason string) bool {
	// The evaluator embeds reason strings in messages. Match against
	// the message content as set by our evaluator/reconciler.
	return len(message) > 0 && len(reason) > 0 &&
		(message == reason || // exact match for simple messages
			len(message) > len(reason) && // prefix or contains check
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
