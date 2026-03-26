package checkpointpriority

import (
	"time"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// DecisionState is the internal decision state produced by the priority
// decision engine. It provides more granular reasons than the API's
// PreemptionState enum (Protected, Active, Cooldown, Preemptible).
//
// Each DecisionState maps to exactly one PreemptionState and one priority
// adjustment, except TelemetryUnknown which branches on the fail-open policy.
type DecisionState string

const (
	// DecisionDisabled indicates no CheckpointPriorityPolicy is attached.
	// The job retains its base WorkloadPriorityClass priority with no
	// preemption state set. This preserves Phase 4 behavior.
	DecisionDisabled DecisionState = "Disabled"

	// DecisionStartupProtected indicates the job is within its startup
	// protection window. The protection window resets on every resume.
	// Maps to PreemptionState=Protected with protectedBoost adjustment.
	DecisionStartupProtected DecisionState = "StartupProtected"

	// DecisionActive indicates the job is running normally with a fresh
	// checkpoint. No priority adjustment is applied.
	// Maps to PreemptionState=Active with zero adjustment.
	DecisionActive DecisionState = "Active"

	// DecisionCheckpointStale indicates the job's latest checkpoint age
	// exceeds the configured freshness target. The preemptibleOffset is
	// applied to lower the job's effective priority.
	// Maps to PreemptionState=Preemptible with preemptibleOffset adjustment.
	DecisionCheckpointStale DecisionState = "CheckpointStale"

	// DecisionCoolingDown indicates the job is within the cooldown period
	// after resuming from a yield. The cooldownBoost is applied to prevent
	// immediate re-preemption (anti-thrashing).
	// Maps to PreemptionState=Cooldown with cooldownBoost adjustment.
	DecisionCoolingDown DecisionState = "CoolingDown"

	// DecisionYieldBudgetExhausted indicates the job has reached or
	// exceeded the maximum number of yields within the configured window.
	// The cooldownBoost is applied to protect the job from further preemption.
	// Maps to PreemptionState=Cooldown with cooldownBoost adjustment.
	DecisionYieldBudgetExhausted DecisionState = "YieldBudgetExhausted"

	// DecisionTelemetryUnknown indicates checkpoint telemetry is unavailable
	// (no checkpoint time from status or catalog). The fail-open/fail-closed
	// policy determines whether the job keeps its base priority (Active) or
	// is treated as preemptible (Preemptible with preemptibleOffset).
	DecisionTelemetryUnknown DecisionState = "TelemetryUnknown"

	// DecisionPreemptible indicates the job is in a preemptible state for
	// reasons not covered by the other specific states. Reserved for future
	// preemption triggers beyond checkpoint staleness.
	// Maps to PreemptionState=Preemptible with preemptibleOffset adjustment.
	DecisionPreemptible DecisionState = "Preemptible"
)

// EvaluationInput contains all data needed by the priority decision engine
// to compute the effective priority and preemption state.
//
// The controller populates this from the TelemetrySnapshot and resolved
// CheckpointPriorityPolicy. The engine operates on plain Go types to
// avoid circular dependencies with the controller package.
type EvaluationInput struct {
	// BasePriority is the static priority from the WorkloadPriorityClass.
	BasePriority int32

	// Now is the evaluation timestamp.
	Now time.Time

	// LastCompletedCheckpointTime is the completion time of the latest
	// checkpoint. Nil when no checkpoint telemetry is available (either
	// the job has never checkpointed or the telemetry source failed).
	LastCompletedCheckpointTime *time.Time

	// RunStartTime is when the current run attempt started (transitioned
	// to Starting or Running). Used as the anchor for the startup
	// protection window when no resume has occurred.
	RunStartTime *time.Time

	// LastResumeTime is when the job last resumed from a checkpoint
	// (Restoring → Running transition). Takes precedence over RunStartTime
	// as the protection window anchor since the protection window resets
	// on every resume.
	LastResumeTime *time.Time

	// LastYieldTime is when the job most recently yielded (preempted).
	LastYieldTime *time.Time

	// RecentYieldCount is the number of yields within the policy's
	// yield window. Computed by the telemetry collector from the yield
	// history annotation.
	RecentYieldCount int32

	// CheckpointStoreError indicates the checkpoint store was unreachable
	// during telemetry collection. When true, the failOpenOnCheckpointStoreErrors
	// policy flag determines the engine's behavior.
	CheckpointStoreError bool
}

// Decision is the output of the priority decision engine. It contains the
// computed effective priority, the preemption state, and a human-readable
// explanation of why the engine made this decision.
type Decision struct {
	// State is the internal decision state (the reason for the decision).
	// More granular than PreemptionState.
	State DecisionState

	// PreemptionState is the API-level preemption state to write to
	// RTJ status.priorityShaping.preemptionState. Empty when State is
	// DecisionDisabled (no policy attached).
	PreemptionState trainingv1alpha1.PreemptionState

	// EffectivePriority is the computed priority to write to
	// Workload.Spec.Priority.
	EffectivePriority int32

	// Reason is a machine-readable reason string for the decision.
	// Written to status.priorityShaping.preemptionStateReason.
	Reason string

	// Message is a human-readable explanation of the decision including
	// relevant durations and thresholds.
	Message string

	// ProtectedUntil is the time until which the startup protection
	// window is active. Nil when not in the StartupProtected state.
	ProtectedUntil *time.Time
}
