package elastic

import (
	"testing"
	"time"
)

var testNow = time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)

func basePlanInput() PlanInput {
	return PlanInput{
		ElasticityEnabled:            true,
		TargetWorkerCount:            4,
		CurrentWorkerCount:           8,
		ActiveWorkerCount:            8,
		MinWorkerCount:               1,
		MaxWorkerCount:               16,
		InPlaceShrinkPolicy:          "IfSupported",
		RuntimeSupportsInPlaceShrink: true,
		WorkloadAdmitted:             true,
		WorkloadExists:               true,
		CurrentResizeState:           "Idle",
		ReclaimablePodsPublished:     false,
		CheckpointReady:              true,
		PreemptionInProgress:         false,
		DRAConstraintsBlock:          false,
		Now:                          testNow,
	}
}

// --- NoResize tests ---

func TestEvaluatePlan_ElasticityDisabled(t *testing.T) {
	input := basePlanInput()
	input.ElasticityEnabled = false

	plan := EvaluatePlan(input)
	if plan.Kind != PlanNoResize {
		t.Errorf("expected NoResize, got %s", plan.Kind)
	}
	if plan.Reason != "ElasticityDisabled" {
		t.Errorf("expected reason ElasticityDisabled, got %s", plan.Reason)
	}
}

func TestEvaluatePlan_TargetEqualsCurrentIsNoOp(t *testing.T) {
	input := basePlanInput()
	input.TargetWorkerCount = 8
	input.CurrentWorkerCount = 8

	plan := EvaluatePlan(input)
	if plan.Kind != PlanNoResize {
		t.Errorf("expected NoResize, got %s", plan.Kind)
	}
	if plan.Reason != "TargetEqualsAdmitted" {
		t.Errorf("expected reason TargetEqualsAdmitted, got %s", plan.Reason)
	}
}

func TestEvaluatePlan_TargetZeroIsNoOp(t *testing.T) {
	input := basePlanInput()
	input.TargetWorkerCount = 0

	plan := EvaluatePlan(input)
	if plan.Kind != PlanNoResize {
		t.Errorf("expected NoResize, got %s", plan.Kind)
	}
}

// --- Shrink in-place tests ---

func TestEvaluatePlan_ShrinkInPlace(t *testing.T) {
	input := basePlanInput()
	input.TargetWorkerCount = 4
	input.CurrentWorkerCount = 8
	input.RuntimeSupportsInPlaceShrink = true
	input.InPlaceShrinkPolicy = "IfSupported"

	plan := EvaluatePlan(input)
	if plan.Kind != PlanShrinkInPlace {
		t.Errorf("expected ShrinkInPlace, got %s", plan.Kind)
	}
	if plan.ReclaimableWorkerDelta != 4 {
		t.Errorf("expected reclaimable delta 4, got %d", plan.ReclaimableWorkerDelta)
	}
	if plan.CheckpointRequired {
		t.Error("expected no checkpoint for in-place shrink")
	}
	if plan.RelaunchRequired {
		t.Error("expected no relaunch for in-place shrink")
	}
	if plan.NewWorkerCount != 4 {
		t.Errorf("expected new worker count 4, got %d", plan.NewWorkerCount)
	}
	if plan.Reason != "InPlaceShrinkSupported" {
		t.Errorf("expected reason InPlaceShrinkSupported, got %s", plan.Reason)
	}
}

func TestEvaluatePlan_ShrinkInPlaceDeltaOne(t *testing.T) {
	input := basePlanInput()
	input.TargetWorkerCount = 7
	input.CurrentWorkerCount = 8
	input.RuntimeSupportsInPlaceShrink = true

	plan := EvaluatePlan(input)
	if plan.Kind != PlanShrinkInPlace {
		t.Errorf("expected ShrinkInPlace, got %s", plan.Kind)
	}
	if plan.ReclaimableWorkerDelta != 1 {
		t.Errorf("expected reclaimable delta 1, got %d", plan.ReclaimableWorkerDelta)
	}
}

// --- Shrink via relaunch tests ---

func TestEvaluatePlan_ShrinkViaRelaunch_PolicyNever(t *testing.T) {
	input := basePlanInput()
	input.TargetWorkerCount = 4
	input.CurrentWorkerCount = 8
	input.InPlaceShrinkPolicy = "Never"
	input.RuntimeSupportsInPlaceShrink = true // Even if runtime supports it, policy says Never.

	plan := EvaluatePlan(input)
	if plan.Kind != PlanShrinkViaRelaunch {
		t.Errorf("expected ShrinkViaRelaunch, got %s", plan.Kind)
	}
	if !plan.CheckpointRequired {
		t.Error("expected checkpoint for relaunch shrink")
	}
	if !plan.RelaunchRequired {
		t.Error("expected relaunch for relaunch shrink")
	}
	if plan.ReclaimableWorkerDelta != 0 {
		t.Errorf("expected zero reclaim delta for relaunch, got %d", plan.ReclaimableWorkerDelta)
	}
	if plan.Reason != "InPlaceShrinkPolicyNever" {
		t.Errorf("expected reason InPlaceShrinkPolicyNever, got %s", plan.Reason)
	}
}

func TestEvaluatePlan_ShrinkViaRelaunch_RuntimeNotSupported(t *testing.T) {
	input := basePlanInput()
	input.TargetWorkerCount = 4
	input.CurrentWorkerCount = 8
	input.InPlaceShrinkPolicy = "IfSupported"
	input.RuntimeSupportsInPlaceShrink = false

	plan := EvaluatePlan(input)
	if plan.Kind != PlanShrinkViaRelaunch {
		t.Errorf("expected ShrinkViaRelaunch, got %s", plan.Kind)
	}
	if !plan.CheckpointRequired {
		t.Error("expected checkpoint for relaunch shrink")
	}
	if plan.Reason != "InPlaceShrinkNotSupported" {
		t.Errorf("expected reason InPlaceShrinkNotSupported, got %s", plan.Reason)
	}
}

// --- Grow tests ---

func TestEvaluatePlan_GrowViaRelaunch(t *testing.T) {
	input := basePlanInput()
	input.TargetWorkerCount = 12
	input.CurrentWorkerCount = 8

	plan := EvaluatePlan(input)
	if plan.Kind != PlanGrowViaRelaunch {
		t.Errorf("expected GrowViaRelaunch, got %s", plan.Kind)
	}
	if !plan.CheckpointRequired {
		t.Error("expected checkpoint for grow")
	}
	if !plan.RelaunchRequired {
		t.Error("expected relaunch for grow")
	}
	if plan.ReclaimableWorkerDelta != 0 {
		t.Errorf("expected zero reclaim delta for grow, got %d", plan.ReclaimableWorkerDelta)
	}
	if plan.NewWorkerCount != 12 {
		t.Errorf("expected new worker count 12, got %d", plan.NewWorkerCount)
	}
	if plan.Reason != "GrowRequiresRelaunch" {
		t.Errorf("expected reason GrowRequiresRelaunch, got %s", plan.Reason)
	}
}

func TestEvaluatePlan_GrowByOne(t *testing.T) {
	input := basePlanInput()
	input.TargetWorkerCount = 9
	input.CurrentWorkerCount = 8

	plan := EvaluatePlan(input)
	if plan.Kind != PlanGrowViaRelaunch {
		t.Errorf("expected GrowViaRelaunch, got %s", plan.Kind)
	}
	if plan.NewWorkerCount != 9 {
		t.Errorf("expected new worker count 9, got %d", plan.NewWorkerCount)
	}
}

// --- Blocked tests ---

func TestEvaluatePlan_BlockedWorkloadNotAdmitted(t *testing.T) {
	input := basePlanInput()
	input.WorkloadAdmitted = false
	input.TargetWorkerCount = 4
	input.CurrentWorkerCount = 8

	plan := EvaluatePlan(input)
	if plan.Kind != PlanResizeBlocked {
		t.Errorf("expected ResizeBlocked, got %s", plan.Kind)
	}
	if plan.Reason != "WorkloadNotAdmitted" {
		t.Errorf("expected reason WorkloadNotAdmitted, got %s", plan.Reason)
	}
}

func TestEvaluatePlan_WorkloadNotAdmitted_NoTargetDelta_IsNoOp(t *testing.T) {
	input := basePlanInput()
	input.WorkloadAdmitted = false
	input.TargetWorkerCount = 8
	input.CurrentWorkerCount = 8

	plan := EvaluatePlan(input)
	if plan.Kind != PlanNoResize {
		t.Errorf("expected NoResize when target==current and not admitted, got %s", plan.Kind)
	}
}

func TestEvaluatePlan_BlockedPreemptionInProgress(t *testing.T) {
	input := basePlanInput()
	input.PreemptionInProgress = true

	plan := EvaluatePlan(input)
	if plan.Kind != PlanResizeBlocked {
		t.Errorf("expected ResizeBlocked, got %s", plan.Kind)
	}
	if plan.Reason != "PreemptionInProgress" {
		t.Errorf("expected reason PreemptionInProgress, got %s", plan.Reason)
	}
}

func TestEvaluatePlan_BlockedTargetBelowMinimum(t *testing.T) {
	input := basePlanInput()
	input.TargetWorkerCount = 0
	input.MinWorkerCount = 1
	// Target 0 is caught as no-op before bounds check, so use a proper case.
	input.TargetWorkerCount = 1
	input.MinWorkerCount = 2

	plan := EvaluatePlan(input)
	if plan.Kind != PlanResizeBlocked {
		t.Errorf("expected ResizeBlocked, got %s", plan.Kind)
	}
	if plan.Reason != "TargetBelowMinimum" {
		t.Errorf("expected reason TargetBelowMinimum, got %s", plan.Reason)
	}
}

func TestEvaluatePlan_BlockedTargetAboveMaximum(t *testing.T) {
	input := basePlanInput()
	input.TargetWorkerCount = 20
	input.MaxWorkerCount = 16

	plan := EvaluatePlan(input)
	if plan.Kind != PlanResizeBlocked {
		t.Errorf("expected ResizeBlocked, got %s", plan.Kind)
	}
	if plan.Reason != "TargetAboveMaximum" {
		t.Errorf("expected reason TargetAboveMaximum, got %s", plan.Reason)
	}
}

func TestEvaluatePlan_BlockedDRAConstraints(t *testing.T) {
	input := basePlanInput()
	input.DRAConstraintsBlock = true

	plan := EvaluatePlan(input)
	if plan.Kind != PlanResizeBlocked {
		t.Errorf("expected ResizeBlocked, got %s", plan.Kind)
	}
	if plan.Reason != "DRAConstraintsBlock" {
		t.Errorf("expected reason DRAConstraintsBlock, got %s", plan.Reason)
	}
}

// --- In-progress / published tests ---

func TestEvaluatePlan_ResizeInProgress(t *testing.T) {
	input := basePlanInput()
	input.CurrentResizeState = "InProgress"

	plan := EvaluatePlan(input)
	if plan.Kind != PlanResizeInProgress {
		t.Errorf("expected ResizeInProgress, got %s", plan.Kind)
	}
}

func TestEvaluatePlan_ReclaimPublished(t *testing.T) {
	input := basePlanInput()
	input.ReclaimablePodsPublished = true

	plan := EvaluatePlan(input)
	if plan.Kind != PlanReclaimPublished {
		t.Errorf("expected ReclaimPublished, got %s", plan.Kind)
	}
	if plan.Reason != "ReclaimablePodsPublished" {
		t.Errorf("expected reason ReclaimablePodsPublished, got %s", plan.Reason)
	}
}

// --- Idempotency tests ---

func TestEvaluatePlan_IdempotentAcrossRepeatedCalls(t *testing.T) {
	input := basePlanInput()
	input.TargetWorkerCount = 4
	input.CurrentWorkerCount = 8
	input.RuntimeSupportsInPlaceShrink = true

	plan1 := EvaluatePlan(input)
	plan2 := EvaluatePlan(input)

	if plan1.Kind != plan2.Kind {
		t.Errorf("plans differ across calls: %s vs %s", plan1.Kind, plan2.Kind)
	}
	if plan1.ReclaimableWorkerDelta != plan2.ReclaimableWorkerDelta {
		t.Errorf("reclaim deltas differ: %d vs %d", plan1.ReclaimableWorkerDelta, plan2.ReclaimableWorkerDelta)
	}
	if plan1.Reason != plan2.Reason {
		t.Errorf("reasons differ: %s vs %s", plan1.Reason, plan2.Reason)
	}
}

func TestEvaluatePlan_IdempotentGrow(t *testing.T) {
	input := basePlanInput()
	input.TargetWorkerCount = 12
	input.CurrentWorkerCount = 8

	plan1 := EvaluatePlan(input)
	plan2 := EvaluatePlan(input)

	if plan1.Kind != plan2.Kind || plan1.Reason != plan2.Reason {
		t.Errorf("grow plans differ across calls")
	}
}

func TestEvaluatePlan_IdempotentNoOp(t *testing.T) {
	input := basePlanInput()
	input.TargetWorkerCount = 8
	input.CurrentWorkerCount = 8

	plan1 := EvaluatePlan(input)
	plan2 := EvaluatePlan(input)

	if plan1.Kind != PlanNoResize || plan2.Kind != PlanNoResize {
		t.Error("expected both calls to return NoResize")
	}
}

// --- Edge case: MaxWorkerCount zero means no upper bound ---

func TestEvaluatePlan_MaxZeroMeansUnbounded(t *testing.T) {
	input := basePlanInput()
	input.TargetWorkerCount = 100
	input.CurrentWorkerCount = 8
	input.MaxWorkerCount = 0

	plan := EvaluatePlan(input)
	if plan.Kind != PlanGrowViaRelaunch {
		t.Errorf("expected GrowViaRelaunch when max=0 (unbounded), got %s", plan.Kind)
	}
}

// --- PlanOutput.String test ---

func TestPlanOutput_String(t *testing.T) {
	plan := PlanOutput{
		Kind:                   PlanShrinkInPlace,
		ReclaimableWorkerDelta: 4,
		NewWorkerCount:         4,
		Reason:                 "InPlaceShrinkSupported",
	}
	s := plan.String()
	if s == "" {
		t.Error("expected non-empty string representation")
	}
}
