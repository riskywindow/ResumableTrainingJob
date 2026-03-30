# Phase 6: Remote Status Plumbing

## Overview

In manager mode, the operator does not create local child JobSets or perform
checkpoint I/O. Instead, it relies on Kueue's generic external-framework
adapter to mirror the remote worker-side RTJ's `.status` to the manager-side
RTJ via unstructured status patch.

The remote status reflector runs on each manager reconcile and:

1. Detects whether the adapter has mirrored remote status.
2. Resolves the execution cluster from the Workload admission check.
3. Populates `status.multiCluster` with a debug-friendly summary.

## Approach: Smallest Coherent Path

Since the Kueue generic adapter already copies the **full** `.status` from the
remote worker RTJ to the manager RTJ (Session 4, OQ-3 resolution), the remote
status reflector does not need a separate remote watcher or polling mechanism.
It simply reads the current status (which may have been updated by the adapter)
and extracts the relevant fields.

This is the "smallest coherent remote-status path" described in the Phase 6
mission. No duplicate worker logic runs on the manager.

## Status Fields Surfaced

| Field | Source | Description |
|---|---|---|
| `multiCluster.executionCluster` | Workload admission check message | Worker cluster name from MultiKueue dispatch |
| `multiCluster.remotePhase` | `status.phase` (mirrored) | The remote worker's training phase |
| `multiCluster.remoteCheckpoint` | `status.lastCompletedCheckpoint` (mirrored) | Latest checkpoint summary from the worker |
| `multiCluster.dispatchPhase` | Heuristic | Pending / Dispatched / Active lifecycle |
| `multiCluster.localExecutionSuppressed` | Always true in manager mode | Confirms no local runtime |
| `multiCluster.remoteObjectRef` | Derived from execution cluster + job identity | Points to the remote RTJ copy |

## Dispatch Phase Classification

| State | Dispatch Phase | Signals |
|---|---|---|
| Not yet dispatched | `Pending` | No execution cluster, no remote status signal |
| Dispatched but no status yet | `Dispatched` | Execution cluster known, no `activeJobSetName` or `currentRunAttempt` |
| Worker is running | `Active` | `activeJobSetName` non-empty or `currentRunAttempt > 0` |

## Remote Status Detection Heuristic

The manager detects that the adapter has mirrored remote status by checking:

- `status.activeJobSetName != ""` — only the worker sets this
- `status.currentRunAttempt > 0` — only the worker increments this

These signals are never produced by the manager-mode controller, making them
reliable indicators that the adapter has copied worker status.

## Cluster Resolution

The `WorkloadClusterResolver` extracts the execution cluster name from the
Kueue Workload's admission check status:

1. Reads the Workload referenced by `status.workloadReference`.
2. Scans `status.admissionChecks` for the MultiKueue admission check.
3. When the check state is `Ready`, the `message` field contains the
   worker cluster name (set by Kueue's MultiKueue workload reconciler).

A `StaticClusterResolver` is also provided for testing.

## Files

| File | Purpose |
|---|---|
| `internal/controller/remote_status.go` | Remote status reflector: `syncRemoteStatus`, classification, equality helpers |
| `internal/controller/remote_status_test.go` | 17 tests: unit + integration |
| `internal/remote/cluster_resolver.go` | `ClusterResolver` interface, `WorkloadClusterResolver`, `StaticClusterResolver` |
| `internal/remote/cluster_resolver_test.go` | 11 tests for cluster resolution |

## Interaction with Existing Code

- `reconcileManagerIntent` in the main controller now calls `syncRemoteStatus`
  instead of only `markManagerSuppressed`. The `markManagerSuppressed` helper
  is still called for the pre-dispatch phase (no remote signal yet) to set the
  manager phase to `Queued`.
- The `ClusterResolver` field on `ResumableTrainingJobReconciler` is optional.
  A nil resolver is gracefully handled (execution cluster reported as unknown).
- All existing Phase 1-5 tests pass unchanged (worker mode is unaffected).
