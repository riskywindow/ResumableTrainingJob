package checkpointpriority

import (
	"math"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// --- Test helpers ---

func int32Ptr(v int32) *int32   { return &v }
func boolPtr(v bool) *bool      { return &v }

func durationPtr(d time.Duration) *metav1.Duration {
	return &metav1.Duration{Duration: d}
}

// defaultTestPolicy returns a fully-configured policy for testing.
func defaultTestPolicy() *trainingv1alpha1.CheckpointPriorityPolicySpec {
	return &trainingv1alpha1.CheckpointPriorityPolicySpec{
		CheckpointFreshnessTarget: metav1.Duration{Duration: 10 * time.Minute},
		StartupProtectionWindow:   metav1.Duration{Duration: 5 * time.Minute},
		MinRuntimeBetweenYields:   metav1.Duration{Duration: 2 * time.Minute},
		MaxYieldsPerWindow:        3,
		YieldWindow:               durationPtr(1 * time.Hour),
		FailOpenOnTelemetryLoss:          boolPtr(true),
		FailOpenOnCheckpointStoreErrors:  boolPtr(false),
		ProtectedBoost:       int32Ptr(50),
		CooldownBoost:        int32Ptr(25),
		StaleCheckpointBoost: int32Ptr(-10),
		PreemptibleOffset:    int32Ptr(-100),
		MinEffectivePriority: int32Ptr(-500),
		MaxEffectivePriority: int32Ptr(10000),
	}
}

// --- Disabled / no-policy tests ---

func TestEvaluate_Disabled_NilPolicy(t *testing.T) {
	input := EvaluationInput{
		BasePriority: 500,
		Now:          time.Now(),
	}

	d := Evaluate(input, nil)

	assertDecision(t, d, DecisionDisabled, "", 500, "PolicyDisabled")
	if d.ProtectedUntil != nil {
		t.Error("expected ProtectedUntil=nil for disabled")
	}
}

func TestEvaluate_Disabled_PreservesBasePriority(t *testing.T) {
	for _, bp := range []int32{-1000, 0, 100, 999999} {
		input := EvaluationInput{BasePriority: bp, Now: time.Now()}
		d := Evaluate(input, nil)
		if d.EffectivePriority != bp {
			t.Errorf("base=%d: expected effective=%d, got %d", bp, bp, d.EffectivePriority)
		}
	}
}

// --- Startup protection tests ---

func TestEvaluate_StartupProtected_WithinWindow(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	input := EvaluationInput{
		BasePriority: 500,
		Now:          now,
		RunStartTime: timePtr(now.Add(-2 * time.Minute)), // started 2m ago, window is 5m
	}

	d := Evaluate(input, policy)

	assertDecision(t, d, DecisionStartupProtected, trainingv1alpha1.PreemptionStateProtected, 550, "WithinProtectionWindow")
	if d.ProtectedUntil == nil {
		t.Fatal("expected ProtectedUntil to be set")
	}
	expectedExpiry := now.Add(-2*time.Minute).Add(5 * time.Minute)
	if !d.ProtectedUntil.Equal(expectedExpiry) {
		t.Errorf("expected ProtectedUntil=%v, got %v", expectedExpiry, *d.ProtectedUntil)
	}
}

func TestEvaluate_StartupProtected_ResumeResetsWindow(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	input := EvaluationInput{
		BasePriority:   500,
		Now:            now,
		RunStartTime:   timePtr(now.Add(-20 * time.Minute)), // started 20m ago (expired)
		LastResumeTime: timePtr(now.Add(-1 * time.Minute)),  // resumed 1m ago (within 5m window)
	}

	d := Evaluate(input, policy)

	assertDecision(t, d, DecisionStartupProtected, trainingv1alpha1.PreemptionStateProtected, 550, "WithinProtectionWindow")
}

func TestEvaluate_StartupProtected_ProtectedBoostApplied(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.ProtectedBoost = int32Ptr(200)

	input := EvaluationInput{
		BasePriority: 500,
		Now:          now,
		RunStartTime: timePtr(now.Add(-1 * time.Minute)),
	}

	d := Evaluate(input, policy)

	if d.EffectivePriority != 700 {
		t.Errorf("expected 500+200=700, got %d", d.EffectivePriority)
	}
}

func TestEvaluate_StartupProtected_ZeroBoost(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.ProtectedBoost = int32Ptr(0)

	input := EvaluationInput{
		BasePriority: 500,
		Now:          now,
		RunStartTime: timePtr(now.Add(-1 * time.Minute)),
	}

	d := Evaluate(input, policy)

	if d.EffectivePriority != 500 {
		t.Errorf("expected 500 with zero boost, got %d", d.EffectivePriority)
	}
}

func TestEvaluate_StartupProtected_OverridesStaleCheckpoint(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	// Checkpoint is stale (20m old, target 10m) BUT protection window is active.
	ckptTime := now.Add(-20 * time.Minute)
	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-1 * time.Minute)),
		LastCompletedCheckpointTime: timePtr(ckptTime),
	}

	d := Evaluate(input, policy)

	// Protection window takes priority over stale checkpoint.
	assertDecision(t, d, DecisionStartupProtected, trainingv1alpha1.PreemptionStateProtected, 550, "WithinProtectionWindow")
}

// --- Stale checkpoint tests ---

func TestEvaluate_CheckpointStale(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)), // protection expired
		LastCompletedCheckpointTime: timePtr(now.Add(-15 * time.Minute)), // 15m old, target 10m
	}

	d := Evaluate(input, policy)

	// 500 + (-100) = 400
	assertDecision(t, d, DecisionCheckpointStale, trainingv1alpha1.PreemptionStatePreemptible, 400, "CheckpointStale")
}

func TestEvaluate_CheckpointFresh(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)), // protection expired
		LastCompletedCheckpointTime: timePtr(now.Add(-3 * time.Minute)),  // 3m old, target 10m
	}

	d := Evaluate(input, policy)

	assertDecision(t, d, DecisionActive, trainingv1alpha1.PreemptionStateActive, 500, "CheckpointFresh")
}

func TestEvaluate_CheckpointExactlyAtTarget(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastCompletedCheckpointTime: timePtr(now.Add(-10 * time.Minute)), // exactly at target
	}

	d := Evaluate(input, policy)

	// At exact target: not stale (> is strict).
	assertDecision(t, d, DecisionActive, trainingv1alpha1.PreemptionStateActive, 500, "CheckpointFresh")
}

// --- Cooldown tests ---

func TestEvaluate_CoolingDown_AfterResume(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	// Protection window is 5m, minRuntimeBetweenYields is 2m.
	// Set resume to 6m ago so protection is expired but make minRuntimeBetweenYields longer.
	policy.MinRuntimeBetweenYields = metav1.Duration{Duration: 10 * time.Minute}

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastResumeTime:              timePtr(now.Add(-6 * time.Minute)), // resumed 6m ago
		LastCompletedCheckpointTime: timePtr(now.Add(-2 * time.Minute)), // fresh checkpoint
	}

	d := Evaluate(input, policy)

	// Protection expired (6m > 5m), but cooldown active (6m < 10m).
	// 500 + 25 = 525
	assertDecision(t, d, DecisionCoolingDown, trainingv1alpha1.PreemptionStateCooldown, 525, "CooldownAfterResume")
}

func TestEvaluate_CoolingDown_CooldownExpired(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastResumeTime:              timePtr(now.Add(-10 * time.Minute)), // resumed 10m ago, cooldown 2m
		LastCompletedCheckpointTime: timePtr(now.Add(-2 * time.Minute)),
	}

	d := Evaluate(input, policy)

	// Both protection and cooldown expired; checkpoint is fresh.
	assertDecision(t, d, DecisionActive, trainingv1alpha1.PreemptionStateActive, 500, "CheckpointFresh")
}

func TestEvaluate_CoolingDown_NoResumeNoCooldown(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastCompletedCheckpointTime: timePtr(now.Add(-2 * time.Minute)),
		// No LastResumeTime → no cooldown.
	}

	d := Evaluate(input, policy)

	assertDecision(t, d, DecisionActive, trainingv1alpha1.PreemptionStateActive, 500, "CheckpointFresh")
}

func TestEvaluate_CoolingDown_AfterYield(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.MinRuntimeBetweenYields = metav1.Duration{Duration: 10 * time.Minute}

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastResumeTime:              timePtr(now.Add(-6 * time.Minute)), // resumed 6m ago (past 5m protection)
		LastYieldTime:               timePtr(now.Add(-8 * time.Minute)), // yielded 8m ago
		LastCompletedCheckpointTime: timePtr(now.Add(-2 * time.Minute)),
	}

	d := Evaluate(input, policy)

	// Protection expired (6m > 5m), cooldown active (6m < 10m).
	assertDecision(t, d, DecisionCoolingDown, trainingv1alpha1.PreemptionStateCooldown, 525, "CooldownAfterResume")
}

// --- Yield budget exhaustion tests ---

func TestEvaluate_YieldBudgetExhausted(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastCompletedCheckpointTime: timePtr(now.Add(-2 * time.Minute)),
		RecentYieldCount:            3, // exactly at max
	}

	d := Evaluate(input, policy)

	// 500 + 25 = 525
	assertDecision(t, d, DecisionYieldBudgetExhausted, trainingv1alpha1.PreemptionStateCooldown, 525, "YieldBudgetExhausted")
}

func TestEvaluate_YieldBudgetExhausted_ExceedsMax(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastCompletedCheckpointTime: timePtr(now.Add(-2 * time.Minute)),
		RecentYieldCount:            5, // exceeds max of 3
	}

	d := Evaluate(input, policy)

	assertDecision(t, d, DecisionYieldBudgetExhausted, trainingv1alpha1.PreemptionStateCooldown, 525, "YieldBudgetExhausted")
}

func TestEvaluate_YieldBudgetNotExhausted(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastCompletedCheckpointTime: timePtr(now.Add(-2 * time.Minute)),
		RecentYieldCount:            2, // under max of 3
	}

	d := Evaluate(input, policy)

	// Not exhausted, checkpoint fresh → Active.
	assertDecision(t, d, DecisionActive, trainingv1alpha1.PreemptionStateActive, 500, "CheckpointFresh")
}

func TestEvaluate_YieldBudgetDisabled_ZeroMax(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.MaxYieldsPerWindow = 0 // disabled

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastCompletedCheckpointTime: timePtr(now.Add(-2 * time.Minute)),
		RecentYieldCount:            10, // high count but budget disabled
	}

	d := Evaluate(input, policy)

	assertDecision(t, d, DecisionActive, trainingv1alpha1.PreemptionStateActive, 500, "CheckpointFresh")
}

func TestEvaluate_YieldBudgetExhausted_OverridesStaleCheckpoint(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastCompletedCheckpointTime: timePtr(now.Add(-20 * time.Minute)), // stale
		RecentYieldCount:            3,                                    // exhausted
	}

	d := Evaluate(input, policy)

	// YieldBudgetExhausted takes priority over CheckpointStale.
	assertDecision(t, d, DecisionYieldBudgetExhausted, trainingv1alpha1.PreemptionStateCooldown, 525, "YieldBudgetExhausted")
}

func TestEvaluate_YieldBudgetExhausted_OverridesTelemetryUnknown(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	input := EvaluationInput{
		BasePriority: 500,
		Now:          now,
		RunStartTime: timePtr(now.Add(-30 * time.Minute)),
		// No checkpoint telemetry.
		RecentYieldCount: 3, // exhausted
	}

	d := Evaluate(input, policy)

	assertDecision(t, d, DecisionYieldBudgetExhausted, trainingv1alpha1.PreemptionStateCooldown, 525, "YieldBudgetExhausted")
}

// --- Fail-open vs fail-closed tests ---

func TestEvaluate_TelemetryUnknown_FailOpen(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.FailOpenOnTelemetryLoss = boolPtr(true)

	input := EvaluationInput{
		BasePriority: 500,
		Now:          now,
		RunStartTime: timePtr(now.Add(-30 * time.Minute)),
		// No LastCompletedCheckpointTime.
	}

	d := Evaluate(input, policy)

	assertDecision(t, d, DecisionTelemetryUnknown, trainingv1alpha1.PreemptionStateActive, 500, "TelemetryUnavailableFailOpen")
}

func TestEvaluate_TelemetryUnknown_FailClosed(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.FailOpenOnTelemetryLoss = boolPtr(false)

	input := EvaluationInput{
		BasePriority: 500,
		Now:          now,
		RunStartTime: timePtr(now.Add(-30 * time.Minute)),
		// No LastCompletedCheckpointTime.
	}

	d := Evaluate(input, policy)

	// 500 + (-100) = 400
	assertDecision(t, d, DecisionTelemetryUnknown, trainingv1alpha1.PreemptionStatePreemptible, 400, "TelemetryUnavailableFailClosed")
}

func TestEvaluate_StoreError_FailOpen(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.FailOpenOnCheckpointStoreErrors = boolPtr(true)

	input := EvaluationInput{
		BasePriority:         500,
		Now:                  now,
		RunStartTime:         timePtr(now.Add(-30 * time.Minute)),
		CheckpointStoreError: true,
	}

	d := Evaluate(input, policy)

	assertDecision(t, d, DecisionTelemetryUnknown, trainingv1alpha1.PreemptionStateActive, 500, "StoreErrorFailOpen")
}

func TestEvaluate_StoreError_FailClosed(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.FailOpenOnCheckpointStoreErrors = boolPtr(false)

	input := EvaluationInput{
		BasePriority:         500,
		Now:                  now,
		RunStartTime:         timePtr(now.Add(-30 * time.Minute)),
		CheckpointStoreError: true,
	}

	d := Evaluate(input, policy)

	// 500 + (-100) = 400
	assertDecision(t, d, DecisionTelemetryUnknown, trainingv1alpha1.PreemptionStatePreemptible, 400, "StoreErrorFailClosed")
}

func TestEvaluate_TelemetryUnknown_NilFailOpenDefaults(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.FailOpenOnTelemetryLoss = nil // nil defaults to false via derefBool

	input := EvaluationInput{
		BasePriority: 500,
		Now:          now,
		RunStartTime: timePtr(now.Add(-30 * time.Minute)),
	}

	d := Evaluate(input, policy)

	// nil FailOpenOnTelemetryLoss → derefBool returns false → fail-closed.
	assertDecision(t, d, DecisionTelemetryUnknown, trainingv1alpha1.PreemptionStatePreemptible, 400, "TelemetryUnavailableFailClosed")
}

func TestEvaluate_TelemetryUnknown_ProtectionOverrides(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.FailOpenOnTelemetryLoss = boolPtr(false) // fail-closed

	input := EvaluationInput{
		BasePriority: 500,
		Now:          now,
		RunStartTime: timePtr(now.Add(-1 * time.Minute)), // within protection window
		// No checkpoint telemetry.
	}

	d := Evaluate(input, policy)

	// Protection window overrides telemetry-unknown.
	assertDecision(t, d, DecisionStartupProtected, trainingv1alpha1.PreemptionStateProtected, 550, "WithinProtectionWindow")
}

// --- Clamping tests ---

func TestEvaluate_Clamping_MinEffectivePriority(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.PreemptibleOffset = int32Ptr(-1000) // would make 500 - 1000 = -500
	policy.MinEffectivePriority = int32Ptr(-200)

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastCompletedCheckpointTime: timePtr(now.Add(-20 * time.Minute)), // stale
	}

	d := Evaluate(input, policy)

	// 500 + (-1000) = -500, clamped to min -200.
	if d.EffectivePriority != -200 {
		t.Errorf("expected clamped to -200, got %d", d.EffectivePriority)
	}
}

func TestEvaluate_Clamping_MaxEffectivePriority(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.ProtectedBoost = int32Ptr(20000)
	policy.MaxEffectivePriority = int32Ptr(5000)

	input := EvaluationInput{
		BasePriority: 500,
		Now:          now,
		RunStartTime: timePtr(now.Add(-1 * time.Minute)), // protected
	}

	d := Evaluate(input, policy)

	// 500 + 20000 = 20500, clamped to max 5000.
	if d.EffectivePriority != 5000 {
		t.Errorf("expected clamped to 5000, got %d", d.EffectivePriority)
	}
}

func TestEvaluate_Clamping_NilBounds(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.MinEffectivePriority = nil
	policy.MaxEffectivePriority = nil
	policy.PreemptibleOffset = int32Ptr(-1000)

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastCompletedCheckpointTime: timePtr(now.Add(-20 * time.Minute)), // stale
	}

	d := Evaluate(input, policy)

	// 500 + (-1000) = -500, no clamp bounds.
	if d.EffectivePriority != -500 {
		t.Errorf("expected -500 with nil bounds, got %d", d.EffectivePriority)
	}
}

func TestEvaluate_Clamping_Int32Overflow(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.ProtectedBoost = int32Ptr(math.MaxInt32)
	policy.MinEffectivePriority = nil
	policy.MaxEffectivePriority = nil

	input := EvaluationInput{
		BasePriority: math.MaxInt32,
		Now:          now,
		RunStartTime: timePtr(now.Add(-1 * time.Minute)),
	}

	d := Evaluate(input, policy)

	// MaxInt32 + MaxInt32 would overflow int32, but computation uses int64.
	// Result clamped to MaxInt32.
	if d.EffectivePriority != math.MaxInt32 {
		t.Errorf("expected MaxInt32, got %d", d.EffectivePriority)
	}
}

func TestEvaluate_Clamping_Int32Underflow(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.PreemptibleOffset = int32Ptr(math.MinInt32)
	policy.MinEffectivePriority = nil
	policy.MaxEffectivePriority = nil

	input := EvaluationInput{
		BasePriority:                math.MinInt32,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastCompletedCheckpointTime: timePtr(now.Add(-20 * time.Minute)),
	}

	d := Evaluate(input, policy)

	// MinInt32 + MinInt32 would underflow, clamped to MinInt32.
	if d.EffectivePriority != math.MinInt32 {
		t.Errorf("expected MinInt32, got %d", d.EffectivePriority)
	}
}

func TestEvaluate_Clamping_ActiveClampedToMax(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.MaxEffectivePriority = int32Ptr(300) // max < base

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastCompletedCheckpointTime: timePtr(now.Add(-2 * time.Minute)),
	}

	d := Evaluate(input, policy)

	// Active: 500 + 0 = 500, clamped to max 300.
	if d.EffectivePriority != 300 {
		t.Errorf("expected 300, got %d", d.EffectivePriority)
	}
}

// --- Evaluation order / priority tests ---

func TestEvaluate_ProtectionOverridesCooldown(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.MinRuntimeBetweenYields = metav1.Duration{Duration: 10 * time.Minute}

	input := EvaluationInput{
		BasePriority:   500,
		Now:            now,
		RunStartTime:   timePtr(now.Add(-30 * time.Minute)),
		LastResumeTime: timePtr(now.Add(-1 * time.Minute)), // resumed 1m ago
		// Both protection (5m) and cooldown (10m) are active.
		// Protection takes priority.
	}

	d := Evaluate(input, policy)

	assertDecision(t, d, DecisionStartupProtected, trainingv1alpha1.PreemptionStateProtected, 550, "WithinProtectionWindow")
}

func TestEvaluate_CooldownOverridesStaleCheckpoint(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.MinRuntimeBetweenYields = metav1.Duration{Duration: 10 * time.Minute}

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastResumeTime:              timePtr(now.Add(-6 * time.Minute)), // protection expired, cooldown active
		LastCompletedCheckpointTime: timePtr(now.Add(-20 * time.Minute)), // stale
	}

	d := Evaluate(input, policy)

	// Cooldown (6m < 10m) overrides stale checkpoint.
	assertDecision(t, d, DecisionCoolingDown, trainingv1alpha1.PreemptionStateCooldown, 525, "CooldownAfterResume")
}

func TestEvaluate_YieldBudgetOverridesCooldown(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.MinRuntimeBetweenYields = metav1.Duration{Duration: 10 * time.Minute}

	input := EvaluationInput{
		BasePriority:                500,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastResumeTime:              timePtr(now.Add(-6 * time.Minute)), // cooldown active
		RecentYieldCount:            3,                                   // exhausted
		LastCompletedCheckpointTime: timePtr(now.Add(-2 * time.Minute)),
	}

	d := Evaluate(input, policy)

	// YieldBudgetExhausted takes priority over CoolingDown.
	assertDecision(t, d, DecisionYieldBudgetExhausted, trainingv1alpha1.PreemptionStateCooldown, 525, "YieldBudgetExhausted")
}

// --- Edge cases ---

func TestEvaluate_NoTimestamps_NoCheckpoint_WithPolicy(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	input := EvaluationInput{
		BasePriority: 500,
		Now:          now,
		// All timestamps nil.
	}

	d := Evaluate(input, policy)

	// No protection window anchor → no protection.
	// No checkpoint time → TelemetryUnknown.
	// Default fail-open=true → Active.
	assertDecision(t, d, DecisionTelemetryUnknown, trainingv1alpha1.PreemptionStateActive, 500, "TelemetryUnavailableFailOpen")
}

func TestEvaluate_NegativeBasePriority(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.MinEffectivePriority = nil // no floor

	input := EvaluationInput{
		BasePriority:                -100,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastCompletedCheckpointTime: timePtr(now.Add(-20 * time.Minute)), // stale
	}

	d := Evaluate(input, policy)

	// -100 + (-100) = -200
	if d.EffectivePriority != -200 {
		t.Errorf("expected -200, got %d", d.EffectivePriority)
	}
}

func TestEvaluate_ZeroBasePriority(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()

	input := EvaluationInput{
		BasePriority:                0,
		Now:                         now,
		RunStartTime:                timePtr(now.Add(-30 * time.Minute)),
		LastCompletedCheckpointTime: timePtr(now.Add(-2 * time.Minute)),
	}

	d := Evaluate(input, policy)

	if d.EffectivePriority != 0 {
		t.Errorf("expected 0 for active with zero base, got %d", d.EffectivePriority)
	}
}

func TestEvaluate_ProtectionWithNilBoost(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.ProtectedBoost = nil // nil defaults to 0

	input := EvaluationInput{
		BasePriority: 500,
		Now:          now,
		RunStartTime: timePtr(now.Add(-1 * time.Minute)),
	}

	d := Evaluate(input, policy)

	// Protected with nil boost → 0 adjustment.
	if d.EffectivePriority != 500 {
		t.Errorf("expected 500 with nil boost, got %d", d.EffectivePriority)
	}
}

func TestEvaluate_NegativeProtectedBoost(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	policy := defaultTestPolicy()
	policy.ProtectedBoost = int32Ptr(-50) // unusual but valid

	input := EvaluationInput{
		BasePriority: 500,
		Now:          now,
		RunStartTime: timePtr(now.Add(-1 * time.Minute)),
	}

	d := Evaluate(input, policy)

	// 500 + (-50) = 450
	if d.EffectivePriority != 450 {
		t.Errorf("expected 450, got %d", d.EffectivePriority)
	}
}

// --- computeEffectivePriority unit tests ---

func TestComputeEffectivePriority_NoClamp(t *testing.T) {
	result := computeEffectivePriority(500, -100, nil, nil)
	if result != 400 {
		t.Errorf("expected 400, got %d", result)
	}
}

func TestComputeEffectivePriority_ClampMin(t *testing.T) {
	min := int32(-200)
	result := computeEffectivePriority(100, -500, &min, nil)
	if result != -200 {
		t.Errorf("expected -200, got %d", result)
	}
}

func TestComputeEffectivePriority_ClampMax(t *testing.T) {
	max := int32(300)
	result := computeEffectivePriority(200, 500, nil, &max)
	if result != 300 {
		t.Errorf("expected 300, got %d", result)
	}
}

func TestComputeEffectivePriority_ClampBoth(t *testing.T) {
	min := int32(-100)
	max := int32(1000)

	// Within bounds.
	if r := computeEffectivePriority(500, 0, &min, &max); r != 500 {
		t.Errorf("expected 500, got %d", r)
	}
	// Below min.
	if r := computeEffectivePriority(0, -200, &min, &max); r != -100 {
		t.Errorf("expected -100, got %d", r)
	}
	// Above max.
	if r := computeEffectivePriority(500, 600, &min, &max); r != 1000 {
		t.Errorf("expected 1000, got %d", r)
	}
}

func TestComputeEffectivePriority_Int32Overflow(t *testing.T) {
	result := computeEffectivePriority(math.MaxInt32, 1, nil, nil)
	if result != math.MaxInt32 {
		t.Errorf("expected MaxInt32, got %d", result)
	}
}

func TestComputeEffectivePriority_Int32Underflow(t *testing.T) {
	result := computeEffectivePriority(math.MinInt32, -1, nil, nil)
	if result != math.MinInt32 {
		t.Errorf("expected MinInt32, got %d", result)
	}
}

// --- deref helper tests ---

func TestDerefInt32(t *testing.T) {
	if v := derefInt32(nil); v != 0 {
		t.Errorf("expected 0, got %d", v)
	}
	v := int32(42)
	if r := derefInt32(&v); r != 42 {
		t.Errorf("expected 42, got %d", r)
	}
}

func TestDerefBool(t *testing.T) {
	if v := derefBool(nil); v != false {
		t.Errorf("expected false, got %v", v)
	}
	v := true
	if r := derefBool(&v); r != true {
		t.Errorf("expected true, got %v", r)
	}
}

// --- Assertion helper ---

func assertDecision(t *testing.T, d Decision, state DecisionState, preemptionState trainingv1alpha1.PreemptionState, effectivePriority int32, reason string) {
	t.Helper()
	if d.State != state {
		t.Errorf("State: expected %s, got %s", state, d.State)
	}
	if d.PreemptionState != preemptionState {
		t.Errorf("PreemptionState: expected %s, got %s", preemptionState, d.PreemptionState)
	}
	if d.EffectivePriority != effectivePriority {
		t.Errorf("EffectivePriority: expected %d, got %d", effectivePriority, d.EffectivePriority)
	}
	if d.Reason != reason {
		t.Errorf("Reason: expected %s, got %s", reason, d.Reason)
	}
	if d.Message == "" {
		t.Error("Message should not be empty")
	}
}
