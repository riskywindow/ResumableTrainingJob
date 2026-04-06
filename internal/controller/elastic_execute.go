package controller

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/elastic"
)

// ElasticExecuteResult captures the outcome of elastic resize execution.
type ElasticExecuteResult struct {
	// Executed is true when a resize action was performed this reconcile.
	Executed bool

	// StatusChanged indicates whether any RTJ status field was modified.
	StatusChanged bool

	// NeedsRequeue indicates the caller should requeue to continue
	// multi-step resize execution (e.g., checkpoint-then-relaunch).
	NeedsRequeue bool

	// TriggerStopFlow is true when the execution requires tearing down
	// the current run for a checkpoint-and-relaunch resize. The main
	// reconciler should enter the stop flow with resize context.
	TriggerStopFlow bool

	// WorkloadPatched is true when a Workload reclaimablePods patch was applied.
	WorkloadPatched bool
}

// executeElasticPlan processes the elastic plan output and performs the
// narrowest controller mutation needed to execute the resize. This function
// is the single entry point for all resize execution during reconciliation.
//
// Execution paths:
//
//	NoResize / ResizeInProgress / ReclaimPublished:
//	  Sync conditions and return (no action needed).
//
//	ResizeBlocked:
//	  Set ResizeBlocked condition; clear active execution conditions.
//
//	ShrinkInPlace:
//	  1. Publish reclaimablePods to Workload.status via SSA patch.
//	  2. Mark ReclaimablePodsPublished on RTJ status.
//	  3. Set ShrinkingInPlace + ShrinkReclaimPublished conditions.
//	  4. Keep the workload admitted and the RTJ running.
//
//	ShrinkViaRelaunch / GrowViaRelaunch:
//	  1. Mark resize as in-progress with checkpoint required.
//	  2. Set ResizeCheckpointing condition.
//	  3. Signal TriggerStopFlow so the reconciler enters checkpoint-and-relaunch.
//	  4. Store the target worker count for the relaunch.
func (r *ResumableTrainingJobReconciler) executeElasticPlan(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	plan elastic.PlanOutput,
	now metav1.Time,
) (ElasticExecuteResult, error) {
	logger := log.FromContext(ctx)
	result := ElasticExecuteResult{}

	// Sync resize conditions for all plan kinds.
	condChanged := syncResizeConditions(job, string(plan.Kind), plan.Reason, plan.Message, now)
	result.StatusChanged = condChanged

	switch plan.Kind {
	case elastic.PlanNoResize:
		// Clear any stale resize state from previous cycles.
		result.StatusChanged = clearStaleResizeState(job) || result.StatusChanged
		return result, nil

	case elastic.PlanResizeBlocked:
		logger.Info("resize blocked", "reason", plan.Reason, "message", plan.Message)
		return result, nil

	case elastic.PlanResizeInProgress:
		logger.V(1).Info("resize in progress, waiting")
		return result, nil

	case elastic.PlanReclaimPublished:
		logger.V(1).Info("reclaimablePods published, waiting for pod termination")
		return result, nil

	case elastic.PlanShrinkInPlace:
		return r.executeShrinkInPlace(ctx, job, plan, now)

	case elastic.PlanShrinkViaRelaunch:
		return r.executeRelaunchResize(ctx, job, plan, now)

	case elastic.PlanGrowViaRelaunch:
		return r.executeRelaunchResize(ctx, job, plan, now)

	default:
		logger.Info("unknown elastic plan kind", "kind", plan.Kind)
		return result, nil
	}
}

// executeShrinkInPlace handles the in-place shrink path:
//  1. Compute the reclaimablePods delta from the plan.
//  2. Build the Kueue ReclaimablePod slice.
//  3. Patch the Workload status with reclaimablePods via SSA.
//  4. Update RTJ elasticity status to mark reclaim as published.
//  5. Set appropriate conditions.
func (r *ResumableTrainingJobReconciler) executeShrinkInPlace(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	plan elastic.PlanOutput,
	now metav1.Time,
) (ElasticExecuteResult, error) {
	logger := log.FromContext(ctx)
	result := ElasticExecuteResult{Executed: true}

	workerPodSetName := resolveWorkerPodSetNameForJob(job)
	reclaimDelta := elastic.ComputeReclaimDelta(plan, workerPodSetName)

	if !reclaimDelta.IsReclaim() {
		logger.Info("shrink-in-place plan has zero reclaim delta, skipping")
		return result, nil
	}

	// Build the reclaimablePods patch payload.
	reclaimPods := elastic.BuildReclaimablePods(reclaimDelta)

	// Apply the SSA patch to the Workload status.
	patchResult, err := r.patchWorkloadReclaimablePods(ctx, job, reclaimPods)
	if err != nil {
		// Record failure but don't fail the reconcile — the next cycle will retry.
		failMsg := fmt.Sprintf("failed to patch reclaimablePods: %v", err)
		logger.Error(err, "shrink-in-place reclaimablePods patch failed")
		result.StatusChanged = setResizeFailedCondition(job, failMsg, now)
		return result, err
	}

	result.WorkloadPatched = patchResult.Patched

	// Update RTJ elasticity status.
	if job.Status.Elasticity == nil {
		job.Status.Elasticity = &trainingv1alpha1.ElasticityStatus{}
	}

	// Mark reclaimablePods as published — this prevents duplicate writes
	// on the next reconcile (the planner will return ReclaimPublished).
	if !job.Status.Elasticity.ReclaimablePodsPublished {
		job.Status.Elasticity.ReclaimablePodsPublished = true
		result.StatusChanged = true
	}

	// Update the resize state to InProgress.
	if job.Status.Elasticity.ResizeState != trainingv1alpha1.ResizeStateInProgress {
		job.Status.Elasticity.ResizeState = trainingv1alpha1.ResizeStateInProgress
		job.Status.Elasticity.LastElasticTransitionTime = &now
		result.StatusChanged = true
	}

	// Set execution conditions.
	changed := setShrinkingInPlaceCondition(job,
		fmt.Sprintf("in-place shrink: %d workers reclaimable on workload %s",
			reclaimDelta.Count, patchResult.WorkloadName), now)
	changed = setShrinkReclaimPublishedCondition(job,
		fmt.Sprintf("reclaimablePods published: %d pods on PodSet %s",
			reclaimDelta.Count, workerPodSetName), now) || changed
	changed = clearResizePendingCondition(job) || changed
	result.StatusChanged = changed || result.StatusChanged

	logger.Info("shrink-in-place executed",
		"reclaimCount", reclaimDelta.Count,
		"podSetName", workerPodSetName,
		"workload", patchResult.WorkloadName,
		"patched", patchResult.Patched,
	)

	return result, nil
}

// executeRelaunchResize handles the checkpoint-and-relaunch path for both
// shrink-via-relaunch and grow-via-relaunch plans.
//
// This does NOT directly perform the checkpoint or relaunch. Instead it:
//  1. Marks the resize as in-progress with checkpoint required.
//  2. Stores the target worker count for the post-checkpoint relaunch.
//  3. Signals TriggerStopFlow so the main reconciler enters the drain flow.
//  4. On the next reconcile (after drain completes), the RTJ will re-enter
//     the queued/launch path with the new target worker count applied.
func (r *ResumableTrainingJobReconciler) executeRelaunchResize(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	plan elastic.PlanOutput,
	now metav1.Time,
) (ElasticExecuteResult, error) {
	logger := log.FromContext(ctx)
	result := ElasticExecuteResult{
		Executed:        true,
		TriggerStopFlow: true,
		NeedsRequeue:    true,
	}

	if job.Status.Elasticity == nil {
		job.Status.Elasticity = &trainingv1alpha1.ElasticityStatus{}
	}
	es := job.Status.Elasticity

	// Mark resize as in-progress.
	if es.ResizeState != trainingv1alpha1.ResizeStateInProgress {
		es.ResizeState = trainingv1alpha1.ResizeStateInProgress
		es.LastElasticTransitionTime = &now
		result.StatusChanged = true
	}

	// Store the target for post-relaunch.
	if es.TargetWorkerCount != plan.NewWorkerCount {
		es.TargetWorkerCount = plan.NewWorkerCount
		result.StatusChanged = true
	}

	// Clear reclaimablePods published flag (fresh cycle).
	if es.ReclaimablePodsPublished {
		es.ReclaimablePodsPublished = false
		result.StatusChanged = true
	}

	// Set execution conditions.
	direction := "shrink"
	if plan.Kind == elastic.PlanGrowViaRelaunch {
		direction = "grow"
	}
	msg := fmt.Sprintf("checkpoint-and-relaunch for %s: %d -> %d workers",
		direction, job.Status.Elasticity.AdmittedWorkerCount, plan.NewWorkerCount)

	changed := setResizeCheckpointingCondition(job, msg, now)
	changed = clearResizePendingCondition(job) || changed
	changed = clearShrinkingInPlaceCondition(job) || changed
	changed = clearShrinkReclaimPublishedCondition(job) || changed
	result.StatusChanged = changed || result.StatusChanged

	// Clear existing reclaimablePods on the Workload if any were published
	// from a previous in-place cycle. This ensures a clean state before
	// entering the drain flow.
	if job.Status.WorkloadReference != nil {
		clearResult, err := r.clearWorkloadReclaimablePods(ctx, job)
		if err != nil {
			logger.Error(err, "failed to clear reclaimablePods before relaunch")
			// Non-fatal: the drain flow will proceed regardless.
		} else if clearResult.Patched {
			result.WorkloadPatched = true
		}
	}

	logger.Info("relaunch resize initiated",
		"direction", direction,
		"currentWorkers", es.AdmittedWorkerCount,
		"targetWorkers", plan.NewWorkerCount,
		"checkpointRequired", plan.CheckpointRequired,
	)

	return result, nil
}

// clearStaleResizeState removes leftover resize state when no resize is needed.
// Called when the plan evaluates to NoResize to ensure a clean state.
func clearStaleResizeState(job *trainingv1alpha1.ResumableTrainingJob) bool {
	if job.Status.Elasticity == nil {
		return false
	}
	es := job.Status.Elasticity
	changed := false

	if es.ResizeState != trainingv1alpha1.ResizeStateIdle && es.ResizeState != "" {
		// If we were in-progress and now target == current, resize completed.
		if es.ResizeState == trainingv1alpha1.ResizeStateInProgress {
			es.ResizeState = trainingv1alpha1.ResizeStateCompleted
		} else {
			es.ResizeState = trainingv1alpha1.ResizeStateIdle
		}
		changed = true
	}

	if es.ReclaimablePodsPublished {
		es.ReclaimablePodsPublished = false
		changed = true
	}

	return changed
}

// isResizeTriggeredStop returns true when the RTJ is being stopped as part
// of a resize checkpoint-and-relaunch flow (not a user pause or Kueue preemption).
func isResizeTriggeredStop(job *trainingv1alpha1.ResumableTrainingJob) bool {
	if job.Status.Elasticity == nil {
		return false
	}
	return job.Status.Elasticity.ResizeState == trainingv1alpha1.ResizeStateInProgress &&
		job.Status.Elasticity.ResizePath == trainingv1alpha1.ResizePathCheckpointAndRelaunch
}

// completeResizeAfterRelaunch is called after a successful relaunch at the
// new worker count. It clears the resize in-progress state and sets the
// appropriate completion markers.
func completeResizeAfterRelaunch(
	job *trainingv1alpha1.ResumableTrainingJob,
	now metav1.Time,
) bool {
	if job.Status.Elasticity == nil {
		return false
	}
	es := job.Status.Elasticity
	changed := false

	if es.ResizeState == trainingv1alpha1.ResizeStateInProgress {
		es.ResizeState = trainingv1alpha1.ResizeStateCompleted
		es.LastElasticTransitionTime = &now
		es.LastResizeCompletedTime = &now
		changed = true
	}

	// Clear the in-progress path marker.
	if es.ResizePath != "" {
		es.ResizePath = ""
		changed = true
	}

	// Clear execution conditions.
	changed = clearResizeCheckpointingCondition(job) || changed
	changed = clearRelaunchingForResizeCondition(job) || changed
	changed = clearResizePendingCondition(job) || changed

	return changed
}

// markResizeRelaunchingCondition transitions from checkpointing to relaunching.
// Called when the drain completes and the RTJ is about to be relaunched.
func markResizeRelaunchingCondition(
	job *trainingv1alpha1.ResumableTrainingJob,
	targetWorkerCount int32,
	now metav1.Time,
) bool {
	msg := fmt.Sprintf("relaunching at %d workers after resize checkpoint", targetWorkerCount)
	changed := setRelaunchingForResizeCondition(job, msg, now)
	changed = clearResizeCheckpointingCondition(job) || changed
	return changed
}
