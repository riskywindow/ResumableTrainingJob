package controller

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
)

// --- Fake catalog for testing ---

type fakeCatalog struct {
	info    *checkpoints.CheckpointInfo
	found   bool
	err     error
	called  bool
	pauseOb *checkpoints.PauseObservation
}

func (f *fakeCatalog) ObservePause(_ context.Context, _ string, _ int32, _ string, _ time.Time) (*checkpoints.PauseObservation, bool, error) {
	return f.pauseOb, f.pauseOb != nil, nil
}

func (f *fakeCatalog) SelectResumeCheckpoint(_ context.Context, _ checkpoints.ResumeRequest) (*trainingv1alpha1.CheckpointReference, bool, error) {
	return nil, false, nil
}

func (f *fakeCatalog) LatestCheckpointInfo(_ context.Context, _ string) (*checkpoints.CheckpointInfo, bool, error) {
	f.called = true
	return f.info, f.found, f.err
}

// --- Helper builders ---

func baseRTJ() *trainingv1alpha1.ResumableTrainingJob {
	return &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rtj",
			Namespace: "default",
		},
		Spec: trainingv1alpha1.ResumableTrainingJobSpec{
			QueueName:                 "default-queue",
			WorkloadPriorityClassName: "default-priority",
			Checkpoint: trainingv1alpha1.CheckpointPolicy{
				StorageURI: "s3://test-bucket/checkpoints",
			},
			Identity: trainingv1alpha1.ResumableTrainingJobIdentity{
				Image:       "test:latest",
				CodeVersion: "v1",
				WorldSize:   2,
				GPUShape:    "cpu",
			},
			Runtime: trainingv1alpha1.ResumableTrainingJobRuntime{
				Mode:          trainingv1alpha1.RuntimeModeDDP,
				OptimizerMode: "adamw",
				ShardingMode:  "replicated-optimizer-state",
			},
		},
	}
}

func rtjWithPolicy() *trainingv1alpha1.ResumableTrainingJob {
	rtj := baseRTJ()
	rtj.Spec.PriorityPolicyRef = &trainingv1alpha1.PriorityPolicyReference{
		Name: "test-policy",
	}
	return rtj
}

func timeAt(minutes int) metav1.Time {
	return metav1.Time{Time: time.Date(2026, 3, 25, 12, minutes, 0, 0, time.UTC)}
}

// --- Test: Checkpoint completion updates RTJ-visible telemetry ---

func TestCollectTelemetry_CheckpointFromStatus(t *testing.T) {
	rtj := rtjWithPolicy()
	completionTime := timeAt(10)
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-001",
		StorageURI:     "s3://test-bucket/checkpoints",
		CompletionTime: &completionTime,
	}

	catalog := &fakeCatalog{}
	now := timeAt(15)

	snap := CollectTelemetry(context.Background(), rtj, catalog, now, 0)

	if snap.LastCompletedCheckpointTime == nil {
		t.Fatal("expected LastCompletedCheckpointTime to be set")
	}
	if !snap.LastCompletedCheckpointTime.Equal(&completionTime) {
		t.Errorf("expected checkpoint time %v, got %v", completionTime, *snap.LastCompletedCheckpointTime)
	}
	if snap.LastCompletedCheckpointID != "ckpt-001" {
		t.Errorf("expected checkpoint ID ckpt-001, got %s", snap.LastCompletedCheckpointID)
	}
	// Catalog should NOT be called when status already has the checkpoint.
	if catalog.called {
		t.Error("catalog.LatestCheckpointInfo should not have been called when status has checkpoint")
	}
}

func TestCollectTelemetry_CheckpointFromCatalogFallback(t *testing.T) {
	rtj := rtjWithPolicy()
	// No status.LastCompletedCheckpoint set.

	catalogTime := timeAt(8)
	catalog := &fakeCatalog{
		info: &checkpoints.CheckpointInfo{
			CheckpointID:        "ckpt-catalog-001",
			GlobalStep:          100,
			CompletionTimestamp: catalogTime,
			ManifestURI:         "s3://test-bucket/checkpoints/manifests/ckpt-catalog-001.manifest.json",
		},
		found: true,
	}
	now := timeAt(15)

	snap := CollectTelemetry(context.Background(), rtj, catalog, now, 0)

	if !catalog.called {
		t.Fatal("expected catalog.LatestCheckpointInfo to be called")
	}
	if snap.LastCompletedCheckpointTime == nil {
		t.Fatal("expected LastCompletedCheckpointTime from catalog")
	}
	if !snap.LastCompletedCheckpointTime.Equal(&catalogTime) {
		t.Errorf("expected checkpoint time %v, got %v", catalogTime, *snap.LastCompletedCheckpointTime)
	}
	if snap.LastCompletedCheckpointID != "ckpt-catalog-001" {
		t.Errorf("expected checkpoint ID ckpt-catalog-001, got %s", snap.LastCompletedCheckpointID)
	}
	if snap.LastCompletedCheckpointStep != 100 {
		t.Errorf("expected checkpoint step 100, got %d", snap.LastCompletedCheckpointStep)
	}
}

func TestCollectTelemetry_NoCatalogNoCheckpoint(t *testing.T) {
	rtj := rtjWithPolicy()
	catalog := &fakeCatalog{found: false}
	now := timeAt(15)

	snap := CollectTelemetry(context.Background(), rtj, catalog, now, 0)

	if snap.LastCompletedCheckpointTime != nil {
		t.Error("expected nil LastCompletedCheckpointTime when no checkpoint available")
	}
	if snap.LastCompletedCheckpointID != "" {
		t.Error("expected empty LastCompletedCheckpointID")
	}
}

// --- Test: Yield/resume lifecycle updates telemetry ---

func TestCollectTelemetry_LifecycleTimestamps(t *testing.T) {
	rtj := rtjWithPolicy()
	startingAt := timeAt(1)
	runningAt := timeAt(2)
	yieldAt := timeAt(5)
	pausedAt := timeAt(7)
	restoringAt := timeAt(9)
	restoreCompletedAt := timeAt(10)

	rtj.Status.TransitionTimestamps = trainingv1alpha1.TransitionTimestamps{
		StartingAt:         &startingAt,
		RunningAt:          &runningAt,
		YieldRequestedAt:   &yieldAt,
		PausedAt:           &pausedAt,
		RestoringAt:        &restoringAt,
		RestoreCompletedAt: &restoreCompletedAt,
	}

	catalog := &fakeCatalog{found: false}
	now := timeAt(15)

	snap := CollectTelemetry(context.Background(), rtj, catalog, now, 0)

	if snap.LastRunStartTime == nil || !snap.LastRunStartTime.Equal(&startingAt) {
		t.Errorf("expected LastRunStartTime=%v, got %v", startingAt, snap.LastRunStartTime)
	}
	if snap.CurrentRunActiveSince == nil || !snap.CurrentRunActiveSince.Equal(&runningAt) {
		t.Errorf("expected CurrentRunActiveSince=%v, got %v", runningAt, snap.CurrentRunActiveSince)
	}
	if snap.LastYieldTime == nil || !snap.LastYieldTime.Equal(&yieldAt) {
		t.Errorf("expected LastYieldTime=%v, got %v", yieldAt, snap.LastYieldTime)
	}
	if snap.LastResumeTime == nil || !snap.LastResumeTime.Equal(&restoreCompletedAt) {
		t.Errorf("expected LastResumeTime=%v, got %v", restoreCompletedAt, snap.LastResumeTime)
	}
	expectedDrain := pausedAt.Sub(yieldAt.Time)
	if snap.LastDrainDuration != expectedDrain {
		t.Errorf("expected LastDrainDuration=%v, got %v", expectedDrain, snap.LastDrainDuration)
	}
}

func TestCollectTelemetry_ResumeTimeFallsBackToRunningAt(t *testing.T) {
	rtj := rtjWithPolicy()
	restoringAt := timeAt(5)
	runningAt := timeAt(6)

	rtj.Status.TransitionTimestamps = trainingv1alpha1.TransitionTimestamps{
		RestoringAt: &restoringAt,
		RunningAt:   &runningAt,
		// No RestoreCompletedAt set.
	}

	catalog := &fakeCatalog{found: false}
	now := timeAt(15)

	snap := CollectTelemetry(context.Background(), rtj, catalog, now, 0)

	if snap.LastResumeTime == nil || !snap.LastResumeTime.Equal(&runningAt) {
		t.Errorf("expected LastResumeTime to fall back to RunningAt=%v, got %v", runningAt, snap.LastResumeTime)
	}
}

func TestCollectTelemetry_DrainDurationNotSetWhenPausedBeforeYield(t *testing.T) {
	rtj := rtjWithPolicy()
	yieldAt := timeAt(10)
	pausedAt := timeAt(5) // Paused before yield (from a previous run)

	rtj.Status.TransitionTimestamps = trainingv1alpha1.TransitionTimestamps{
		YieldRequestedAt: &yieldAt,
		PausedAt:         &pausedAt,
	}

	catalog := &fakeCatalog{found: false}
	now := timeAt(15)

	snap := CollectTelemetry(context.Background(), rtj, catalog, now, 0)

	if snap.LastDrainDuration != 0 {
		t.Errorf("expected zero LastDrainDuration when paused before yield, got %v", snap.LastDrainDuration)
	}
}

// --- Test: RecentYieldCount windowing ---

func TestYieldCountWindowing_NoHistory(t *testing.T) {
	rtj := rtjWithPolicy()
	now := timeAt(15)

	snap := CollectTelemetry(context.Background(), rtj, &fakeCatalog{found: false}, now, 30*time.Minute)

	if snap.RecentYieldCount != 0 {
		t.Errorf("expected 0 yields with no history, got %d", snap.RecentYieldCount)
	}
}

func TestYieldCountWindowing_AllWithinWindow(t *testing.T) {
	rtj := rtjWithPolicy()
	// Record 3 yields all within a 30-minute window.
	rtj.Annotations = map[string]string{
		yieldHistoryAnnotation: marshalYieldHistory([]time.Time{
			timeAt(10).Time,
			timeAt(12).Time,
			timeAt(14).Time,
		}),
	}
	now := timeAt(15)

	snap := CollectTelemetry(context.Background(), rtj, &fakeCatalog{found: false}, now, 30*time.Minute)

	if snap.RecentYieldCount != 3 {
		t.Errorf("expected 3 yields within window, got %d", snap.RecentYieldCount)
	}
}

func TestYieldCountWindowing_SomeExpired(t *testing.T) {
	rtj := rtjWithPolicy()
	// 3 yields: one at minute 0 (expired), two within 10-minute window.
	rtj.Annotations = map[string]string{
		yieldHistoryAnnotation: marshalYieldHistory([]time.Time{
			timeAt(0).Time,  // expired (15 minutes ago, window is 10)
			timeAt(10).Time, // within window
			timeAt(14).Time, // within window
		}),
	}
	now := timeAt(15)

	snap := CollectTelemetry(context.Background(), rtj, &fakeCatalog{found: false}, now, 10*time.Minute)

	if snap.RecentYieldCount != 2 {
		t.Errorf("expected 2 yields within 10-minute window, got %d", snap.RecentYieldCount)
	}
}

func TestYieldCountWindowing_ZeroWindowDisablesCounting(t *testing.T) {
	rtj := rtjWithPolicy()
	rtj.Annotations = map[string]string{
		yieldHistoryAnnotation: marshalYieldHistory([]time.Time{
			timeAt(10).Time,
			timeAt(14).Time,
		}),
	}
	now := timeAt(15)

	snap := CollectTelemetry(context.Background(), rtj, &fakeCatalog{found: false}, now, 0)

	if snap.RecentYieldCount != 0 {
		t.Errorf("expected 0 yields when window is zero, got %d", snap.RecentYieldCount)
	}
}

// --- Test: RecordYieldEvent ---

func TestRecordYieldEvent_AppendsAndPrunes(t *testing.T) {
	rtj := rtjWithPolicy()
	window := 10 * time.Minute

	// Record first yield at minute 5.
	RecordYieldEvent(rtj, timeAt(5), window)
	if count := countYieldsInWindow(rtj, timeAt(5), window); count != 1 {
		t.Errorf("expected 1 yield after first record, got %d", count)
	}

	// Record second yield at minute 8.
	RecordYieldEvent(rtj, timeAt(8), window)
	if count := countYieldsInWindow(rtj, timeAt(8), window); count != 2 {
		t.Errorf("expected 2 yields after second record, got %d", count)
	}

	// Record third yield at minute 14.
	RecordYieldEvent(rtj, timeAt(14), window)
	if count := countYieldsInWindow(rtj, timeAt(14), window); count != 3 {
		t.Errorf("expected 3 yields within window (minute 5 + 8 + 14), got %d", count)
	}

	// Record fourth yield at minute 20 — minute 5 and 8 are outside the window (cutoff = minute 10).
	RecordYieldEvent(rtj, timeAt(20), window)
	if count := countYieldsInWindow(rtj, timeAt(20), window); count != 2 {
		t.Errorf("expected 2 yields after pruning (minute 14 + minute 20), got %d", count)
	}
}

func TestRecordYieldEvent_CreatesAnnotationsMapIfNil(t *testing.T) {
	rtj := rtjWithPolicy()
	rtj.Annotations = nil

	RecordYieldEvent(rtj, timeAt(5), 10*time.Minute)

	if rtj.Annotations == nil {
		t.Fatal("expected annotations map to be created")
	}
	if _, ok := rtj.Annotations[yieldHistoryAnnotation]; !ok {
		t.Error("expected yield history annotation to be set")
	}
}

// --- Test: SyncPriorityShapingTelemetry ---

func TestSyncTelemetry_NoPolicyClearsStatus(t *testing.T) {
	rtj := baseRTJ() // No policy ref.
	rtj.Status.PriorityShaping = &trainingv1alpha1.PriorityShapingStatus{
		BasePriority: 100,
	}
	snap := TelemetrySnapshot{}
	now := timeAt(15)

	changed := SyncPriorityShapingTelemetry(rtj, snap, now)

	if !changed {
		t.Error("expected changed=true when clearing stale telemetry")
	}
	if rtj.Status.PriorityShaping != nil {
		t.Error("expected PriorityShaping to be nil when no policy is attached")
	}
}

func TestSyncTelemetry_InitializesStatusWhenPolicySet(t *testing.T) {
	rtj := rtjWithPolicy()
	completionTime := timeAt(10)
	yieldTime := timeAt(5)
	resumeTime := timeAt(8)
	snap := TelemetrySnapshot{
		LastCompletedCheckpointTime: &completionTime,
		LastYieldTime:               &yieldTime,
		LastResumeTime:              &resumeTime,
		RecentYieldCount:            2,
	}
	now := timeAt(15)

	changed := SyncPriorityShapingTelemetry(rtj, snap, now)

	if !changed {
		t.Error("expected changed=true on first sync")
	}
	ps := rtj.Status.PriorityShaping
	if ps == nil {
		t.Fatal("expected PriorityShaping to be initialized")
	}
	if !ps.LastCompletedCheckpointTime.Equal(&completionTime) {
		t.Errorf("expected LastCompletedCheckpointTime=%v, got %v", completionTime, ps.LastCompletedCheckpointTime)
	}
	if ps.CheckpointAge != "5m0s" {
		t.Errorf("expected CheckpointAge=5m0s, got %s", ps.CheckpointAge)
	}
	if !ps.LastYieldTime.Equal(&yieldTime) {
		t.Errorf("expected LastYieldTime=%v, got %v", yieldTime, ps.LastYieldTime)
	}
	if !ps.LastResumeTime.Equal(&resumeTime) {
		t.Errorf("expected LastResumeTime=%v, got %v", resumeTime, ps.LastResumeTime)
	}
	if ps.RecentYieldCount != 2 {
		t.Errorf("expected RecentYieldCount=2, got %d", ps.RecentYieldCount)
	}
	if ps.AppliedPolicyRef != "test-policy" {
		t.Errorf("expected AppliedPolicyRef=test-policy, got %s", ps.AppliedPolicyRef)
	}
}

func TestSyncTelemetry_IdempotentWhenNoChange(t *testing.T) {
	rtj := rtjWithPolicy()
	completionTime := timeAt(10)
	snap := TelemetrySnapshot{
		LastCompletedCheckpointTime: &completionTime,
	}
	now := timeAt(15)

	// First sync.
	SyncPriorityShapingTelemetry(rtj, snap, now)

	// Second sync with same data.
	changed := SyncPriorityShapingTelemetry(rtj, snap, now)

	if changed {
		t.Error("expected changed=false on idempotent sync")
	}
}

func TestSyncTelemetry_CheckpointAgeRecomputedEachReconcile(t *testing.T) {
	rtj := rtjWithPolicy()
	completionTime := timeAt(10)
	snap := TelemetrySnapshot{
		LastCompletedCheckpointTime: &completionTime,
	}

	// First reconcile at minute 15.
	SyncPriorityShapingTelemetry(rtj, snap, timeAt(15))
	if rtj.Status.PriorityShaping.CheckpointAge != "5m0s" {
		t.Errorf("expected 5m0s, got %s", rtj.Status.PriorityShaping.CheckpointAge)
	}

	// Second reconcile at minute 20.
	changed := SyncPriorityShapingTelemetry(rtj, snap, timeAt(20))
	if !changed {
		t.Error("expected changed=true when checkpoint age increases")
	}
	if rtj.Status.PriorityShaping.CheckpointAge != "10m0s" {
		t.Errorf("expected 10m0s, got %s", rtj.Status.PriorityShaping.CheckpointAge)
	}
}

// --- Test: Operator restart/reconcile does not corrupt telemetry ---

func TestOperatorRestart_PreservesExistingTelemetry(t *testing.T) {
	// Simulate an operator restart by creating an RTJ that already has
	// PriorityShapingStatus populated from a previous operator instance.
	rtj := rtjWithPolicy()
	completionTime := timeAt(10)
	yieldTime := timeAt(5)
	resumeTime := timeAt(8)
	rtj.Status.PriorityShaping = &trainingv1alpha1.PriorityShapingStatus{
		BasePriority:                100,
		EffectivePriority:           90,
		PreemptionState:             trainingv1alpha1.PreemptionStateActive,
		LastCompletedCheckpointTime: &completionTime,
		LastYieldTime:               &yieldTime,
		LastResumeTime:              &resumeTime,
		RecentYieldCount:            1,
		AppliedPolicyRef:            "test-policy",
	}
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-001",
		CompletionTime: &completionTime,
	}
	rtj.Annotations = map[string]string{
		yieldHistoryAnnotation: marshalYieldHistory([]time.Time{yieldTime.Time}),
	}
	rtj.Status.TransitionTimestamps = trainingv1alpha1.TransitionTimestamps{
		YieldRequestedAt: &yieldTime,
	}

	// After restart, the new operator collects telemetry.
	catalog := &fakeCatalog{found: false}
	now := timeAt(20)

	snap := CollectTelemetry(context.Background(), rtj, catalog, now, 30*time.Minute)

	// Verify the telemetry comes from the persisted status, not a catalog lookup.
	if catalog.called {
		t.Error("catalog should not be called when status has checkpoint data")
	}
	if snap.LastCompletedCheckpointTime == nil || !snap.LastCompletedCheckpointTime.Equal(&completionTime) {
		t.Errorf("expected persisted checkpoint time to survive restart")
	}
	if snap.LastCompletedCheckpointID != "ckpt-001" {
		t.Errorf("expected persisted checkpoint ID to survive restart")
	}
	if snap.LastYieldTime == nil || !snap.LastYieldTime.Equal(&yieldTime) {
		t.Errorf("expected persisted yield time to survive restart")
	}
	if snap.RecentYieldCount != 1 {
		t.Errorf("expected persisted yield count to survive restart, got %d", snap.RecentYieldCount)
	}

	// Now sync telemetry — should preserve the existing base/effective priority
	// and preemption state (those are set by the priority shaping controller,
	// not the telemetry sync).
	changed := SyncPriorityShapingTelemetry(rtj, snap, now)

	// CheckpointAge should update since `now` changed.
	if !changed {
		t.Error("expected change due to checkpoint age update")
	}
	// BasePriority and EffectivePriority should be preserved (not zeroed).
	if rtj.Status.PriorityShaping.BasePriority != 100 {
		t.Errorf("expected BasePriority=100 preserved, got %d", rtj.Status.PriorityShaping.BasePriority)
	}
	if rtj.Status.PriorityShaping.EffectivePriority != 90 {
		t.Errorf("expected EffectivePriority=90 preserved, got %d", rtj.Status.PriorityShaping.EffectivePriority)
	}
	// PreemptionState should be preserved (telemetry sync doesn't touch it).
	if rtj.Status.PriorityShaping.PreemptionState != trainingv1alpha1.PreemptionStateActive {
		t.Errorf("expected PreemptionState=Active preserved, got %s", rtj.Status.PriorityShaping.PreemptionState)
	}
}

func TestOperatorRestart_FallsBackToCatalogWhenStatusLacksCheckpoint(t *testing.T) {
	// Simulate an operator restart where the RTJ status was partially lost
	// (e.g., status was reset or checkpoint ref was cleared).
	rtj := rtjWithPolicy()
	// No status.LastCompletedCheckpoint.

	catalogTime := timeAt(8)
	catalog := &fakeCatalog{
		info: &checkpoints.CheckpointInfo{
			CheckpointID:        "ckpt-recovered",
			GlobalStep:          50,
			CompletionTimestamp: catalogTime,
		},
		found: true,
	}
	now := timeAt(20)

	snap := CollectTelemetry(context.Background(), rtj, catalog, now, 0)

	if !catalog.called {
		t.Error("expected catalog to be called when status lacks checkpoint")
	}
	if snap.LastCompletedCheckpointID != "ckpt-recovered" {
		t.Errorf("expected catalog checkpoint ID, got %s", snap.LastCompletedCheckpointID)
	}
	if snap.LastCompletedCheckpointStep != 50 {
		t.Errorf("expected catalog checkpoint step, got %d", snap.LastCompletedCheckpointStep)
	}
}

// --- Test: Status helper Phase 5 functions ---

func TestRecordYieldForTelemetry_NoPolicyNoOp(t *testing.T) {
	rtj := baseRTJ() // No policy ref.
	now := timeAt(10)

	changed := recordYieldForTelemetry(rtj, now, 30*time.Minute)

	if changed {
		t.Error("expected no change when no policy is attached")
	}
	if rtj.Annotations != nil && rtj.Annotations[yieldHistoryAnnotation] != "" {
		t.Error("expected no yield history annotation without policy")
	}
}

func TestRecordYieldForTelemetry_WithPolicy(t *testing.T) {
	rtj := rtjWithPolicy()
	now := timeAt(10)

	changed := recordYieldForTelemetry(rtj, now, 30*time.Minute)

	if !changed {
		t.Error("expected change when recording yield with policy")
	}
	if rtj.Status.PriorityShaping == nil {
		t.Fatal("expected PriorityShaping to be initialized")
	}
	if !rtj.Status.PriorityShaping.LastYieldTime.Equal(&now) {
		t.Errorf("expected LastYieldTime=%v, got %v", now, rtj.Status.PriorityShaping.LastYieldTime)
	}
	if count := countYieldsInWindow(rtj, now, 30*time.Minute); count != 1 {
		t.Errorf("expected 1 yield in window, got %d", count)
	}
}

func TestRecordResumeForTelemetry_NoPolicyNoOp(t *testing.T) {
	rtj := baseRTJ()
	now := timeAt(10)

	changed := recordResumeForTelemetry(rtj, now)

	if changed {
		t.Error("expected no change when no policy is attached")
	}
}

func TestRecordResumeForTelemetry_WithPolicy(t *testing.T) {
	rtj := rtjWithPolicy()
	now := timeAt(10)

	changed := recordResumeForTelemetry(rtj, now)

	if !changed {
		t.Error("expected change when recording resume with policy")
	}
	if rtj.Status.PriorityShaping == nil {
		t.Fatal("expected PriorityShaping to be initialized")
	}
	if !rtj.Status.PriorityShaping.LastResumeTime.Equal(&now) {
		t.Errorf("expected LastResumeTime=%v, got %v", now, rtj.Status.PriorityShaping.LastResumeTime)
	}
	if rtj.Status.PriorityShaping.AppliedPolicyRef != "test-policy" {
		t.Errorf("expected AppliedPolicyRef=test-policy, got %s", rtj.Status.PriorityShaping.AppliedPolicyRef)
	}
}

func TestClearPriorityShapingOnQueued(t *testing.T) {
	rtj := rtjWithPolicy()
	yieldTime := timeAt(5)
	resumeTime := timeAt(8)
	protectedUntil := timeAt(20)
	rtj.Status.PriorityShaping = &trainingv1alpha1.PriorityShapingStatus{
		BasePriority:         100,
		EffectivePriority:    90,
		PreemptionState:      trainingv1alpha1.PreemptionStateProtected,
		PreemptionStateReason: "WithinProtectionWindow",
		ProtectedUntil:       &protectedUntil,
		CheckpointAge:        "5m0s",
		LastYieldTime:        &yieldTime,
		LastResumeTime:       &resumeTime,
		RecentYieldCount:     1,
		AppliedPolicyRef:     "test-policy",
	}

	changed := clearPriorityShapingOnQueued(rtj)

	if !changed {
		t.Error("expected change when clearing runtime fields")
	}
	ps := rtj.Status.PriorityShaping
	// Runtime-only fields should be cleared.
	if ps.PreemptionState != "" {
		t.Errorf("expected PreemptionState cleared, got %s", ps.PreemptionState)
	}
	if ps.PreemptionStateReason != "" {
		t.Errorf("expected PreemptionStateReason cleared, got %s", ps.PreemptionStateReason)
	}
	if ps.ProtectedUntil != nil {
		t.Error("expected ProtectedUntil cleared")
	}
	if ps.CheckpointAge != "" {
		t.Errorf("expected CheckpointAge cleared, got %s", ps.CheckpointAge)
	}
	if ps.EffectivePriority != 100 {
		t.Errorf("expected EffectivePriority reset to BasePriority=100, got %d", ps.EffectivePriority)
	}
	// Historical fields should be preserved.
	if ps.LastYieldTime == nil || !ps.LastYieldTime.Equal(&yieldTime) {
		t.Error("expected LastYieldTime preserved")
	}
	if ps.LastResumeTime == nil || !ps.LastResumeTime.Equal(&resumeTime) {
		t.Error("expected LastResumeTime preserved")
	}
	if ps.RecentYieldCount != 1 {
		t.Error("expected RecentYieldCount preserved")
	}
	if ps.AppliedPolicyRef != "test-policy" {
		t.Error("expected AppliedPolicyRef preserved")
	}
}

func TestClearPriorityShapingOnQueued_NilStatusNoOp(t *testing.T) {
	rtj := rtjWithPolicy()
	// No PriorityShaping set.

	changed := clearPriorityShapingOnQueued(rtj)

	if changed {
		t.Error("expected no change when PriorityShaping is nil")
	}
}

// --- Test: parseYieldHistory / marshalYieldHistory round-trip ---

func TestYieldHistoryRoundTrip(t *testing.T) {
	original := []time.Time{
		timeAt(1).Time,
		timeAt(5).Time,
		timeAt(10).Time,
	}

	serialized := marshalYieldHistory(original)
	parsed := parseYieldHistory(serialized)

	if len(parsed) != len(original) {
		t.Fatalf("expected %d timestamps, got %d", len(original), len(parsed))
	}
	for i, ts := range parsed {
		if !ts.Equal(original[i]) {
			t.Errorf("timestamp %d: expected %v, got %v", i, original[i], ts)
		}
	}
}

func TestParseYieldHistory_InvalidJSON(t *testing.T) {
	result := parseYieldHistory("not-json")
	if result != nil {
		t.Error("expected nil for invalid JSON")
	}
}

func TestParseYieldHistory_EmptyArray(t *testing.T) {
	result := parseYieldHistory("[]")
	if len(result) != 0 {
		t.Errorf("expected empty result for empty array, got %d", len(result))
	}
}

func TestParseYieldHistory_InvalidTimestamp(t *testing.T) {
	result := parseYieldHistory(`["not-a-time","2026-03-25T12:00:00Z"]`)
	if len(result) != 1 {
		t.Errorf("expected 1 valid timestamp, got %d", len(result))
	}
}

// --- Test: NilCatalog safety ---

func TestCollectTelemetry_NilCatalog(t *testing.T) {
	rtj := rtjWithPolicy()
	now := timeAt(15)

	// Should not panic with nil catalog.
	snap := CollectTelemetry(context.Background(), rtj, nil, now, 0)

	if snap.LastCompletedCheckpointTime != nil {
		t.Error("expected nil checkpoint time with nil catalog")
	}
}
