# Migration from Phase 4

## What Stays the Same

### Kueue Authority Model

RTJ remains the **only** Kueue-managed admission object. Child JobSets remain
**plain runtime resources** with no Kueue management metadata. The external
`jobframework` integration path, the `RTJGenericJob` adapter, and the Kueue
generic reconciler are unchanged in their overall structure.

Kueue remains the queueing, admission, and preemption authority. Phase 5 does
NOT implement a custom scheduler, a custom victim-selection engine, or any
preemption logic. It influences Kueue's existing preemption decisions by
shaping the effective priority input.

### Lifecycle State Machine

All lifecycle phases are unchanged:

```
Pending → Queued → Admitted → Starting → Running
    → YieldRequested → Draining → Queued (Kueue re-queue)
    → Restoring → Running
    → Succeeded | Failed
```

Phase 5 does not add new lifecycle phases. The priority-shaping evaluation
runs alongside the existing reconcile loop for Running workloads. The existing
graceful yield, checkpoint, and resume paths are reused without modification.

### Suspend Semantics

- `spec.suspend` remains the Kueue-facing admission gate.
- `spec.control.desiredState` remains the user-facing manual hold surface.
- These two fields are not aliases. Their semantics are unchanged from Phase 2.

### Checkpoint Contract

The checkpoint storage layout, manifest schema, manifest-last publication
semantics, yield-marker contract, and checkpoint completeness/validity rules
are **unchanged** from Phase 3/4. Phase 5 adds a **read-only consumer** of
checkpoint manifests: the Priority Shaping Controller reads manifest
timestamps to determine freshness. It does not write checkpoints or modify
the storage layout.

### Graceful Yield and Drain

The graceful yield protocol is unchanged. Control ConfigMap, step-boundary
yield, DCP checkpoint, yield marker, manifest publication, bounded drain
timer, fail-closed on timeout — all identical to Phase 2/3/4. The difference
in Phase 5 is that some yields will be triggered by Kueue preemption that
was itself influenced by effective-priority shaping. The yield protocol is
the same regardless of why Kueue preempted the workload.

### Resume Selection

The `LatestCompatibleComplete` source policy remains the only supported
policy. The selection algorithm is unchanged. World-size-flexible resume
from Phase 3 (`allowWorldSizeChange`) continues to work. Phase 5 does not
add new checkpoint selection criteria.

### Flavor-Aware Rendering

Phase 3's flavor-aware child JobSet rendering (nodeSelector, tolerations from
ResourceFlavors, admitted replica counts, Kueue label stripping) continues to
work unchanged.

### Topology-Aware Admission Pipeline

Phase 4's topology-aware Workload synthesis, topology-aware runtime
materialization, ResumeReadiness AdmissionCheck controller, and admission-
gated launch pipeline are all unchanged. Phase 5 priority shaping is
orthogonal to topology — they compose without interaction.

### Phase 3/4 API Fields

All Phase 3 and Phase 4 spec and status fields are preserved:

- `spec.parallelism` (preferredCount, minCount, podSetName, enablePartialAdmission)
- `spec.resume.allowWorldSizeChange`
- `spec.topology` (mode, topologyLevel, leaderWorkerColocation)
- `status.admission` (admittedWorkerCount, preferredWorkerCount, admittedFlavors)
- `status.restore` (restoreMode, checkpointWorldSize, restoreWorldSize)
- `status.launchReadiness` (gateState, reason, message)
- `status.topology` (levels, domains)
- `status.effectiveLaunchShape` (workerCount, worldSize, resumeMode, checkpointID)

### Existing Environment Variables

All Phase 1/2/3/4 environment variables remain supported and unchanged.

### Pinned Versions

Kueue v0.15.1, JobSet v0.10.1, controller-runtime v0.22.4. No version bumps.

## What Changes in Priority Handling

### Before (Phase 4)

In Phase 4, priority is static and user-declared:

1. User sets `spec.workloadPriorityClassName` on the RTJ.
2. Kueue's GenericJob reconciler creates a Workload with `Spec.Priority`
   set from the `WorkloadPriorityClass.Value`.
3. The priority never changes for the lifetime of the Workload.
4. Kueue uses this static priority for all admission and preemption decisions.

**Consequence:** A low-priority job that has been running for hours with a
fresh checkpoint is treated the same as one that just started. There is no
signal to Kueue that one job is "more preemptable" than another at the same
priority level.

### After (Phase 5)

In Phase 5, priority is dynamic and derived:

1. User sets `spec.workloadPriorityClassName` (base priority, unchanged).
2. User optionally sets `spec.priorityShapingRef` pointing to a
   `PriorityShapingPolicy`.
3. Kueue creates the Workload with initial priority from the class (unchanged).
4. The Priority Shaping Controller periodically evaluates:
   - Is the protection window active? → keep base priority.
   - Is the latest checkpoint fresh? → keep base priority.
   - Is the checkpoint stale? → reduce effective priority.
5. The controller writes the computed effective priority to
   `Workload.Spec.Priority`.
6. Kueue uses the effective priority for preemption decisions.

**Consequence:** A running job whose checkpoint becomes stale has its
effective priority gradually lowered, making it a natural preemption candidate
when higher-priority (or fresher-checkpoint) workloads are pending.

### Without Priority Shaping (Phase 4 Compatibility)

When `spec.priorityShapingRef` is nil:

1. No Priority Shaping Controller evaluation for this RTJ.
2. `Workload.Spec.Priority` is set by Kueue's GenericJob reconciler and
   never mutated by the operator.
3. Preemption is based on static declared priority (identical to Phase 4).
4. `status.effectivePriority` is nil.

No behavioural change from Phase 4.

## Why Effective Priority Is a Derived Value

The effective priority is a **derived value** computed by the operator, not a
user-declared field. This is a deliberate design choice:

### Why not let users set effective priority directly?

1. **Correctness.** The effective priority must reflect real checkpoint state.
   If users set it manually, it may diverge from actual freshness, leading
   to incorrect preemption decisions (preempting a job that has no checkpoint,
   or protecting a job whose checkpoint is hours old).

2. **Composability.** The derivation combines multiple signals (base priority,
   checkpoint age, protection window) into a single value. Users declare the
   policy; the controller computes the result.

3. **Safety.** The fail-safe (keep base priority when telemetry unavailable)
   is enforced by the controller, not by user discipline.

### Why not store effective priority on the RTJ instead of the Workload?

1. **Kueue reads Workload.Spec.Priority, not RTJ status.** For the priority
   to influence preemption, it must be on the Workload where Kueue reads it.

2. **RTJ status records the derivation.** `status.effectivePriority` captures
   the full derivation context (base, penalty, protection state, checkpoint
   age, reason) for observability. The Workload carries the bare integer.

### Why not create a new WorkloadPriorityClass per RTJ?

1. **WorkloadPriorityClass is cluster-scoped and shared.** Creating
   per-RTJ classes would pollute the cluster namespace and create a
   management burden.

2. **WorkloadPriorityClass.Value is static.** Changing a class value does not
   affect existing Workloads that reference it. Per-RTJ priority changes
   require writing to `Workload.Spec.Priority`, not the class.

3. **PriorityClassRef is immutable once quota is reserved.** The Workload
   cannot be re-pointed to a different class after admission. The mutable
   `Priority` integer is the only lever.

## Why Cohort/Fair-Sharing Policy Innovation Is Deferred

Phase 5 targets **within-ClusterQueue preemption** only. Cross-ClusterQueue
preemption, cohort-level priority, and fair-sharing innovations are deferred:

### Cohort-level preemption

Kueue supports cohort-level preemption where workloads in one ClusterQueue
can preempt workloads in another ClusterQueue within the same cohort. Phase 5
effective priority shaping affects `Workload.Spec.Priority`, which Kueue
uses for both within-CQ and cohort preemption. However:

1. **Cohort preemption semantics are complex.** The interaction between
   borrowing limits, nominal quotas, and priority ordering across queues
   creates edge cases that require careful testing.
2. **Local dev is single-CQ.** The `kind` dev environment uses a single
   ClusterQueue. Cohort testing requires a multi-CQ setup that adds
   infrastructure complexity without testing the core Phase 5 logic.
3. **Risk of unexpected cross-CQ effects.** Lowering effective priority
   in one CQ may cause the workload to be preempted by a workload in
   another CQ that the user did not expect. This requires explicit UX
   design and documentation.

**Deferred to:** A future phase that focuses on multi-CQ priority policy.

### Fair-sharing

Kueue's fair-sharing feature distributes quota across workloads within a
cohort based on weights. Priority shaping interacts with fair-sharing
because lowered effective priority may affect a workload's fair-share
allocation.

1. **Fair-sharing weight is orthogonal to priority.** In Kueue, fair-sharing
   weight and priority are independent dimensions. Changing priority does
   not directly change fair-share allocation.
2. **Combining both creates a two-dimensional policy space.** Users would
   need to reason about both weight-based and priority-based ordering.
   Phase 5 introduces priority shaping; combining it with fair-sharing
   in the same phase would complicate the UX and testing matrix.

**Deferred to:** A future phase that focuses on fair-sharing integration.

### Cross-ClusterQueue preemption policy

Custom preemption policies (e.g., "never preempt across CQs even if
priority is lower") require Kueue configuration that is beyond the RTJ
operator's scope. Phase 5 shapes the priority; Kueue applies its configured
preemption policy.

**Deferred to:** Kueue upstream and multi-CQ policy design.

## What New Telemetry/Status Is Needed

### New Status Fields

| Field | Type | Description |
| --- | --- | --- |
| `status.effectivePriority.basePriority` | int32 | Base priority from WorkloadPriorityClass |
| `status.effectivePriority.effectivePriority` | int32 | Current computed effective priority |
| `status.effectivePriority.penalty` | int32 | Current penalty applied |
| `status.effectivePriority.protectionWindowActive` | bool | Whether protection window is active |
| `status.effectivePriority.protectionWindowExpiresAt` | Time | When protection expires |
| `status.effectivePriority.lastCheckpointAge` | Duration | Age of most recent checkpoint |
| `status.effectivePriority.lastEvaluationTime` | Time | When priority was last evaluated |
| `status.effectivePriority.reason` | string | Human-readable explanation |

### New Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `priority_evaluations_total` | Counter | — | Total priority evaluations performed |
| `priority_penalties_applied_total` | Counter | — | Evaluations that resulted in a penalty |
| `priority_protection_window_active` | Gauge | — | Number of RTJs currently in protection window |
| `priority_effective_value` | Gauge | `rtj_name`, `namespace` | Current effective priority per RTJ |
| `priority_telemetry_failures_total` | Counter | — | Checkpoint telemetry read failures |
| `priority_driven_preemptions_total` | Counter | — | Kueue preemptions where effective < base |

### New Environment Variables

None. Phase 5 does not inject new environment variables into the training
pods. Priority shaping is a control-plane-only feature.

## Upgrade Path

### From Phase 4 to Phase 5 (No Feature Changes)

1. Deploy the Phase 5 operator.
2. Existing RTJs with no `spec.priorityShapingRef` continue to work
   identically to Phase 4.
3. No behavioural changes unless priority shaping is explicitly enabled.

### Enabling Priority Shaping

1. Create a `PriorityShapingPolicy` CR:
   ```yaml
   apiVersion: training.checkpoint.example.io/v1alpha1
   kind: PriorityShapingPolicy
   metadata:
     name: default-shaping
   spec:
     protectionDuration: 10m
     freshnessThreshold: 5m
     penaltyStepSize: 100
     maxPenalty: 500
     evaluationInterval: 30s
   ```
2. Set `spec.priorityShapingRef: default-shaping` on the RTJ.
3. The Priority Shaping Controller will begin evaluating and shaping the
   effective priority for the RTJ's Workload.

### Verifying Priority Shaping

```bash
# Check RTJ effective priority status
kubectl get rtj my-training -o jsonpath='{.status.effectivePriority}'

# Check Workload priority
kubectl get workload -l kueue.x-k8s.io/job-uid=<rtj-uid> \
  -o jsonpath='{.items[0].spec.priority}'

# View priority metrics
curl -s http://localhost:8080/metrics | grep priority
```
