package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PreemptionState describes the current preemption lifecycle state of the RTJ
// as determined by the checkpoint-aware priority shaping controller.
// +kubebuilder:validation:Enum=Protected;Active;Cooldown;Preemptible
type PreemptionState string

const (
	PreemptionStateProtected   PreemptionState = "Protected"
	PreemptionStateActive      PreemptionState = "Active"
	PreemptionStateCooldown    PreemptionState = "Cooldown"
	PreemptionStatePreemptible PreemptionState = "Preemptible"
)

const (
	DefaultFailOpenOnTelemetryLoss            = true
	DefaultFailOpenOnCheckpointStoreErrors    = false
	DefaultProtectedBoost                int32 = 0
	DefaultCooldownBoost                 int32 = 0
	DefaultStaleCheckpointBoost          int32 = 0
	DefaultPreemptibleOffset             int32 = 0
	MaxPriorityBound                     int32 = 1000000000
	MinPriorityBound                     int32 = -1000000000
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
type CheckpointPriorityPolicySpec struct {
	// CheckpointFreshnessTarget is the maximum acceptable age for the latest
	// completed checkpoint. When the checkpoint is older than this duration,
	// the job transitions from Active to Preemptible state.
	// Required.
	CheckpointFreshnessTarget metav1.Duration `json:"checkpointFreshnessTarget"`

	// StartupProtectionWindow is the duration after a job starts or resumes
	// during which it is shielded from checkpoint-staleness-driven priority
	// reduction. This gives the job time to produce its first checkpoint.
	// Required.
	StartupProtectionWindow metav1.Duration `json:"startupProtectionWindow"`

	// MinRuntimeBetweenYields is the minimum duration a job must run between
	// successive yields.
	// Required.
	MinRuntimeBetweenYields metav1.Duration `json:"minRuntimeBetweenYields"`

	// MaxYieldsPerWindow limits the number of times a job can be preempted
	// within the yieldWindow duration.
	// +optional
	MaxYieldsPerWindow int32 `json:"maxYieldsPerWindow,omitempty"`

	// YieldWindow is the sliding time window over which yields are counted.
	// Required when maxYieldsPerWindow > 0.
	// +optional
	YieldWindow *metav1.Duration `json:"yieldWindow,omitempty"`

	// FailOpenOnTelemetryLoss controls behavior when checkpoint telemetry
	// is temporarily unavailable. Default: true.
	// +optional
	FailOpenOnTelemetryLoss *bool `json:"failOpenOnTelemetryLoss,omitempty"`

	// FailOpenOnCheckpointStoreErrors controls behavior when the checkpoint
	// store is temporarily unreachable. Default: false.
	// +optional
	FailOpenOnCheckpointStoreErrors *bool `json:"failOpenOnCheckpointStoreErrors,omitempty"`

	// ProtectedBoost is the priority offset added during Protected state.
	// Must be within [-1000000000, 1000000000]. Default: 0.
	// +optional
	ProtectedBoost *int32 `json:"protectedBoost,omitempty"`

	// CooldownBoost is the priority offset added during Cooldown state.
	// Must be within [-1000000000, 1000000000]. Default: 0.
	// +optional
	CooldownBoost *int32 `json:"cooldownBoost,omitempty"`

	// StaleCheckpointBoost is the priority offset applied when the checkpoint
	// exceeds the freshness target.
	// Must be within [-1000000000, 1000000000]. Default: 0.
	// +optional
	StaleCheckpointBoost *int32 `json:"staleCheckpointBoost,omitempty"`

	// PreemptibleOffset is the priority offset applied in Preemptible state.
	// Negative values are allowed.
	// Must be within [-1000000000, 1000000000]. Default: 0.
	// +optional
	PreemptibleOffset *int32 `json:"preemptibleOffset,omitempty"`

	// MinEffectivePriority is the floor for the computed effective priority.
	// Must be within [-1000000000, 1000000000].
	// +optional
	MinEffectivePriority *int32 `json:"minEffectivePriority,omitempty"`

	// MaxEffectivePriority is the ceiling for the computed effective priority.
	// Must be within [-1000000000, 1000000000].
	// +optional
	MaxEffectivePriority *int32 `json:"maxEffectivePriority,omitempty"`
}

// CheckpointPriorityPolicyStatus defines the observed state of CheckpointPriorityPolicy.
type CheckpointPriorityPolicyStatus struct {
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
// checkpoint-aware priority shaping.
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
// for an RTJ.
type PriorityShapingStatus struct {
	// +optional
	BasePriority int32 `json:"basePriority,omitempty"`
	// +optional
	EffectivePriority int32 `json:"effectivePriority,omitempty"`
	// +optional
	PreemptionState PreemptionState `json:"preemptionState,omitempty"`
	// +optional
	PreemptionStateReason string `json:"preemptionStateReason,omitempty"`
	// +optional
	ProtectedUntil *metav1.Time `json:"protectedUntil,omitempty"`
	// +optional
	LastCompletedCheckpointTime *metav1.Time `json:"lastCompletedCheckpointTime,omitempty"`
	// +optional
	CheckpointAge string `json:"checkpointAge,omitempty"`
	// +optional
	LastYieldTime *metav1.Time `json:"lastYieldTime,omitempty"`
	// +optional
	LastResumeTime *metav1.Time `json:"lastResumeTime,omitempty"`
	// +optional
	RecentYieldCount int32 `json:"recentYieldCount,omitempty"`
	// +optional
	AppliedPolicyRef string `json:"appliedPolicyRef,omitempty"`
}

func (s PreemptionState) String() string {
	return string(s)
}
