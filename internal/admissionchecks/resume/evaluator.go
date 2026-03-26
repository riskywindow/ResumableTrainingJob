package resume

import (
	"fmt"
	"time"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
)

// ReadinessDecision is the outcome of evaluating whether a workload is ready
// for admission from a checkpoint/resume perspective.
type ReadinessDecision struct {
	// State maps directly to a Kueue CheckState: Ready, Retry, or Rejected.
	State kueuev1beta2.CheckState

	// Reason is a machine-readable token (one of the Reason* constants).
	Reason string

	// Message is a human-readable explanation.
	Message string
}

// EvaluatorInput bundles everything the evaluator needs to make a decision.
// The reconciler is responsible for gathering these inputs (I/O); the evaluator
// is a pure function.
type EvaluatorInput struct {
	// RTJ is the owning ResumableTrainingJob.
	RTJ *trainingv1alpha1.ResumableTrainingJob

	// Policy is the resolved (defaults-applied) readiness policy.
	Policy ResolvedPolicy

	// SelectedCheckpoint is the latest compatible complete checkpoint found
	// in storage. Nil when no compatible checkpoint exists or when the catalog
	// was not queried (e.g., catalog unavailable).
	SelectedCheckpoint *checkpoints.CheckpointManifest

	// CatalogQueried is true when the catalog was successfully queried,
	// regardless of whether a checkpoint was found. When false,
	// SelectedCheckpoint is meaningless and CatalogError may explain why.
	CatalogQueried bool

	// CatalogError is non-nil when the catalog query failed due to a
	// transient error (store unreachable, auth failure, etc.).
	CatalogError error

	// Now is the evaluation timestamp for age calculations.
	Now time.Time
}

// Evaluate makes the readiness decision. It is a pure function with no I/O.
//
// Decision tree:
//
//  1. Catalog error → apply failure policy (FailOpen → Ready, FailClosed → Retry).
//  2. No compatible checkpoint found → check allowInitialLaunchWithoutCheckpoint.
//     - Allowed → Ready (initial launch).
//     - Blocked → Rejected.
//  3. Checkpoint found but too old → Rejected.
//  4. Checkpoint found, age OK → Ready.
//
// The evaluator validates what is knowable pre-launch: checkpoint existence,
// completeness, age, and compatibility. Shape-specific validation (exact
// world size after partial admission) is left to the operator at launch time.
func Evaluate(input EvaluatorInput) ReadinessDecision {
	// --- Case 1: catalog error (transient store/catalog failure) ---
	if input.CatalogError != nil {
		return catalogErrorDecision(input.Policy, input.CatalogError)
	}

	// --- Case 2: catalog was not queried (no catalog configured) ---
	if !input.CatalogQueried {
		return noCatalogDecision(input.Policy)
	}

	// --- Case 3: no compatible checkpoint found ---
	if input.SelectedCheckpoint == nil {
		return noCheckpointDecision(input.Policy, input.RTJ)
	}

	// --- Case 4: checkpoint found — validate age ---
	if input.Policy.MaxCheckpointAge != nil {
		tooOld, reason := checkpoints.IsCheckpointTooOld(*input.SelectedCheckpoint, *input.Policy.MaxCheckpointAge, input.Now)
		if tooOld {
			return ReadinessDecision{
				State:   kueuev1beta2.CheckStateRejected,
				Reason:  ReasonCheckpointTooOld,
				Message: fmt.Sprintf("selected checkpoint %q is too old: %s", input.SelectedCheckpoint.CheckpointID, reason),
			}
		}
	}

	// --- Case 5: checkpoint found, complete, age OK → Ready ---
	return ReadinessDecision{
		State:   kueuev1beta2.CheckStateReady,
		Reason:  ReasonCheckpointReady,
		Message: fmt.Sprintf("checkpoint %q is compatible, complete, and within age limits", input.SelectedCheckpoint.CheckpointID),
	}
}

// catalogErrorDecision handles transient catalog/store failures.
func catalogErrorDecision(policy ResolvedPolicy, err error) ReadinessDecision {
	switch policy.FailurePolicy {
	case trainingv1alpha1.FailurePolicyFailOpen:
		return ReadinessDecision{
			State:   kueuev1beta2.CheckStateReady,
			Reason:  ReasonStorageUnavailable,
			Message: fmt.Sprintf("checkpoint store unreachable (%v); proceeding per FailOpen policy", err),
		}
	default: // FailClosed
		return ReadinessDecision{
			State:   kueuev1beta2.CheckStateRetry,
			Reason:  ReasonStorageUnavailable,
			Message: fmt.Sprintf("checkpoint store unreachable (%v); retrying per FailClosed policy", err),
		}
	}
}

// noCatalogDecision handles the case where no catalog is configured.
func noCatalogDecision(policy ResolvedPolicy) ReadinessDecision {
	if policy.AllowInitialLaunchWithoutCheckpoint {
		return ReadinessDecision{
			State:   kueuev1beta2.CheckStateReady,
			Reason:  ReasonInitialLaunchReady,
			Message: "no checkpoint catalog configured; initial launch allowed by policy",
		}
	}
	switch policy.FailurePolicy {
	case trainingv1alpha1.FailurePolicyFailOpen:
		return ReadinessDecision{
			State:   kueuev1beta2.CheckStateReady,
			Reason:  ReasonStorageUnavailable,
			Message: "no checkpoint catalog configured; proceeding per FailOpen policy",
		}
	default:
		return ReadinessDecision{
			State:   kueuev1beta2.CheckStateRetry,
			Reason:  ReasonStorageUnavailable,
			Message: "no checkpoint catalog configured; retrying per FailClosed policy",
		}
	}
}

// noCheckpointDecision handles the case where the catalog was queried but
// no compatible checkpoint was found.
func noCheckpointDecision(policy ResolvedPolicy, rtj *trainingv1alpha1.ResumableTrainingJob) ReadinessDecision {
	if policy.AllowInitialLaunchWithoutCheckpoint {
		msg := "no compatible checkpoint found; initial launch allowed by policy"
		if rtj != nil && rtj.Status.CurrentRunAttempt > 0 {
			msg = "no compatible checkpoint found after prior run attempt(s); launch allowed by policy (allowInitialLaunchWithoutCheckpoint=true)"
		}
		return ReadinessDecision{
			State:   kueuev1beta2.CheckStateReady,
			Reason:  ReasonInitialLaunchReady,
			Message: msg,
		}
	}

	// Not allowed to launch without a checkpoint.
	if rtj != nil && rtj.Status.CurrentRunAttempt == 0 && rtj.Status.LastCompletedCheckpoint == nil {
		return ReadinessDecision{
			State:   kueuev1beta2.CheckStateRejected,
			Reason:  ReasonInitialLaunchBlocked,
			Message: "no checkpoint available and allowInitialLaunchWithoutCheckpoint is false",
		}
	}
	return ReadinessDecision{
		State:   kueuev1beta2.CheckStateRejected,
		Reason:  ReasonNoCheckpointAvailable,
		Message: "no compatible checkpoint found for resume",
	}
}
