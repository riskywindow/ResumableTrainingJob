# Phase 6 Goals

## Mission

Implement multi-cluster checkpoint-native spillover for ResumableTrainingJob
(RTJ), using Kueue MultiKueue external-framework support. A manager cluster
dispatches RTJ Workloads to worker clusters via MultiKueue. Worker clusters
run the existing Phase 5 runtime path. A shared checkpoint store enables
remote pause/resume across cluster boundaries, with the manager driving
lifecycle transitions and reflecting remote status.

## Goals

### G1: MultiKueue External-Framework Integration for RTJ

The RTJ GenericJob adapter MUST be compatible with Kueue MultiKueue's
external-framework dispatch protocol. When a ClusterQueue is backed by a
MultiKueue AdmissionCheck, Kueue dispatches the RTJ Workload to a remote
worker cluster. The worker cluster runs the full Phase 5 lifecycle locally.

The RTJ operator on the **manager** cluster MUST NOT create local child
JobSets for RTJs dispatched via MultiKueue. The manager-side RTJ remains
a control-plane-only resource whose runtime is entirely remote.

The RTJ operator on the **worker** cluster MUST run the existing Phase 5
reconciliation path without modification: launch gating, topology-aware
rendering, checkpoint-aware priority shaping, graceful yield, and resume.

**Acceptance:** An RTJ created on the manager cluster is dispatched to a
worker cluster by MultiKueue. The worker creates a child JobSet and runs
training. The manager does not create any local runtime resources.

### G2: Manager/Worker Operator Split

The RTJ operator MUST support two operational modes, selected at startup:

- **Manager mode:** The operator watches RTJs and their associated
  Workloads. When an RTJ is dispatched via MultiKueue, the operator does
  NOT create local child JobSets. Instead, it reflects the remote worker's
  RTJ status (phase, checkpoint state, conditions) to the manager-side
  RTJ. The operator translates manager-side `spec.control.desiredState`
  changes into the appropriate remote lifecycle transitions.

- **Worker mode:** The operator runs the full Phase 5 reconciliation path
  unchanged. This is the default mode when MultiKueue is not configured.
  It is identical to the Phase 5 single-cluster path.

The mode MUST be determined by a startup flag or environment variable, not
by runtime inspection of MultiKueue configuration.

**Acceptance:** The operator binary starts in either manager or worker mode
via a CLI flag. Manager mode does not create local child JobSets for
MultiKueue-dispatched RTJs. Worker mode is identical to Phase 5.

### G3: Shared-Checkpoint Remote Pause/Resume

The RTJ system MUST support remote pause/resume across worker clusters
using a shared S3-compatible checkpoint store:

1. **Remote pause:** The manager patches the manager-side RTJ to
   `desiredState=Paused`. This propagates to the worker cluster via
   MultiKueue. The worker executes the existing graceful yield path:
   step-boundary yield, DCP checkpoint write to shared store, manifest
   publication, child JobSet teardown.

2. **Remote resume:** The manager patches the manager-side RTJ to
   `desiredState=Running`. MultiKueue dispatches the Workload to a worker
   cluster (same or different). The worker selects the latest compatible
   checkpoint from the shared store and creates a new child JobSet to
   resume training.

The shared checkpoint store MUST be accessible from all worker clusters.
The checkpoint contract (manifest-last publication, completeness,
compatibility) from Phase 0 applies unchanged.

**Acceptance:** An RTJ is paused on Worker-A, writing a checkpoint to the
shared store. The RTJ is then resumed on Worker-B, which reads the
checkpoint from the shared store and continues training with monotonic
global step count.

### G4: Manager-Visible Remote Status

The manager-side RTJ status MUST reflect the remote worker's lifecycle
state with sufficient fidelity for operators to observe training progress
without directly inspecting the worker cluster:

- Lifecycle phase (Queued, Admitted, Running, YieldRequested, Draining,
  Paused, Restoring, Succeeded, Failed).
- Last completed checkpoint (step count, timestamp, storage URI).
- Checkpoint freshness (if priority shaping is active on the worker).
- Active conditions (Admitted, Running, YieldRequested, CheckpointReady,
  Degraded, PriorityShaping).
- Worker cluster identity (which cluster is running the RTJ).

The status mirroring MUST be eventual-consistency: the manager status may
lag the worker by one reconciliation cycle, but MUST converge.

**Acceptance:** An operator on the manager cluster can run
`kubectl get rtj my-training -o yaml` and see the current worker cluster,
lifecycle phase, last checkpoint step, and active conditions without
accessing the worker cluster.

### G5: Deterministic Local Three-Cluster Dev/Test Profile

Phase 6 MUST deliver a local development profile with:

- **One manager kind cluster** running Kueue with MultiKueue enabled and
  the RTJ operator in manager mode.
- **Two worker kind clusters** running Kueue with the RTJ external
  framework and the RTJ operator in worker mode.
- **Shared MinIO instance** accessible from all three clusters, serving as
  the checkpoint store.
- **MultiKueue configuration** connecting the manager to both workers via
  MultiKueueCluster and MultiKueueConfig resources.
- **Deterministic dispatch:** A ClusterQueue on the manager with a
  MultiKueue AdmissionCheck that dispatches to worker clusters based on
  available quota.

**Acceptance:** `make phase6-up` creates the three-cluster environment.
`make phase6-smoke` validates connectivity, CRD installation, MultiKueue
configuration, and shared storage access across all clusters.

## Non-Goals (Explicitly Out of Scope)

| Non-Goal | Reason |
| --- | --- |
| Live migration of an admitted Workload across workers | Requires Workload re-admission, state transfer, and pod migration. Deferred to a future phase. |
| Custom MultiKueue external dispatch implementation | Uses upstream MultiKueue external-framework protocol. No custom dispatch logic. |
| MultiKueue + autoscaler / ProvisioningRequest | Not a required milestone. Worker-side ProvisioningRequest is independent. |
| Manager cluster as a worker | Manager is control-plane only. No local runtime for MultiKueue-managed RTJs. |
| Custom cross-cluster preemption engine | Kueue owns preemption. No custom cross-cluster victim selection. |
| Custom scheduler | Kueue and MultiKueue own scheduling and dispatch. |
| Elastic Workloads or in-place scaling | Deferred; Phase 3 handles resume-time shape changes only. |
| Transparent CUDA or container snapshots | Phase 0 contract: DCP checkpointing only. |
| Cross-worker checkpoint garbage collection | Workers write to shared store; GC is a storage policy concern. |

## Success Criteria

### Must-Ship

1. RTJ created on the manager is dispatched to a worker via MultiKueue.
2. Worker runs the full Phase 5 lifecycle (launch, checkpoint, yield, resume).
3. Manager does not create local child JobSets for MultiKueue-managed RTJs.
4. Manager-side pause propagates to the worker and triggers graceful yield.
5. Resume after pause uses the shared checkpoint store and produces
   monotonic global step count.
6. Manager-side RTJ status reflects remote lifecycle phase, checkpoint
   state, and worker identity.
7. Phase 5 single-cluster behavior is preserved when MultiKueue is not
   configured.

### Should-Ship

8. Resume on a different worker cluster (cross-worker resume) using the
   shared checkpoint store.
9. Three-cluster local dev/test profile with `make phase6-up`.
10. Smoke test validates MultiKueue connectivity, dispatch, and shared
    storage access.
11. Manager-side metrics track dispatch events, remote status sync, and
    pause/resume latency.

### Exit Criteria

- All must-ship acceptance criteria pass in the three-cluster kind dev
  environment.
- Unit tests cover: manager-mode skip-local-launch, status mirroring,
  pause propagation, shared-checkpoint selection.
- E2E test covers: dispatch + run + pause + resume across two workers.
- Documentation updated: architecture, migration, ADR, session handoff.
- Phase 5 regression: all Phase 5 tests continue to pass in worker mode.
