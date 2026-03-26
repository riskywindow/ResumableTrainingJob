package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FailurePolicy describes how the AdmissionCheck controller behaves when it
// cannot reach the checkpoint store or catalog.
// +kubebuilder:validation:Enum=FailOpen;FailClosed
type FailurePolicy string

const (
	// FailurePolicyFailOpen allows the workload to proceed when the readiness
	// check cannot be evaluated due to transient store or catalog errors.
	FailurePolicyFailOpen FailurePolicy = "FailOpen"

	// FailurePolicyFailClosed blocks the workload until the readiness check
	// can be positively confirmed. This is the safe default.
	FailurePolicyFailClosed FailurePolicy = "FailClosed"
)

const (
	DefaultFailurePolicy                       = FailurePolicyFailClosed
	DefaultRequireCompleteCheckpoint           = true
	DefaultAllowInitialLaunchWithoutCheckpoint = true
)

// ResumeReadinessPolicySpec defines the policy for the ResumeReadiness
// AdmissionCheck controller. This object is referenced by AdmissionCheck
// resources via spec.parameters.
type ResumeReadinessPolicySpec struct {
	// RequireCompleteCheckpoint controls whether a resume attempt must have
	// a complete checkpoint available. When true, the admission check will
	// not pass until a complete checkpoint is confirmed. When false, the
	// controller allows resume even if the checkpoint completeness cannot
	// be verified (useful for development/testing).
	// Default: true.
	// +optional
	RequireCompleteCheckpoint *bool `json:"requireCompleteCheckpoint,omitempty"`

	// MaxCheckpointAge is the maximum acceptable age for the latest checkpoint
	// used in a resume. If the most recent checkpoint is older than this
	// duration, the admission check will not pass. Zero means no age limit.
	// +optional
	MaxCheckpointAge *metav1.Duration `json:"maxCheckpointAge,omitempty"`

	// FailurePolicy defines how the controller behaves when the checkpoint
	// store or catalog is temporarily unreachable.
	// FailOpen: allow the workload to proceed (optimistic).
	// FailClosed: block the workload until the check can be confirmed (safe default).
	// Default: FailClosed.
	// +optional
	// +kubebuilder:validation:Enum=FailOpen;FailClosed
	FailurePolicy FailurePolicy `json:"failurePolicy,omitempty"`

	// AllowInitialLaunchWithoutCheckpoint controls whether the very first
	// launch of a training job (no prior checkpoint exists) is allowed to
	// proceed. When true, the readiness check passes immediately for initial
	// launches. When false, even initial launches require checkpoint evidence
	// (rarely useful; mainly for strict compliance scenarios).
	// Default: true.
	// +optional
	AllowInitialLaunchWithoutCheckpoint *bool `json:"allowInitialLaunchWithoutCheckpoint,omitempty"`
}

// ResumeReadinessPolicyStatus defines the observed state of ResumeReadinessPolicy.
type ResumeReadinessPolicyStatus struct {
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
// +kubebuilder:resource:scope=Cluster,shortName=rrp
// +kubebuilder:printcolumn:name="FailurePolicy",type=string,JSONPath=`.spec.failurePolicy`
// +kubebuilder:printcolumn:name="RequireComplete",type=boolean,JSONPath=`.spec.requireCompleteCheckpoint`
// +kubebuilder:printcolumn:name="AllowInitial",type=boolean,JSONPath=`.spec.allowInitialLaunchWithoutCheckpoint`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ResumeReadinessPolicy is the Schema for the resumereadinesspolicies API.
// It is a cluster-scoped parameter object referenced by Kueue AdmissionCheck
// resources to configure the ResumeReadiness admission check controller.
type ResumeReadinessPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResumeReadinessPolicySpec   `json:"spec,omitempty"`
	Status ResumeReadinessPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResumeReadinessPolicyList contains a list of ResumeReadinessPolicy.
type ResumeReadinessPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResumeReadinessPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResumeReadinessPolicy{}, &ResumeReadinessPolicyList{})
}

// Default applies default values to the ResumeReadinessPolicy.
func (p *ResumeReadinessPolicy) Default() {
	if p.Spec.RequireCompleteCheckpoint == nil {
		d := DefaultRequireCompleteCheckpoint
		p.Spec.RequireCompleteCheckpoint = &d
	}
	if p.Spec.FailurePolicy == "" {
		p.Spec.FailurePolicy = DefaultFailurePolicy
	}
	if p.Spec.AllowInitialLaunchWithoutCheckpoint == nil {
		d := DefaultAllowInitialLaunchWithoutCheckpoint
		p.Spec.AllowInitialLaunchWithoutCheckpoint = &d
	}
}
