# Phase 5 API Reference

## CheckpointPriorityPolicy (cluster-scoped)

**Group:** `training.checkpoint.example.io`
**Version:** `v1alpha1`
**Kind:** `CheckpointPriorityPolicy`
**Short name:** `cpp`
**Scope:** Cluster

### Spec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `checkpointFreshnessTarget` | `Duration` | Yes | — | Maximum acceptable checkpoint age before a job becomes Preemptible. |
| `startupProtectionWindow` | `Duration` | Yes | — | Duration of priority protection after start/resume. Resets on every resume. |
| `minRuntimeBetweenYields` | `Duration` | Yes | — | Minimum runtime between successive yields (anti-thrashing). |
| `maxYieldsPerWindow` | `int32` | No | `0` | Max yields within `yieldWindow`. 0 disables yield counting. |
| `yieldWindow` | `*Duration` | No | — | Sliding window for yield counting. Required when `maxYieldsPerWindow > 0`. |
| `failOpenOnTelemetryLoss` | `*bool` | No | `true` | Keep base priority when checkpoint telemetry is unavailable. |
| `failOpenOnCheckpointStoreErrors` | `*bool` | No | `false` | Keep base priority when checkpoint store is unreachable. |
| `protectedBoost` | `*int32` | No | `0` | Priority offset during Protected state. |
| `cooldownBoost` | `*int32` | No | `0` | Priority offset during Cooldown state. |
| `staleCheckpointBoost` | `*int32` | No | `0` | Priority offset when checkpoint exceeds freshness target. |
| `preemptibleOffset` | `*int32` | No | `0` | Priority offset during Preemptible state (negative allowed). |
| `minEffectivePriority` | `*int32` | No | — | Floor for computed effective priority. |
| `maxEffectivePriority` | `*int32` | No | — | Ceiling for computed effective priority. |

All boost/offset and min/max priority fields must be within `[-1000000000, 1000000000]`.

### Status

| Field | Type | Description |
|-------|------|-------------|
| `conditions` | `[]Condition` | Standard Kubernetes conditions. |

### Validation Rules

1. `checkpointFreshnessTarget`, `startupProtectionWindow`, `minRuntimeBetweenYields` must be positive.
2. `yieldWindow` is required and must be positive when `maxYieldsPerWindow > 0`.
3. `yieldWindow` must be positive when explicitly set.
4. `minEffectivePriority <= maxEffectivePriority` when both are set.
5. All boost/offset/priority fields must be within `[-1000000000, 1000000000]`.
6. Negative `preemptibleOffset` is explicitly allowed.

### Defaulting Rules

| Field | Condition | Default |
|-------|-----------|---------|
| `failOpenOnTelemetryLoss` | nil | `true` |
| `failOpenOnCheckpointStoreErrors` | nil | `false` |
| `protectedBoost` | nil | `0` |
| `cooldownBoost` | nil | `0` |
| `staleCheckpointBoost` | nil | `0` |
| `preemptibleOffset` | nil | `0` |

---

## RTJ Spec Extension

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec.priorityPolicyRef` | `*PriorityPolicyReference` | No | `nil` | Reference to a cluster-scoped CheckpointPriorityPolicy. |
| `spec.priorityPolicyRef.name` | `string` | Yes (when ref set) | — | Name of the CheckpointPriorityPolicy. |

### Behavior When Absent

When `spec.priorityPolicyRef` is nil:
- No priority shaping is applied.
- The RTJ's Workload priority equals the base WorkloadPriorityClass value.
- `status.priorityShaping` remains nil.
- Phase 4 behavior is fully preserved.

### Validation Rules

1. When `priorityPolicyRef` is set, `name` must be non-empty.

---

## RTJ Status Extension

| Field | Type | Description |
|-------|------|-------------|
| `status.priorityShaping` | `*PriorityShapingStatus` | Nil when no policy is referenced. |
| `.basePriority` | `int32` | Static priority from WorkloadPriorityClass. |
| `.effectivePriority` | `int32` | Computed priority written to Workload.Spec.Priority. |
| `.preemptionState` | `enum` | `Protected`, `Active`, `Cooldown`, or `Preemptible`. |
| `.preemptionStateReason` | `string` | Machine-readable reason (e.g., "WithinProtectionWindow", "CheckpointStale"). |
| `.protectedUntil` | `*Time` | When the protection window expires. |
| `.lastCompletedCheckpointTime` | `*Time` | Timestamp of most recent checkpoint. |
| `.checkpointAge` | `string` | Human-readable checkpoint age (e.g., "5m30s"). |
| `.lastYieldTime` | `*Time` | Timestamp of most recent yield. |
| `.lastResumeTime` | `*Time` | Timestamp of most recent resume. |
| `.recentYieldCount` | `int32` | Yields within the policy's yield window. |
| `.appliedPolicyRef` | `string` | Name of the CheckpointPriorityPolicy used. |

### Preemption State Machine

```
                    ┌─────────────┐
    start/resume ──>│  Protected  │
                    └──────┬──────┘
                           │ protection window expires
                           v
                    ┌─────────────┐
                    │   Active    │<──── checkpoint refreshed
                    └──────┬──────┘
                           │ checkpoint stale
                           v
                    ┌──────────────┐
                    │ Preemptible  │
                    └──────┬───────┘
                           │ yielded + yield budget exceeded
                           v
                    ┌─────────────┐
                    │  Cooldown   │──── cooldown expires ──> Active
                    └─────────────┘
```

---

## Effective Priority Formula

```
effective_priority = clamp(
    base_priority + state_adjustment,
    minEffectivePriority,    // floor (if set)
    maxEffectivePriority,    // ceiling (if set)
)
```

Where `state_adjustment` depends on `preemptionState`:

| State | Adjustment |
|-------|-----------|
| Protected | `+protectedBoost` |
| Active (fresh checkpoint) | `0` |
| Active (stale checkpoint) | `+staleCheckpointBoost` |
| Cooldown | `+cooldownBoost` |
| Preemptible | `+preemptibleOffset` |

The `base_priority` is the integer value of the Kueue WorkloadPriorityClass
referenced by `spec.workloadPriorityClassName`. It is immutable and owned by
Kueue. The operator only writes the computed `effective_priority` to
`Workload.Spec.Priority`.

---

## Example

```yaml
apiVersion: training.checkpoint.example.io/v1alpha1
kind: CheckpointPriorityPolicy
metadata:
  name: standard-shaping
spec:
  checkpointFreshnessTarget: 10m
  startupProtectionWindow: 5m
  minRuntimeBetweenYields: 2m
  maxYieldsPerWindow: 3
  yieldWindow: 1h
  protectedBoost: 50
  cooldownBoost: 25
  preemptibleOffset: -100
  minEffectivePriority: -500
  maxEffectivePriority: 10000
---
apiVersion: training.checkpoint.example.io/v1alpha1
kind: ResumableTrainingJob
metadata:
  name: my-training
  namespace: research
spec:
  queueName: gpu-queue
  workloadPriorityClassName: batch-medium
  priorityPolicyRef:
    name: standard-shaping
  # ... rest of RTJ spec unchanged ...
```
