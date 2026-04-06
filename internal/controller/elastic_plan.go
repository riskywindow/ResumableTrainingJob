package controller

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/elastic"
)

// ElasticPlanResult captures the outcome of elastic plan evaluation
// for the main reconcile loop.
type ElasticPlanResult struct {
	// Plan is the computed elastic plan output.
	Plan elastic.PlanOutput

	// StatusChanged indicates whether any status.elasticity field was modified.
	StatusChanged bool
}

// buildElasticPlanInput constructs an elastic.PlanInput from the current RTJ
// state and observed Workload admission state. This is a pure extraction
// function with no side effects.
func buildElasticPlanInput(
	job *trainingv1alpha1.ResumableTrainingJob,
	workloadAdmitted bool,
	workloadExists bool,
	now metav1.Time,
) elastic.PlanInput {
	input := elastic.PlanInput{
		ElasticityEnabled: job.IsElasticityEnabled(),
		MinWorkerCount:    job.EffectiveElasticityMinCount(),
		MaxWorkerCount:    job.EffectivePreferredCount(),
		WorkloadAdmitted:  workloadAdmitted,
		WorkloadExists:    workloadExists,
		Now:               now.Time,
	}

	// Target from spec.elasticity.targetWorkerCount.
	if job.IsElasticityEnabled() && job.Spec.Elasticity.TargetWorkerCount != nil {
		input.TargetWorkerCount = *job.Spec.Elasticity.TargetWorkerCount
	}

	// Current admitted count from status.elasticity or fallback to preferred count.
	if job.Status.Elasticity != nil && job.Status.Elasticity.AdmittedWorkerCount > 0 {
		input.CurrentWorkerCount = job.Status.Elasticity.AdmittedWorkerCount
	} else {
		input.CurrentWorkerCount = job.EffectivePreferredCount()
	}

	// Active worker count from status.
	if job.Status.Elasticity != nil {
		input.ActiveWorkerCount = job.Status.Elasticity.ActiveWorkerCount
	}

	// In-place shrink policy.
	if job.Spec.Elasticity != nil {
		input.InPlaceShrinkPolicy = string(job.Spec.Elasticity.InPlaceShrinkPolicy)
	}

	// Runtime capability from status.
	if job.Status.Elasticity != nil {
		input.RuntimeSupportsInPlaceShrink = job.Status.Elasticity.InPlaceShrinkSupported
	}

	// Resize state.
	if job.Status.Elasticity != nil {
		input.CurrentResizeState = string(job.Status.Elasticity.ResizeState)
		input.ReclaimablePodsPublished = job.Status.Elasticity.ReclaimablePodsPublished
		input.LastResizeCheckpointExists = job.Status.Elasticity.LastResizeCheckpoint != nil
	}

	// Checkpoint readiness: check if there is a last completed checkpoint.
	input.CheckpointReady = job.Status.LastCompletedCheckpoint != nil

	// Preemption detection: RTJ is being preempted when suspend is set by Kueue
	// and we are in a draining or yield-requested phase.
	input.PreemptionInProgress = job.IsSuspendedForKueue() &&
		(job.Status.Phase == trainingv1alpha1.PhaseDraining ||
			job.Status.Phase == trainingv1alpha1.PhaseYieldRequested)

	// DRA constraints: not yet wired (Phase 8 forward); default to false.
	input.DRAConstraintsBlock = false

	return input
}

// evaluateElasticPlan runs the elastic planner and syncs the results to
// the RTJ's status.elasticity fields. Returns the plan and whether status
// was modified.
func evaluateElasticPlan(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	workloadAdmitted bool,
	workloadExists bool,
	now metav1.Time,
) ElasticPlanResult {
	logger := log.FromContext(ctx)

	input := buildElasticPlanInput(job, workloadAdmitted, workloadExists, now)
	plan := elastic.EvaluatePlan(input)

	logger.V(1).Info("elastic plan evaluated",
		"kind", plan.Kind,
		"reason", plan.Reason,
		"reclaimDelta", plan.ReclaimableWorkerDelta,
		"newWorkerCount", plan.NewWorkerCount,
	)

	changed := syncElasticityStatus(job, plan, input, now)

	return ElasticPlanResult{
		Plan:          plan,
		StatusChanged: changed,
	}
}

// syncElasticityStatus updates the RTJ's status.elasticity fields from
// the plan output and input snapshot. Returns true if any field changed.
func syncElasticityStatus(
	job *trainingv1alpha1.ResumableTrainingJob,
	plan elastic.PlanOutput,
	input elastic.PlanInput,
	now metav1.Time,
) bool {
	if job.Status.Elasticity == nil {
		job.Status.Elasticity = &trainingv1alpha1.ElasticityStatus{}
	}
	es := job.Status.Elasticity
	changed := false

	// Desired worker count (from spec upper bound).
	if es.DesiredWorkerCount != input.MaxWorkerCount {
		es.DesiredWorkerCount = input.MaxWorkerCount
		changed = true
	}

	// Target worker count (from spec.elasticity.targetWorkerCount).
	if es.TargetWorkerCount != input.TargetWorkerCount {
		es.TargetWorkerCount = input.TargetWorkerCount
		changed = true
	}

	// Admitted worker count.
	if es.AdmittedWorkerCount != input.CurrentWorkerCount {
		es.AdmittedWorkerCount = input.CurrentWorkerCount
		changed = true
	}

	// Active worker count (passthrough from input).
	if es.ActiveWorkerCount != input.ActiveWorkerCount {
		es.ActiveWorkerCount = input.ActiveWorkerCount
		changed = true
	}

	// Execution mode.
	execMode := trainingv1alpha1.ExecutionModeFixed
	if input.ElasticityEnabled {
		execMode = trainingv1alpha1.ExecutionModeElastic
	}
	if es.CurrentExecutionMode != execMode {
		es.CurrentExecutionMode = execMode
		changed = true
	}

	// In-place shrink supported.
	if es.InPlaceShrinkSupported != input.RuntimeSupportsInPlaceShrink {
		es.InPlaceShrinkSupported = input.RuntimeSupportsInPlaceShrink
		changed = true
	}

	// Resize state, path, reason from plan.
	newState := planKindToResizeState(plan.Kind)
	if es.ResizeState != newState {
		es.ResizeState = newState
		changed = true
		es.LastElasticTransitionTime = &now
	}

	newPath := planKindToResizePath(plan.Kind)
	if es.ResizePath != newPath {
		es.ResizePath = newPath
		changed = true
	}

	if es.ResizeReason != plan.Reason {
		es.ResizeReason = plan.Reason
		changed = true
	}

	// Reclaimable worker count.
	if es.ReclaimableWorkerCount != plan.ReclaimableWorkerDelta {
		es.ReclaimableWorkerCount = plan.ReclaimableWorkerDelta
		changed = true
	}

	// Last resize event (for observability).
	event := plan.Message
	if event != "" && es.LastResizeEvent != event {
		es.LastResizeEvent = event
		changed = true
	}

	return changed
}

// planKindToResizeState maps PlanKind to the API ResizeState.
func planKindToResizeState(kind elastic.PlanKind) trainingv1alpha1.ResizeState {
	switch kind {
	case elastic.PlanNoResize:
		return trainingv1alpha1.ResizeStateIdle
	case elastic.PlanShrinkInPlace:
		return trainingv1alpha1.ResizeStatePending
	case elastic.PlanShrinkViaRelaunch:
		return trainingv1alpha1.ResizeStatePending
	case elastic.PlanGrowViaRelaunch:
		return trainingv1alpha1.ResizeStatePending
	case elastic.PlanResizeBlocked:
		return trainingv1alpha1.ResizeStateBlocked
	case elastic.PlanResizeInProgress:
		return trainingv1alpha1.ResizeStateInProgress
	case elastic.PlanReclaimPublished:
		return trainingv1alpha1.ResizeStateInProgress
	default:
		return trainingv1alpha1.ResizeStateIdle
	}
}

// planKindToResizePath maps PlanKind to the API ResizePath.
func planKindToResizePath(kind elastic.PlanKind) trainingv1alpha1.ResizePath {
	switch kind {
	case elastic.PlanShrinkInPlace, elastic.PlanReclaimPublished:
		return trainingv1alpha1.ResizePathInPlace
	case elastic.PlanShrinkViaRelaunch, elastic.PlanGrowViaRelaunch:
		return trainingv1alpha1.ResizePathCheckpointAndRelaunch
	default:
		return ""
	}
}
