package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PreemptionState describes the current preemption lifecycle state of the RTJ
// as determined by the checkpoint-aware priority shaping controller.
// +kubebuilder:validation:Enum=Protected;Active;Cooldown;Preemptible
type PreemptionState string

const (
	// PreemptionStateProtected indicates the job is within its startup
	// protection window and will not receive checkpoint-staleness penalties.
	PreemptionStateProtected PreemptionState = "Protected"

	// PreemptionStateActive indicates the job is running normally with
	// checkpoint-aware priority shaping active.
	PreemptionStateActive PreemptionState = "Active"

	// PreemptionStateCooldown indicates the job recently yielded and is in
	// a cooldown period with a priority boost.
	PreemptionStateCooldown PreemptionState = "Cooldown"

	// PreemptionStatePreemptible indicates the job has a stale checkpoint
	// or has exceeded yield budgets and has reduced effective priority.
	PreemptionStatePreemptible PreemptionState = "Preemptible"
)

const (
	// DefaultFailOpenOnTelemetryLoss preserves base priority when checkpoint
	// telemetry is unavailable (fail-safe: no silent demotion).
	DefaultFailOpenOnTelemetryLoss = true

	// DefaultFailOpenOnCheckpointStoreErrors preserves base priority when the
	// checkpoint store is unreachable.
	DefaultFailOpenOnCheckpointStoreErrors = false

	// DefaultProtectedBoost is the priority boost applied during the startup
	// protection window.
	DefaultProtectedBoost int32 = 0

	// DefaultCooldownBoost is the priority boost applied during the post-yield
	// cooldown period.
	DefaultCooldownBoost int32 = 0

	// DefaultStaleCheckpointBoost is the priority offset applied when the
	// checkpoint exceeds the freshness target.
	DefaultStaleCheckpointBoost int32 = 0

	// DefaultPreemptibleOffset is the offset applied when a job is in
	// Preemptible state (typically negative to lower priority).
	DefaultPreemptibleOffset int32 = 0

	// MaxPriorityBound is the practical safe bound for int32 priority values.
	// Keeps priority arithmetic safe from overflow.
	MaxPriorityBound int32 = 1000000000

	// MinPriorityBound is the practical safe lower bound for int32 priority values.
	MinPriorityBound int32 = -1000000000
)

// CheckpointPriorityPolicySpec defines the configuration for checkpoint-aware
// priority shaping. The priority shaping controller uses these parameters to
// compute an effective priority for RTJ-backed Kueue Workloads.
//
// Effective priority formula:
//
//	effective_priority = clamp(
//	    base_priority + state_adjustment,
//	    minEffectivePriority,
//	    maxEffectivePriority,
//	)
//
// Where state_adjustment depends on the preemption state:
//   - Protected: +protectedBoost
//   - Active: 0 (or staleCheckpointBoost when checkpoint is stale)
//   - Cooldown: +cooldownBoost
//   - Preemptible: +preemptibleOffset (typically negative)
//
// The base_priority comes from the RTJ's WorkloadPriorityClass, which remains
// immutable and owned by Kueue. The effective priority is a derived value
// written to Workload.Spec.Priority by the priority shaping controller.
type CheckpointPriorityPolicySpec struct {
	// CheckpointFreshnessTarget is the maximum acceptable age for the latest
	// completed checkpoint. When the checkpoint is older than this duration,
	// the job transitions from Active to Preemptible state.
	// Required.
	CheckpointFreshnessTarget metav1.Duration `json:"checkpointFreshnessTarget"`

	// StartupProtectionWindow is the duration after a job starts or resumes
	// during which it is shielded from checkpoint-staleness-driven priority
	// reduction. This gives the job time to produce its first checkpoint.
	// The protection window resets on every resume.
	// Required.
	StartupProtectionWindow metav1.Duration `json:"startupProtectionWindow"`

	// MinRuntimeBetweenYields is the minimum duration a job must run between
	// successive yields. This prevents thrashing where a job is preempted
	// immediately after resuming before it can make useful progress.
	// Required.
	MinRuntimeBetweenYields metav1.Duration `json:"minRuntimeBetweenYields"`

	// MaxYieldsPerWindow limits the number of times a job can be preempted
	// within the yieldWindow duration. When exceeded, the job enters Cooldown
	// state with a cooldownBoost to protect it from further preemption.
	// When zero, yield counting is disabled.
	// +optional
	MaxYieldsPerWindow int32 `json:"maxYieldsPerWindow,omitempty"`

	// YieldWindow is the sliding time window over which yields are counted
	// for the maxYieldsPerWindow budget. Required when maxYieldsPerWindow > 0.
	// +optional
	YieldWindow *metav1.Duration `json:"yieldWindow,omitempty"`

	// FailOpenOnTelemetryLoss controls behavior when checkpoint telemetry
	// (timestamps, manifest metadata) is temporarily unavailable.
	// When true (default), the job keeps its base priority (no penalty).
	// When false, the job is treated as having a stale checkpoint.
	// Default: true (fail-safe: no silent demotion on I/O failure).
	// +optional
	FailOpenOnTelemetryLoss *bool `json:"failOpenOnTelemetryLoss,omitempty"`

	// FailOpenOnCheckpointStoreErrors controls behavior when the checkpoint
	// store (S3, GCS, etc.) is temporarily unreachable.
	// When true, the job keeps its base priority.
	// When false (default), the job is treated as having a stale checkpoint.
	// Default: false.
	// +optional
	FailOpenOnCheckpointStoreErrors *bool `json:"failOpenOnCheckpointStoreErrors,omitempty"`

	// ProtectedBoost is the priority offset added to base priority while the
	// job is in Protected state (within the startup protection window).
	// Must be within [-1000000000, 1000000000].
	// Default: 0.
	// +optional
	ProtectedBoost *int32 `json:"protectedBoost,omitempty"`

	// CooldownBoost is the priority offset added to base priority while the
	// job is in Cooldown state (exceeded yield budget). Typically positive to
	// protect a frequently-preempted job from further preemption.
	// Must be within [-1000000000, 1000000000].
	// Default: 0.
	// +optional
	CooldownBoost *int32 `json:"cooldownBoost,omitempty"`

	// StaleCheckpointBoost is the priority offset applied when the job's
	// latest checkpoint exceeds the checkpointFreshnessTarget but the job is
	// still in Active state (not yet transitioned to Preemptible).
	// Typically negative to gradually lower priority of stale jobs.
	// Must be within [-1000000000, 1000000000].
	// Default: 0.
	// +optional
	StaleCheckpointBoost *int32 `json:"staleCheckpointBoost,omitempty"`

	// PreemptibleOffset is the priority offset applied when the job is in
	// Preemptible state. Typically negative to make stale-checkpoint jobs
	// more likely to be preempted. Negative values are explicitly allowed.
	// Must be within [-1000000000, 1000000000].
	// Default: 0.
	// +optional
	PreemptibleOffset *int32 `json:"preemptibleOffset,omitempty"`

	// MinEffectivePriority is the floor for the computed effective priority.
	// The effective priority will never go below this value regardless of
	// penalties and offsets. Must be <= maxEffectivePriority when both are set.
	// Must be within [-1000000000, 1000000000].
	// +optional
	MinEffectivePriority *int32 `json:"minEffectivePriority,omitempty"`

	// MaxEffectivePriority is the ceiling for the computed effective priority.
	// The effective priority will never exceed this value regardless of boosts.
	// Must be >= minEffectivePriority when both are set.
	// Must be within [-1000000000, 1000000000].
	// +optional
	MaxEffectivePriority *int32 `json:"maxEffectivePriority,omitempty"`
}

// CheckpointPriorityPolicyStatus defines the observed state of CheckpointPriorityPolicy.
type CheckpointPriorityPolicyStatus struct {
	// Conditions hold the latest available observations of the policy's state.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=cpp
// +kubebuilder:printcolumn:name="FreshnessTarget",type=string,JSONPath=`.spec.checkpointFreshnessTarget`
// +kubebuilder:printcolumn:name="ProtectionWindow",type=string,JSONPath=`.spec.startupProtectionWindow`
// +kubebuilder:printcolumn:name="FailOpenTelemetry",type=boolean,JSONPath=`.spec.failOpenOnTelemetryLoss`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// CheckpointPriorityPolicy is a cluster-scoped configuration object for
// checkpoint-aware priority shaping. It is referenced by ResumableTrainingJob
// resources via spec.priorityPolicyRef to enable dynamic effective priority
// derivation based on checkpoint freshness and yield budgets.
//
// When no CheckpointPriorityPolicy is referenced by an RTJ, the RTJ retains
// its base WorkloadPriorityClass priority with no dynamic shaping (Phase 4
// behavior preserved).
type CheckpointPriorityPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CheckpointPriorityPolicySpec   `json:"spec,omitempty"`
	Status CheckpointPriorityPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CheckpointPriorityPolicyList contains a list of CheckpointPriorityPolicy.
type CheckpointPriorityPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CheckpointPriorityPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CheckpointPriorityPolicy{}, &CheckpointPriorityPolicyList{})
}

// Default applies safe default values to the CheckpointPriorityPolicy.
func (p *CheckpointPriorityPolicy) Default() {
	if p.Spec.FailOpenOnTelemetryLoss == nil {
		d := DefaultFailOpenOnTelemetryLoss
		p.Spec.FailOpenOnTelemetryLoss = &d
	}
	if p.Spec.FailOpenOnCheckpointStoreErrors == nil {
		d := DefaultFailOpenOnCheckpointStoreErrors
		p.Spec.FailOpenOnCheckpointStoreErrors = &d
	}
	if p.Spec.ProtectedBoost == nil {
		d := DefaultProtectedBoost
		p.Spec.ProtectedBoost = &d
	}
	if p.Spec.CooldownBoost == nil {
		d := DefaultCooldownBoost
		p.Spec.CooldownBoost = &d
	}
	if p.Spec.StaleCheckpointBoost == nil {
		d := DefaultStaleCheckpointBoost
		p.Spec.StaleCheckpointBoost = &d
	}
	if p.Spec.PreemptibleOffset == nil {
		d := DefaultPreemptibleOffset
		p.Spec.PreemptibleOffset = &d
	}
}

// PriorityShapingStatus holds the controller-published priority shaping state
// for an RTJ. Added to ResumableTrainingJobStatus in Phase 5.
type PriorityShapingStatus struct {
	// BasePriority is the static priority from the WorkloadPriorityClass.
	// This value is resolved from the Kueue WorkloadPriorityClass referenced
	// by the RTJ's spec.workloadPriorityClassName. It does not change during
	// the lifetime of the RTJ.
	// +optional
	BasePriority int32 `json:"basePriority,omitempty"`

	// EffectivePriority is the dynamically computed priority written to
	// Workload.Spec.Priority by the priority shaping controller.
	// +optional
	EffectivePriority int32 `json:"effectivePriority,omitempty"`

	// PreemptionState is the current preemption lifecycle state.
	// +optional
	PreemptionState PreemptionState `json:"preemptionState,omitempty"`

	// PreemptionStateReason is a machine-readable reason for the current
	// preemption state (e.g., "WithinProtectionWindow", "CheckpointStale",
	// "YieldBudgetExceeded").
	// +optional
	PreemptionStateReason string `json:"preemptionStateReason,omitempty"`

	// ProtectedUntil is the timestamp until which the startup protection
	// window is active. Nil when no policy is attached or the job is not
	// in Protected state.
	// +optional
	ProtectedUntil *metav1.Time `json:"protectedUntil,omitempty"`

	// LastCompletedCheckpointTime is the completion timestamp of the most
	// recent checkpoint, as observed by the priority shaping controller.
	// +optional
	LastCompletedCheckpointTime *metav1.Time `json:"lastCompletedCheckpointTime,omitempty"`

	// CheckpointAge is the age of the most recent checkpoint as a duration
	// string (e.g., "5m30s"). Computed at evaluation time for observability.
	// +optional
	CheckpointAge string `json:"checkpointAge,omitempty"`

	// LastYieldTime is the timestamp of the most recent yield (preemption).
	// +optional
	LastYieldTime *metav1.Time `json:"lastYieldTime,omitempty"`

	// LastResumeTime is the timestamp of the most recent resume.
	// +optional
	LastResumeTime *metav1.Time `json:"lastResumeTime,omitempty"`

	// RecentYieldCount is the number of yields within the policy's
	// yieldWindow. Zero when yield counting is disabled.
	// +optional
	RecentYieldCount int32 `json:"recentYieldCount,omitempty"`

	// AppliedPolicyRef records the name of the CheckpointPriorityPolicy
	// that was last used to compute the effective priority. Empty when no
	// policy is attached.
	// +optional
	AppliedPolicyRef string `json:"appliedPolicyRef,omitempty"`
}

func (s PreemptionState) String() string {
	return string(s)
}
