package v1alpha1

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func minimalValidCPP() *CheckpointPriorityPolicy {
	return &CheckpointPriorityPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-shaping",
		},
		Spec: CheckpointPriorityPolicySpec{
			CheckpointFreshnessTarget: metav1.Duration{Duration: 10 * time.Minute},
			StartupProtectionWindow:   metav1.Duration{Duration: 5 * time.Minute},
			MinRuntimeBetweenYields:   metav1.Duration{Duration: 2 * time.Minute},
		},
	}
}

// --- Defaulting tests ---

func TestCheckpointPriorityPolicyDefaultSetsAllDefaults(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	if err := wh.Default(context.Background(), policy); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	if policy.Spec.FailOpenOnTelemetryLoss == nil || !*policy.Spec.FailOpenOnTelemetryLoss {
		t.Fatalf("expected failOpenOnTelemetryLoss to default to true")
	}
	if policy.Spec.FailOpenOnCheckpointStoreErrors == nil || *policy.Spec.FailOpenOnCheckpointStoreErrors {
		t.Fatalf("expected failOpenOnCheckpointStoreErrors to default to false")
	}
	if policy.Spec.ProtectedBoost == nil || *policy.Spec.ProtectedBoost != DefaultProtectedBoost {
		t.Fatalf("expected protectedBoost to default to %d", DefaultProtectedBoost)
	}
	if policy.Spec.CooldownBoost == nil || *policy.Spec.CooldownBoost != DefaultCooldownBoost {
		t.Fatalf("expected cooldownBoost to default to %d", DefaultCooldownBoost)
	}
	if policy.Spec.StaleCheckpointBoost == nil || *policy.Spec.StaleCheckpointBoost != 0 {
		t.Fatalf("expected staleCheckpointBoost to default to 0")
	}
	if policy.Spec.PreemptibleOffset == nil || *policy.Spec.PreemptibleOffset != DefaultPreemptibleOffset {
		t.Fatalf("expected preemptibleOffset to default to %d", DefaultPreemptibleOffset)
	}
}

func TestCheckpointPriorityPolicyDefaultPreservesExplicitValues(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	policy.Spec.FailOpenOnTelemetryLoss = ptr.To(false)
	policy.Spec.FailOpenOnCheckpointStoreErrors = ptr.To(true)
	policy.Spec.ProtectedBoost = ptr.To[int32](100)
	policy.Spec.CooldownBoost = ptr.To[int32](50)
	policy.Spec.StaleCheckpointBoost = ptr.To[int32](-25)
	policy.Spec.PreemptibleOffset = ptr.To[int32](-200)

	if err := wh.Default(context.Background(), policy); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	if *policy.Spec.FailOpenOnTelemetryLoss {
		t.Fatalf("expected failOpenOnTelemetryLoss to stay false")
	}
	if !*policy.Spec.FailOpenOnCheckpointStoreErrors {
		t.Fatalf("expected failOpenOnCheckpointStoreErrors to stay true")
	}
	if *policy.Spec.ProtectedBoost != 100 {
		t.Fatalf("expected protectedBoost to stay 100, got %d", *policy.Spec.ProtectedBoost)
	}
	if *policy.Spec.CooldownBoost != 50 {
		t.Fatalf("expected cooldownBoost to stay 50, got %d", *policy.Spec.CooldownBoost)
	}
	if *policy.Spec.StaleCheckpointBoost != -25 {
		t.Fatalf("expected staleCheckpointBoost to stay -25, got %d", *policy.Spec.StaleCheckpointBoost)
	}
	if *policy.Spec.PreemptibleOffset != -200 {
		t.Fatalf("expected preemptibleOffset to stay -200, got %d", *policy.Spec.PreemptibleOffset)
	}
}

// --- Validation: accepts ---

func TestCheckpointPriorityPolicyValidateCreateAcceptsMinimalSpec(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	policy.Default()

	if _, err := wh.ValidateCreate(context.Background(), policy); err != nil {
		t.Fatalf("expected minimal policy to pass validation, got %v", err)
	}
}

func TestCheckpointPriorityPolicyValidateCreateAcceptsFullSpec(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	policy.Spec.MaxYieldsPerWindow = 3
	policy.Spec.YieldWindow = &metav1.Duration{Duration: 1 * time.Hour}
	policy.Spec.ProtectedBoost = ptr.To[int32](100)
	policy.Spec.CooldownBoost = ptr.To[int32](50)
	policy.Spec.StaleCheckpointBoost = ptr.To[int32](-25)
	policy.Spec.PreemptibleOffset = ptr.To[int32](-200)
	policy.Spec.MinEffectivePriority = ptr.To[int32](-1000)
	policy.Spec.MaxEffectivePriority = ptr.To[int32](5000)
	policy.Default()

	if _, err := wh.ValidateCreate(context.Background(), policy); err != nil {
		t.Fatalf("expected full spec to pass validation, got %v", err)
	}
}

func TestCheckpointPriorityPolicyValidateCreateAcceptsNegativePreemptibleOffset(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	policy.Spec.PreemptibleOffset = ptr.To[int32](-500)
	policy.Default()

	if _, err := wh.ValidateCreate(context.Background(), policy); err != nil {
		t.Fatalf("expected negative preemptibleOffset to pass, got %v", err)
	}
}

func TestCheckpointPriorityPolicyValidateCreateAcceptsEqualMinMaxPriority(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	policy.Spec.MinEffectivePriority = ptr.To[int32](100)
	policy.Spec.MaxEffectivePriority = ptr.To[int32](100)
	policy.Default()

	if _, err := wh.ValidateCreate(context.Background(), policy); err != nil {
		t.Fatalf("expected equal min/max priority to pass, got %v", err)
	}
}

func TestCheckpointPriorityPolicyValidateCreateAcceptsNegativeMinEffectivePriority(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	policy.Spec.MinEffectivePriority = ptr.To[int32](-500)
	policy.Default()

	if _, err := wh.ValidateCreate(context.Background(), policy); err != nil {
		t.Fatalf("expected negative minEffectivePriority to pass, got %v", err)
	}
}

func TestCheckpointPriorityPolicyValidateCreateAcceptsYieldWindowWithoutMaxYields(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	// yieldWindow without maxYieldsPerWindow is valid (window is inert).
	policy := minimalValidCPP()
	policy.Spec.YieldWindow = &metav1.Duration{Duration: 30 * time.Minute}
	policy.Default()

	if _, err := wh.ValidateCreate(context.Background(), policy); err != nil {
		t.Fatalf("expected yieldWindow without maxYieldsPerWindow to pass, got %v", err)
	}
}

// --- Validation: rejects ---

func TestCheckpointPriorityPolicyValidateCreateRejectsZeroFreshnessTarget(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	policy.Spec.CheckpointFreshnessTarget = metav1.Duration{Duration: 0}
	policy.Default()

	_, err := wh.ValidateCreate(context.Background(), policy)
	if err == nil {
		t.Fatalf("expected validation to reject zero checkpointFreshnessTarget")
	}
	if !strings.Contains(err.Error(), "checkpointFreshnessTarget") {
		t.Fatalf("expected error about checkpointFreshnessTarget, got %v", err)
	}
}

func TestCheckpointPriorityPolicyValidateCreateRejectsNegativeProtectionWindow(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	policy.Spec.StartupProtectionWindow = metav1.Duration{Duration: -1 * time.Second}
	policy.Default()

	_, err := wh.ValidateCreate(context.Background(), policy)
	if err == nil {
		t.Fatalf("expected validation to reject negative startupProtectionWindow")
	}
	if !strings.Contains(err.Error(), "startupProtectionWindow") {
		t.Fatalf("expected error about startupProtectionWindow, got %v", err)
	}
}

func TestCheckpointPriorityPolicyValidateCreateRejectsZeroMinRuntimeBetweenYields(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	policy.Spec.MinRuntimeBetweenYields = metav1.Duration{Duration: 0}
	policy.Default()

	_, err := wh.ValidateCreate(context.Background(), policy)
	if err == nil {
		t.Fatalf("expected validation to reject zero minRuntimeBetweenYields")
	}
	if !strings.Contains(err.Error(), "minRuntimeBetweenYields") {
		t.Fatalf("expected error about minRuntimeBetweenYields, got %v", err)
	}
}

func TestCheckpointPriorityPolicyValidateCreateRejectsMaxYieldsWithoutWindow(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	policy.Spec.MaxYieldsPerWindow = 3
	// YieldWindow not set
	policy.Default()

	_, err := wh.ValidateCreate(context.Background(), policy)
	if err == nil {
		t.Fatalf("expected validation to reject maxYieldsPerWindow without yieldWindow")
	}
	if !strings.Contains(err.Error(), "yieldWindow") {
		t.Fatalf("expected error about yieldWindow, got %v", err)
	}
}

func TestCheckpointPriorityPolicyValidateCreateRejectsNegativeYieldWindow(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	policy.Spec.YieldWindow = &metav1.Duration{Duration: -1 * time.Minute}
	policy.Default()

	_, err := wh.ValidateCreate(context.Background(), policy)
	if err == nil {
		t.Fatalf("expected validation to reject negative yieldWindow")
	}
	if !strings.Contains(err.Error(), "yieldWindow") {
		t.Fatalf("expected error about yieldWindow, got %v", err)
	}
}

func TestCheckpointPriorityPolicyValidateCreateRejectsMinGreaterThanMaxPriority(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	policy.Spec.MinEffectivePriority = ptr.To[int32](500)
	policy.Spec.MaxEffectivePriority = ptr.To[int32](100)
	policy.Default()

	_, err := wh.ValidateCreate(context.Background(), policy)
	if err == nil {
		t.Fatalf("expected validation to reject minEffectivePriority > maxEffectivePriority")
	}
	if !strings.Contains(err.Error(), "minEffectivePriority") {
		t.Fatalf("expected error about minEffectivePriority, got %v", err)
	}
}

func TestCheckpointPriorityPolicyValidateCreateRejectsBoostOutOfBounds(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	policy.Spec.ProtectedBoost = ptr.To[int32](MaxPriorityBound + 1)
	policy.Default()

	_, err := wh.ValidateCreate(context.Background(), policy)
	if err == nil {
		t.Fatalf("expected validation to reject out-of-bounds protectedBoost")
	}
	if !strings.Contains(err.Error(), "protectedBoost") {
		t.Fatalf("expected error about protectedBoost, got %v", err)
	}
}

func TestCheckpointPriorityPolicyValidateCreateRejectsOffsetBelowLowerBound(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	policy.Spec.PreemptibleOffset = ptr.To[int32](MinPriorityBound - 1)
	policy.Default()

	_, err := wh.ValidateCreate(context.Background(), policy)
	if err == nil {
		t.Fatalf("expected validation to reject out-of-bounds preemptibleOffset")
	}
	if !strings.Contains(err.Error(), "preemptibleOffset") {
		t.Fatalf("expected error about preemptibleOffset, got %v", err)
	}
}

// --- Update and Delete ---

func TestCheckpointPriorityPolicyValidateUpdateAcceptsChange(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	oldPolicy := minimalValidCPP()
	oldPolicy.Default()

	newPolicy := oldPolicy.DeepCopy()
	newPolicy.Spec.CheckpointFreshnessTarget = metav1.Duration{Duration: 20 * time.Minute}

	if _, err := wh.ValidateUpdate(context.Background(), oldPolicy, newPolicy); err != nil {
		t.Fatalf("expected policy update to pass, got %v", err)
	}
}

func TestCheckpointPriorityPolicyValidateDeleteAllowed(t *testing.T) {
	wh := &CheckpointPriorityPolicyWebhook{}

	policy := minimalValidCPP()
	if _, err := wh.ValidateDelete(context.Background(), policy); err != nil {
		t.Fatalf("expected delete to be allowed, got %v", err)
	}
}

// --- Deep copy tests ---

func TestDeepCopyCheckpointPriorityPolicy(t *testing.T) {
	policy := minimalValidCPP()
	policy.Spec.MaxYieldsPerWindow = 3
	policy.Spec.YieldWindow = &metav1.Duration{Duration: 1 * time.Hour}
	policy.Spec.ProtectedBoost = ptr.To[int32](100)
	policy.Spec.MinEffectivePriority = ptr.To[int32](-500)
	policy.Spec.MaxEffectivePriority = ptr.To[int32](5000)
	policy.Default()

	cp := policy.DeepCopy()

	if cp.Spec.CheckpointFreshnessTarget.Duration != 10*time.Minute {
		t.Fatalf("expected checkpointFreshnessTarget 10m, got %v", cp.Spec.CheckpointFreshnessTarget.Duration)
	}
	if *cp.Spec.ProtectedBoost != 100 {
		t.Fatalf("expected protectedBoost 100, got %d", *cp.Spec.ProtectedBoost)
	}
	if *cp.Spec.MinEffectivePriority != -500 {
		t.Fatalf("expected minEffectivePriority -500, got %d", *cp.Spec.MinEffectivePriority)
	}

	// Verify independence.
	*cp.Spec.ProtectedBoost = 999
	if *policy.Spec.ProtectedBoost != 100 {
		t.Fatalf("mutating copy affected original")
	}
}

func TestDeepCopyPriorityShapingStatus(t *testing.T) {
	now := metav1.Now()
	orig := &PriorityShapingStatus{
		BasePriority:                100,
		EffectivePriority:           80,
		PreemptionState:             PreemptionStateActive,
		PreemptionStateReason:       "CheckpointFresh",
		ProtectedUntil:              &now,
		LastCompletedCheckpointTime: &now,
		CheckpointAge:               "5m0s",
		LastYieldTime:               &now,
		LastResumeTime:              &now,
		RecentYieldCount:            2,
		AppliedPolicyRef:            "default-shaping",
	}

	cp := orig.DeepCopy()

	if cp.BasePriority != 100 {
		t.Fatalf("expected basePriority 100, got %d", cp.BasePriority)
	}
	if cp.PreemptionState != PreemptionStateActive {
		t.Fatalf("expected preemptionState Active, got %s", cp.PreemptionState)
	}
	if cp.ProtectedUntil == nil {
		t.Fatalf("expected protectedUntil to be copied")
	}

	// Verify time pointer independence.
	laterTime := metav1.NewTime(now.Add(1 * time.Hour))
	cp.ProtectedUntil = &laterTime
	if orig.ProtectedUntil.Time.Equal(laterTime.Time) {
		t.Fatalf("mutating copy affected original time pointer")
	}
}
