package elastic

import "fmt"

// EvaluatePlan is the core planning function. It takes a snapshot of the
// current state and returns a deterministic plan output. This function
// has no side effects and no dependencies on Kubernetes clients.
//
// Decision tree:
//
//	elasticity disabled? → NoResize
//	workload not admitted? → ResizeBlocked (no quota context)
//	preemption in progress? → ResizeBlocked (coalesce with preemption)
//	target == 0 or target == current? → NoResize
//	target out of bounds? → ResizeBlocked
//	reclaimablePods already published? → ReclaimPublished (wait for completion)
//	current resize in progress? → ResizeInProgress (wait)
//	DRA constraints block? → ResizeBlocked
//	target < current (shrink):
//	  in-place policy == Never? → ShrinkViaRelaunch
//	  runtime supports in-place? → ShrinkInPlace
//	  else → ShrinkViaRelaunch
//	target > current (grow) → GrowViaRelaunch
func EvaluatePlan(input PlanInput) PlanOutput {
	// Gate: elasticity must be enabled.
	if !input.ElasticityEnabled {
		return noResize(input.CurrentWorkerCount, "ElasticityDisabled",
			"elasticity is disabled; no resize evaluation")
	}

	// Gate: workload must be admitted for the planner to reason about quota.
	if !input.WorkloadAdmitted {
		if input.TargetWorkerCount != 0 && input.TargetWorkerCount != input.CurrentWorkerCount {
			return blocked(input.CurrentWorkerCount, "WorkloadNotAdmitted",
				"resize blocked: workload is not yet admitted by Kueue")
		}
		return noResize(input.CurrentWorkerCount, "WorkloadNotAdmitted",
			"no resize needed; workload is not yet admitted")
	}

	// Gate: do not start a new resize while Kueue preemption is in progress.
	// Per OQ-4, we coalesce: the preemption flow will tear down the run,
	// and any pending resize target will be picked up on re-admission.
	if input.PreemptionInProgress {
		return blocked(input.CurrentWorkerCount, "PreemptionInProgress",
			"resize blocked: Kueue preemption is in progress; resize will be evaluated after re-admission")
	}

	// No target set or target equals current: no-op.
	if input.TargetWorkerCount == 0 || input.TargetWorkerCount == input.CurrentWorkerCount {
		return noResize(input.CurrentWorkerCount, "TargetEqualsAdmitted",
			"target worker count matches current admitted count")
	}

	// Bounds check.
	if input.TargetWorkerCount < input.MinWorkerCount {
		return blocked(input.CurrentWorkerCount, "TargetBelowMinimum",
			fmt.Sprintf("target %d is below minimum %d", input.TargetWorkerCount, input.MinWorkerCount))
	}
	if input.MaxWorkerCount > 0 && input.TargetWorkerCount > input.MaxWorkerCount {
		return blocked(input.CurrentWorkerCount, "TargetAboveMaximum",
			fmt.Sprintf("target %d exceeds maximum %d", input.TargetWorkerCount, input.MaxWorkerCount))
	}

	// If reclaimablePods have already been published for this cycle,
	// we are waiting for pod termination and cleanup.
	if input.ReclaimablePodsPublished {
		return PlanOutput{
			Kind:                   PlanReclaimPublished,
			ReclaimableWorkerDelta: 0,
			CheckpointRequired:     false,
			RelaunchRequired:       false,
			NewWorkerCount:         input.TargetWorkerCount,
			Reason:                 "ReclaimablePodsPublished",
			Message:                "reclaimablePods written; waiting for surplus pod termination",
		}
	}

	// If a resize is already in progress, wait for it to complete.
	if input.CurrentResizeState == "InProgress" {
		return PlanOutput{
			Kind:                   PlanResizeInProgress,
			ReclaimableWorkerDelta: 0,
			CheckpointRequired:     false,
			RelaunchRequired:       false,
			NewWorkerCount:         input.TargetWorkerCount,
			Reason:                 "ResizeInProgress",
			Message:                "a resize operation is currently in progress",
		}
	}

	// DRA/topology constraints check.
	if input.DRAConstraintsBlock {
		return blocked(input.CurrentWorkerCount, "DRAConstraintsBlock",
			"resize blocked: DRA or topology constraints prevent the target configuration")
	}

	delta := input.TargetWorkerCount - input.CurrentWorkerCount

	// Shrink path.
	if delta < 0 {
		return evaluateShrink(input, -delta)
	}

	// Grow path: always requires checkpoint-and-relaunch.
	return PlanOutput{
		Kind:                   PlanGrowViaRelaunch,
		ReclaimableWorkerDelta: 0,
		CheckpointRequired:     true,
		RelaunchRequired:       true,
		NewWorkerCount:         input.TargetWorkerCount,
		Reason:                 "GrowRequiresRelaunch",
		Message: fmt.Sprintf("growing from %d to %d workers requires checkpoint-and-relaunch for new Kueue admission",
			input.CurrentWorkerCount, input.TargetWorkerCount),
	}
}

// evaluateShrink determines the shrink path based on policy and runtime capability.
func evaluateShrink(input PlanInput, reclaimCount int32) PlanOutput {
	// Policy: Never → always checkpoint-and-relaunch.
	if input.InPlaceShrinkPolicy == "Never" {
		return PlanOutput{
			Kind:                   PlanShrinkViaRelaunch,
			ReclaimableWorkerDelta: 0,
			CheckpointRequired:     true,
			RelaunchRequired:       true,
			NewWorkerCount:         input.TargetWorkerCount,
			Reason:                 "InPlaceShrinkPolicyNever",
			Message: fmt.Sprintf("shrinking from %d to %d workers via checkpoint-and-relaunch (policy=Never)",
				input.CurrentWorkerCount, input.TargetWorkerCount),
		}
	}

	// Policy: IfSupported (default) → check runtime capability.
	if input.RuntimeSupportsInPlaceShrink {
		return PlanOutput{
			Kind:                   PlanShrinkInPlace,
			ReclaimableWorkerDelta: reclaimCount,
			CheckpointRequired:     false,
			RelaunchRequired:       false,
			NewWorkerCount:         input.TargetWorkerCount,
			Reason:                 "InPlaceShrinkSupported",
			Message: fmt.Sprintf("shrinking from %d to %d workers in-place; %d pods reclaimable",
				input.CurrentWorkerCount, input.TargetWorkerCount, reclaimCount),
		}
	}

	// Fallback: runtime does not support in-place shrink.
	return PlanOutput{
		Kind:                   PlanShrinkViaRelaunch,
		ReclaimableWorkerDelta: 0,
		CheckpointRequired:     true,
		RelaunchRequired:       true,
		NewWorkerCount:         input.TargetWorkerCount,
		Reason:                 "InPlaceShrinkNotSupported",
		Message: fmt.Sprintf("shrinking from %d to %d workers via checkpoint-and-relaunch (runtime does not support in-place shrink)",
			input.CurrentWorkerCount, input.TargetWorkerCount),
	}
}

func noResize(workerCount int32, reason, message string) PlanOutput {
	return PlanOutput{
		Kind:                   PlanNoResize,
		ReclaimableWorkerDelta: 0,
		CheckpointRequired:     false,
		RelaunchRequired:       false,
		NewWorkerCount:         workerCount,
		Reason:                 reason,
		Message:                message,
	}
}

func blocked(workerCount int32, reason, message string) PlanOutput {
	return PlanOutput{
		Kind:                   PlanResizeBlocked,
		ReclaimableWorkerDelta: 0,
		CheckpointRequired:     false,
		RelaunchRequired:       false,
		NewWorkerCount:         workerCount,
		Reason:                 reason,
		Message:                message,
	}
}
