# Migration from Phase 5

## What Stays the Same

### Kueue Authority Model

RTJ remains the **only** Kueue-managed admission object. Child JobSets
remain **plain runtime resources** with no Kueue management metadata.
The external `jobframework` integration path, the `RTJGenericJob` adapter,
and the Kueue generic reconciler are unchanged in their overall structure.

Kueue remains the queueing, admission, and preemption authority. Phase 6
does NOT implement a custom scheduler, a custom victim-selection engine,
or any cross-cluster preemption logic. MultiKueue owns worker cluster
selection and dispatch.

### Lifecycle State Machine

All lifecycle phases are unchanged:

```
Pending -> Queued -> Admitted -> Starting -> Running
    -> YieldRequested -> Draining -> Queued (Kueue re-queue)
    -> Restoring -> Running
    -> Succeeded | Failed
```

Phase 6 does not add new lifecycle phases. Both manager and worker RTJs
use the same phase enum. The manager mirrors the worker's phase.

### Suspend and Manual Pause Semantics

- `spec.suspend` remains the Kueue-facing admission gate.
- `spec.control.desiredState` remains the user-facing manual hold surface.
- These two fields are not aliases. Their semantics are unchanged from
  Phase 2.
- On the manager, `desiredState` changes propagate to the remote RTJ.
- On the worker, `desiredState` drives the same graceful yield path.

### Checkpoint Contract

The checkpoint storage layout, manifest schema, manifest-last publication
semantics, yield-marker contract, and checkpoint completeness/validity
rules are **unchanged** from Phase 0/3/4/5. Phase 6 adds a new
operational requirement (shared store accessibility) but does not change
the checkpoint format, protocol, or contract.

### Graceful Yield and Drain

The graceful yield protocol is unchanged. Control ConfigMap, step-boundary
yield, DCP checkpoint, yield marker, manifest publication, bounded drain
timer, fail-closed on timeout -- all identical to Phase 2/3/4/5. The
worker executes this path locally regardless of whether it was dispatched
by a manager or created directly.

### Resume Selection

The `LatestCompatibleComplete` source policy remains the only supported
policy. The selection algorithm is unchanged. World-size-flexible resume
from Phase 3 (`allowWorldSizeChange`) continues to work. The only
difference is that the checkpoint may have been written by a different
worker cluster -- but since the manifest format is unchanged and the
shared store is accessible, the selection algorithm works identically.

### Priority Shaping

Phase 5 priority shaping (CheckpointPriorityPolicy, effective priority
derivation, yield budgets, protection windows) continues to work on
worker clusters. The worker's local Kueue instance reads the effective
priority from the local Workload. No changes to the priority shaping
path.

On the manager cluster, priority shaping status is mirrored from the
worker but is not locally evaluated. The manager does not run the
priority shaping evaluation loop.

### Flavor-Aware and Topology-Aware Rendering

Phase 3's flavor-aware child JobSet rendering and Phase 4's topology-aware
admission pipeline continue to work unchanged on worker clusters. These
are local concerns that do not cross cluster boundaries.

### Phase 5 API Fields

All Phase 5 spec and status fields are preserved:

- `spec.priorityPolicyRef` (optional, Phase 5)
- `status.priorityShaping` (Phase 5, nil when no policy)
- All Phase 3/4 fields: parallelism, topology, launchReadiness, etc.

### Pinned Versions

Kueue v0.15.1, JobSet v0.10.1, controller-runtime v0.22.4. No version
bumps in Phase 6.

### Existing Environment Variables

All Phase 1/2/3/4/5 environment variables remain supported and unchanged
on worker clusters.

## What Moves to Manager vs Worker Responsibility

### Before (Phase 5): Single-Cluster

In Phase 5, one operator instance handles everything:

```
User -> RTJ -> Kueue (admission) -> RTJ Operator -> Child JobSet -> Training Pods
                                        |
                                        +-> Checkpoint Store (local access)
```

All concerns (admission, launch, checkpoint, yield, resume, priority
shaping) are handled by a single operator in a single cluster.

### After (Phase 6): Manager/Worker Split

| Concern | Manager Cluster | Worker Cluster |
| --- | --- | --- |
| RTJ creation | User creates here | Remote copy by MultiKueue |
| Workload creation | GenericJob adapter | GenericJob adapter (local) |
| Admission | Kueue + MultiKueue dispatch | Kueue (local admission) |
| Child JobSet creation | **MUST NOT** | Authoritative |
| Launch gating | Not applicable | Phase 4 pipeline |
| Topology rendering | Not applicable | Phase 4 pipeline |
| Priority shaping | Not applicable (mirrors only) | Phase 5 pipeline |
| Graceful yield | Initiates (propagates desiredState) | Executes (checkpoint, teardown) |
| Checkpoint I/O | **MUST NOT** | Reads and writes |
| Resume dispatch | Re-queues Workload | Selects checkpoint, creates JobSet |
| Status observation | Mirrors from worker | Produces status |
| Manual pause/resume | Receives user intent, propagates | Executes yield/restore |

### Key Ownership Boundaries

1. **The manager is control-plane only.** It does not create runtime
   resources (no child JobSets, no training pods, no checkpoint I/O).
   Its job is to observe user intent, propagate it to the worker via
   MultiKueue, and mirror remote status back.

2. **The worker is the execution authority.** It creates child JobSets,
   manages training pods, writes and reads checkpoints, executes graceful
   yield, and performs resume. The worker does not know about the manager.

3. **MultiKueue is the transport.** It creates remote RTJs, mirrors
   status, and propagates spec changes. The RTJ operator adapts to
   MultiKueue's protocol but does not implement custom dispatch.

## Why the Core Phase Does Not Promise Cross-Worker Live Migration

Cross-worker live migration means moving an already-admitted, already-
running RTJ from one worker cluster to another while training is in
progress. This is explicitly out of scope for Phase 6 core.

### What live migration would require

1. **Workload re-admission on the target worker.** The Workload on
   Worker-A would need to be evicted and a new Workload admitted on
   Worker-B. This involves Kueue quota accounting on two clusters
   simultaneously and requires MultiKueue to support Workload re-dispatch
   without user intervention.

2. **Pod state transfer.** Training pods on Worker-A have in-memory
   state (optimizer state, gradient buffers, communication groups). This
   state is not in the checkpoint. Live migration would require either:
   - Forcing a checkpoint immediately before migration (which is just
     pause/resume with extra steps).
   - Transparent process migration (CRIU or equivalent), which violates
     the Phase 0 DCP-only contract.

3. **Network identity continuity.** PyTorch DDP/FSDP relies on
   rank-to-address mappings established at startup. Moving pods across
   clusters changes addresses. The rendezvous would need to be re-done,
   which is equivalent to a restart.

4. **Simultaneous resource accounting.** During migration, resources are
   consumed on both clusters. Kueue would need to account for this
   transitional state.

### What Phase 6 provides instead

Phase 6 provides **cross-worker resume**, not live migration:

1. Pause on Worker-A (graceful yield + checkpoint to shared store).
2. Re-queue the Workload on the manager.
3. MultiKueue dispatches to Worker-B (or back to Worker-A).
4. Worker-B resumes from the shared checkpoint.

This is a deliberate, observable lifecycle transition with a clear
checkpoint boundary. The training state is persisted in the shared store.
There is a brief pause in training while the transition occurs.

### Why this is sufficient for the core milestone

- **Cross-cluster failover is supported.** If Worker-A becomes unhealthy,
  the RTJ can be paused and resumed on Worker-B.
- **Resource rebalancing is supported.** An operator can pause RTJs on
  an overloaded worker and let MultiKueue dispatch them to a different
  worker.
- **No data loss.** The checkpoint ensures training progress is preserved
  across the transition.
- **Observable and debuggable.** The pause/resume lifecycle is the same
  Phase 2 protocol, just crossing a cluster boundary.

Live migration (zero-downtime worker switch) is a future optimization
that builds on the cross-worker resume foundation.

## Why a Shared Checkpoint Store Is Required

### Single-cluster (Phase 5): Local store is sufficient

In Phase 5, the checkpoint store is accessed by a single cluster:

```
Worker Pods -> S3-compatible store -> Worker RTJ Operator
```

The store only needs to be accessible from one cluster's network.

### Multi-cluster (Phase 6): Shared store is required

In Phase 6, multiple workers may need to access the same checkpoints:

```
Worker-A Pods -> Shared S3 store <- Worker-B Pods
                      ^
                      |
               Worker-B RTJ Operator (reads manifests for selection)
```

Without a shared store:

1. **Cross-worker resume is impossible.** Worker-B cannot read
   checkpoints written by Worker-A.
2. **Checkpoint selection fails.** The `LatestCompatibleComplete` policy
   scans the manifest directory. If Worker-B has a different store, it
   sees no manifests and treats the RTJ as a fresh start.
3. **Training progress is lost.** Steps completed on Worker-A are
   invisible to Worker-B.

### What "shared" means operationally

- **Same S3-compatible endpoint** accessible from all worker clusters.
- **Same bucket and prefix** in `spec.checkpoint.storageURI`.
- **No checkpoint replication or copying.** The store itself provides
  multi-cluster access. Workers read and write directly.
- **Credentials provisioned on all workers.** Each worker needs
  credentials (IAM role, service account, secret) to access the shared
  store.

### Why the manager does not need store access

The manager does not read or write checkpoint artifacts. It only needs
checkpoint metadata (step count, timestamp, storage URI) which it gets
from the worker's RTJ status. This keeps the manager lightweight and
avoids credential management for the checkpoint store on the manager.

## What New Status Fields Are Needed

### Manager-Side Status Extension

Phase 6 adds a `remoteStatus` sub-object to the RTJ status:

| Field | Type | Description |
| --- | --- | --- |
| `status.remoteStatus.workerCluster` | string | Name of the worker cluster running the RTJ |
| `status.remoteStatus.remotePhase` | string | Raw lifecycle phase on the worker |
| `status.remoteStatus.lastSyncTime` | Time | When the status was last mirrored |

These fields are nil when the RTJ is not MultiKueue-managed (Phase 5
backward compatibility).

### No Worker-Side Status Changes

The worker does not add new status fields. It produces the same status
as Phase 5. The manager reads this status via MultiKueue and interprets
it.

## Upgrade Path

### From Phase 5 to Phase 6 (No Multi-Cluster)

1. Deploy the Phase 6 operator binary in worker mode (default).
2. All existing RTJs continue to work identically to Phase 5.
3. No behavioral changes unless MultiKueue is configured.
4. No new CRDs required.

### Enabling Multi-Cluster

1. Deploy a manager cluster with Kueue + MultiKueue enabled.
2. Deploy the Phase 6 operator in manager mode on the manager cluster.
3. Deploy two or more worker clusters with Kueue + RTJ external framework.
4. Deploy the Phase 6 operator in worker mode on each worker cluster.
5. Configure MultiKueueCluster and MultiKueueConfig on the manager.
6. Create a ClusterQueue on the manager with a MultiKueue AdmissionCheck.
7. Point `spec.checkpoint.storageURI` to a shared S3-compatible store.
8. Create RTJs on the manager cluster.

### Verifying Multi-Cluster

```bash
# On the manager cluster
kubectl get rtj my-training -o jsonpath='{.status.remoteStatus}'

# Check which worker is running the RTJ
kubectl get rtj my-training -o jsonpath='{.status.remoteStatus.workerCluster}'

# Check remote phase
kubectl get rtj my-training -o jsonpath='{.status.phase}'

# On the worker cluster (for debugging)
kubectl get rtj --context worker-1
kubectl get jobset --context worker-1
```
