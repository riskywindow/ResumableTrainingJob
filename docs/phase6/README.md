# Phase 6: Multi-Cluster Checkpoint-Native Spillover

Phase 6 adds multi-cluster checkpoint-native spillover for
ResumableTrainingJob (RTJ), using Kueue MultiKueue external-framework
support to dispatch RTJs from a manager cluster to worker clusters. Worker
clusters run the existing Phase 5 runtime path. A shared checkpoint store
enables remote pause/resume across cluster boundaries.

## What Phase 6 Delivers

1. **MultiKueue external-framework integration for RTJ.** The RTJ
   GenericJob adapter is extended so MultiKueue can dispatch RTJ Workloads
   to worker clusters using the standard external-framework protocol.
   Worker clusters run the full Phase 5 lifecycle path locally.

2. **Manager/worker operator split.** The RTJ operator runs in two modes:
   - **Manager mode:** observes RTJ state, does NOT create local child
     JobSets, relies on MultiKueue for remote dispatch. Reflects remote
     worker status to the manager-side RTJ.
   - **Worker mode:** full Phase 5 behavior. Creates child JobSets, runs
     launch gating, checkpointing, graceful yield, and resume locally.

3. **Shared-checkpoint remote pause/resume.** The shared S3-compatible
   checkpoint store is accessible from all worker clusters. A pause on the
   manager propagates to the remote worker, which executes the existing
   graceful yield path and writes a checkpoint to the shared store. A
   subsequent resume (on the same or a different worker) selects the latest
   compatible checkpoint from the shared store.

4. **Manager-visible remote status.** The manager-side RTJ status reflects
   the remote worker's lifecycle phase, checkpoint state, and conditions.
   The manager surfaces enough information for operators to observe
   training progress, checkpoint freshness, and yield events without
   directly inspecting the worker cluster.

5. **Deterministic local three-cluster dev/test profile.** A kind-based
   local environment with one manager cluster and two worker clusters,
   a shared MinIO instance for checkpoint storage, and MultiKueue
   configuration connecting all three clusters.

## What Does Not Change

- RTJ remains the **only** Kueue-managed admission object.
- The child JobSet remains a **plain runtime resource** with no Kueue metadata.
- All Phase 0 through Phase 5 invariants are preserved.
- When MultiKueue is not configured, Phase 5 single-cluster behavior is
  the default stable path.
- Kueue remains the queueing, admission, and preemption authority.
- The RTJ operator remains the lifecycle owner for yield, checkpoint, and resume.
- Worker clusters run the exact Phase 5 reconciliation path.

## What Remains Out of Scope

- Live migration of an already-admitted Workload across worker clusters.
- Custom MultiKueue external dispatch implementation (uses upstream protocol).
- MultiKueue + autoscaler / ProvisioningRequest as a required milestone.
- Manager cluster acting as a worker.
- Custom cross-cluster preemption engine or custom scheduler.
- Elastic Workloads or in-place scaling.
- Transparent CUDA or container snapshots.

## Quick Navigation

See [index.md](index.md) for the full document map.
