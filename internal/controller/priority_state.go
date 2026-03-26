package controller

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	rtjkueue "github.com/example/checkpoint-native-preemption-controller/internal/kueue"
	"github.com/example/checkpoint-native-preemption-controller/internal/policy/checkpointpriority"
)

const (
	// conditionTypePriorityShaping is the RTJ condition type for priority
	// shaping state. Set when a CheckpointPriorityPolicy is attached and
	// the priority shaping controller has evaluated the RTJ.
	conditionTypePriorityShaping = "PriorityShaping"

	// effectivePriorityAnnotation is set on the RTJ for quick observability.
	effectivePriorityAnnotation = "training.checkpoint.example.io/effective-priority"

	// preemptionStateAnnotation is set on the RTJ for quick observability.
	preemptionStateAnnotation = "training.checkpoint.example.io/preemption-state"
)

// PriorityStateResult captures the outcome of a priority state reconciliation.
// It is returned to the caller so it can decide whether a status update is needed.
type PriorityStateResult struct {
	// StatusChanged is true when RTJ status fields were modified.
	StatusChanged bool

	// WorkloadPatched is true when the Workload.Spec.Priority was updated.
	WorkloadPatched bool

	// AnnotationsChanged is true when RTJ annotations were modified.
	AnnotationsChanged bool

	// Decision is the priority decision engine's output (nil when no policy).
	Decision *checkpointpriority.Decision

	// RequeueAfter is the suggested requeue interval for the priority
	// shaping evaluation. Zero means no priority-driven requeue is needed.
	RequeueAfter time.Duration
}

// reconcilePriorityState evaluates the priority decision engine and
// materializes effective priority into the Workload and RTJ status.
//
// This function is called from the main RTJ reconcile loop when the job
// is in an active phase (Running, Starting, Restoring, YieldRequested, Draining).
// It is a no-op when no CheckpointPriorityPolicy is referenced.
//
// The function:
//  1. Resolves the CheckpointPriorityPolicy from the cluster.
//  2. Resolves the base priority from the WorkloadPriorityClass.
//  3. Collects telemetry from RTJ status and checkpoint catalog.
//  4. Converts telemetry to EvaluationInput and calls Evaluate().
//  5. Updates RTJ status.priorityShaping with the decision.
//  6. Patches Workload.Spec.Priority when the effective priority changes.
//  7. Sets conditions and annotations for observability.
//
// Returns a PriorityStateResult indicating what changed. The caller is
// responsible for persisting RTJ status changes (the Workload is patched
// directly within this function).
func (r *ResumableTrainingJobReconciler) reconcilePriorityState(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	now metav1.Time,
) PriorityStateResult {
	logger := log.FromContext(ctx)
	result := PriorityStateResult{}

	// No-op when no policy is attached. Preserve Phase 4 behavior.
	if !job.IsPriorityShapingEnabled() {
		// Clear stale priority shaping status if present.
		if job.Status.PriorityShaping != nil {
			job.Status.PriorityShaping = nil
			result.StatusChanged = true
		}
		// Clear stale annotations.
		result.AnnotationsChanged = clearPriorityAnnotations(job)
		// Clear stale condition.
		if clearCondition(job, conditionTypePriorityShaping) {
			result.StatusChanged = true
		}
		return result
	}

	// 1. Resolve the CheckpointPriorityPolicy.
	policy, err := r.resolvePolicy(ctx, job)
	if err != nil {
		logger.Error(err, "failed to resolve CheckpointPriorityPolicy",
			"policyRef", job.Spec.PriorityPolicyRef.Name)
		result.StatusChanged = setPriorityShapingCondition(job,
			metav1.ConditionFalse, "PolicyResolutionFailed",
			fmt.Sprintf("Failed to resolve CheckpointPriorityPolicy %q: %v",
				job.Spec.PriorityPolicyRef.Name, err), now)
		return result
	}

	// 2. Resolve the base priority from the WorkloadPriorityClass.
	basePriority, err := r.resolveBasePriority(ctx, job)
	if err != nil {
		logger.Error(err, "failed to resolve base priority",
			"priorityClassName", job.Spec.WorkloadPriorityClassName)
		result.StatusChanged = setPriorityShapingCondition(job,
			metav1.ConditionFalse, "BasePriorityResolutionFailed",
			fmt.Sprintf("Failed to resolve WorkloadPriorityClass %q: %v",
				job.Spec.WorkloadPriorityClassName, err), now)
		return result
	}

	// 3. Collect telemetry.
	yieldWindow := time.Duration(0)
	if policy.YieldWindow != nil {
		yieldWindow = policy.YieldWindow.Duration
	}
	snap := CollectTelemetry(ctx, job, r.checkpointCatalog(), now, yieldWindow)

	// 4. Build evaluation input and evaluate.
	input := buildEvaluationInput(basePriority, snap, now)
	decision := checkpointpriority.Evaluate(input, &policy)
	result.Decision = &decision

	// 5. Sync telemetry to status.
	if SyncPriorityShapingTelemetry(job, snap, now) {
		result.StatusChanged = true
	}

	// 6. Update PriorityShapingStatus with decision output.
	if syncDecisionToStatus(job, basePriority, &decision, now) {
		result.StatusChanged = true
	}

	// 7. Patch Workload.Spec.Priority when it differs.
	patched, err := r.patchWorkloadPriority(ctx, job, decision.EffectivePriority)
	if err != nil {
		logger.Error(err, "failed to patch Workload priority")
		result.StatusChanged = setPriorityShapingCondition(job,
			metav1.ConditionFalse, "WorkloadPatchFailed",
			fmt.Sprintf("Failed to patch Workload priority: %v", err), now) || result.StatusChanged
		return result
	}
	result.WorkloadPatched = patched

	// 8. Set observability annotations.
	result.AnnotationsChanged = syncPriorityAnnotations(job, &decision)

	// 9. Set condition.
	condMessage := fmt.Sprintf("Priority shaping active: state=%s, base=%d, effective=%d, reason=%s",
		decision.PreemptionState, basePriority, decision.EffectivePriority, decision.Reason)
	if setPriorityShapingCondition(job, metav1.ConditionTrue, decision.Reason, condMessage, now) {
		result.StatusChanged = true
	}

	// 10. Compute requeue interval for protection window expiry.
	if decision.ProtectedUntil != nil {
		remaining := decision.ProtectedUntil.Sub(now.Time)
		if remaining > 0 {
			// Requeue slightly after the protection window expires to
			// re-evaluate priority promptly.
			result.RequeueAfter = remaining + time.Second
		}
	}

	return result
}

// resolvePolicy fetches the CheckpointPriorityPolicy referenced by the RTJ.
func (r *ResumableTrainingJobReconciler) resolvePolicy(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
) (trainingv1alpha1.CheckpointPriorityPolicySpec, error) {
	if job.Spec.PriorityPolicyRef == nil {
		return trainingv1alpha1.CheckpointPriorityPolicySpec{}, fmt.Errorf("no priorityPolicyRef set")
	}

	var policy trainingv1alpha1.CheckpointPriorityPolicy
	key := types.NamespacedName{Name: job.Spec.PriorityPolicyRef.Name}
	if err := r.Get(ctx, key, &policy); err != nil {
		return trainingv1alpha1.CheckpointPriorityPolicySpec{}, err
	}

	// Apply defaults to ensure all optional fields have safe values.
	policy.Default()

	return policy.Spec, nil
}

// resolveBasePriority fetches the WorkloadPriorityClass and returns its Value.
func (r *ResumableTrainingJobReconciler) resolveBasePriority(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
) (int32, error) {
	var wpc kueuev1beta2.WorkloadPriorityClass
	key := types.NamespacedName{Name: job.Spec.WorkloadPriorityClassName}
	if err := r.Get(ctx, key, &wpc); err != nil {
		return 0, err
	}
	return wpc.Value, nil
}

// buildEvaluationInput converts telemetry and base priority into the engine's input type.
func buildEvaluationInput(
	basePriority int32,
	snap TelemetrySnapshot,
	now metav1.Time,
) checkpointpriority.EvaluationInput {
	input := checkpointpriority.EvaluationInput{
		BasePriority:     basePriority,
		Now:              now.Time,
		RecentYieldCount: snap.RecentYieldCount,
	}

	if snap.LastCompletedCheckpointTime != nil {
		t := snap.LastCompletedCheckpointTime.Time
		input.LastCompletedCheckpointTime = &t
	}
	if snap.LastRunStartTime != nil {
		t := snap.LastRunStartTime.Time
		input.RunStartTime = &t
	}
	if snap.LastResumeTime != nil {
		t := snap.LastResumeTime.Time
		input.LastResumeTime = &t
	}
	if snap.LastYieldTime != nil {
		t := snap.LastYieldTime.Time
		input.LastYieldTime = &t
	}

	return input
}

// syncDecisionToStatus writes the decision engine output to RTJ status.
// Returns true when any field changed.
func syncDecisionToStatus(
	job *trainingv1alpha1.ResumableTrainingJob,
	basePriority int32,
	decision *checkpointpriority.Decision,
	now metav1.Time,
) bool {
	if job.Status.PriorityShaping == nil {
		job.Status.PriorityShaping = &trainingv1alpha1.PriorityShapingStatus{}
	}
	ps := job.Status.PriorityShaping
	changed := false

	if ps.BasePriority != basePriority {
		ps.BasePriority = basePriority
		changed = true
	}
	if ps.EffectivePriority != decision.EffectivePriority {
		ps.EffectivePriority = decision.EffectivePriority
		changed = true
	}
	if ps.PreemptionState != decision.PreemptionState {
		ps.PreemptionState = decision.PreemptionState
		changed = true
	}
	if ps.PreemptionStateReason != decision.Reason {
		ps.PreemptionStateReason = decision.Reason
		changed = true
	}

	// ProtectedUntil
	var protectedUntil *metav1.Time
	if decision.ProtectedUntil != nil {
		protectedUntil = &metav1.Time{Time: *decision.ProtectedUntil}
	}
	if !timesEqual(ps.ProtectedUntil, protectedUntil) {
		ps.ProtectedUntil = protectedUntil
		changed = true
	}

	return changed
}

// patchWorkloadPriority patches the Workload.Spec.Priority field when the
// effective priority differs from the current value. Returns (true, nil) when
// a patch was applied, (false, nil) when no patch was needed, and (false, err)
// on failure.
func (r *ResumableTrainingJobReconciler) patchWorkloadPriority(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	effectivePriority int32,
) (bool, error) {
	workloadName := rtjkueue.WorkloadNameForObject(job)
	if workloadName == "" {
		return false, nil
	}

	var workload kueuev1beta2.Workload
	key := types.NamespacedName{
		Name:      workloadName,
		Namespace: job.Namespace,
	}
	if err := r.Get(ctx, key, &workload); err != nil {
		// Workload may not exist yet (e.g., during initial creation).
		return false, client.IgnoreNotFound(err)
	}

	// Check if the priority already matches.
	currentPriority := ptr.Deref(workload.Spec.Priority, 0)
	if currentPriority == effectivePriority {
		return false, nil
	}

	// Patch only the priority field using a merge patch.
	patch := client.MergeFrom(workload.DeepCopy())
	workload.Spec.Priority = ptr.To(effectivePriority)
	if err := r.Patch(ctx, &workload, patch); err != nil {
		return false, fmt.Errorf("patch Workload %s priority from %d to %d: %w",
			workloadName, currentPriority, effectivePriority, err)
	}

	log.FromContext(ctx).Info("patched Workload effective priority",
		"workload", workloadName,
		"from", currentPriority,
		"to", effectivePriority)
	return true, nil
}

// syncPriorityAnnotations sets observability annotations on the RTJ.
// Returns true when annotations were modified.
func syncPriorityAnnotations(
	job *trainingv1alpha1.ResumableTrainingJob,
	decision *checkpointpriority.Decision,
) bool {
	if job.Annotations == nil {
		job.Annotations = map[string]string{}
	}
	changed := false

	epStr := fmt.Sprintf("%d", decision.EffectivePriority)
	if job.Annotations[effectivePriorityAnnotation] != epStr {
		job.Annotations[effectivePriorityAnnotation] = epStr
		changed = true
	}

	psStr := string(decision.PreemptionState)
	if job.Annotations[preemptionStateAnnotation] != psStr {
		job.Annotations[preemptionStateAnnotation] = psStr
		changed = true
	}

	return changed
}

// clearPriorityAnnotations removes priority shaping annotations from the RTJ.
// Returns true when annotations were modified.
func clearPriorityAnnotations(job *trainingv1alpha1.ResumableTrainingJob) bool {
	if job.Annotations == nil {
		return false
	}
	changed := false
	if _, ok := job.Annotations[effectivePriorityAnnotation]; ok {
		delete(job.Annotations, effectivePriorityAnnotation)
		changed = true
	}
	if _, ok := job.Annotations[preemptionStateAnnotation]; ok {
		delete(job.Annotations, preemptionStateAnnotation)
		changed = true
	}
	return changed
}

// setPriorityShapingCondition sets the PriorityShaping condition on the RTJ.
// Returns true when the condition changed.
func setPriorityShapingCondition(
	job *trainingv1alpha1.ResumableTrainingJob,
	status metav1.ConditionStatus,
	reason, message string,
	now metav1.Time,
) bool {
	return setCondition(job, conditionTypePriorityShaping, status, reason, message, now)
}

// isActivePriorityPhase returns true when the RTJ is in a phase where
// priority shaping evaluation is meaningful.
func isActivePriorityPhase(phase trainingv1alpha1.ResumableTrainingJobPhase) bool {
	switch phase {
	case trainingv1alpha1.PhaseStarting,
		trainingv1alpha1.PhaseRunning,
		trainingv1alpha1.PhaseRestoring,
		trainingv1alpha1.PhaseYieldRequested,
		trainingv1alpha1.PhaseDraining:
		return true
	default:
		return false
	}
}
