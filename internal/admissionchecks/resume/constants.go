// Package resume implements the ResumeReadiness custom AdmissionCheck controller
// for Kueue. It gates workload admission until resume-readiness conditions are
// met (checkpoint completeness, age, etc.) based on a ResumeReadinessPolicy.
package resume

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

const (
	// ControllerName is the AdmissionCheck controllerName string that
	// identifies this controller to Kueue. Cluster administrators set this
	// value in AdmissionCheck.spec.controllerName.
	ControllerName = "training.checkpoint.example.io/resume-readiness"
)

// Condition types and reasons for AdmissionCheck status.
const (
	// ConditionActive is the condition type on AdmissionCheck that signals
	// whether this controller is actively processing checks.
	ConditionActive = "Active"

	// ReasonControllerReady indicates the controller is running and the
	// referenced ResumeReadinessPolicy exists.
	ReasonControllerReady = "ControllerReady"

	// ReasonPolicyNotFound indicates the referenced ResumeReadinessPolicy
	// does not exist or the parameters reference is invalid.
	ReasonPolicyNotFound = "PolicyNotFound"

	// ReasonParametersMissing indicates the AdmissionCheck has no parameters
	// reference or points at the wrong kind.
	ReasonParametersMissing = "ParametersMissing"
)

// Workload check reasons used in AdmissionCheckState entries.
// Each reason is a machine-readable token; the accompanying message is human-readable.
const (
	// ReasonInitialLaunchReady — first launch with no prior checkpoint; policy allows it.
	ReasonInitialLaunchReady = "InitialLaunchReady"

	// ReasonCheckpointReady — a compatible, complete, age-valid checkpoint was found.
	ReasonCheckpointReady = "CheckpointReady"

	// ReasonNoCheckpointAvailable — no compatible checkpoint exists and initial
	// launch without checkpoint is not allowed by policy.
	ReasonNoCheckpointAvailable = "NoCheckpointAvailable"

	// ReasonCheckpointTooOld — a compatible checkpoint was found but it exceeds
	// the policy's maxCheckpointAge.
	ReasonCheckpointTooOld = "CheckpointTooOld"

	// ReasonCheckpointIncomplete — the selected checkpoint failed completeness
	// validation (missing manifest, missing artifacts).
	ReasonCheckpointIncomplete = "CheckpointIncomplete"

	// ReasonCheckpointIncompatible — no checkpoint passes the RTJ's resume
	// compatibility rules (identity, code version, world-size constraints).
	ReasonCheckpointIncompatible = "CheckpointIncompatible"

	// ReasonStorageUnavailable — the checkpoint store/catalog could not be
	// reached. Outcome depends on the policy's failurePolicy field.
	ReasonStorageUnavailable = "StorageUnavailable"

	// ReasonInitialLaunchBlocked — no checkpoint found and
	// allowInitialLaunchWithoutCheckpoint is false.
	ReasonInitialLaunchBlocked = "InitialLaunchBlocked"

	// ReasonPolicyResolutionFailed — the AdmissionCheck's parameters could
	// not be resolved to a valid ResumeReadinessPolicy.
	ReasonPolicyResolutionFailed = "PolicyResolutionFailed"

	// ReasonOwnerNotFound — the Workload does not have an RTJ owner reference,
	// or the owning RTJ could not be fetched.
	ReasonOwnerNotFound = "OwnerNotFound"
)

// ResumeReadinessPolicyGVK is the GroupVersionKind for the parameter object.
var ResumeReadinessPolicyGVK = schema.GroupVersionKind{
	Group:   trainingv1alpha1.GroupVersion.Group,
	Version: trainingv1alpha1.GroupVersion.Version,
	Kind:    "ResumeReadinessPolicy",
}
