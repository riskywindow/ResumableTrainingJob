# Effective Priority Materialization

## Summary

This document describes how Phase 5 effective priority is materialized into
the Kueue Workload without being clobbered by Kueue's GenericJob reconciler.

## Ownership Model

**Approach: RTJ reconciler owns effective priority materialization.**

The RTJ reconciler derives effective priority from the checkpoint-aware
decision engine and patches `Workload.Spec.Priority` directly. This is the
smallest coherent ownership model because:

1. The RTJ reconciler already has access to all inputs (RTJ status,
   checkpoint catalog, policy reference, WorkloadPriorityClass).
2. The RTJ reconciler already observes phase transitions that trigger
   priority re-evaluation (Running, YieldRequested, etc.).
3. No separate controller or timer loop is needed — priority evaluation
   runs inline during the reconcile when the job is in an active phase.

## Anti-Clobbering Strategy

Kueue's `GenericJob` reconciler sets `Workload.Spec.Priority` at **creation
time** by resolving the `WorkloadPriorityClass` referenced by the job's
`PriorityClass()` method. On subsequent reconciles, Kueue does **not**
overwrite `Spec.Priority` — it only reads it for preemption decisions.

The RTJ adapter implements `PriorityClass()` to return the RTJ's
`spec.workloadPriorityClassName`, which tells Kueue which class to resolve
at Workload creation. After creation, the RTJ reconciler patches
`Workload.Spec.Priority` with the effective priority computed by the
checkpoint-aware decision engine.

This means:
- **Workload creation:** Kueue sets `Spec.Priority` from the
  WorkloadPriorityClass value (base priority).
- **Subsequent reconciles:** The RTJ controller patches `Spec.Priority`
  with the effective priority. Kueue does not clobber it.

## Immutable Fields

The following Workload fields remain immutable and Kueue-owned:
- `Spec.PriorityClassSource` — set by Kueue at creation time.
- `Spec.PriorityClassName` — set by Kueue at creation time from
  `PriorityClass()`.

The mutable field the RTJ controller owns:
- `Spec.Priority` — the numeric priority value. Set by Kueue at creation
  from the WorkloadPriorityClass value, then patched by the RTJ controller
  when effective priority differs from base.

## Evaluation Trigger

Priority evaluation runs during the RTJ reconcile loop when:
1. The job is in an active phase (Starting, Running, Restoring,
   YieldRequested, Draining).
2. A `CheckpointPriorityPolicy` is referenced via `spec.priorityPolicyRef`.

When either condition is false, priority evaluation is skipped:
- **No policy:** `Workload.Spec.Priority` retains the base
  WorkloadPriorityClass value. Phase 4 behavior is fully preserved.
- **Inactive phase:** Priority is not evaluated. When the job transitions
  to Queued, `clearPriorityShapingOnQueued()` resets the effective priority
  to base.

## Reconcile Flow

```
RTJ Reconcile
  ├── Phase transitions (mark*, sync*)
  ├── Active job exists?
  │   ├── Yes → markRunning() + reconcilePriorityState()
  │   │   ├── Resolve CheckpointPriorityPolicy
  │   │   ├── Resolve WorkloadPriorityClass (base priority)
  │   │   ├── CollectTelemetry() → TelemetrySnapshot
  │   │   ├── buildEvaluationInput() → EvaluationInput
  │   │   ├── Evaluate() → Decision
  │   │   ├── SyncPriorityShapingTelemetry() → update status
  │   │   ├── syncDecisionToStatus() → update status
  │   │   ├── patchWorkloadPriority() → patch Workload
  │   │   ├── syncPriorityAnnotations() → update annotations
  │   │   └── setPriorityShapingCondition() → update condition
  │   └── No → launch/resume flow
  └── ...
```

## Idempotency

The reconciliation is idempotent:

1. **Status sync:** `syncDecisionToStatus()` compares each field before
   writing. If the decision matches the existing status, no change is
   reported.
2. **Workload patch:** `patchWorkloadPriority()` reads the current
   `Workload.Spec.Priority` and only patches when the value differs.
3. **Annotations:** `syncPriorityAnnotations()` compares string values
   before setting.
4. **Conditions:** `setPriorityShapingCondition()` uses the existing
   `setCondition()` helper which skips updates when the condition already
   has the same status, reason, and message.

This prevents infinite reconcile loops: if the telemetry and policy are
unchanged, the reconcile produces the same decision and makes no writes.

## Observability

### RTJ Status

`status.priorityShaping` exposes:
- `basePriority` — static priority from WorkloadPriorityClass.
- `effectivePriority` — computed priority written to Workload.
- `preemptionState` — Protected, Active, Cooldown, or Preemptible.
- `preemptionStateReason` — machine-readable reason.
- `protectedUntil` — protection window expiry time.
- `lastCompletedCheckpointTime` — checkpoint freshness input.
- `checkpointAge` — computed age for quick debugging.
- `lastYieldTime`, `lastResumeTime`, `recentYieldCount` — yield telemetry.
- `appliedPolicyRef` — name of the applied CheckpointPriorityPolicy.

### RTJ Conditions

The `PriorityShaping` condition is set to:
- `True` with the decision reason when priority shaping is active.
- `False` with an error reason when policy or priority class resolution
  fails.

### RTJ Annotations

For quick `kubectl get` observability:
- `training.checkpoint.example.io/effective-priority` — numeric string.
- `training.checkpoint.example.io/preemption-state` — state string.

## Backward Compatibility

When `spec.priorityPolicyRef` is nil:
- `reconcilePriorityState()` is a no-op.
- `status.priorityShaping` is nil.
- `Workload.Spec.Priority` is never patched by the RTJ controller.
- All annotations and conditions related to priority shaping are cleared.
- Behavior is identical to Phase 4.

## Requeue Strategy

When the job is in the `StartupProtected` state, the reconciler calculates
the remaining protection window duration and requeues slightly after it
expires (remaining + 1 second). This ensures prompt re-evaluation when the
protection window lapses, without requiring a separate timer-based
controller.

For other states, no priority-driven requeue is needed — the next reconcile
will be triggered by phase transitions or Kueue events.
