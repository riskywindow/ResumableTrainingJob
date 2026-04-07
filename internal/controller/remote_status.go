package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// -------------------------------------------------------------------------
// Remote status reflector
// -------------------------------------------------------------------------
//
// In manager mode, the Kueue generic adapter periodically copies the full
// .status from the remote worker-side RTJ to the manager-side RTJ via an
// unstructured status patch. After each adapter sync:
//
//   - status.phase         = remote worker's phase
//   - status.lastCompleted = remote worker's lastCompletedCheckpoint
//   - status.activeJobSet  = remote worker's activeJobSetName (non-empty when running)
//   - status.multiCluster  = nil (the worker does not populate this)
//
// The remote status reflector runs on each manager reconcile and:
//   1. Detects whether the adapter has mirrored remote status (heuristic:
//      activeJobSetName is non-empty or currentRunAttempt > 0).
//   2. Re-populates status.multiCluster with the execution cluster,
//      dispatch phase, remote phase summary, and remote checkpoint summary.
//   3. Sets LocalExecutionSuppressed = true (always, for manager mode).
//
// This is the "smallest coherent remote-status path" described in the
// Phase 6 mission.

const (
	reasonRemoteStatusActive   = "RemoteStatusActive"
	reasonRemoteDispatched     = "RemoteDispatched"
	reasonRemotePending        = "RemotePending"
	reasonRemoteStatusUnknown  = "RemoteStatusUnknown"
	reasonRemotePauseComplete  = "RemotePauseComplete"

	messageRemoteActive     = "Remote worker cluster is executing this RTJ; status is mirrored from the worker."
	messageRemoteDispatched = "RTJ has been dispatched to a worker cluster; waiting for the worker to initialize."
	messageRemotePending    = "Manager mode: waiting for MultiKueue to dispatch this RTJ to a worker cluster."
	messageRemotePaused     = "Remote RTJ execution has been paused. Checkpoint preserved in shared store for cross-worker resume."
)

// syncRemoteStatus populates the MultiClusterStatus fields on a manager-
// side RTJ based on the current status (which may have been mirrored from
// the remote worker by Kueue's adapter) and the resolved execution cluster.
//
// Returns true when any status field changed.
func syncRemoteStatus(
	job *trainingv1alpha1.ResumableTrainingJob,
	executionCluster string,
	now metav1.Time,
) bool {
	if job.Status.MultiCluster == nil {
		job.Status.MultiCluster = &trainingv1alpha1.MultiClusterStatus{}
	}
	mc := job.Status.MultiCluster

	changed := false

	// Always mark local execution as suppressed in manager mode.
	if !mc.LocalExecutionSuppressed {
		mc.LocalExecutionSuppressed = true
		changed = true
	}

	// Set execution cluster.
	if mc.ExecutionCluster != executionCluster {
		mc.ExecutionCluster = executionCluster
		changed = true
	}

	// Determine dispatch phase and remote phase.
	dispatchPhase, remotePhase := classifyRemoteState(job, executionCluster)
	if mc.DispatchPhase != dispatchPhase {
		mc.DispatchPhase = dispatchPhase
		changed = true
	}
	if mc.RemotePhase != remotePhase {
		mc.RemotePhase = remotePhase
		changed = true
	}

	// Build remote object reference when execution cluster is known.
	changed = syncRemoteObjectRef(mc, job, executionCluster) || changed

	// Mirror remote checkpoint summary.
	changed = syncRemoteCheckpointSummary(mc, job.Status.LastCompletedCheckpoint) || changed

	// Mirror observed generation from the remote status.
	if job.Status.ObservedGeneration != mc.RemoteObservedGeneration && hasRemoteStatusSignal(job) {
		mc.RemoteObservedGeneration = job.Status.ObservedGeneration
		changed = true
	}

	return changed
}

// classifyRemoteState determines the dispatch phase and remote phase
// from the current RTJ status and resolved execution cluster.
func classifyRemoteState(
	job *trainingv1alpha1.ResumableTrainingJob,
	executionCluster string,
) (trainingv1alpha1.MultiClusterDispatchPhase, trainingv1alpha1.ResumableTrainingJobPhase) {
	if hasRemoteStatusSignal(job) {
		// The adapter has mirrored remote status. The current status.phase
		// IS the remote worker's phase.
		return trainingv1alpha1.DispatchPhaseActive, job.Status.Phase
	}
	if executionCluster != "" {
		// We know the cluster but haven't received mirrored status yet.
		return trainingv1alpha1.DispatchPhaseDispatched, ""
	}
	// Not yet dispatched.
	return trainingv1alpha1.DispatchPhasePending, ""
}

// hasRemoteStatusSignal returns true when the RTJ's status contains
// signals that it was populated by the remote worker's status (mirrored
// by the Kueue adapter). Heuristic: the worker sets activeJobSetName or
// increments currentRunAttempt, which the manager never does.
func hasRemoteStatusSignal(job *trainingv1alpha1.ResumableTrainingJob) bool {
	return job.Status.ActiveJobSetName != "" || job.Status.CurrentRunAttempt > 0
}

// syncRemoteObjectRef populates the RemoteObjectRef when the execution
// cluster is known. The remote RTJ has the same name and namespace as
// the manager-side RTJ (per the generic adapter's copy behavior).
func syncRemoteObjectRef(
	mc *trainingv1alpha1.MultiClusterStatus,
	job *trainingv1alpha1.ResumableTrainingJob,
	executionCluster string,
) bool {
	if executionCluster == "" {
		if mc.RemoteObjectRef != nil {
			mc.RemoteObjectRef = nil
			return true
		}
		return false
	}

	desired := &trainingv1alpha1.RemoteObjectReference{
		Cluster:   executionCluster,
		Namespace: job.Namespace,
		Name:      job.Name,
	}
	if remoteObjectRefEqual(mc.RemoteObjectRef, desired) {
		return false
	}
	mc.RemoteObjectRef = desired
	return true
}

// syncRemoteCheckpointSummary mirrors the latest completed checkpoint from
// the worker status into the MultiClusterStatus.RemoteCheckpoint summary.
func syncRemoteCheckpointSummary(
	mc *trainingv1alpha1.MultiClusterStatus,
	checkpoint *trainingv1alpha1.CheckpointReference,
) bool {
	if checkpoint == nil {
		if mc.RemoteCheckpoint != nil {
			mc.RemoteCheckpoint = nil
			return true
		}
		return false
	}

	desired := &trainingv1alpha1.RemoteCheckpointSummary{
		LastCompletedCheckpointID:   checkpoint.ID,
		LastCompletedCheckpointTime: checkpoint.CompletionTime,
		StorageURI:                  checkpoint.StorageURI,
	}
	if remoteCheckpointSummaryEqual(mc.RemoteCheckpoint, desired) {
		return false
	}
	mc.RemoteCheckpoint = desired
	return true
}

// -------------------------------------------------------------------------
// Equality helpers
// -------------------------------------------------------------------------

func remoteObjectRefEqual(a, b *trainingv1alpha1.RemoteObjectReference) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Cluster == b.Cluster &&
		a.Namespace == b.Namespace &&
		a.Name == b.Name &&
		a.UID == b.UID
}

func remoteCheckpointSummaryEqual(a, b *trainingv1alpha1.RemoteCheckpointSummary) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.LastCompletedCheckpointID == b.LastCompletedCheckpointID &&
		a.StorageURI == b.StorageURI &&
		timesEqual(a.LastCompletedCheckpointTime, b.LastCompletedCheckpointTime)
}

// -------------------------------------------------------------------------
// Phase 7: Remote launch summary
// -------------------------------------------------------------------------
//
// When the Kueue adapter mirrors the worker's full .status to the manager
// RTJ, Phase 7 status fields (launchGate, provisioning, startupRecovery,
// capacity) are included in the mirror. The manager controller does NOT
// populate these fields itself — they are entirely worker-sourced.
//
// buildRemoteLaunchSummary extracts key Phase 7 indicators from the
// already-mirrored status for structured logging on the manager side.
// This enables operators to observe worker-side launch gating, provisioning
// progress, and capacity guarantees from the manager cluster's logs.

// remoteLaunchSummary captures Phase 7 state extracted from a manager-side
// RTJ whose status was mirrored from the worker by the Kueue adapter.
// Used for manager-side observability, not for decision-making.
type remoteLaunchSummary struct {
	// LaunchGateState is the worker's launch gate state (Open, Blocked, or empty).
	LaunchGateState string

	// ProvisioningState is the worker's provisioning state (NotConfigured,
	// Pending, Provisioned, Failed, or empty).
	ProvisioningState string

	// CapacityGuaranteeActive is true when the worker reports an active
	// capacity guarantee (provisioning satisfied + quota reserved).
	CapacityGuaranteeActive bool

	// StartupState is the worker's startup recovery state (NotStarted,
	// Starting, Running, StartupTimedOut, etc., or empty).
	StartupState string
}

// buildRemoteLaunchSummary extracts a Phase 7 launch summary from the
// RTJ status. Returns zero values for any field not present (Phase 6
// workers will have all fields empty).
func buildRemoteLaunchSummary(job *trainingv1alpha1.ResumableTrainingJob) remoteLaunchSummary {
	var summary remoteLaunchSummary
	if job.Status.LaunchGate != nil {
		summary.LaunchGateState = string(job.Status.LaunchGate.State)
	}
	if job.Status.Provisioning != nil {
		summary.ProvisioningState = string(job.Status.Provisioning.State)
	}
	if job.Status.Capacity != nil {
		summary.CapacityGuaranteeActive = job.Status.Capacity.GuaranteeActive
	}
	if job.Status.StartupRecovery != nil {
		summary.StartupState = string(job.Status.StartupRecovery.StartupState)
	}
	return summary
}

// hasPhase7RemoteStatus returns true when the mirrored status contains
// any Phase 7 status fields from the worker. This indicates the worker
// is running with Phase 7 features active.
func hasPhase7RemoteStatus(job *trainingv1alpha1.ResumableTrainingJob) bool {
	return job.Status.LaunchGate != nil ||
		job.Status.Provisioning != nil ||
		job.Status.StartupRecovery != nil ||
		job.Status.Capacity != nil
}

// -------------------------------------------------------------------------
// Phase 8: Remote DRA / device summary
// -------------------------------------------------------------------------
//
// When the Kueue adapter mirrors the worker's full .status to the manager
// RTJ, Phase 8 status fields (devices.*) are included in the mirror. The
// manager controller does NOT populate these fields itself and does NOT
// create ResourceClaimTemplates or ResourceClaims — they are entirely
// worker-sourced.
//
// buildRemoteDRASummary extracts key Phase 8 DRA indicators from the
// already-mirrored status for structured logging on the manager side.
// This enables operators to observe worker-side device allocation state
// from the manager cluster's logs.

// remoteDRASummary captures Phase 8 DRA state extracted from a manager-side
// RTJ whose status was mirrored from the worker by the Kueue adapter.
// Used for manager-side observability, not for decision-making.
type remoteDRASummary struct {
	// DeviceMode is the worker's device mode (DRA, Disabled, or empty).
	DeviceMode string

	// DeviceProfileFingerprint is the worker's current device profile fingerprint.
	DeviceProfileFingerprint string

	// ClaimAllocationState is the worker's claim allocation state
	// (Pending, Allocated, Failed, or empty).
	ClaimAllocationState string

	// AllocatedClaimCount is the count of allocated claims on the worker.
	AllocatedClaimCount int32

	// RequestedDeviceClasses lists the device classes the worker is using.
	RequestedDeviceClasses []string
}

// buildRemoteDRASummary extracts a Phase 8 DRA summary from the
// RTJ status. Returns zero values for any field not present (Phase 7
// and earlier workers will have all fields empty).
func buildRemoteDRASummary(job *trainingv1alpha1.ResumableTrainingJob) remoteDRASummary {
	var summary remoteDRASummary
	if job.Status.Devices == nil {
		return summary
	}
	ds := job.Status.Devices
	summary.DeviceMode = string(ds.DeviceMode)
	summary.DeviceProfileFingerprint = ds.CurrentDeviceProfileFingerprint
	summary.ClaimAllocationState = string(ds.ClaimAllocationState)
	summary.AllocatedClaimCount = ds.AllocatedClaimCount
	summary.RequestedDeviceClasses = ds.RequestedDeviceClasses
	return summary
}

// hasPhase8RemoteStatus returns true when the mirrored status contains
// Phase 8 DRA device status fields from the worker. This indicates the
// worker is running with Phase 8 DRA features active.
func hasPhase8RemoteStatus(job *trainingv1alpha1.ResumableTrainingJob) bool {
	return job.Status.Devices != nil &&
		job.Status.Devices.DeviceMode != "" &&
		job.Status.Devices.DeviceMode != trainingv1alpha1.DeviceModeDisabled
}

// -------------------------------------------------------------------------
// Phase 9: Remote elasticity summary
// -------------------------------------------------------------------------
//
// When the Kueue adapter mirrors the worker's full .status to the manager
// RTJ, Phase 9 status fields (elasticity.*) are included in the mirror.
// The manager controller does NOT evaluate elastic plans, execute resize
// operations, publish reclaimablePods, or create any reclaim helper state
// for remote RTJs — these are entirely worker-side operations.
//
// buildRemoteElasticitySummary extracts key Phase 9 elasticity indicators
// from the already-mirrored status for structured logging on the manager
// side. This enables operators to observe worker-side resize state,
// reclaim progress, and execution mode from the manager cluster's logs.

// remoteElasticitySummary captures Phase 9 elasticity state extracted from
// a manager-side RTJ whose status was mirrored from the worker by the
// Kueue adapter. Used for manager-side observability, not for decision-making.
type remoteElasticitySummary struct {
	// ResizeState is the worker's resize state (Idle, Pending, InProgress,
	// Blocked, Completed, Failed, or empty).
	ResizeState string

	// ResizePath is the worker's resize path (InPlace, CheckpointAndRelaunch,
	// or empty).
	ResizePath string

	// TargetWorkerCount is the worker's current resize target.
	TargetWorkerCount int32

	// ActiveWorkerCount is the worker's observed active pod count.
	ActiveWorkerCount int32

	// AdmittedWorkerCount is the worker's current Kueue admission size.
	AdmittedWorkerCount int32

	// ReclaimablePodsPublished is true when the worker has written
	// reclaimablePods to its local Workload for in-place shrink.
	ReclaimablePodsPublished bool

	// InPlaceShrinkSupported indicates whether the worker's runtime
	// supports in-place shrink.
	InPlaceShrinkSupported bool

	// CurrentExecutionMode is the worker's execution mode (Fixed or Elastic).
	CurrentExecutionMode string
}

// buildRemoteElasticitySummary extracts a Phase 9 elasticity summary from
// the RTJ status. Returns zero values for any field not present (Phase 8
// and earlier workers will have all fields empty).
func buildRemoteElasticitySummary(job *trainingv1alpha1.ResumableTrainingJob) remoteElasticitySummary {
	var summary remoteElasticitySummary
	if job.Status.Elasticity == nil {
		return summary
	}
	es := job.Status.Elasticity
	summary.ResizeState = string(es.ResizeState)
	summary.ResizePath = string(es.ResizePath)
	summary.TargetWorkerCount = es.TargetWorkerCount
	summary.ActiveWorkerCount = es.ActiveWorkerCount
	summary.AdmittedWorkerCount = es.AdmittedWorkerCount
	summary.ReclaimablePodsPublished = es.ReclaimablePodsPublished
	summary.InPlaceShrinkSupported = es.InPlaceShrinkSupported
	summary.CurrentExecutionMode = string(es.CurrentExecutionMode)
	return summary
}

// hasPhase9RemoteStatus returns true when the mirrored status contains
// Phase 9 elasticity status fields from the worker. This indicates the
// worker is running with Phase 9 elasticity features active.
func hasPhase9RemoteStatus(job *trainingv1alpha1.ResumableTrainingJob) bool {
	return job.Status.Elasticity != nil &&
		job.Status.Elasticity.CurrentExecutionMode != "" &&
		job.Status.Elasticity.CurrentExecutionMode != trainingv1alpha1.ExecutionModeFixed
}

// -------------------------------------------------------------------------
// Remote pause / resume helpers
// -------------------------------------------------------------------------
//
// When the user patches spec.control.desiredState to Paused on a manager-
// side MultiKueue-managed RTJ, the Kueue generic adapter detects the spec
// drift and tears down the remote copy (delete + recreate with the updated
// spec). The new remote RTJ enters the worker's manual-hold path
// (PendingPaused). The manager controller must:
//
//  1. Preserve the last known remote checkpoint summary before the
//     adapter overwrites status with the fresh remote RTJ's empty status.
//  2. Mark the manager-side phase as Paused once the remote is no longer
//     active (the adapter has completed the teardown).
//
// For resume: the user patches desiredState back to Running. The adapter
// sees another spec drift, tears down the Paused remote, and creates a
// new Running remote. The worker resumes from the shared checkpoint store.

// isRemotePauseRequested returns true when the user has requested pause
// on the manager-side RTJ.
func isRemotePauseRequested(job *trainingv1alpha1.ResumableTrainingJob) bool {
	return job.Spec.Control != nil &&
		job.Spec.Control.DesiredState == trainingv1alpha1.DesiredStatePaused
}

// markRemotePaused sets the manager-side RTJ phase to Paused and updates
// the multiCluster.remotePhase to reflect the pause. This is called when
// the remote RTJ has been torn down (no active remote signal) and the
// user has requested pause.
//
// Returns true when any status field changed.
func markRemotePaused(job *trainingv1alpha1.ResumableTrainingJob, now metav1.Time) bool {
	changed := false

	if job.Status.SetPhase(trainingv1alpha1.PhasePaused, reasonRemotePauseComplete, messageRemotePaused, now) {
		changed = true
	}

	if job.Status.MultiCluster == nil {
		job.Status.MultiCluster = &trainingv1alpha1.MultiClusterStatus{}
		changed = true
	}
	mc := job.Status.MultiCluster

	if mc.RemotePhase != trainingv1alpha1.PhasePaused {
		mc.RemotePhase = trainingv1alpha1.PhasePaused
		changed = true
	}

	if !mc.LocalExecutionSuppressed {
		mc.LocalExecutionSuppressed = true
		changed = true
	}

	if job.Status.ObservedGeneration != job.Generation {
		job.Status.ObservedGeneration = job.Generation
		changed = true
	}
	return changed
}

// preserveRemoteCheckpoint captures the current RemoteCheckpointSummary
// so it can be restored after syncRemoteStatus potentially clears it
// (due to the adapter mirroring fresh status from a recreated remote RTJ).
// Returns nil if there is nothing to preserve.
func preserveRemoteCheckpoint(job *trainingv1alpha1.ResumableTrainingJob) *trainingv1alpha1.RemoteCheckpointSummary {
	if job.Status.MultiCluster == nil || job.Status.MultiCluster.RemoteCheckpoint == nil {
		return nil
	}
	cp := *job.Status.MultiCluster.RemoteCheckpoint
	return &cp
}

// restoreRemoteCheckpoint re-applies a previously preserved checkpoint
// summary if syncRemoteStatus cleared it. Returns true when the summary
// was restored.
func restoreRemoteCheckpoint(
	job *trainingv1alpha1.ResumableTrainingJob,
	preserved *trainingv1alpha1.RemoteCheckpointSummary,
) bool {
	if preserved == nil {
		return false
	}
	if job.Status.MultiCluster == nil {
		job.Status.MultiCluster = &trainingv1alpha1.MultiClusterStatus{}
	}
	if job.Status.MultiCluster.RemoteCheckpoint != nil {
		// syncRemoteStatus already populated it from the new remote;
		// do not overwrite.
		return false
	}
	job.Status.MultiCluster.RemoteCheckpoint = preserved
	return true
}
