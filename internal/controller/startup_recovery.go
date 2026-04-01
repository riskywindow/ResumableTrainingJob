package controller

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// Kueue condition types and reasons used by waitForPodsReady eviction.
// These match Kueue v0.15.1 constants.
const (
	kueueConditionEvicted   = "Evicted"
	kueueConditionPodsReady = "PodsReady"

	kueueEvictionReasonPodsReadyTimeout = "PodsReadyTimeout"
	kueueEvictionReasonPreempted        = "Preempted"
	kueueEvictionReasonInactiveWorkload = "InactiveWorkload"

	// Phase 7: startup/recovery condition types.
	conditionTypeStartupTimeoutEvicted  = "StartupTimeoutEvicted"
	conditionTypeRecoveryTimeoutEvicted = "RecoveryTimeoutEvicted"

	// Phase 7: startup/recovery reasons.
	reasonPodsReadyTimeout      = "PodsReadyTimeout"
	reasonRequeuedAfterEviction = "RequeuedAfterEviction"
)

// EvictionClassification captures the result of inspecting a Kueue Workload
// for eviction conditions. Zero value means no eviction detected.
type EvictionClassification struct {
	// Evicted is true when the Workload has an Evicted=True condition.
	Evicted bool

	// Reason is the eviction reason from the Kueue condition.
	Reason string

	// Message is the human-readable eviction message from Kueue.
	Message string

	// IsPodsReadyTimeout is true when the eviction is due to
	// waitForPodsReady startup or recovery timeout.
	IsPodsReadyTimeout bool

	// IsPreemption is true when the eviction is due to Kueue preemption.
	IsPreemption bool
}

// ClassifyEviction inspects the Workload's conditions to detect and classify
// eviction events set by Kueue's waitForPodsReady mechanism.
//
// Returns a zero-value classification when the Workload is nil or has no
// Evicted=True condition. This function is pure and does not perform I/O.
func ClassifyEviction(workload *kueuev1beta2.Workload) EvictionClassification {
	if workload == nil {
		return EvictionClassification{}
	}

	for _, cond := range workload.Status.Conditions {
		if cond.Type == kueueConditionEvicted && cond.Status == metav1.ConditionTrue {
			ec := EvictionClassification{
				Evicted: true,
				Reason:  cond.Reason,
				Message: cond.Message,
			}
			switch cond.Reason {
			case kueueEvictionReasonPodsReadyTimeout:
				ec.IsPodsReadyTimeout = true
			case kueueEvictionReasonPreempted:
				ec.IsPreemption = true
			}
			return ec
		}
	}

	return EvictionClassification{}
}

// ClassifyStartupState determines the appropriate StartupState based on the
// RTJ's current phase, whether it was previously running, and the eviction
// classification from Kueue.
//
// The key distinction: when Kueue evicts due to PodsReadyTimeout, the state
// is StartupTimedOut if pods never reached Ready, or RecoveryTimedOut if they
// were previously running. This must not be confused with Kueue preemption
// or manual pause.
func ClassifyStartupState(
	currentPhase trainingv1alpha1.ResumableTrainingJobPhase,
	wasRunning bool,
	eviction EvictionClassification,
) trainingv1alpha1.StartupState {
	// Timeout evictions classified by prior running state.
	if eviction.Evicted && eviction.IsPodsReadyTimeout {
		if wasRunning {
			return trainingv1alpha1.StartupRecoveryTimedOut
		}
		return trainingv1alpha1.StartupTimedOut
	}

	// Non-timeout evictions (preemption, inactive workload).
	if eviction.Evicted {
		return trainingv1alpha1.StartupEvicted
	}

	// Active runtime states (no eviction).
	switch currentPhase {
	case trainingv1alpha1.PhaseStarting, trainingv1alpha1.PhaseRestoring:
		return trainingv1alpha1.StartupStarting
	case trainingv1alpha1.PhaseRunning:
		return trainingv1alpha1.StartupRunning
	default:
		return trainingv1alpha1.StartupNotStarted
	}
}

// syncStartupRecoveryStatus updates the RTJ's status.startupRecovery section.
// Returns true if any field changed.
//
// This function is idempotent: calling it multiple times with the same inputs
// produces the same result. It does not perform I/O or mutate anything
// outside the RTJ status.
func syncStartupRecoveryStatus(
	job *trainingv1alpha1.ResumableTrainingJob,
	startupState trainingv1alpha1.StartupState,
	podsReadyState trainingv1alpha1.PodsReadyState,
	eviction EvictionClassification,
	now metav1.Time,
) bool {
	if job.Status.StartupRecovery == nil {
		job.Status.StartupRecovery = &trainingv1alpha1.StartupRecoveryStatus{}
	}

	sr := job.Status.StartupRecovery
	changed := false

	if sr.StartupState != startupState {
		sr.StartupState = startupState
		nowCopy := now.DeepCopy()
		sr.LastTransitionTime = nowCopy
		changed = true
	}

	if sr.PodsReadyState != podsReadyState {
		sr.PodsReadyState = podsReadyState
		changed = true
	}

	// Record eviction details only when an eviction has occurred.
	if eviction.Evicted && sr.LastEvictionReason != eviction.Reason {
		sr.LastEvictionReason = eviction.Reason
		changed = true
	}

	// Record requeue reason for timeout evictions.
	if eviction.IsPodsReadyTimeout && sr.LastRequeueReason != reasonRequeuedAfterEviction {
		sr.LastRequeueReason = reasonRequeuedAfterEviction
		changed = true
	}

	return changed
}

// syncStartupRecoveryOnLaunch updates the startupRecovery status when a new
// launch or restore attempt starts. Sets state to Starting with PodsNotReady.
// Returns true if any field changed.
func syncStartupRecoveryOnLaunch(
	job *trainingv1alpha1.ResumableTrainingJob,
	now metav1.Time,
) bool {
	return syncStartupRecoveryStatus(
		job,
		trainingv1alpha1.StartupStarting,
		trainingv1alpha1.PodsNotReady,
		EvictionClassification{},
		now,
	)
}

// syncStartupRecoveryOnRunning updates the startupRecovery status when the
// child runtime transitions to Running (all pods Ready).
// Returns true if any field changed.
func syncStartupRecoveryOnRunning(
	job *trainingv1alpha1.ResumableTrainingJob,
	now metav1.Time,
) bool {
	return syncStartupRecoveryStatus(
		job,
		trainingv1alpha1.StartupRunning,
		trainingv1alpha1.PodsReady,
		EvictionClassification{},
		now,
	)
}

// syncStartupRecoveryOnEviction updates the startupRecovery status when a
// Kueue eviction is detected on the Workload. Classifies the eviction as
// startup timeout, recovery timeout, or generic eviction.
// Returns true if any field changed.
func syncStartupRecoveryOnEviction(
	job *trainingv1alpha1.ResumableTrainingJob,
	eviction EvictionClassification,
	wasRunning bool,
	now metav1.Time,
) bool {
	startupState := ClassifyStartupState(job.Status.Phase, wasRunning, eviction)
	return syncStartupRecoveryStatus(
		job,
		startupState,
		trainingv1alpha1.PodsNotReady,
		eviction,
		now,
	)
}

// setStartupRecoveryConditions sets Phase 7 conditions based on the eviction
// classification. StartupTimeoutEvicted and RecoveryTimeoutEvicted conditions
// are mutually exclusive.
//
// Returns true if any condition changed.
func setStartupRecoveryConditions(
	job *trainingv1alpha1.ResumableTrainingJob,
	eviction EvictionClassification,
	wasRunning bool,
	now metav1.Time,
) bool {
	changed := false

	if !eviction.Evicted || !eviction.IsPodsReadyTimeout {
		// Clear timeout conditions when there's no timeout eviction.
		changed = clearCondition(job, conditionTypeStartupTimeoutEvicted) || changed
		changed = clearCondition(job, conditionTypeRecoveryTimeoutEvicted) || changed
		return changed
	}

	if wasRunning {
		changed = setCondition(job, conditionTypeRecoveryTimeoutEvicted,
			metav1.ConditionTrue,
			reasonPodsReadyTimeout,
			eviction.Message,
			now) || changed
		changed = clearCondition(job, conditionTypeStartupTimeoutEvicted) || changed
	} else {
		changed = setCondition(job, conditionTypeStartupTimeoutEvicted,
			metav1.ConditionTrue,
			reasonPodsReadyTimeout,
			eviction.Message,
			now) || changed
		changed = clearCondition(job, conditionTypeRecoveryTimeoutEvicted) || changed
	}

	return changed
}

// wasPhaseRunning returns true when the RTJ was previously in the Running
// phase. Checks both the current phase and the previously recorded
// StartupRecovery state to handle the case where the phase has already
// transitioned away from Running (e.g., to YieldRequested) on a subsequent
// reconcile.
func wasPhaseRunning(job *trainingv1alpha1.ResumableTrainingJob) bool {
	if job.Status.Phase == trainingv1alpha1.PhaseRunning {
		return true
	}
	if job.Status.StartupRecovery != nil &&
		job.Status.StartupRecovery.StartupState == trainingv1alpha1.StartupRunning {
		return true
	}
	return false
}

// detectAndRecordEviction is a reconciler method that fetches the Workload,
// classifies any eviction, and records the result in the RTJ's startupRecovery
// status and conditions. Returns true if the status was changed.
//
// This is the primary integration point between Kueue's eviction mechanism
// and the RTJ's startup/recovery tracking. Called from the main Reconcile
// loop when a Kueue stop is detected.
func (r *ResumableTrainingJobReconciler) detectAndRecordEviction(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	now metav1.Time,
) bool {
	logger := log.FromContext(ctx)

	workload, err := r.findWorkloadForRTJ(ctx, job)
	if err != nil {
		logger.Error(err, "failed to find workload for eviction detection; skipping")
		return false
	}
	if workload == nil {
		return false
	}

	eviction := ClassifyEviction(workload)
	if !eviction.Evicted {
		return false
	}

	running := wasPhaseRunning(job)
	changed := syncStartupRecoveryOnEviction(job, eviction, running, now)
	changed = setStartupRecoveryConditions(job, eviction, running, now) || changed

	if eviction.IsPodsReadyTimeout {
		if running {
			logger.Info("detected recovery timeout eviction from Kueue waitForPodsReady",
				"evictionReason", eviction.Reason, "message", eviction.Message)
		} else {
			logger.Info("detected startup timeout eviction from Kueue waitForPodsReady",
				"evictionReason", eviction.Reason, "message", eviction.Message)
		}
	} else {
		logger.Info("detected Kueue eviction (non-timeout)",
			"evictionReason", eviction.Reason, "message", eviction.Message)
	}

	return changed
}
