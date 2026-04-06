# Phase 9: Resize Execution Design

**Status:** Implemented
**Prerequisite:** Phase 9 elastic planning model (elastic-planning.md)

## Overview

This document describes how the RTJ controller executes resize operations
planned by the elastic planning model. The execution engine receives a
`PlanOutput` from `EvaluatePlan()` and performs the minimal controller
mutations needed to bring the RTJ to the desired worker count.

## Execution Paths

### 1. In-Place Shrink (ShrinkInPlace)

**Trigger:** `targetWorkerCount < currentWorkerCount`, runtime supports
in-place shrink, policy is `IfSupported`.

**Steps:**
1. Compute `ReclaimDelta` from the plan (number of workers to release).
2. Build `reclaimablePods` slice for the worker PodSet.
3. Apply SSA patch to `Workload.status.reclaimablePods` using the
   dedicated field manager `rtj-elastic-reclaim`.
4. Set `status.elasticity.reclaimablePodsPublished = true`.
5. Transition `resizeState` to `InProgress`.
6. Set conditions: `ShrinkingInPlace`, `ShrinkReclaimPublished`.

**Invariant:** The Workload remains admitted. The RTJ remains Running.
Kueue reads `reclaimablePods` and releases the freed quota to other
workloads in the ClusterQueue.

**Idempotency:** On the next reconcile, the planner sees
`reclaimablePodsPublished=true` and returns `PlanReclaimPublished`,
preventing duplicate SSA patches.

### 2. Checkpoint-and-Relaunch Grow (GrowViaRelaunch)

**Trigger:** `targetWorkerCount > currentWorkerCount`. Always requires
checkpoint-and-relaunch because growing needs new Kueue admission for
the additional workers.

**Steps:**
1. Mark `resizeState = InProgress`, `resizePath = CheckpointAndRelaunch`.
2. Set condition: `ResizeCheckpointing`.
3. Signal `TriggerStopFlow` to the main reconciler.
4. The reconciler enters the existing drain flow (stopSourceResize).
5. Runtime checkpoints, drain completes, child JobSet is cleaned up.
6. RTJ transitions to Paused, then (since desiredState=Running) to Queued.
7. Kueue re-admits with the new worker count.
8. Launch gates are re-evaluated (Phase 7 provisioning, topology, DRA).
9. New child JobSet is rendered with the target worker count.
10. Condition transitions: `ResizeCheckpointing` -> `RelaunchingForResize`.
11. On successful transition to Running, `completeResizeAfterRelaunch()`
    sets `resizeState = Completed` and clears all execution conditions.

### 3. Checkpoint-and-Relaunch Shrink Fallback (ShrinkViaRelaunch)

**Trigger:** `targetWorkerCount < currentWorkerCount` AND one of:
- `inPlaceShrinkPolicy = Never`
- Runtime does not support in-place shrink (annotation missing)

**Steps:** Identical to GrowViaRelaunch. The difference is cosmetic
(shrink direction in conditions/messages).

**Blocker note:** If the runtime fixture reports
`supports_in_place_shrink=false` (as DDP currently does), all shrinks
will use this fallback path. This is by design: in-place shrink requires
the training framework to support live worker removal, which DDP/PyTorch
does not natively support. The controller documents this clearly in the
`ResizeCheckpointing` condition message.

### 4. No-Op / Blocked / In-Progress

- **NoResize:** Clears any stale resize state. If a previous
  `InProgress` state is found, transitions to `Completed`.
- **ResizeBlocked:** Sets the `ResizeBlocked` condition with the
  specific reason (preemption, bounds, DRA, workload not admitted).
- **ResizeInProgress / ReclaimPublished:** No execution action; waits
  for the current operation to complete.

## Condition Lifecycle

Exactly one resize condition is active at a time:

```
ResizePending
  -> ShrinkingInPlace + ShrinkReclaimPublished  (in-place path)
  -> ResizeCheckpointing -> RelaunchingForResize (relaunch path)
  -> (all cleared on completion or NoResize)

ResizeBlocked  (mutually exclusive with all others)
ResizeFailed   (set on SSA patch failure)
```

All conditions carry `observedGeneration` to detect stale state.

## DRA Consistency

When DRA is enabled:

- **In-place shrink:** DRA helper objects (ResourceClaimTemplates,
  ResourceClaims) are not modified. The worker pods being reclaimed
  release their claims naturally when deleted.
- **Relaunch path:** The full DRA reconciliation runs on the new launch:
  `reconcileDRATemplates()` recomputes templates for the new shape,
  the render path injects DRA claims into the new child JobSet.
  The old templates are cleaned up by owner reference GC.

## RTJ Status Fields

### status.elasticity

| Field | Type | Description |
|-------|------|-------------|
| `resizeState` | ResizeState | Idle, Pending, InProgress, Completed, Blocked, Failed |
| `resizePath` | ResizePath | InPlace, CheckpointAndRelaunch, or empty |
| `resizeReason` | string | Machine-readable reason for current state |
| `reclaimablePodsPublished` | bool | True when SSA patch has been applied |
| `reclaimableWorkerCount` | int32 | Number of workers declared reclaimable |
| `lastResizeEvent` | string | Human-readable description |
| `lastElasticTransitionTime` | Time | When state last changed |
| `lastResizeCompletedTime` | Time | When last resize completed |

### Conditions

| Condition | Reason | When Set |
|-----------|--------|----------|
| `ResizePending` | ShrinkInPlacePending / ShrinkViaRelaunchPending / GrowViaRelaunchPending | Plan computed, execution not started |
| `ShrinkingInPlace` | ShrinkInPlaceExecuting | SSA patch being applied |
| `ShrinkReclaimPublished` | ShrinkReclaimPublished | reclaimablePods written to Workload |
| `ResizeCheckpointing` | ResizeCheckpointing | Drain flow active for relaunch |
| `RelaunchingForResize` | RelaunchingForResize | Post-drain, re-admission in progress |
| `ResizeBlocked` | ResizeBlockedBy{Workload,Preemption,Bounds,DRA} | Resize cannot proceed |
| `ResizeFailed` | ResizeFailed | Execution error (e.g., SSA patch failed) |

## Integration Points

### Main Reconcile Loop

```
Reconcile()
  -> (existing Phase 1-8 checks)
  -> if activeExists && elasticityEnabled:
       evaluateElasticPlan()      // pure-function plan
       executeElasticPlan()       // mutation
       if TriggerStopFlow:
         reconcileResizeStopFlow() -> reconcileStopFlow(stopSourceResize)
  -> if wasRestoring && isResizeTriggeredStop:
       completeResizeAfterRelaunch()
```

### Render Integration

`RenderInput.ElasticTargetWorkerCount` injects
`YIELD_SDK_TARGET_WORKER_COUNT` into all containers, so the runtime
SDK can observe the desired worker count for coordination.

### Launch Gate Integration

`LaunchGateResult.ResizeRelaunch` is set when the launch is triggered
by a resize-and-relaunch flow, enabling the `RelaunchingForResize`
condition on the launch/resume path.

## Test Coverage

| Test | What it proves |
|------|---------------|
| Manual shrink in-place | Plan produces ShrinkInPlace, execution sets conditions |
| reclaimablePods monotonicity | Published flag prevents duplicate SSA patches |
| Manual grow checkpoint-and-relaunch | Plan produces GrowViaRelaunch, triggers stop flow |
| Shrink fallback | When in-place not supported, plan falls back to relaunch |
| DRA-enabled resize coherence | DRA status preserved, reclaim flag cleared for relaunch |
| Repeated reconcile idempotency | Second execution of same plan does not duplicate actions |
| Condition lifecycle | Each plan kind sets/clears the correct conditions |
| Resize completion | completeResizeAfterRelaunch clears state on success |

## Invariants

- **I-9 (preserved):** Elasticity disabled = Phase 8 behavior. The
  execution engine short-circuits on `PlanNoResize`.
- **I-10 (preserved):** Scale-up always checkpoint-and-relaunch.
- **I-11 (preserved):** `reclaimablePods` is the only quota-release
  signal for in-place shrink.
- **New:** Resize execution is idempotent. Repeated reconciles with
  the same plan do not duplicate mutations.
- **New:** Exactly one resize condition is active at any time (except
  `ShrinkingInPlace` + `ShrinkReclaimPublished` which are paired).
