package checkpointpriority

import (
	"testing"
	"time"
)

func timePtr(t time.Time) *time.Time {
	return &t
}

// --- CheckProtectionWindow tests ---

func TestCheckProtectionWindow_WithinWindow(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	start := now.Add(-2 * time.Minute) // started 2 min ago
	duration := 5 * time.Minute

	result := CheckProtectionWindow(now, timePtr(start), nil, duration)

	if !result.Protected {
		t.Error("expected Protected=true within window")
	}
	expectedExpiry := start.Add(duration)
	if !result.ProtectedUntil.Equal(expectedExpiry) {
		t.Errorf("expected ProtectedUntil=%v, got %v", expectedExpiry, result.ProtectedUntil)
	}
	if !result.Anchor.Equal(start) {
		t.Errorf("expected Anchor=%v, got %v", start, result.Anchor)
	}
}

func TestCheckProtectionWindow_Expired(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	start := now.Add(-10 * time.Minute) // started 10 min ago
	duration := 5 * time.Minute

	result := CheckProtectionWindow(now, timePtr(start), nil, duration)

	if result.Protected {
		t.Error("expected Protected=false after window expired")
	}
}

func TestCheckProtectionWindow_ExactExpiry(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	start := now.Add(-5 * time.Minute) // started exactly 5 min ago
	duration := 5 * time.Minute

	result := CheckProtectionWindow(now, timePtr(start), nil, duration)

	// now == expiresAt, and we use Before (strict), so not protected.
	if result.Protected {
		t.Error("expected Protected=false at exact expiry (boundary is exclusive)")
	}
}

func TestCheckProtectionWindow_ResumeResetsWindow(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	start := now.Add(-10 * time.Minute)  // started 10 min ago (expired)
	resume := now.Add(-2 * time.Minute)  // resumed 2 min ago (within window)
	duration := 5 * time.Minute

	result := CheckProtectionWindow(now, timePtr(start), timePtr(resume), duration)

	if !result.Protected {
		t.Error("expected Protected=true: resume should reset the window")
	}
	if !result.Anchor.Equal(resume) {
		t.Errorf("expected anchor to be resume time %v, got %v", resume, result.Anchor)
	}
}

func TestCheckProtectionWindow_ResumeBeforeStart_UsesStart(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	start := now.Add(-2 * time.Minute)
	resume := now.Add(-8 * time.Minute) // resume before start (unusual but possible)
	duration := 5 * time.Minute

	result := CheckProtectionWindow(now, timePtr(start), timePtr(resume), duration)

	if !result.Protected {
		t.Error("expected Protected=true: start is later than resume")
	}
	if !result.Anchor.Equal(start) {
		t.Errorf("expected anchor to be start time %v, got %v", start, result.Anchor)
	}
}

func TestCheckProtectionWindow_NilBothTimes(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	result := CheckProtectionWindow(now, nil, nil, 5*time.Minute)

	if result.Protected {
		t.Error("expected Protected=false when both times are nil")
	}
}

func TestCheckProtectionWindow_NilStartTimeUsesResume(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	resume := now.Add(-1 * time.Minute)
	result := CheckProtectionWindow(now, nil, timePtr(resume), 5*time.Minute)

	if !result.Protected {
		t.Error("expected Protected=true when only resume time is set")
	}
}

func TestCheckProtectionWindow_NilResumeTimeUsesStart(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	start := now.Add(-1 * time.Minute)
	result := CheckProtectionWindow(now, timePtr(start), nil, 5*time.Minute)

	if !result.Protected {
		t.Error("expected Protected=true when only start time is set")
	}
}

func TestCheckProtectionWindow_ZeroDuration(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	start := now.Add(-1 * time.Minute)
	result := CheckProtectionWindow(now, timePtr(start), nil, 0)

	if result.Protected {
		t.Error("expected Protected=false when duration is zero")
	}
}

func TestCheckProtectionWindow_NegativeDuration(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	start := now.Add(-1 * time.Minute)
	result := CheckProtectionWindow(now, timePtr(start), nil, -5*time.Minute)

	if result.Protected {
		t.Error("expected Protected=false when duration is negative")
	}
}

// --- CheckCooldown tests ---

func TestCheckCooldown_WithinCooldown(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	resume := now.Add(-30 * time.Second) // resumed 30s ago
	minRuntime := 2 * time.Minute

	if !CheckCooldown(now, timePtr(resume), minRuntime) {
		t.Error("expected cooldown=true within minRuntimeBetweenYields")
	}
}

func TestCheckCooldown_CooldownExpired(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	resume := now.Add(-5 * time.Minute) // resumed 5 min ago
	minRuntime := 2 * time.Minute

	if CheckCooldown(now, timePtr(resume), minRuntime) {
		t.Error("expected cooldown=false after minRuntimeBetweenYields expired")
	}
}

func TestCheckCooldown_ExactExpiry(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	resume := now.Add(-2 * time.Minute) // resumed exactly 2 min ago
	minRuntime := 2 * time.Minute

	// now == resume + minRuntime, Before is strict, so cooldown is over.
	if CheckCooldown(now, timePtr(resume), minRuntime) {
		t.Error("expected cooldown=false at exact expiry")
	}
}

func TestCheckCooldown_NilResumeTime(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	if CheckCooldown(now, nil, 2*time.Minute) {
		t.Error("expected cooldown=false when resume time is nil")
	}
}

func TestCheckCooldown_ZeroMinRuntime(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	resume := now.Add(-10 * time.Second)
	if CheckCooldown(now, timePtr(resume), 0) {
		t.Error("expected cooldown=false when minRuntimeBetweenYields is zero")
	}
}

func TestCheckCooldown_NegativeMinRuntime(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	resume := now.Add(-10 * time.Second)
	if CheckCooldown(now, timePtr(resume), -1*time.Minute) {
		t.Error("expected cooldown=false when minRuntimeBetweenYields is negative")
	}
}

// --- IsYieldBudgetExhausted tests ---

func TestIsYieldBudgetExhausted_Exhausted(t *testing.T) {
	if !IsYieldBudgetExhausted(3, 3) {
		t.Error("expected exhausted when count == max")
	}
}

func TestIsYieldBudgetExhausted_Exceeded(t *testing.T) {
	if !IsYieldBudgetExhausted(5, 3) {
		t.Error("expected exhausted when count > max")
	}
}

func TestIsYieldBudgetExhausted_NotExhausted(t *testing.T) {
	if IsYieldBudgetExhausted(2, 3) {
		t.Error("expected not exhausted when count < max")
	}
}

func TestIsYieldBudgetExhausted_ZeroMax(t *testing.T) {
	if IsYieldBudgetExhausted(5, 0) {
		t.Error("expected not exhausted when max is zero (disabled)")
	}
}

func TestIsYieldBudgetExhausted_NegativeMax(t *testing.T) {
	if IsYieldBudgetExhausted(1, -1) {
		t.Error("expected not exhausted when max is negative")
	}
}

func TestIsYieldBudgetExhausted_ZeroCount(t *testing.T) {
	if IsYieldBudgetExhausted(0, 3) {
		t.Error("expected not exhausted when count is zero")
	}
}

// --- CheckCheckpointFreshness tests ---

func TestCheckCheckpointFreshness_Fresh(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	ckpt := now.Add(-3 * time.Minute) // 3 min old
	target := 10 * time.Minute

	age, stale := CheckCheckpointFreshness(now, timePtr(ckpt), target)

	if stale {
		t.Error("expected fresh checkpoint")
	}
	if age != 3*time.Minute {
		t.Errorf("expected age 3m, got %s", age)
	}
}

func TestCheckCheckpointFreshness_Stale(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	ckpt := now.Add(-15 * time.Minute) // 15 min old
	target := 10 * time.Minute

	age, stale := CheckCheckpointFreshness(now, timePtr(ckpt), target)

	if !stale {
		t.Error("expected stale checkpoint")
	}
	if age != 15*time.Minute {
		t.Errorf("expected age 15m, got %s", age)
	}
}

func TestCheckCheckpointFreshness_ExactlyAtTarget(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	ckpt := now.Add(-10 * time.Minute) // exactly at target
	target := 10 * time.Minute

	_, stale := CheckCheckpointFreshness(now, timePtr(ckpt), target)

	// age == target, and we use > (strict), so it's still fresh.
	if stale {
		t.Error("expected fresh at exact target boundary (stale requires > target)")
	}
}

func TestCheckCheckpointFreshness_NilCheckpointTime(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	age, stale := CheckCheckpointFreshness(now, nil, 10*time.Minute)

	if stale {
		t.Error("expected not stale when checkpoint time is nil")
	}
	if age != 0 {
		t.Errorf("expected age 0, got %s", age)
	}
}

func TestCheckCheckpointFreshness_FutureCheckpoint(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	ckpt := now.Add(5 * time.Minute) // future checkpoint (clock skew)
	target := 10 * time.Minute

	age, stale := CheckCheckpointFreshness(now, timePtr(ckpt), target)

	if stale {
		t.Error("expected not stale for future checkpoint")
	}
	if age != 0 {
		t.Errorf("expected age clamped to 0, got %s", age)
	}
}

func TestCheckCheckpointFreshness_VeryStale(t *testing.T) {
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	ckpt := now.Add(-2 * time.Hour) // 2 hours old
	target := 10 * time.Minute

	age, stale := CheckCheckpointFreshness(now, timePtr(ckpt), target)

	if !stale {
		t.Error("expected stale for very old checkpoint")
	}
	if age != 2*time.Hour {
		t.Errorf("expected age 2h, got %s", age)
	}
}
