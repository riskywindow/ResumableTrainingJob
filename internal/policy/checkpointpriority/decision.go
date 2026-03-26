package checkpointpriority

import (
	"fmt"
	"math"
	"time"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// Evaluate runs the priority decision engine. It takes the evaluation input
// (base priority, timestamps, telemetry) and the resolved policy spec,
// and returns a Decision with the computed effective priority and preemption
// state.
//
// If policy is nil, the engine returns DecisionDisabled with the base
// priority unchanged. This preserves Phase 4 behavior.
//
// Evaluation order (first matching state wins):
//  1. Disabled:             policy is nil
//  2. StartupProtected:     within startup protection window
//  3. YieldBudgetExhausted: yield count >= maxYieldsPerWindow
//  4. CoolingDown:          within minRuntimeBetweenYields since last resume
//  5. TelemetryUnknown:     no checkpoint telemetry available
//  6. CheckpointStale:      checkpoint age > freshness target
//  7. Active:               checkpoint is fresh, normal operation
//
// The effective priority formula for all states is:
//
//	effective = clamp(base + adjustment, minEffectivePriority, maxEffectivePriority)
//
// Where adjustment depends on the decision state (see state-to-adjustment
// mapping in docs/phase5/policy-engine.md).
func Evaluate(input EvaluationInput, policy *trainingv1alpha1.CheckpointPriorityPolicySpec) Decision {
	// 1. Disabled: no policy attached.
	if policy == nil {
		return Decision{
			State:             DecisionDisabled,
			EffectivePriority: input.BasePriority,
			Reason:            "PolicyDisabled",
			Message:           "No CheckpointPriorityPolicy attached; using base priority",
		}
	}

	// 2. StartupProtected: within startup protection window.
	pw := CheckProtectionWindow(
		input.Now,
		input.RunStartTime,
		input.LastResumeTime,
		policy.StartupProtectionWindow.Duration,
	)
	if pw.Protected {
		adj := derefInt32(policy.ProtectedBoost)
		eff := computeEffectivePriority(input.BasePriority, adj, policy.MinEffectivePriority, policy.MaxEffectivePriority)
		protectedUntil := pw.ProtectedUntil
		return Decision{
			State:             DecisionStartupProtected,
			PreemptionState:   trainingv1alpha1.PreemptionStateProtected,
			EffectivePriority: eff,
			Reason:            "WithinProtectionWindow",
			Message:           fmt.Sprintf("Job is within startup protection window (expires at %s)", protectedUntil.UTC().Format(time.RFC3339)),
			ProtectedUntil:    &protectedUntil,
		}
	}

	// 3. YieldBudgetExhausted: yield count >= maxYieldsPerWindow.
	if IsYieldBudgetExhausted(input.RecentYieldCount, policy.MaxYieldsPerWindow) {
		adj := derefInt32(policy.CooldownBoost)
		eff := computeEffectivePriority(input.BasePriority, adj, policy.MinEffectivePriority, policy.MaxEffectivePriority)
		return Decision{
			State:             DecisionYieldBudgetExhausted,
			PreemptionState:   trainingv1alpha1.PreemptionStateCooldown,
			EffectivePriority: eff,
			Reason:            "YieldBudgetExhausted",
			Message:           fmt.Sprintf("Yield budget exhausted (%d yields in window, max %d)", input.RecentYieldCount, policy.MaxYieldsPerWindow),
		}
	}

	// 4. CoolingDown: within minRuntimeBetweenYields since last resume.
	if CheckCooldown(input.Now, input.LastResumeTime, policy.MinRuntimeBetweenYields.Duration) {
		adj := derefInt32(policy.CooldownBoost)
		eff := computeEffectivePriority(input.BasePriority, adj, policy.MinEffectivePriority, policy.MaxEffectivePriority)
		elapsed := input.Now.Sub(*input.LastResumeTime)
		return Decision{
			State:             DecisionCoolingDown,
			PreemptionState:   trainingv1alpha1.PreemptionStateCooldown,
			EffectivePriority: eff,
			Reason:            "CooldownAfterResume",
			Message:           fmt.Sprintf("Job is in cooldown period (%s since last resume, min %s)", elapsed.Round(time.Second), policy.MinRuntimeBetweenYields.Duration),
		}
	}

	// 5. TelemetryUnknown: no checkpoint telemetry available.
	if input.LastCompletedCheckpointTime == nil {
		return evaluateTelemetryUnknown(input, policy)
	}

	// 6. CheckpointStale: checkpoint age > freshness target.
	age, stale := CheckCheckpointFreshness(input.Now, input.LastCompletedCheckpointTime, policy.CheckpointFreshnessTarget.Duration)
	if stale {
		adj := derefInt32(policy.PreemptibleOffset)
		eff := computeEffectivePriority(input.BasePriority, adj, policy.MinEffectivePriority, policy.MaxEffectivePriority)
		return Decision{
			State:             DecisionCheckpointStale,
			PreemptionState:   trainingv1alpha1.PreemptionStatePreemptible,
			EffectivePriority: eff,
			Reason:            "CheckpointStale",
			Message:           fmt.Sprintf("Checkpoint age %s exceeds freshness target %s", age.Round(time.Second), policy.CheckpointFreshnessTarget.Duration),
		}
	}

	// 7. Active: checkpoint is fresh, normal operation.
	eff := computeEffectivePriority(input.BasePriority, 0, policy.MinEffectivePriority, policy.MaxEffectivePriority)
	return Decision{
		State:             DecisionActive,
		PreemptionState:   trainingv1alpha1.PreemptionStateActive,
		EffectivePriority: eff,
		Reason:            "CheckpointFresh",
		Message:           fmt.Sprintf("Checkpoint is fresh (age %s, target %s)", age.Round(time.Second), policy.CheckpointFreshnessTarget.Duration),
	}
}

// evaluateTelemetryUnknown handles the case where checkpoint telemetry is
// unavailable. The behavior depends on the fail-open/fail-closed policy
// and whether the unavailability was caused by a store error.
func evaluateTelemetryUnknown(input EvaluationInput, policy *trainingv1alpha1.CheckpointPriorityPolicySpec) Decision {
	var failOpen bool
	var reason, message string

	if input.CheckpointStoreError {
		failOpen = derefBool(policy.FailOpenOnCheckpointStoreErrors)
		if failOpen {
			reason = "StoreErrorFailOpen"
			message = "Checkpoint store error; fail-open: keeping base priority"
		} else {
			reason = "StoreErrorFailClosed"
			message = "Checkpoint store error; fail-closed: applying preemptible offset"
		}
	} else {
		failOpen = derefBool(policy.FailOpenOnTelemetryLoss)
		if failOpen {
			reason = "TelemetryUnavailableFailOpen"
			message = "Checkpoint telemetry unavailable; fail-open: keeping base priority"
		} else {
			reason = "TelemetryUnavailableFailClosed"
			message = "Checkpoint telemetry unavailable; fail-closed: applying preemptible offset"
		}
	}

	if failOpen {
		eff := computeEffectivePriority(input.BasePriority, 0, policy.MinEffectivePriority, policy.MaxEffectivePriority)
		return Decision{
			State:             DecisionTelemetryUnknown,
			PreemptionState:   trainingv1alpha1.PreemptionStateActive,
			EffectivePriority: eff,
			Reason:            reason,
			Message:           message,
		}
	}

	adj := derefInt32(policy.PreemptibleOffset)
	eff := computeEffectivePriority(input.BasePriority, adj, policy.MinEffectivePriority, policy.MaxEffectivePriority)
	return Decision{
		State:             DecisionTelemetryUnknown,
		PreemptionState:   trainingv1alpha1.PreemptionStatePreemptible,
		EffectivePriority: eff,
		Reason:            reason,
		Message:           message,
	}
}

// computeEffectivePriority applies the adjustment to the base priority and
// clamps the result to the policy's min/max bounds and int32 range.
//
// The computation uses int64 internally to prevent overflow when adding
// adjustment to base priority.
func computeEffectivePriority(basePriority int32, adjustment int32, minPriority, maxPriority *int32) int32 {
	raw := int64(basePriority) + int64(adjustment)

	// Apply policy min/max bounds (if configured).
	if minPriority != nil && raw < int64(*minPriority) {
		raw = int64(*minPriority)
	}
	if maxPriority != nil && raw > int64(*maxPriority) {
		raw = int64(*maxPriority)
	}

	// Clamp to int32 range as a safety net.
	if raw < math.MinInt32 {
		raw = math.MinInt32
	}
	if raw > math.MaxInt32 {
		raw = math.MaxInt32
	}

	return int32(raw)
}

func derefInt32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

func derefBool(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}
