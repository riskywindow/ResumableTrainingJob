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

	messageRemoteActive    = "Remote worker cluster is executing this RTJ; status is mirrored from the worker."
	messageRemoteDispatched = "RTJ has been dispatched to a worker cluster; waiting for the worker to initialize."
	messageRemotePending   = "Manager mode: waiting for MultiKueue to dispatch this RTJ to a worker cluster."
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
