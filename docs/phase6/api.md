# Phase 6 RTJ API Extensions

## Overview

Phase 6 extends the ResumableTrainingJob (RTJ) API to support MultiKueue
external-framework dispatch and manager-visible remote execution status.
All changes are backward-compatible: Phase 5 single-cluster semantics are
preserved when `spec.managedBy` is absent.

## Spec Changes

### `spec.managedBy` (new, optional, immutable)

| Property | Value |
| --- | --- |
| Type | `string` |
| Default | `""` (empty) |
| Max length | 256 |
| Mutability | Immutable once set |
| Author | User |

Identifies the external controller responsible for the RTJ's Workload
lifecycle. Follows the Kubernetes `managedBy` convention (domain-prefixed
value containing `/`).

**Single-cluster path (Phase 5):** When empty or absent, the RTJ behaves
identically to Phase 5. The RTJ operator creates child JobSets locally,
manages checkpoint lifecycle, and drives the full reconciliation loop.

**MultiKueue path (Phase 6):** When set to `kueue.x-k8s.io/multikueue`,
the RTJ is eligible for MultiKueue dispatch to a remote worker cluster.
The manager-mode operator does NOT create local child JobSets; instead,
MultiKueue creates a mirrored RTJ copy on the selected worker cluster.
The worker runs the full Phase 5 reconciliation path unchanged.

**Validation rules:**
- Must be empty or contain at least one `/` (domain-prefix convention).
- Maximum 256 characters.
- Immutable: cannot be changed or removed after creation.

**Example:**

```yaml
spec:
  managedBy: "kueue.x-k8s.io/multikueue"
  queueName: research-a
  # ... rest of spec unchanged
```

## Status Changes

### `status.multiCluster` (new, optional, controller-owned)

Populated only when `spec.managedBy` is set to the MultiKueue controller
value and the operator is running in manager mode. Nil in single-cluster
mode. All fields are controller-owned; users must not write to this section.

#### `status.multiCluster.dispatchPhase`

| Value | Meaning |
| --- | --- |
| `Pending` | RTJ is waiting for MultiKueue to dispatch to a worker cluster. |
| `Dispatched` | MultiKueue has created a remote copy on a worker cluster. |
| `Active` | Worker cluster has acknowledged the RTJ (remote phase populated). |

#### `status.multiCluster.nominatedClusters`

List of worker cluster names that MultiKueue considered for dispatching
this RTJ. Updated by the manager-mode controller based on MultiKueue's
cluster selection.

#### `status.multiCluster.executionCluster`

Name of the worker cluster where the RTJ is currently dispatched and
executing. Empty before dispatch. Changes when MultiKueue re-dispatches
to a different worker (e.g., after pause/resume).

#### `status.multiCluster.remoteObjectRef`

Reference to the remote RTJ copy on the worker cluster:

| Field | Type | Description |
| --- | --- | --- |
| `cluster` | string (required) | Worker cluster name |
| `namespace` | string | Namespace on the worker |
| `name` | string (required) | Name on the worker |
| `uid` | string | UID on the worker |

#### `status.multiCluster.remotePhase`

Mirrors the worker-side RTJ's `.status.phase`. Uses the same
`ResumableTrainingJobPhase` enum (Pending, Running, Paused, etc.).
Empty before the worker has initialized status.

#### `status.multiCluster.remoteCheckpoint`

Lightweight summary of the worker's latest completed checkpoint:

| Field | Type | Description |
| --- | --- | --- |
| `lastCompletedCheckpointID` | string | Checkpoint ID from worker |
| `lastCompletedCheckpointTime` | date-time | Completion timestamp |
| `storageURI` | string | Checkpoint storage URI |

#### `status.multiCluster.remoteObservedGeneration`

The `metadata.generation` of the remote worker-side RTJ as of the last
status mirror. Used as a sync marker to detect when the worker has
observed a spec change propagated through MultiKueue.

#### `status.multiCluster.localExecutionSuppressed`

Boolean indicating that the manager-mode controller has suppressed local
child JobSet creation because this RTJ is managed by MultiKueue. Always
`true` when `spec.managedBy` is set to the MultiKueue value and the
operator runs in manager mode.

## Field Ownership Summary

| Field | Author | Path |
| --- | --- | --- |
| `spec.managedBy` | User | Set at creation time |
| `status.multiCluster.*` | Controller (manager mode) | All subfields |

## Single-Cluster vs MultiKueue Path

| Aspect | Single-cluster (Phase 5) | MultiKueue (Phase 6) |
| --- | --- | --- |
| `spec.managedBy` | Empty / absent | `kueue.x-k8s.io/multikueue` |
| Child JobSet | Created locally | Created on worker by worker operator |
| Checkpoint I/O | Local operator | Worker operator only |
| Status source | Local reconciliation | Mirrored from worker via MultiKueue |
| `status.multiCluster` | Nil | Populated by manager-mode controller |
| Priority shaping | Evaluated locally | Evaluated on worker |
| Topology | Evaluated locally | Evaluated on worker |

## Mirrored Worker Copy Behavior

When MultiKueue dispatches an RTJ to a worker cluster:

1. MultiKueue creates a remote RTJ copy on the worker with the same spec.
2. The worker operator treats the remote copy as a normal RTJ and runs
   the full Phase 5 reconciliation path (admission, launch, checkpoint,
   yield, resume).
3. The worker does not know it was dispatched by a manager. Its behavior
   is identical to a locally-created RTJ.
4. MultiKueue mirrors the worker's RTJ status back to the manager.
5. The manager-mode controller populates `status.multiCluster` from the
   mirrored worker status.
6. Spec mutations on the manager (e.g., `desiredState=Paused`) are
   propagated to the worker by MultiKueue.

## Backward Compatibility

- All new spec fields are optional with zero-value defaults.
- All new status fields are optional and controller-owned.
- Phase 5 manifests decode without error (no new required fields).
- Phase 5 behavior is preserved when `spec.managedBy` is empty.
- Existing printer columns, webhook paths, and CRD short names are
  unchanged.
