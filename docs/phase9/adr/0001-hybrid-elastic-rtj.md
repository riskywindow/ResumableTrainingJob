# ADR-0001: Hybrid Elastic RTJ

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-05 |
| Deciders | Phase 9 design session |
| Supersedes | None |
| Depends on | Phases 0–8 (all invariants preserved) |

## Context

ResumableTrainingJob (RTJ) supports checkpoint-native preemption, multi-cluster
dispatch, launch gating, and DRA device requests (Phases 0–8).  However, the
worker count is fixed at admission time and can only change through a full
pause/resume cycle.  Platform teams need the ability to:

1. Shrink a running job to free quota for higher-priority work.
2. Grow a running job when additional quota becomes available.
3. Release freed quota immediately (not after a full checkpoint cycle).
4. Do all of the above without breaking existing multi-cluster, launch gating,
   or DRA behavior.

Kueue provides `Workload.status.reclaimablePods` for signaling quota release,
and the RTJ controller already owns the checkpoint-and-relaunch lifecycle.
Phase 9 combines these into a hybrid resize model.

## Decision

### Core architecture

Phase 9 implements **hybrid elastic RTJ** with two resize paths:

1. **In-place shrink** (fast path): Patch the child JobSet's replica count
   down, write `reclaimablePods` to the Workload status for immediate quota
   release.  No checkpoint required.

2. **Checkpoint-and-relaunch** (safe path): Gracefully yield, write
   checkpoint, suspend the Workload, mutate PodSet counts, and request
   re-admission at the new size.  Used for:
   - All scale-up (grow) operations.
   - Scale-down (shrink) when the runtime does not support in-place
     replica reduction.

### Must-ship Phase 9 demo

The demo proves the following end-to-end flow on a single cluster:

1. Submit RTJ with `spec.elasticity.mode: Manual` and 8 workers.
2. Patch `spec.elasticity.targetWorkerCount: 4` → observe shrink.
   - If runtime supports in-place: observe JobSet replica patch +
     `reclaimablePods` quota release (no checkpoint, no relaunch).
   - If runtime does not support in-place: observe checkpoint →
     relaunch at 4 workers.
3. Patch `spec.elasticity.targetWorkerCount: 6` → observe grow.
   - Observe checkpoint → Workload suspended → PodSets mutated →
     re-admission at 6 → relaunch at 6 with DCP restore.
4. Verify training continuity: correct step count, no data loss.
5. Verify `status.elasticity` reflects correct state at each transition.

### Stable path vs. experimental path

| Path | Status | Description |
|---|---|---|
| Checkpoint-and-relaunch shrink | **Stable** | Uses proven pause/checkpoint/resume machinery from Phases 1–8 |
| Checkpoint-and-relaunch grow | **Stable** | Same machinery, with Workload PodSet mutation before re-admission |
| In-place shrink | **Experimental** | Gated on runtime annotation; fallback to stable C&R path |
| In-place grow | **Not implemented** | Requires upstream Kueue Workload resize; future work |

The stable path requires no new runtime capabilities.  It works with any
training framework that supports DCP checkpoint/restore.

The experimental in-place shrink path requires:
- The child JobSet controller to handle live replica reduction.
- The training framework to handle elastic membership changes (e.g.,
  TorchElastic with NCCL elastic scaling).
- The operator to explicitly opt in via the
  `training.io/supports-in-place-shrink: "true"` annotation on the
  child JobSet template.

### What counts as "in-place shrink support"

The RTJ controller checks for the runtime capability annotation
`training.io/supports-in-place-shrink: "true"` on the child JobSet.

**The annotation asserts that:**
1. The JobSet controller will scale down the corresponding Job's parallelism
   when `replicatedJobs[].replicas` is decreased.
2. The training framework (running inside the pods) will detect the
   membership change and continue training with the smaller world size
   without data loss or deadlock.
3. No checkpoint is required for the shrink to succeed.

**If the annotation is absent or "false":**
- The controller falls back to checkpoint-and-relaunch shrink.
- No error is raised; this is the expected default behavior.

### How quota release is represented

For in-place shrink:

```yaml
# Written by RTJ controller to Workload.status
status:
  reclaimablePods:
    - name: "workers"     # PodSet name
      count: 4            # number of pods being released
```

Kueue reads `reclaimablePods` and subtracts the declared count from the
ClusterQueue's usage for this Workload.  The freed quota becomes available
for other workloads.

The RTJ controller:
1. Writes the entry after patching the JobSet replicas down.
2. Waits for surplus pods to terminate.
3. Clears the entry once the resize is confirmed complete.

For checkpoint-and-relaunch (both shrink and grow):
- No `reclaimablePods` is used.  The Workload is fully suspended, which
  releases all quota.  Re-admission at the new size allocates the correct
  amount.

### How grow differs from shrink

| Dimension | Shrink | Grow |
|---|---|---|
| Quota impact | Releases quota | Requires additional quota |
| In-place possible? | Yes (if runtime supports it) | No (requires new admission) |
| Checkpoint required? | Only if in-place not supported | Always |
| Workload suspended? | No (in-place) / Yes (C&R) | Always |
| Kueue re-admission? | No (in-place) / Yes (C&R) | Always |
| DCP resharding? | No (in-place) / Yes (C&R) | Always |
| Can block on quota? | No | Yes (larger size may not fit) |

### RTJ controller is the sole resize orchestrator

- The RTJ controller detects the target/current delta.
- The RTJ controller chooses the resize path.
- The RTJ controller executes the resize (patch JobSet, write reclaimablePods,
  suspend Workload, mutate PodSets, etc.).
- Kueue's role is limited to: reading reclaimablePods (in-place shrink),
  de-admitting/re-admitting (checkpoint-and-relaunch).
- No custom scheduler, quota engine, or autoscaler is involved.

### Compatibility guarantees

| Existing feature | Compatibility |
|---|---|
| Phase 6 MultiKueue | Manager propagates spec.elasticity; worker executes resize; status mirrored |
| Phase 7 Launch gating | Launch gates apply to relaunch step of C&R resize |
| Phase 8 DRA | Device profile immutable during resize; claim templates reused; device compat check on relaunch |
| Phase 3 Adaptive parallelism | `allowWorldSizeChange: true` required for any resize |
| Phase 4 Topology | TopologyRequest updated in Workload PodSets before re-admission |
| Phase 5 Priority | Effective priority recalculated after resize completes |

## Alternatives considered

### Alternative 1: Native Kueue Workload Slices

Decompose the Workload into independently-admittable slices.  Grow by adding
a slice; shrink by releasing a slice.

**Rejected because:**
- Workload Slices are experimental and not required for manual resize.
- Adds complexity (multiple admission lifecycles per RTJ).
- `reclaimablePods` is simpler and sufficient for quota release.
- Can be adopted later as an optimization if it stabilizes.

### Alternative 2: Automatic metric-driven autoscaling

Use custom metrics (GPU utilization, training throughput) to automatically
adjust targetWorkerCount.

**Deferred because:**
- Adds significant complexity (metric collection, scaling policy, cooldown).
- Manual target is sufficient for the core milestone.
- Can be layered on top of Phase 9's manual resize API.

### Alternative 3: In-place grow via Workload resize

Grow the Workload in-place by mutating PodSet counts on an admitted Workload
and having Kueue allocate additional quota.

**Deferred because:**
- Requires upstream Kueue support for in-flight Workload spec mutation
  (KEP pending).
- Checkpoint-and-relaunch grow is safe and works today.

## Consequences

### Positive

- Platform teams can dynamically right-size training jobs.
- Freed quota is released immediately via reclaimablePods (in-place shrink).
- All existing Phase 0–8 behavior preserved.
- Conservative annotation gate prevents unsafe in-place shrink.

### Negative

- Grow always incurs a checkpoint-and-relaunch cycle (latency).
- In-place shrink requires opt-in and runtime support.
- MultiKueue reclaimablePods mirror is deferred (OQ-3).

### Neutral

- Training frameworks that don't support elastic membership pay no penalty
  (fall back to proven C&R path).
- Phase 9 API (`spec.elasticity`) is a clean extension that can be disabled.
