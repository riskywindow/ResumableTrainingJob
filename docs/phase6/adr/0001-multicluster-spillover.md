# ADR 0001: Multi-Cluster Checkpoint-Native Spillover

- **Status:** Accepted
- **Date:** 2026-03-29
- **Context:** Phase 6 design lock for the checkpoint-native preemption controller.

## Decision

Phase 6 extends the RTJ system with multi-cluster checkpoint-native
spillover:

1. **MultiKueue external-framework integration for RTJ.**
2. **Manager/worker operator split.**
3. **Shared-checkpoint remote pause/resume.**
4. **Manager-visible remote status.**
5. **Deterministic local three-cluster dev/test profile.**

These capabilities compose with Phase 5's checkpoint-aware priority
shaping, Phase 4's topology-aware pipeline, Phase 3's flavor-aware
rendering, and all prior phase features without replacing them.

## Context

Phase 5 delivered checkpoint-aware priority shaping within a single
Kubernetes cluster. However, production training workloads often span
multiple clusters for resource availability, cost optimization, or
failure isolation. Kueue's MultiKueue feature provides multi-cluster
Workload dispatch for supported job types. Phase 6 extends this to RTJ.

Two gaps motivate Phase 6:

1. **No multi-cluster dispatch for RTJ.** RTJ can only run on the
   cluster where it is created. If that cluster is full, the RTJ waits.
   There is no spillover to other clusters with available capacity.

2. **No cross-cluster resume.** If an RTJ is paused on one cluster, it
   can only resume on the same cluster. There is no mechanism to resume
   training on a different cluster using a shared checkpoint.

Phase 6 addresses both gaps by integrating RTJ with MultiKueue and
requiring a shared checkpoint store for cross-cluster resume.

## Decisions

### D1: RTJ Remains the Only Kueue-Managed Admission Object

**Decision:** RTJ is the only object that Kueue manages for admission,
on both manager and worker clusters. The child JobSet remains a plain
runtime resource with no Kueue metadata. This is unchanged from Phase 2.

**Rationale:** The single-admission-object invariant is the foundation
of the Kueue integration. Multi-cluster dispatch does not change this:
MultiKueue dispatches RTJ Workloads, not JobSet Workloads.

### D2: Manager Does Not Create Local Child JobSets

**Decision:** When the operator runs in manager mode, it MUST NOT create
local child JobSets for any RTJ. The manager is control-plane only:
it observes RTJ state, propagates user intent, and mirrors remote status.

**Rationale:** The hard boundary says "do NOT make the manager cluster
also act as a worker." Creating local child JobSets would violate this
boundary and create ambiguity about where training actually runs.

**Alternative considered:** Allow the manager to optionally run RTJs
locally (hybrid mode). Rejected because it complicates failure reasoning,
resource accounting, and checkpoint store semantics. The manager and
worker roles must be cleanly separated.

### D3: Worker Runs the Full Phase 5 Path Unchanged

**Decision:** The worker-mode operator runs the exact Phase 5
reconciliation path. It does not know or care that it was dispatched by
a manager. The remote RTJ is treated as a locally-created RTJ.

**Rationale:** Worker isolation is the key simplicity property. The
worker does not need multi-cluster awareness, manager-side status
mirroring logic, or MultiKueue integration code. All multi-cluster
complexity is isolated to the manager mode and MultiKueue.

**Consequence:** Worker-side bug fixes and feature improvements
automatically apply to multi-cluster deployments because the worker
path is identical.

### D4: Shared Checkpoint Store Is Required for Multi-Cluster

**Decision:** Multi-cluster RTJ requires all worker clusters to access
the same S3-compatible checkpoint store (same endpoint, same bucket/
prefix). The store must provide read-after-write consistency.

**Rationale:** Cross-worker resume requires Worker-B to read checkpoints
written by Worker-A. Without a shared store, resume is impossible. The
checkpoint contract (manifest-last publication, completeness,
compatibility) applies unchanged -- the only new requirement is
network-level accessibility from all workers.

**Alternative considered:** Checkpoint replication between per-cluster
stores. Rejected because it adds complexity (replication lag, conflict
resolution, consistency guarantees) without benefit. Modern S3-compatible
storage (AWS S3, GCS, MinIO) natively supports multi-region/multi-client
access.

### D5: Manager Does Not Do Checkpoint I/O

**Decision:** The manager cluster does not read or write checkpoint
artifacts. It only reflects checkpoint metadata (step count, timestamp,
storage URI) from the worker's RTJ status.

**Rationale:** The manager is control-plane only. Checkpoint I/O would
require:
- Credentials for the shared store on the manager.
- S3 client dependencies in the manager-mode code path.
- Potential conflicts with worker-side checkpoint catalog operations.

None of these are necessary. The worker produces all checkpoint metadata
in its RTJ status, which is mirrored to the manager.

### D6: Operator Mode Is Determined at Startup

**Decision:** The operator binary accepts a `--mode=manager|worker` flag
(default: `worker`). The mode is fixed for the lifetime of the process.
No runtime mode switching.

**Rationale:** Startup-time mode selection is the simplest boundary:
- The operator knows its role before it starts reconciling.
- No conditional logic based on per-RTJ inspection.
- Clean separation: manager and worker code paths can be tested in
  isolation.

**Alternative considered:** Per-RTJ mode detection (check if the
Workload has a MultiKueue AdmissionCheck). Rejected because it blurs
the manager/worker boundary and would allow the manager to act as a
worker for some RTJs, violating the hard boundary.

### D7: Phase 5 Single-Cluster Path Is the Default

**Decision:** Worker mode is the default (`--mode=worker`). When no
MultiKueue configuration exists, the operator behaves identically to
Phase 5. No configuration changes are required for single-cluster
deployments.

**Rationale:** Phase 5 single-cluster is the stable, well-tested path.
Multi-cluster is a new capability that should be explicitly opted into.
Existing deployments must not be affected by the Phase 6 binary.

### D8: Live Migration Is Not a Core Phase 6 Requirement

**Decision:** Phase 6 does NOT promise live migration of an already-
admitted Workload across worker clusters. It provides cross-worker
resume via the shared checkpoint store (pause + re-dispatch + resume).

**Rationale:** Live migration would require:
- Simultaneous resource accounting on two clusters.
- Pod state transfer or transparent process migration.
- Network identity continuity across clusters.
- Workload re-admission without user intervention.

Each of these is a significant engineering challenge. Cross-worker
resume (pause -> checkpoint -> re-dispatch -> restore) achieves the same
outcome (training continues on a different cluster) with a brief,
observable pause. The checkpoint contract ensures no training progress
is lost.

**Consequence:** There is a non-zero downtime when moving an RTJ between
workers. This is acceptable for the core milestone. Live migration can
be built on top of cross-worker resume in a future phase.

### D9: No Custom Cross-Cluster Preemption Engine

**Decision:** Phase 6 does NOT add a custom cross-cluster preemption
engine. Kueue and MultiKueue own scheduling, dispatch, and preemption.
The RTJ operator shapes priority (Phase 5) but does not make cross-
cluster preemption decisions.

**Rationale:** The hard boundary says "do NOT add a custom scheduler or
custom cross-cluster preemption engine." Cross-cluster preemption would
require:
- A global view of all clusters' resource state.
- Cross-cluster priority comparison.
- Coordinated victim selection.

This is Kueue/MultiKueue's domain, not the RTJ operator's.

### D10: MultiKueue Path Is Isolated Behind Feature Gate

**Decision:** The MultiKueue path is activated only by the `--mode=manager`
startup flag. If upstream MultiKueue support is alpha or feature-gated,
the Phase 6 documentation clearly labels it as experimental.

The isolation ensures:
- Worker mode is stable and matches Phase 5.
- Manager mode is opt-in and documented as experimental if needed.
- No alpha/experimental code runs in the default path.

### D11: Three-Cluster Local Dev Profile

**Decision:** The local dev/test profile uses three kind clusters:
- `phase6-manager`: Manager cluster with Kueue + MultiKueue.
- `phase6-worker-1`: Worker cluster with Kueue + RTJ.
- `phase6-worker-2`: Worker cluster with Kueue + RTJ.
- Shared MinIO instance for checkpoint storage.

**Rationale:** Three clusters is the minimum for demonstrating:
- Manager/worker dispatch.
- Cross-worker resume (pause on worker-1, resume on worker-2).
- Worker selection (MultiKueue chooses between two workers).

## Phase 6 Must-Ship Demo

The must-ship demo exercises the full multi-cluster spillover lifecycle
in the local three-cluster kind environment:

### Setup

1. Three kind clusters: manager, worker-1, worker-2.
2. Shared MinIO instance accessible from all clusters.
3. MultiKueue configured on the manager with both workers.
4. ClusterQueue on the manager with MultiKueue AdmissionCheck.

### Scenario

1. Create RTJ on the manager cluster with `desiredState=Running`.
2. MultiKueue dispatches the RTJ Workload to worker-1.
3. Worker-1 creates a child JobSet, training runs, checkpoints written
   to shared MinIO.
4. Manager-side RTJ shows `phase=Running`,
   `remoteStatus.workerCluster=worker-1`, `lastCompletedCheckpoint`
   reflecting worker progress.
5. User patches manager-side RTJ `desiredState=Paused`.
6. Pause propagates to worker-1. Worker-1 executes graceful yield,
   writes final checkpoint.
7. Manager-side RTJ shows `phase=Paused`.
8. User patches manager-side RTJ `desiredState=Running`.
9. MultiKueue re-dispatches to worker-2 (different worker).
10. Worker-2 selects the checkpoint from shared MinIO, creates new child
    JobSet, resumes training.
11. Manager-side RTJ shows `phase=Running`,
    `remoteStatus.workerCluster=worker-2`.
12. Global step count is monotonic across the worker switch.

### Observable Assertions

- Manager never creates a local child JobSet.
- Worker-1 creates and tears down a child JobSet.
- Worker-2 creates a child JobSet from the shared checkpoint.
- Checkpoint exists in shared MinIO after worker-1 pause.
- Global step count continues from worker-1's last step on worker-2.
- Manager status reflects worker identity changes.

## Manager/Worker Ownership Split

| Concern | Manager | Worker | MultiKueue |
| --- | --- | --- | --- |
| RTJ creation | Receives from user | Receives from MultiKueue | Creates remote copy |
| Workload lifecycle | GenericJob adapter | GenericJob adapter | Dispatch + status mirror |
| Child JobSet | MUST NOT create | Creates, manages, deletes | Not involved |
| Checkpoint I/O | MUST NOT do | Reads and writes | Not involved |
| Graceful yield | Propagates intent | Executes protocol | Transport |
| Resume | Re-queues Workload | Selects checkpoint, restores | Re-dispatch |
| Status | Mirrors from worker | Produces locally | Status transport |
| Priority shaping | Not evaluated | Evaluated locally | Not involved |
| Launch gating | Not applicable | Phase 4 pipeline | Not involved |
| Topology | Not applicable | Phase 4 pipeline | Not involved |

## Experimental vs Stable in Phase 6

| Feature | Stability | Rationale |
| --- | --- | --- |
| Worker mode (default) | **Stable** | Identical to Phase 5. No new code in the default path. |
| Manager mode | **Experimental** | New code path. Depends on MultiKueue protocol which may be alpha/beta. |
| MultiKueue integration | **Experimental** | Depends on Kueue MultiKueue feature gate maturity. |
| Shared checkpoint store | **Stable (contract)** | The checkpoint contract is unchanged. Only the operational requirement (shared access) is new. |
| Cross-worker resume | **Experimental** | Depends on MultiKueue re-dispatch and shared store. |
| Status mirroring | **Experimental** | New manager-side status fields. Depends on MultiKueue status propagation. |
| Three-cluster dev profile | **Dev only** | Not for production. kind-specific. |
| Phase 5 single-cluster path | **Stable** | Fully preserved. No regression. |

## Consequences

### Positive

- Multi-cluster spillover: RTJs can run on whichever worker has capacity.
- Cross-worker resume: training can continue on a different cluster.
- No custom scheduling or preemption: fully delegated to Kueue/MultiKueue.
- Clean manager/worker separation: worker code is unchanged from Phase 5.
- Phase 5 backward compatible: default path is identical to Phase 5.

### Negative

- Requires shared S3-compatible checkpoint store across clusters.
- Requires MultiKueue configuration and credential management.
- Manager-side status has eventual-consistency lag.
- No live migration (pause/resume has brief training downtime).
- Three-cluster dev profile is more complex than single-cluster.

### Neutral

- Child JobSet remains plain runtime. No changes to the JobSet
  controller or JobSet CRD.
- Checkpoint contract is unchanged. Only accessibility changes.
- Graceful yield protocol is unchanged. Same Phase 2 protocol.
- Priority shaping runs on the worker, not the manager.

## Verification

| Decision | Verification |
| --- | --- |
| D1 | Unit test: child JobSet has no `kueue.x-k8s.io/*` labels (existing test, unchanged). |
| D2 | Unit test: manager-mode reconciler does not create child JobSets. |
| D3 | Unit test: worker mode produces identical behavior to Phase 5 tests. |
| D4 | E2E test: cross-worker resume reads checkpoint from shared store. |
| D5 | Code review: no checkpoint catalog usage in manager mode code path. |
| D6 | Unit test: mode flag parsing, manager/worker conditional startup. |
| D7 | Unit test: default mode is worker, Phase 5 behavior preserved. |
| D8 | Design doc: live migration explicitly documented as out of scope. |
| D9 | Code review: no cross-cluster preemption logic in operator. |
| D10 | Startup flag inspection: `--mode=manager` activates manager path. |
| D11 | E2E test: three-cluster dev profile smoke test passes. |
