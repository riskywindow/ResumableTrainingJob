package controller

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/provisioning"
)

const (
	reasonDesiredStatePaused     = "DesiredStatePaused"
	reasonWaitingForAdmission    = "WaitingForAdmission"
	reasonCreatingRunAttempt     = "CreatingRunAttempt"
	reasonActiveRunPresent       = "ActiveRunPresent"
	reasonLaunchFailed           = "LaunchFailed"
	reasonPauseRequested         = "PauseRequested"
	reasonKueueStopRequested     = "KueueStopRequested"
	reasonDrainInProgress        = "DrainInProgress"
	reasonKueueDrainInProgress   = "KueueDrainInProgress"
	reasonPausedComplete         = "PausedComplete"
	reasonDrainTimedOut          = "DrainTimedOut"
	reasonRestoring              = "RestoringFromCheckpoint"
	reasonNoCompatibleCheckpoint = "NoCompatibleCheckpoint"
	reasonKueueSuspended         = "KueueSuspended"
	reasonManualPause            = "ManualPause"

	conditionTypeDegraded       = "Degraded"
	conditionTypeKueueSuspended = "KueueSuspended"
)

func markPendingPaused(job *trainingv1alpha1.ResumableTrainingJob, now metav1.Time) bool {
	changed := false
	if job.Status.SetPhase(trainingv1alpha1.PhasePending, reasonDesiredStatePaused, "Launch is deferred because desiredState is Paused.", now) {
		changed = true
	}
	changed = syncSuspensionStatus(job, now) || changed
	if job.Status.ObservedGeneration != job.Generation {
		job.Status.ObservedGeneration = job.Generation
		changed = true
	}
	return changed
}

func markQueuedForAdmission(job *trainingv1alpha1.ResumableTrainingJob, now metav1.Time) bool {
	changed := false
	if job.Status.SetPhase(trainingv1alpha1.PhaseQueued, reasonWaitingForAdmission, "Waiting for Kueue admission before creating a child JobSet.", now) {
		changed = true
	}
	changed = syncSuspensionStatus(job, now) || changed
	if job.Status.ObservedGeneration != job.Generation {
		job.Status.ObservedGeneration = job.Generation
		changed = true
	}
	return changed
}

func markStarting(job *trainingv1alpha1.ResumableTrainingJob, runAttempt int32, controlConfigMapName, jobSetName string, now metav1.Time) bool {
	changed := false
	if job.Status.CurrentRunAttempt != runAttempt {
		job.Status.CurrentRunAttempt = runAttempt
		changed = true
	}
	if job.Status.ActiveControlConfigMapName != controlConfigMapName {
		job.Status.ActiveControlConfigMapName = controlConfigMapName
		changed = true
	}
	if job.Status.ActiveJobSetName != jobSetName {
		job.Status.ActiveJobSetName = jobSetName
		changed = true
	}
	message := fmt.Sprintf("Created control ConfigMap %s and child JobSet %s for run attempt %d.", controlConfigMapName, jobSetName, runAttempt)
	if job.Status.SetPhase(trainingv1alpha1.PhaseStarting, reasonCreatingRunAttempt, message, now) {
		changed = true
	}
	changed = syncSuspensionStatus(job, now) || changed
	if job.Status.ObservedGeneration != job.Generation {
		job.Status.ObservedGeneration = job.Generation
		changed = true
	}
	return changed
}

func markRunning(job *trainingv1alpha1.ResumableTrainingJob, now metav1.Time) bool {
	changed := false
	message := fmt.Sprintf("Active child JobSet %s exists for run attempt %d.", job.Status.ActiveJobSetName, job.Status.CurrentRunAttempt)
	if job.Status.SetPhase(trainingv1alpha1.PhaseRunning, reasonActiveRunPresent, message, now) {
		changed = true
	}
	changed = syncSuspensionStatus(job, now) || changed
	changed = clearCondition(job, conditionTypeDegraded) || changed
	if job.Status.ObservedGeneration != job.Generation {
		job.Status.ObservedGeneration = job.Generation
		changed = true
	}
	return changed
}

func markStopRequested(job *trainingv1alpha1.ResumableTrainingJob, requestID string, source stopSource, now metav1.Time) bool {
	changed := false
	if job.Status.PauseRequestID != requestID {
		job.Status.PauseRequestID = requestID
		changed = true
	}
	reason := reasonPauseRequested
	message := fmt.Sprintf("Pause request %s was published to control ConfigMap %s.", requestID, job.Status.ActiveControlConfigMapName)
	if source == stopSourceKueue {
		reason = reasonKueueStopRequested
		message = fmt.Sprintf("Kueue suspension requested graceful yield %s through control ConfigMap %s.", requestID, job.Status.ActiveControlConfigMapName)
	}
	if job.Status.SetPhase(trainingv1alpha1.PhaseYieldRequested, reason, message, now) {
		changed = true
	}
	changed = syncSuspensionStatus(job, now) || changed
	if job.Status.ObservedGeneration != job.Generation {
		job.Status.ObservedGeneration = job.Generation
		changed = true
	}
	return changed
}

func markDraining(job *trainingv1alpha1.ResumableTrainingJob, markerURI string, source stopSource, now metav1.Time) bool {
	changed := false
	reason := reasonDrainInProgress
	message := fmt.Sprintf("Waiting for yield marker %s and a completed checkpoint manifest newer than the pause request.", markerURI)
	if source == stopSourceKueue {
		reason = reasonKueueDrainInProgress
		message = fmt.Sprintf("Waiting for yield marker %s and a completed checkpoint manifest newer than the Kueue suspend request.", markerURI)
	}
	if job.Status.SetPhase(trainingv1alpha1.PhaseDraining, reason, message, now) {
		changed = true
	}
	changed = syncSuspensionStatus(job, now) || changed
	if job.Status.ObservedGeneration != job.Generation {
		job.Status.ObservedGeneration = job.Generation
		changed = true
	}
	return changed
}

func markPaused(job *trainingv1alpha1.ResumableTrainingJob, now metav1.Time) bool {
	changed := false
	message := fmt.Sprintf("Run attempt %d drained successfully and the active child JobSet was removed.", job.Status.CurrentRunAttempt)
	if job.Status.SetPhase(trainingv1alpha1.PhasePaused, reasonPausedComplete, message, now) {
		changed = true
	}
	changed = syncSuspensionStatus(job, now) || changed
	changed = clearCondition(job, conditionTypeDegraded) || changed
	if job.Status.ObservedGeneration != job.Generation {
		job.Status.ObservedGeneration = job.Generation
		changed = true
	}
	return changed
}

func markRestoring(
	job *trainingv1alpha1.ResumableTrainingJob,
	runAttempt int32,
	controlConfigMapName string,
	jobSetName string,
	selectedCheckpoint *trainingv1alpha1.CheckpointReference,
	now metav1.Time,
) bool {
	changed := false
	if job.Status.CurrentRunAttempt != runAttempt {
		job.Status.CurrentRunAttempt = runAttempt
		changed = true
	}
	if job.Status.ActiveControlConfigMapName != controlConfigMapName {
		job.Status.ActiveControlConfigMapName = controlConfigMapName
		changed = true
	}
	if job.Status.ActiveJobSetName != jobSetName {
		job.Status.ActiveJobSetName = jobSetName
		changed = true
	}
	if job.Status.PauseRequestID != "" {
		job.Status.PauseRequestID = ""
		changed = true
	}
	if selectedCheckpoint != nil {
		if job.Status.SelectedCheckpoint == nil || job.Status.SelectedCheckpoint.ManifestURI != selectedCheckpoint.ManifestURI {
			copied := *selectedCheckpoint
			job.Status.SelectedCheckpoint = &copied
			changed = true
		}
	}
	message := fmt.Sprintf("Creating run attempt %d from selected checkpoint %s.", runAttempt, job.Status.SelectedCheckpoint.ManifestURI)
	if job.Status.SetPhase(trainingv1alpha1.PhaseRestoring, reasonRestoring, message, now) {
		changed = true
	}
	changed = syncSuspensionStatus(job, now) || changed
	changed = clearCondition(job, conditionTypeDegraded) || changed
	if job.Status.ObservedGeneration != job.Generation {
		job.Status.ObservedGeneration = job.Generation
		changed = true
	}
	return changed
}

func markFailed(job *trainingv1alpha1.ResumableTrainingJob, reason, message string, now metav1.Time) bool {
	changed := false
	if job.Status.SetPhase(trainingv1alpha1.PhaseFailed, reason, message, now) {
		changed = true
	}
	changed = syncSuspensionStatus(job, now) || changed
	if job.Status.ObservedGeneration != job.Generation {
		job.Status.ObservedGeneration = job.Generation
		changed = true
	}
	return changed
}

func syncSuspensionStatus(job *trainingv1alpha1.ResumableTrainingJob, now metav1.Time) bool {
	switch {
	case job.IsSuspendedForKueue():
		message := "Kueue has suspended the RTJ; runtime launch is blocked until admission is restored."
		if job.Status.ActiveJobSetName != "" {
			message = "Kueue has suspended the RTJ; the active child JobSet is draining toward checkpointed teardown."
		}
		changed := setCurrentSuspension(job, &trainingv1alpha1.SuspensionStatus{
			Suspended:  true,
			Source:     trainingv1alpha1.SuspensionSourceKueue,
			Reason:     reasonKueueSuspended,
			Message:    message,
			ObservedAt: &now,
		})
		return setCondition(job, conditionTypeKueueSuspended, metav1.ConditionTrue, reasonKueueSuspended, message, now) || changed
	case job.Spec.Control != nil && job.Spec.Control.DesiredState == trainingv1alpha1.DesiredStatePaused && job.Status.ActiveJobSetName == "":
		changed := setCurrentSuspension(job, &trainingv1alpha1.SuspensionStatus{
			Suspended:  true,
			Source:     trainingv1alpha1.SuspensionSourceManual,
			Reason:     reasonManualPause,
			Message:    "Manual pause keeps the RTJ stopped until desiredState returns to Running.",
			ObservedAt: &now,
		})
		return clearCondition(job, conditionTypeKueueSuspended) || changed
	default:
		changed := setCurrentSuspension(job, nil)
		return clearCondition(job, conditionTypeKueueSuspended) || changed
	}
}

func setCurrentSuspension(job *trainingv1alpha1.ResumableTrainingJob, status *trainingv1alpha1.SuspensionStatus) bool {
	current := job.Status.CurrentSuspension
	if currentSuspensionEqual(current, status) {
		return false
	}
	if status == nil {
		job.Status.CurrentSuspension = nil
		return true
	}
	copied := *status
	job.Status.CurrentSuspension = &copied
	return true
}

func currentSuspensionEqual(left, right *trainingv1alpha1.SuspensionStatus) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	}

	return left.Suspended == right.Suspended &&
		left.Source == right.Source &&
		left.Reason == right.Reason &&
		left.Message == right.Message &&
		timesEqual(left.ObservedAt, right.ObservedAt)
}

func timesEqual(left, right *metav1.Time) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return left.Equal(right)
	}
}

func setCondition(
	job *trainingv1alpha1.ResumableTrainingJob,
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
	message string,
	now metav1.Time,
) bool {
	before := len(job.Status.Conditions)
	current := meta.FindStatusCondition(job.Status.Conditions, conditionType)
	if current != nil &&
		current.Status == status &&
		current.Reason == reason &&
		current.Message == message &&
		current.ObservedGeneration == job.Generation {
		return false
	}

	meta.SetStatusCondition(&job.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
		ObservedGeneration: job.Generation,
	})
	if len(job.Status.Conditions) != before {
		return true
	}
	updated := meta.FindStatusCondition(job.Status.Conditions, conditionType)
	return updated != nil &&
		(updated.Status != currentStatus(current) ||
			updated.Reason != currentReason(current) ||
			updated.Message != currentMessage(current) ||
			updated.ObservedGeneration != currentObservedGeneration(current))
}

func clearCondition(job *trainingv1alpha1.ResumableTrainingJob, conditionType string) bool {
	before := len(job.Status.Conditions)
	filtered := job.Status.Conditions[:0]
	for _, condition := range job.Status.Conditions {
		if condition.Type == conditionType {
			continue
		}
		filtered = append(filtered, condition)
	}
	job.Status.Conditions = filtered
	return len(job.Status.Conditions) != before
}

func currentStatus(condition *metav1.Condition) metav1.ConditionStatus {
	if condition == nil {
		return ""
	}
	return condition.Status
}

func currentReason(condition *metav1.Condition) string {
	if condition == nil {
		return ""
	}
	return condition.Reason
}

func currentMessage(condition *metav1.Condition) string {
	if condition == nil {
		return ""
	}
	return condition.Message
}

func currentObservedGeneration(condition *metav1.Condition) int64 {
	if condition == nil {
		return 0
	}
	return condition.ObservedGeneration
}

// syncAdmissionStatus updates the RTJ admission status with the admitted
// worker count, preferred worker count, and admitted flavors. Returns true
// when any field changed.
func syncAdmissionStatus(
	job *trainingv1alpha1.ResumableTrainingJob,
	admittedWorkerCount int32,
	preferredWorkerCount int32,
	admittedFlavors map[string]string,
) bool {
	desired := &trainingv1alpha1.AdmissionStatus{
		AdmittedWorkerCount:  admittedWorkerCount,
		PreferredWorkerCount: preferredWorkerCount,
		AdmittedFlavors:      admittedFlavors,
	}

	if admissionStatusEqual(job.Status.Admission, desired) {
		return false
	}
	job.Status.Admission = desired
	return true
}

// clearAdmissionStatus resets the admission status when the RTJ is not admitted.
func clearAdmissionStatus(job *trainingv1alpha1.ResumableTrainingJob) bool {
	if job.Status.Admission == nil {
		return false
	}
	job.Status.Admission = nil
	return true
}

// syncRestoreStatus records details about the most recent checkpoint restore.
// checkpointWorldSize is the world size from the selected checkpoint manifest.
// restoreWorldSize is the admitted world size at which the restore will run.
func syncRestoreStatus(
	job *trainingv1alpha1.ResumableTrainingJob,
	checkpointWorldSize int32,
	restoreWorldSize int32,
) bool {
	mode := trainingv1alpha1.RestoreModeSameSize
	if checkpointWorldSize != restoreWorldSize {
		mode = trainingv1alpha1.RestoreModeReshard
	}

	desired := &trainingv1alpha1.RestoreStatus{
		LastCheckpointWorldSize: checkpointWorldSize,
		LastRestoreWorldSize:    restoreWorldSize,
		RestoreMode:             mode,
	}

	if restoreStatusEqual(job.Status.Restore, desired) {
		return false
	}
	job.Status.Restore = desired
	return true
}

func admissionStatusEqual(left, right *trainingv1alpha1.AdmissionStatus) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	}
	if left.AdmittedWorkerCount != right.AdmittedWorkerCount {
		return false
	}
	if left.PreferredWorkerCount != right.PreferredWorkerCount {
		return false
	}
	if left.ActiveWorkerCount != right.ActiveWorkerCount {
		return false
	}
	return stringMapsEqual(left.AdmittedFlavors, right.AdmittedFlavors)
}

func restoreStatusEqual(left, right *trainingv1alpha1.RestoreStatus) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	}
	return left.LastCheckpointWorldSize == right.LastCheckpointWorldSize &&
		left.LastRestoreWorldSize == right.LastRestoreWorldSize &&
		left.RestoreMode == right.RestoreMode
}

func stringMapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for k, v := range left {
		if right[k] != v {
			return false
		}
	}
	return true
}

// --- Phase 6: Manager-mode status helpers ---

const (
	reasonLocalExecutionSuppressed = "LocalExecutionSuppressed"
)

// markManagerSuppressed sets the MultiCluster status fields that indicate
// this RTJ's local runtime execution has been suppressed because the
// operator is running in manager mode and the RTJ is managed by MultiKueue.
//
// Returns true when any status field changed.
func markManagerSuppressed(job *trainingv1alpha1.ResumableTrainingJob, now metav1.Time) bool {
	changed := false

	if job.Status.MultiCluster == nil {
		job.Status.MultiCluster = &trainingv1alpha1.MultiClusterStatus{}
		changed = true
	}
	mc := job.Status.MultiCluster

	if !mc.LocalExecutionSuppressed {
		mc.LocalExecutionSuppressed = true
		changed = true
	}

	if mc.DispatchPhase == "" {
		mc.DispatchPhase = trainingv1alpha1.DispatchPhasePending
		changed = true
	}

	message := "Manager mode: local child JobSet creation is suppressed for this MultiKueue-managed RTJ. Runtime execution is delegated to a remote worker cluster."
	if job.Status.SetPhase(trainingv1alpha1.PhaseQueued, reasonLocalExecutionSuppressed, message, now) {
		changed = true
	}

	if job.Status.ObservedGeneration != job.Generation {
		job.Status.ObservedGeneration = job.Generation
		changed = true
	}
	return changed
}

// --- Phase 5: Telemetry lifecycle helpers ---
//
// These helpers layer Phase 5 telemetry recording on top of existing mark*
// functions. They are called by the main reconciler and the priority shaping
// controller to keep priority shaping status in sync with lifecycle events.
// They do NOT modify existing Phase 4 behavior—they only populate
// PriorityShapingStatus fields when a policy is attached.

// recordYieldForTelemetry appends a yield event to the yield history
// annotation and updates the PriorityShapingStatus.LastYieldTime.
// Call after markStopRequested when the stop source is a Kueue preemption
// or manual pause. The yieldWindow controls how far back the windowed
// yield count extends.
//
// Returns true when the RTJ's metadata or status was changed.
func recordYieldForTelemetry(
	job *trainingv1alpha1.ResumableTrainingJob,
	now metav1.Time,
	yieldWindow time.Duration,
) bool {
	if !job.IsPriorityShapingEnabled() {
		return false
	}
	changed := RecordYieldEvent(job, now, yieldWindow)
	if job.Status.PriorityShaping == nil {
		job.Status.PriorityShaping = &trainingv1alpha1.PriorityShapingStatus{}
	}
	if !timesEqual(job.Status.PriorityShaping.LastYieldTime, &now) {
		job.Status.PriorityShaping.LastYieldTime = &now
		changed = true
	}
	return changed
}

// recordResumeForTelemetry updates the PriorityShapingStatus.LastResumeTime
// when the RTJ transitions from Restoring to Running. Call after markRunning
// when the previous phase was Restoring.
//
// Returns true when the RTJ's status was changed.
func recordResumeForTelemetry(
	job *trainingv1alpha1.ResumableTrainingJob,
	now metav1.Time,
) bool {
	if !job.IsPriorityShapingEnabled() {
		return false
	}
	if job.Status.PriorityShaping == nil {
		job.Status.PriorityShaping = &trainingv1alpha1.PriorityShapingStatus{}
	}
	if !timesEqual(job.Status.PriorityShaping.LastResumeTime, &now) {
		job.Status.PriorityShaping.LastResumeTime = &now
		changed := true
		// Record applied policy ref.
		if job.Spec.PriorityPolicyRef != nil && job.Status.PriorityShaping.AppliedPolicyRef != job.Spec.PriorityPolicyRef.Name {
			job.Status.PriorityShaping.AppliedPolicyRef = job.Spec.PriorityPolicyRef.Name
			changed = true
		}
		return changed
	}
	return false
}

// clearPriorityShapingOnQueued clears the priority shaping status fields
// that are only meaningful while running. Called when the RTJ transitions
// to Queued so that the priority reverts to base (Phase 5 design: queued
// RTJs reset to base priority).
//
// Returns true when the RTJ's status was changed.
func clearPriorityShapingOnQueued(
	job *trainingv1alpha1.ResumableTrainingJob,
) bool {
	if job.Status.PriorityShaping == nil {
		return false
	}
	ps := job.Status.PriorityShaping
	changed := false
	// Clear runtime-only fields but preserve historical timestamps
	// (LastYieldTime, LastResumeTime) for the priority shaping controller
	// to use when computing yield budgets on re-admission.
	if ps.PreemptionState != "" {
		ps.PreemptionState = ""
		changed = true
	}
	if ps.PreemptionStateReason != "" {
		ps.PreemptionStateReason = ""
		changed = true
	}
	if ps.ProtectedUntil != nil {
		ps.ProtectedUntil = nil
		changed = true
	}
	if ps.CheckpointAge != "" {
		ps.CheckpointAge = ""
		changed = true
	}
	if ps.EffectivePriority != ps.BasePriority {
		ps.EffectivePriority = ps.BasePriority
		changed = true
	}
	return changed
}

// --- Phase 7: Launch gate, provisioning, and capacity status helpers ---

const (
	conditionTypeCapacityPending              = "CapacityPending"
	conditionTypeProvisioningInProgress       = "ProvisioningInProgress"
	conditionTypeProvisioningFailed           = "ProvisioningFailed"
	conditionTypeTopologyPendingSecondPass    = "TopologyPendingSecondPass"
	conditionTypeLaunchReady                  = "LaunchReady"
	conditionTypeLaunchBlockedConflictingUpdate = "LaunchBlockedByConflictingPodSetUpdate"

	capacityReasonProvisioningSatisfied = "ProvisioningSatisfied"
	capacityReasonQuotaOnlyAdmission    = "QuotaOnlyAdmission"
	capacityReasonProvisioningPending   = "ProvisioningPending"
	capacityReasonNotAdmitted           = "NotAdmitted"
)

// syncLaunchGateStatus updates the RTJ's Phase 7 LaunchGate status from the
// LaunchReadinessView and the gate evaluation result. Returns true if changed.
func syncLaunchGateStatus(
	job *trainingv1alpha1.ResumableTrainingJob,
	gateResult *LaunchGateResult,
	now metav1.Time,
) bool {
	if gateResult == nil || gateResult.LaunchView == nil {
		if job.Status.LaunchGate == nil {
			return false
		}
		job.Status.LaunchGate = nil
		return true
	}

	view := gateResult.LaunchView

	// Build the desired LaunchGateStatus.
	desired := &trainingv1alpha1.LaunchGateStatus{
		Reason:  gateResult.Reason,
		Message: gateResult.Message,
	}

	if gateResult.Ready {
		desired.State = trainingv1alpha1.LaunchGateOpen
	} else {
		desired.State = trainingv1alpha1.LaunchGateBlocked
	}

	// Build admission check summary.
	if len(view.AdmissionChecks) > 0 {
		desired.AdmissionCheckSummary = make(map[string]trainingv1alpha1.AdmissionCheckState, len(view.AdmissionChecks))
		for _, ac := range view.AdmissionChecks {
			desired.AdmissionCheckSummary[ac.Name] = mapCheckState(ac.State)
		}
	}

	// Build topology gate state.
	if !view.TopologyState.Configured {
		desired.TopologyGateState = trainingv1alpha1.TopologyGateNotConfigured
	} else if view.TopologyState.SecondPassPending {
		desired.TopologyGateState = trainingv1alpha1.TopologyGatePending
	} else if view.TopologyState.Assigned {
		desired.TopologyGateState = trainingv1alpha1.TopologyGateAssigned
	} else {
		desired.TopologyGateState = trainingv1alpha1.TopologyGatePending
	}

	if launchGateStatusEqual(job.Status.LaunchGate, desired) {
		return false
	}

	desired.LastTransitionTime = &now
	job.Status.LaunchGate = desired
	return true
}

// syncProvisioningStatus updates the RTJ's Phase 7 Provisioning status from
// the LaunchReadinessView. Returns true if changed.
func syncProvisioningStatus(
	job *trainingv1alpha1.ResumableTrainingJob,
	view *provisioning.LaunchReadinessView,
	now metav1.Time,
) bool {
	if view == nil {
		if job.Status.Provisioning == nil {
			return false
		}
		job.Status.Provisioning = nil
		return true
	}

	desired := &trainingv1alpha1.ProvisioningStatus{
		State: mapProvisioningState(view.Provisioning),
	}

	switch view.Provisioning {
	case provisioning.ProvisioningPending, provisioning.ProvisioningRetry:
		desired.Reason = "BackendProcessing"
		desired.Message = "ProvisioningRequest backend is processing the capacity request."
	case provisioning.ProvisioningProvisioned:
		desired.Reason = "CapacityConfirmed"
		desired.Message = "ProvisioningRequest backend confirmed physical capacity is available."
	case provisioning.ProvisioningFailed:
		desired.Reason = "BackendRejected"
		desired.Message = "ProvisioningRequest backend rejected the capacity request."
	default:
		desired.Reason = "NotConfigured"
		desired.Message = "No ProvisioningRequest AdmissionCheck is configured."
	}

	if view.ProvisioningRequestRef != nil {
		desired.ProvisioningRequestRef = &trainingv1alpha1.ProvisioningRequestReference{
			Name:      view.ProvisioningRequestRef.Name,
			Namespace: view.ProvisioningRequestRef.Namespace,
		}
		desired.Attempt = 1
	}

	if provisioningStatusEqual(job.Status.Provisioning, desired) {
		return false
	}

	desired.LastTransitionTime = &now
	job.Status.Provisioning = desired
	return true
}

// syncCapacityStatus updates the RTJ's Phase 7 Capacity status derived from
// provisioning and admission state. Returns true if changed.
func syncCapacityStatus(
	job *trainingv1alpha1.ResumableTrainingJob,
	view *provisioning.LaunchReadinessView,
) bool {
	if view == nil {
		if job.Status.Capacity == nil {
			return false
		}
		job.Status.Capacity = nil
		return true
	}

	desired := &trainingv1alpha1.CapacityStatus{}

	switch {
	case view.Provisioning == provisioning.ProvisioningProvisioned && view.QuotaReserved:
		desired.GuaranteeActive = true
		desired.Reason = capacityReasonProvisioningSatisfied
	case view.Provisioning == provisioning.ProvisioningNotConfigured && view.QuotaReserved:
		desired.GuaranteeActive = false
		desired.Reason = capacityReasonQuotaOnlyAdmission
	case view.ProvisioningRequestPresent && !view.AllChecksReady:
		desired.GuaranteeActive = false
		desired.Reason = capacityReasonProvisioningPending
	default:
		desired.GuaranteeActive = false
		desired.Reason = capacityReasonNotAdmitted
	}

	if capacityStatusEqual(job.Status.Capacity, desired) {
		return false
	}
	job.Status.Capacity = desired
	return true
}

// syncPhase7Conditions sets Phase 7 conditions based on the gate result.
// Returns true if any condition changed.
func syncPhase7Conditions(
	job *trainingv1alpha1.ResumableTrainingJob,
	gateResult *LaunchGateResult,
	now metav1.Time,
) bool {
	if gateResult == nil || gateResult.LaunchView == nil {
		// Clear all Phase 7 conditions.
		changed := clearCondition(job, conditionTypeCapacityPending)
		changed = clearCondition(job, conditionTypeProvisioningInProgress) || changed
		changed = clearCondition(job, conditionTypeProvisioningFailed) || changed
		changed = clearCondition(job, conditionTypeTopologyPendingSecondPass) || changed
		changed = clearCondition(job, conditionTypeLaunchReady) || changed
		changed = clearCondition(job, conditionTypeLaunchBlockedConflictingUpdate) || changed
		return changed
	}

	view := gateResult.LaunchView
	changed := false

	// CapacityPending
	if view.ProvisioningRequestPresent && !view.AllChecksReady {
		changed = setCondition(job, conditionTypeCapacityPending, metav1.ConditionTrue,
			reasonCapacityPending,
			"Waiting for ProvisioningRequest to be satisfied before launching child runtime.",
			now) || changed
	} else {
		changed = clearCondition(job, conditionTypeCapacityPending) || changed
	}

	// ProvisioningInProgress
	if view.Provisioning == provisioning.ProvisioningPending || view.Provisioning == provisioning.ProvisioningRetry {
		changed = setCondition(job, conditionTypeProvisioningInProgress, metav1.ConditionTrue,
			reasonProvisioningInProgress,
			"ProvisioningRequest backend is processing the capacity request.",
			now) || changed
	} else {
		changed = clearCondition(job, conditionTypeProvisioningInProgress) || changed
	}

	// ProvisioningFailed
	if view.Provisioning == provisioning.ProvisioningFailed {
		changed = setCondition(job, conditionTypeProvisioningFailed, metav1.ConditionTrue,
			reasonProvisioningFailed,
			"ProvisioningRequest backend rejected the capacity request.",
			now) || changed
	} else {
		changed = clearCondition(job, conditionTypeProvisioningFailed) || changed
	}

	// TopologyPendingSecondPass
	if view.TopologyState.Configured && view.TopologyState.SecondPassPending {
		changed = setCondition(job, conditionTypeTopologyPendingSecondPass, metav1.ConditionTrue,
			reasonTopologyPendingSecondPass,
			"Topology is configured but the topology assignment is not yet available (second-pass pending).",
			now) || changed
	} else {
		changed = clearCondition(job, conditionTypeTopologyPendingSecondPass) || changed
	}

	// LaunchReady
	if gateResult.Ready {
		changed = setCondition(job, conditionTypeLaunchReady, metav1.ConditionTrue,
			reasonLaunchReady,
			"All launch gates satisfied; child runtime may be created.",
			now) || changed
	} else {
		changed = clearCondition(job, conditionTypeLaunchReady) || changed
	}

	return changed
}

// setLaunchBlockedByConflictingUpdate sets the
// LaunchBlockedByConflictingPodSetUpdate condition. Returns true if changed.
func setLaunchBlockedByConflictingUpdate(
	job *trainingv1alpha1.ResumableTrainingJob,
	message string,
	now metav1.Time,
) bool {
	return setCondition(job, conditionTypeLaunchBlockedConflictingUpdate,
		metav1.ConditionTrue,
		reasonLaunchBlockedByConflictingUpdate,
		message,
		now)
}

// --- Phase 7 status comparison helpers ---

func mapCheckState(state provisioning.CheckStateClassification) trainingv1alpha1.AdmissionCheckState {
	switch state {
	case provisioning.CheckReady:
		return trainingv1alpha1.AdmissionCheckReady
	case provisioning.CheckPending:
		return trainingv1alpha1.AdmissionCheckPending
	case provisioning.CheckRetry:
		return trainingv1alpha1.AdmissionCheckRetry
	case provisioning.CheckRejected:
		return trainingv1alpha1.AdmissionCheckRejected
	default:
		return trainingv1alpha1.AdmissionCheckPending
	}
}

func mapProvisioningState(state provisioning.ProvisioningClassification) trainingv1alpha1.ProvisioningState {
	switch state {
	case provisioning.ProvisioningNotConfigured:
		return trainingv1alpha1.ProvisioningNotConfigured
	case provisioning.ProvisioningPending, provisioning.ProvisioningRetry:
		return trainingv1alpha1.ProvisioningPending
	case provisioning.ProvisioningProvisioned:
		return trainingv1alpha1.ProvisioningProvisioned
	case provisioning.ProvisioningFailed:
		return trainingv1alpha1.ProvisioningFailed
	default:
		return trainingv1alpha1.ProvisioningNotConfigured
	}
}

func launchGateStatusEqual(left, right *trainingv1alpha1.LaunchGateStatus) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	}
	if left.State != right.State || left.Reason != right.Reason ||
		left.Message != right.Message || left.TopologyGateState != right.TopologyGateState {
		return false
	}
	if len(left.AdmissionCheckSummary) != len(right.AdmissionCheckSummary) {
		return false
	}
	for k, v := range left.AdmissionCheckSummary {
		if right.AdmissionCheckSummary[k] != v {
			return false
		}
	}
	return true
}

func provisioningStatusEqual(left, right *trainingv1alpha1.ProvisioningStatus) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	}
	if left.State != right.State || left.Reason != right.Reason ||
		left.Message != right.Message || left.Attempt != right.Attempt {
		return false
	}
	return provisioningRefEqual(left.ProvisioningRequestRef, right.ProvisioningRequestRef)
}

func provisioningRefEqual(left, right *trainingv1alpha1.ProvisioningRequestReference) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	}
	return left.Name == right.Name && left.Namespace == right.Namespace
}

func capacityStatusEqual(left, right *trainingv1alpha1.CapacityStatus) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	}
	return left.GuaranteeActive == right.GuaranteeActive && left.Reason == right.Reason
}
