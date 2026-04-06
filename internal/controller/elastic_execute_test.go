package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/utils/ptr"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/elastic"
)

// --- syncResizeConditions tests ---

func TestSyncResizeConditions_NoResize_ClearsAllConditions(t *testing.T) {
	job := elasticTestRTJ()
	// Pre-set some conditions that should be cleared.
	setResizePendingCondition(job, "test", "msg", elasticTestNow)
	setShrinkingInPlaceCondition(job, "msg", elasticTestNow)

	changed := syncResizeConditions(job, "NoResize", "", "", elasticTestNow)

	if !changed {
		t.Error("expected change when clearing conditions")
	}
	if meta.FindStatusCondition(job.Status.Conditions, conditionTypeResizePending) != nil {
		t.Error("expected ResizePending condition to be cleared")
	}
	if meta.FindStatusCondition(job.Status.Conditions, conditionTypeShrinkingInPlace) != nil {
		t.Error("expected ShrinkingInPlace condition to be cleared")
	}
}

func TestSyncResizeConditions_ShrinkInPlace_SetsResizePending(t *testing.T) {
	job := elasticTestRTJ()

	changed := syncResizeConditions(job, "ShrinkInPlace", "InPlaceShrinkSupported",
		"shrinking from 8 to 4 workers in-place", elasticTestNow)

	if !changed {
		t.Error("expected change")
	}
	cond := meta.FindStatusCondition(job.Status.Conditions, conditionTypeResizePending)
	if cond == nil {
		t.Fatal("expected ResizePending condition")
	}
	if cond.Reason != reasonResizePendingShrinkInPlace {
		t.Errorf("expected reason=%s, got %s", reasonResizePendingShrinkInPlace, cond.Reason)
	}
}

func TestSyncResizeConditions_GrowViaRelaunch_SetsResizePending(t *testing.T) {
	job := elasticTestRTJ()

	syncResizeConditions(job, "GrowViaRelaunch", "GrowRequiresRelaunch",
		"growing from 4 to 8 workers", elasticTestNow)

	cond := meta.FindStatusCondition(job.Status.Conditions, conditionTypeResizePending)
	if cond == nil {
		t.Fatal("expected ResizePending condition")
	}
	if cond.Reason != reasonResizePendingGrowRelaunch {
		t.Errorf("expected reason=%s, got %s", reasonResizePendingGrowRelaunch, cond.Reason)
	}
}

func TestSyncResizeConditions_ResizeBlocked_SetsBlockCondition(t *testing.T) {
	job := elasticTestRTJ()

	syncResizeConditions(job, "ResizeBlocked", "PreemptionInProgress",
		"resize blocked: preemption in progress", elasticTestNow)

	cond := meta.FindStatusCondition(job.Status.Conditions, conditionTypeResizeBlocked)
	if cond == nil {
		t.Fatal("expected ResizeBlocked condition")
	}
	if cond.Reason != reasonResizeBlockedByPreemption {
		t.Errorf("expected reason=%s, got %s", reasonResizeBlockedByPreemption, cond.Reason)
	}

	// Ensure no conflicting conditions.
	if meta.FindStatusCondition(job.Status.Conditions, conditionTypeResizePending) != nil {
		t.Error("ResizePending should be cleared when blocked")
	}
}

func TestSyncResizeConditions_ResizeBlocked_BoundsReason(t *testing.T) {
	job := elasticTestRTJ()

	syncResizeConditions(job, "ResizeBlocked", "TargetBelowMinimum",
		"target 0 is below minimum 1", elasticTestNow)

	cond := meta.FindStatusCondition(job.Status.Conditions, conditionTypeResizeBlocked)
	if cond == nil {
		t.Fatal("expected ResizeBlocked condition")
	}
	if cond.Reason != reasonResizeBlockedByBounds {
		t.Errorf("expected reason=%s, got %s", reasonResizeBlockedByBounds, cond.Reason)
	}
}

func TestSyncResizeConditions_ResizeBlocked_DRAReason(t *testing.T) {
	job := elasticTestRTJ()

	syncResizeConditions(job, "ResizeBlocked", "DRAConstraintsBlock",
		"DRA constraints prevent resize", elasticTestNow)

	cond := meta.FindStatusCondition(job.Status.Conditions, conditionTypeResizeBlocked)
	if cond == nil {
		t.Fatal("expected ResizeBlocked condition")
	}
	if cond.Reason != reasonResizeBlockedByDRA {
		t.Errorf("expected reason=%s, got %s", reasonResizeBlockedByDRA, cond.Reason)
	}
}

func TestSyncResizeConditions_ReclaimPublished_SetsCondition(t *testing.T) {
	job := elasticTestRTJ()

	syncResizeConditions(job, "ReclaimPublished", "ReclaimablePodsPublished",
		"reclaimablePods written", elasticTestNow)

	cond := meta.FindStatusCondition(job.Status.Conditions, conditionTypeShrinkReclaimPublished)
	if cond == nil {
		t.Fatal("expected ShrinkReclaimPublished condition")
	}
}

func TestSyncResizeConditions_ResizeInProgress_ClearsConflicts(t *testing.T) {
	job := elasticTestRTJ()
	// Pre-set pending and blocked conditions.
	setResizePendingCondition(job, "test", "msg", elasticTestNow)
	setResizeBlockedCondition(job, "test", "msg", elasticTestNow)

	changed := syncResizeConditions(job, "ResizeInProgress", "", "", elasticTestNow)

	if !changed {
		t.Error("expected change")
	}
	if meta.FindStatusCondition(job.Status.Conditions, conditionTypeResizePending) != nil {
		t.Error("ResizePending should be cleared during in-progress")
	}
	if meta.FindStatusCondition(job.Status.Conditions, conditionTypeResizeBlocked) != nil {
		t.Error("ResizeBlocked should be cleared during in-progress")
	}
}

// --- clearStaleResizeState tests ---

func TestClearStaleResizeState_ClearsInProgressToCompleted(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity.ResizeState = trainingv1alpha1.ResizeStateInProgress
	job.Status.Elasticity.ReclaimablePodsPublished = true

	changed := clearStaleResizeState(job)

	if !changed {
		t.Error("expected change")
	}
	if job.Status.Elasticity.ResizeState != trainingv1alpha1.ResizeStateCompleted {
		t.Errorf("expected Completed, got %s", job.Status.Elasticity.ResizeState)
	}
	if job.Status.Elasticity.ReclaimablePodsPublished {
		t.Error("expected ReclaimablePodsPublished to be cleared")
	}
}

func TestClearStaleResizeState_ClearsPendingToIdle(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity.ResizeState = trainingv1alpha1.ResizeStatePending

	changed := clearStaleResizeState(job)

	if !changed {
		t.Error("expected change")
	}
	if job.Status.Elasticity.ResizeState != trainingv1alpha1.ResizeStateIdle {
		t.Errorf("expected Idle, got %s", job.Status.Elasticity.ResizeState)
	}
}

func TestClearStaleResizeState_NilElasticity(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity = nil

	changed := clearStaleResizeState(job)

	if changed {
		t.Error("expected no change for nil elasticity")
	}
}

// --- isResizeTriggeredStop tests ---

func TestIsResizeTriggeredStop_TrueWhenInProgressRelaunch(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity.ResizeState = trainingv1alpha1.ResizeStateInProgress
	job.Status.Elasticity.ResizePath = trainingv1alpha1.ResizePathCheckpointAndRelaunch

	if !isResizeTriggeredStop(job) {
		t.Error("expected isResizeTriggeredStop=true")
	}
}

func TestIsResizeTriggeredStop_FalseWhenInPlace(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity.ResizeState = trainingv1alpha1.ResizeStateInProgress
	job.Status.Elasticity.ResizePath = trainingv1alpha1.ResizePathInPlace

	if isResizeTriggeredStop(job) {
		t.Error("expected isResizeTriggeredStop=false for in-place")
	}
}

func TestIsResizeTriggeredStop_FalseWhenIdle(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity.ResizeState = trainingv1alpha1.ResizeStateIdle

	if isResizeTriggeredStop(job) {
		t.Error("expected isResizeTriggeredStop=false when idle")
	}
}

func TestIsResizeTriggeredStop_NilElasticity(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity = nil

	if isResizeTriggeredStop(job) {
		t.Error("expected isResizeTriggeredStop=false for nil elasticity")
	}
}

// --- completeResizeAfterRelaunch tests ---

func TestCompleteResizeAfterRelaunch_CompletesInProgress(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity.ResizeState = trainingv1alpha1.ResizeStateInProgress
	job.Status.Elasticity.ResizePath = trainingv1alpha1.ResizePathCheckpointAndRelaunch
	// Set some conditions that should be cleared.
	setResizeCheckpointingCondition(job, "checkpointing", elasticTestNow)
	setRelaunchingForResizeCondition(job, "relaunching", elasticTestNow)

	changed := completeResizeAfterRelaunch(job, elasticTestNow)

	if !changed {
		t.Error("expected change")
	}
	if job.Status.Elasticity.ResizeState != trainingv1alpha1.ResizeStateCompleted {
		t.Errorf("expected Completed, got %s", job.Status.Elasticity.ResizeState)
	}
	if job.Status.Elasticity.ResizePath != "" {
		t.Errorf("expected empty path, got %s", job.Status.Elasticity.ResizePath)
	}
	if job.Status.Elasticity.LastResizeCompletedTime == nil {
		t.Error("expected LastResizeCompletedTime to be set")
	}
	// Conditions should be cleared.
	if meta.FindStatusCondition(job.Status.Conditions, conditionTypeResizeCheckpointing) != nil {
		t.Error("expected ResizeCheckpointing to be cleared")
	}
	if meta.FindStatusCondition(job.Status.Conditions, conditionTypeRelaunchingForResize) != nil {
		t.Error("expected RelaunchingForResize to be cleared")
	}
}

func TestCompleteResizeAfterRelaunch_NoOpWhenNotInProgress(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity.ResizeState = trainingv1alpha1.ResizeStateIdle

	changed := completeResizeAfterRelaunch(job, elasticTestNow)

	if changed {
		t.Error("expected no change when not in progress")
	}
}

func TestCompleteResizeAfterRelaunch_NilElasticity(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity = nil

	changed := completeResizeAfterRelaunch(job, elasticTestNow)

	if changed {
		t.Error("expected no change for nil elasticity")
	}
}

// --- markResizeRelaunchingCondition tests ---

func TestMarkResizeRelaunchingCondition_SetsCondition(t *testing.T) {
	job := elasticTestRTJ()
	// Pre-set checkpointing condition.
	setResizeCheckpointingCondition(job, "checkpointing", elasticTestNow)

	changed := markResizeRelaunchingCondition(job, 4, elasticTestNow)

	if !changed {
		t.Error("expected change")
	}
	cond := meta.FindStatusCondition(job.Status.Conditions, conditionTypeRelaunchingForResize)
	if cond == nil {
		t.Fatal("expected RelaunchingForResize condition")
	}
	// Checkpointing should be cleared.
	if meta.FindStatusCondition(job.Status.Conditions, conditionTypeResizeCheckpointing) != nil {
		t.Error("expected ResizeCheckpointing to be cleared")
	}
}

// --- executeElasticPlan tests (unit tests for the plan dispatch logic) ---
// These test the dispatch/condition-setting logic without a real client.

func TestExecuteElasticPlan_NoResize_ClearsState(t *testing.T) {
	r := &ResumableTrainingJobReconciler{}
	job := elasticTestRTJ()
	job.Status.Elasticity.ResizeState = trainingv1alpha1.ResizeStatePending

	plan := elastic.PlanOutput{
		Kind:           elastic.PlanNoResize,
		Reason:         "TargetEqualsAdmitted",
		NewWorkerCount: 8,
	}

	result, err := r.executeElasticPlan(nil, job, plan, elasticTestNow)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Executed {
		t.Error("NoResize should not execute")
	}
	if result.TriggerStopFlow {
		t.Error("NoResize should not trigger stop flow")
	}
	// Conditions should be cleared.
	if meta.FindStatusCondition(job.Status.Conditions, conditionTypeResizePending) != nil {
		t.Error("expected ResizePending to be cleared")
	}
}

func TestExecuteElasticPlan_ResizeBlocked_SetsCondition(t *testing.T) {
	r := &ResumableTrainingJobReconciler{}
	job := elasticTestRTJ()

	plan := elastic.PlanOutput{
		Kind:           elastic.PlanResizeBlocked,
		Reason:         "WorkloadNotAdmitted",
		Message:        "resize blocked: workload is not yet admitted",
		NewWorkerCount: 8,
	}

	result, err := r.executeElasticPlan(nil, job, plan, elasticTestNow)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Executed {
		t.Error("blocked should not execute")
	}
	cond := meta.FindStatusCondition(job.Status.Conditions, conditionTypeResizeBlocked)
	if cond == nil {
		t.Fatal("expected ResizeBlocked condition")
	}
}

func TestExecuteElasticPlan_GrowViaRelaunch_TriggerStopFlow(t *testing.T) {
	r := &ResumableTrainingJobReconciler{}
	job := elasticTestRTJ()
	job.Status.WorkloadReference = nil // No workload to clear reclaimablePods from.

	plan := elastic.PlanOutput{
		Kind:               elastic.PlanGrowViaRelaunch,
		Reason:             "GrowRequiresRelaunch",
		Message:            "growing from 8 to 16 workers",
		NewWorkerCount:     16,
		CheckpointRequired: true,
		RelaunchRequired:   true,
	}

	result, err := r.executeElasticPlan(nil, job, plan, elasticTestNow)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Executed {
		t.Error("expected Executed=true")
	}
	if !result.TriggerStopFlow {
		t.Error("expected TriggerStopFlow=true for grow via relaunch")
	}
	if !result.NeedsRequeue {
		t.Error("expected NeedsRequeue=true")
	}
	if job.Status.Elasticity.ResizeState != trainingv1alpha1.ResizeStateInProgress {
		t.Errorf("expected ResizeState=InProgress, got %s", job.Status.Elasticity.ResizeState)
	}
	if job.Status.Elasticity.TargetWorkerCount != 16 {
		t.Errorf("expected TargetWorkerCount=16, got %d", job.Status.Elasticity.TargetWorkerCount)
	}
	// ResizeCheckpointing condition should be set.
	cond := meta.FindStatusCondition(job.Status.Conditions, conditionTypeResizeCheckpointing)
	if cond == nil {
		t.Fatal("expected ResizeCheckpointing condition")
	}
}

func TestExecuteElasticPlan_ShrinkViaRelaunch_TriggerStopFlow(t *testing.T) {
	r := &ResumableTrainingJobReconciler{}
	job := elasticTestRTJ()
	job.Status.Elasticity.InPlaceShrinkSupported = false
	job.Status.WorkloadReference = nil

	plan := elastic.PlanOutput{
		Kind:               elastic.PlanShrinkViaRelaunch,
		Reason:             "InPlaceShrinkNotSupported",
		Message:            "shrinking from 8 to 4 workers via relaunch",
		NewWorkerCount:     4,
		CheckpointRequired: true,
		RelaunchRequired:   true,
	}

	result, err := r.executeElasticPlan(nil, job, plan, elasticTestNow)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.TriggerStopFlow {
		t.Error("expected TriggerStopFlow=true for shrink via relaunch")
	}
	if job.Status.Elasticity.TargetWorkerCount != 4 {
		t.Errorf("expected TargetWorkerCount=4, got %d", job.Status.Elasticity.TargetWorkerCount)
	}
}

func TestExecuteElasticPlan_RepeatedCallsAreIdempotent(t *testing.T) {
	r := &ResumableTrainingJobReconciler{}
	job := elasticTestRTJ()
	job.Status.WorkloadReference = nil

	plan := elastic.PlanOutput{
		Kind:               elastic.PlanGrowViaRelaunch,
		Reason:             "GrowRequiresRelaunch",
		Message:            "growing from 8 to 16",
		NewWorkerCount:     16,
		CheckpointRequired: true,
		RelaunchRequired:   true,
	}

	// First call.
	_, err := r.executeElasticPlan(nil, job, plan, elasticTestNow)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}

	// Second call with the same plan — should still signal stop flow
	// but should not duplicate state changes.
	result2, err := r.executeElasticPlan(nil, job, plan, elasticTestNow)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if !result2.TriggerStopFlow {
		t.Error("second call should still trigger stop flow")
	}
	// TargetWorkerCount should remain the same.
	if job.Status.Elasticity.TargetWorkerCount != 16 {
		t.Errorf("target should remain 16, got %d", job.Status.Elasticity.TargetWorkerCount)
	}
}

func TestExecuteElasticPlan_ResizeInProgress_NoAction(t *testing.T) {
	r := &ResumableTrainingJobReconciler{}
	job := elasticTestRTJ()

	plan := elastic.PlanOutput{
		Kind:           elastic.PlanResizeInProgress,
		Reason:         "ResizeInProgress",
		NewWorkerCount: 4,
	}

	result, err := r.executeElasticPlan(nil, job, plan, elasticTestNow)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Executed {
		t.Error("ResizeInProgress should not execute")
	}
	if result.TriggerStopFlow {
		t.Error("ResizeInProgress should not trigger stop flow")
	}
}

func TestExecuteElasticPlan_ReclaimPublished_NoAction(t *testing.T) {
	r := &ResumableTrainingJobReconciler{}
	job := elasticTestRTJ()

	plan := elastic.PlanOutput{
		Kind:           elastic.PlanReclaimPublished,
		Reason:         "ReclaimablePodsPublished",
		NewWorkerCount: 4,
	}

	result, err := r.executeElasticPlan(nil, job, plan, elasticTestNow)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Executed {
		t.Error("ReclaimPublished should not execute")
	}
}

// --- ReclaimablePods monotonicity test ---

func TestReclaimablePods_MonotonicPublication(t *testing.T) {
	// Verify that once reclaimablePodsPublished is set to true,
	// subsequent plan evaluations produce PlanReclaimPublished
	// instead of re-executing the shrink.
	job := elasticTestRTJ()
	job.Status.Elasticity.ReclaimablePodsPublished = true
	job.Spec.Elasticity.TargetWorkerCount = ptr.To[int32](4)

	input := buildElasticPlanInput(job, true, true, elasticTestNow)
	plan := elastic.EvaluatePlan(input)

	if plan.Kind != elastic.PlanReclaimPublished {
		t.Errorf("expected PlanReclaimPublished when already published, got %s", plan.Kind)
	}
	if plan.ReclaimableWorkerDelta != 0 {
		t.Errorf("expected zero delta for already-published, got %d", plan.ReclaimableWorkerDelta)
	}
}

// --- DRA-aware resize coherency tests ---

func TestExecuteElasticPlan_DRAEnabled_RelaunchClearsReclaimFlag(t *testing.T) {
	r := &ResumableTrainingJobReconciler{}
	job := elasticTestRTJ()
	job.Status.Devices = &trainingv1alpha1.DeviceStatus{
		DeviceMode:                      trainingv1alpha1.DeviceModeDRA,
		CurrentDeviceProfileFingerprint: "fp-abc123",
	}
	job.Status.Elasticity.ReclaimablePodsPublished = true
	job.Status.WorkloadReference = nil

	plan := elastic.PlanOutput{
		Kind:               elastic.PlanGrowViaRelaunch,
		Reason:             "GrowRequiresRelaunch",
		Message:            "grow with DRA",
		NewWorkerCount:     16,
		CheckpointRequired: true,
		RelaunchRequired:   true,
	}

	_, err := r.executeElasticPlan(nil, job, plan, elasticTestNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify reclaimable pods published flag is cleared for the new cycle.
	if job.Status.Elasticity.ReclaimablePodsPublished {
		t.Error("ReclaimablePodsPublished should be cleared for relaunch cycle")
	}

	// Verify DRA device status is preserved (not cleared by resize execution).
	if job.Status.Devices == nil {
		t.Error("DRA device status should be preserved during resize")
	}
	if job.Status.Devices.CurrentDeviceProfileFingerprint != "fp-abc123" {
		t.Error("DRA fingerprint should be preserved during resize")
	}
}

// --- Shrink fallback documentation test ---

func TestShrinkFallback_ClearWhenInPlaceNotSupported(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity.InPlaceShrinkSupported = false
	job.Spec.Elasticity.InPlaceShrinkPolicy = trainingv1alpha1.InPlaceShrinkPolicyIfSupported

	input := buildElasticPlanInput(job, true, true, elasticTestNow)
	plan := elastic.EvaluatePlan(input)

	if plan.Kind != elastic.PlanShrinkViaRelaunch {
		t.Errorf("expected ShrinkViaRelaunch when in-place not supported, got %s", plan.Kind)
	}
	if plan.Reason != "InPlaceShrinkNotSupported" {
		t.Errorf("expected reason=InPlaceShrinkNotSupported, got %s", plan.Reason)
	}
	if !plan.CheckpointRequired {
		t.Error("expected CheckpointRequired=true for fallback")
	}
	if !plan.RelaunchRequired {
		t.Error("expected RelaunchRequired=true for fallback")
	}
}

func TestShrinkFallback_PolicyNever(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity.InPlaceShrinkSupported = true // Even though runtime supports it
	job.Spec.Elasticity.InPlaceShrinkPolicy = trainingv1alpha1.InPlaceShrinkPolicyNever

	input := buildElasticPlanInput(job, true, true, elasticTestNow)
	plan := elastic.EvaluatePlan(input)

	if plan.Kind != elastic.PlanShrinkViaRelaunch {
		t.Errorf("expected ShrinkViaRelaunch when policy=Never, got %s", plan.Kind)
	}
	if plan.Reason != "InPlaceShrinkPolicyNever" {
		t.Errorf("expected reason=InPlaceShrinkPolicyNever, got %s", plan.Reason)
	}
}

// --- clearAllResizeConditions test ---

func TestClearAllResizeConditions(t *testing.T) {
	job := elasticTestRTJ()
	// Set all conditions.
	setResizePendingCondition(job, "test", "msg", elasticTestNow)
	setShrinkingInPlaceCondition(job, "msg", elasticTestNow)
	setShrinkReclaimPublishedCondition(job, "msg", elasticTestNow)
	setResizeCheckpointingCondition(job, "msg", elasticTestNow)
	setRelaunchingForResizeCondition(job, "msg", elasticTestNow)
	setResizeBlockedCondition(job, "test", "msg", elasticTestNow)
	setResizeFailedCondition(job, "msg", elasticTestNow)

	condCountBefore := len(job.Status.Conditions)
	if condCountBefore != 7 {
		t.Fatalf("expected 7 conditions set, got %d", condCountBefore)
	}

	changed := clearAllResizeConditions(job)

	if !changed {
		t.Error("expected change")
	}
	if len(job.Status.Conditions) != 0 {
		t.Errorf("expected all conditions cleared, got %d", len(job.Status.Conditions))
	}
}

// --- Grow uses checkpoint-and-relaunch test ---

func TestManualGrow_UsesCheckpointAndRelaunch(t *testing.T) {
	job := elasticTestRTJ()
	// Set up: current=4 workers, target=8
	job.Status.Elasticity.AdmittedWorkerCount = 4
	job.Status.Elasticity.ActiveWorkerCount = 4
	job.Spec.Elasticity.TargetWorkerCount = ptr.To[int32](8)

	input := buildElasticPlanInput(job, true, true, elasticTestNow)
	plan := elastic.EvaluatePlan(input)

	if plan.Kind != elastic.PlanGrowViaRelaunch {
		t.Errorf("expected GrowViaRelaunch, got %s", plan.Kind)
	}
	if !plan.CheckpointRequired {
		t.Error("expected CheckpointRequired=true")
	}
	if !plan.RelaunchRequired {
		t.Error("expected RelaunchRequired=true")
	}
	if plan.NewWorkerCount != 8 {
		t.Errorf("expected NewWorkerCount=8, got %d", plan.NewWorkerCount)
	}
}

// --- Manual shrink updates runtime/control path test ---

func TestManualShrink_InPlace_UpdatesStatus(t *testing.T) {
	job := elasticTestRTJ()
	// Shrink from 8 to 4 with in-place support.
	job.Spec.Elasticity.TargetWorkerCount = ptr.To[int32](4)
	job.Status.Elasticity.InPlaceShrinkSupported = true

	input := buildElasticPlanInput(job, true, true, elasticTestNow)
	plan := elastic.EvaluatePlan(input)

	if plan.Kind != elastic.PlanShrinkInPlace {
		t.Fatalf("expected ShrinkInPlace, got %s", plan.Kind)
	}
	if plan.ReclaimableWorkerDelta != 4 {
		t.Errorf("expected 4 reclaimable workers, got %d", plan.ReclaimableWorkerDelta)
	}

	// Verify plan output fields.
	if plan.NewWorkerCount != 4 {
		t.Errorf("expected NewWorkerCount=4, got %d", plan.NewWorkerCount)
	}
	if plan.CheckpointRequired {
		t.Error("in-place shrink should not require checkpoint")
	}
	if plan.RelaunchRequired {
		t.Error("in-place shrink should not require relaunch")
	}
}

// --- Repeated reconcile idempotency test ---

func TestRepeatedReconcile_DoesNotDuplicateExecution(t *testing.T) {
	r := &ResumableTrainingJobReconciler{}
	job := elasticTestRTJ()
	job.Status.WorkloadReference = nil

	// Simulate grow via relaunch.
	plan := elastic.PlanOutput{
		Kind:               elastic.PlanGrowViaRelaunch,
		Reason:             "GrowRequiresRelaunch",
		Message:            "growing",
		NewWorkerCount:     16,
		CheckpointRequired: true,
		RelaunchRequired:   true,
	}

	// First execution.
	r.executeElasticPlan(nil, job, plan, elasticTestNow)

	// After first execution, resizeState should be InProgress.
	if job.Status.Elasticity.ResizeState != trainingv1alpha1.ResizeStateInProgress {
		t.Fatalf("expected InProgress after first execution, got %s",
			job.Status.Elasticity.ResizeState)
	}

	// On the next reconcile, the planner should see InProgress and return
	// ResizeInProgress (not re-trigger the stop flow).
	job.Status.Elasticity.ResizeState = trainingv1alpha1.ResizeStateInProgress
	inProgressPlan := elastic.PlanOutput{
		Kind:           elastic.PlanResizeInProgress,
		Reason:         "ResizeInProgress",
		NewWorkerCount: 16,
	}

	result, err := r.executeElasticPlan(nil, job, inProgressPlan, elasticTestNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Executed {
		t.Error("ResizeInProgress should not execute again")
	}
	if result.TriggerStopFlow {
		t.Error("ResizeInProgress should not trigger stop flow again")
	}
}
