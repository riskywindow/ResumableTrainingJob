# Remote Pause/Resume Propagation Model

- Phase: 6, Session 8
- Status: Implemented

## Overview

This document describes how pause/resume control propagates from the
manager cluster to a remote worker for MultiKueue-managed RTJs.

## Propagation Mechanism

The Kueue generic external-framework adapter does **not propagate spec
mutations** to running remote jobs (Session 4, OQ-2). When the manager
RTJ's spec changes (e.g., `desiredState: Paused`), the adapter detects
the spec drift between the local (manager) and remote (worker) RTJ, and
handles it by **deleting the remote Workload and RTJ, then recreating
both with the updated spec**.

This means remote pause uses the adapter's delete-recreate cycle rather
than a graceful in-place yield signal.

### Pause Flow

```
User patches manager RTJ: spec.control.desiredState = "Paused"
     |
     v
Kueue adapter detects spec drift (desiredState changed)
     |
     v
Adapter deletes remote Workload + remote RTJ on worker
     |
     v
Worker training pods are terminated (cascading delete)
     |
     v
Adapter creates new remote RTJ with desiredState = "Paused"
     |
     v
Worker controller enters manual hold path (PendingPaused)
     |
     v
Adapter mirrors worker status to manager (phase = Pending)
     |
     v
Manager controller detects:
  - isPauseRequested = true
  - hasRemoteStatusSignal = false (new remote has no active JobSet)
  - Preserves the pre-pause remote checkpoint summary
     |
     v
Manager RTJ: phase = Paused, multiCluster.remotePhase = Paused
             multiCluster.remoteCheckpoint = preserved from before pause
```

### Resume Flow

```
User patches manager RTJ: spec.control.desiredState = "Running"
     |
     v
Kueue adapter detects spec drift (desiredState changed)
     |
     v
Adapter deletes remote RTJ (which was in PendingPaused state)
     |
     v
Adapter creates new remote RTJ with desiredState = "Running"
     |
     v
Worker Kueue admits the new RTJ
     |
     v
Worker controller finds checkpoint in shared store
  (same storageURI, same identity → compatible checkpoint)
     |
     v
Worker resumes from checkpoint: new child JobSet created
     |
     v
Adapter mirrors worker status to manager
     |
     v
Manager RTJ: phase = Running, multiCluster.remotePhase = Running
             multiCluster.remoteCheckpoint updated with new checkpoint
```

## Difference from Single-Cluster Pause

| Aspect | Single-Cluster | Multi-Cluster (Remote) |
|--------|---------------|----------------------|
| Pause signal | Control ConfigMap (in-band) | Spec drift → delete-recreate |
| Yield behavior | Graceful yield at step boundary | Periodic checkpoint + pod termination |
| Checkpoint source | Yield-triggered write | Latest periodic checkpoint before teardown |
| Resume mechanism | Same RTJ, new child JobSet | New remote RTJ, new child JobSet |
| Checkpoint store | Local or shared | Shared (required for cross-worker) |

**Key difference:** In single-cluster mode, the pause flow includes a
graceful yield at a step boundary with a fresh checkpoint. In multi-
cluster mode, the pause relies on the training job's periodic checkpoint
writes. The latest periodic checkpoint before teardown is the recovery
point.

This means multi-cluster pause has a potentially larger checkpoint gap
(up to one checkpoint interval) compared to single-cluster mode's
step-boundary yield. For most practical workloads, periodic checkpointing
at reasonable intervals (e.g., 10-30 seconds) limits this gap.

## Manager-Side Controller Plumbing

The `reconcileManagerIntent` method in Session 8 adds:

1. **Pause detection:** `isRemotePauseRequested(job)` checks if
   `spec.control.desiredState == "Paused"`.

2. **Checkpoint preservation:** Before `syncRemoteStatus` runs (which
   reads the adapter-mirrored status), the controller snapshots
   `multiCluster.remoteCheckpoint`. After sync, if the checkpoint was
   cleared (because the adapter mirrored empty status from the new
   remote RTJ), the controller restores it.

3. **Paused marking:** When `isPauseRequested && !hasRemoteStatusSignal`,
   the controller calls `markRemotePaused` which sets:
   - `status.phase = Paused`
   - `status.multiCluster.remotePhase = Paused`
   - `reason = RemotePauseComplete`

4. **Requeue during transition:** When the remote is still active but
   pause is requested, the controller requeues (5s) to poll for the
   adapter's teardown completion.

## Shared Checkpoint Store Requirement

Cross-worker resume requires all worker clusters to access the same
S3-compatible checkpoint store. The resume path on the new worker finds
the checkpoint written by the previous worker using the same
`spec.checkpoint.storageURI` and `spec.identity` compatibility matching.

See `docs/phase6/shared-checkpoint-store.md` for store setup and
endpoint validation details.

## E2E Test Coverage

`TestMultiClusterRemotePauseResume` validates the full cycle:

1. Submit RTJ to manager → dispatched to worker-1
2. Worker runs and writes periodic checkpoint
3. Manager RTJ patched to Paused
4. Manager becomes Paused with preserved checkpoint
5. Checkpoint verified in shared S3 store
6. No manager-local child JobSet
7. Remote phase surfaced on manager status
8. Manager RTJ patched to Running
9. Worker resumes from shared checkpoint
10. New checkpoint written (different ID → monotonic progression)

## Files

| File | Purpose |
|------|---------|
| `internal/controller/remote_status.go` | `isRemotePauseRequested`, `markRemotePaused`, `preserveRemoteCheckpoint`, `restoreRemoteCheckpoint` |
| `internal/controller/resumabletrainingjob_controller.go` | Updated `reconcileManagerIntent` with pause/resume handling |
| `test/e2e/multicluster_remote_pause_resume_test.go` | E2E test |
| `test/e2e/testdata/phase6/rtj-remote-pause-resume.yaml` | Test RTJ template |
| `test/e2e/phase6_helpers_test.go` | Extended view types for checkpoint fields |
