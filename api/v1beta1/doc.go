// Package v1beta1 contains API Schema definitions for the
// training.checkpoint.example.io v1beta1 API group.
//
// v1beta1 promotes the core CRDs (ResumableTrainingJob,
// CheckpointPriorityPolicy, ResumeReadinessPolicy) from v1alpha1 to beta.
// The schema is identical to v1alpha1; no field renames, removals, or
// semantic changes are introduced. Experimental fields (partialAdmission,
// devices, elasticity) are carried forward but documented as experimental
// and may change without a deprecation period.
//
// +kubebuilder:object:generate=true
// +groupName=training.checkpoint.example.io
package v1beta1
