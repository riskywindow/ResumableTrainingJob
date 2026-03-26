# Phase 5 Telemetry Reference

This document describes the telemetry and status plumbing that supports
checkpoint-aware priority shaping decisions. It covers data sources,
field semantics, idempotency guarantees, and Prometheus metrics.

## Telemetry Fields

The priority shaping controller consumes a `TelemetrySnapshot` built by
`CollectTelemetry()` on each evaluation cycle. The snapshot is derived
from three data sources listed in order of preference (cheapest first):

| Field | Type | Source | Approximate? |
|-------|------|--------|-------------|
| `lastCompletedCheckpointTime` | `*metav1.Time` | RTJ `status.lastCompletedCheckpoint.completionTime` | No — authoritative from manifest validation during drain |
| `lastCompletedCheckpointID` | `string` | RTJ `status.lastCompletedCheckpoint.id` | No |
| `lastCompletedCheckpointStep` | `int64` | Checkpoint catalog (`LatestCheckpointInfo`) | No — but only available after catalog lookup |
| `lastRunStartTime` | `*metav1.Time` | RTJ `status.transitionTimestamps.startingAt` | No — set by reconciler on phase transition |
| `lastResumeTime` | `*metav1.Time` | RTJ `status.transitionTimestamps.restoreCompletedAt` (preferred) or `runningAt` when a `restoringAt` exists | Slightly approximate when using `runningAt` fallback |
| `lastYieldTime` | `*metav1.Time` | RTJ `status.transitionTimestamps.yieldRequestedAt` | No |
| `lastDrainDuration` | `time.Duration` | Computed: `pausedAt - yieldRequestedAt` | Yes — approximation; measures operator-observed wall clock, not trainer-side drain |
| `recentYieldCount` | `int32` | RTJ annotation `training.checkpoint.example.io/yield-history` | No — windowed count from persisted history |
| `currentRunActiveSince` | `*metav1.Time` | RTJ `status.transitionTimestamps.runningAt` | No |

### RTJ Status Fields (PriorityShapingStatus)

The `SyncPriorityShapingTelemetry()` function writes telemetry into
`status.priorityShaping` on the RTJ. These fields are RTJ-visible and
available via `kubectl get rtj -o yaml`:

| Status Field | Source | Notes |
|-------------|--------|-------|
| `lastCompletedCheckpointTime` | Telemetry snapshot | Authoritative |
| `checkpointAge` | Computed at reconcile time: `now - lastCompletedCheckpointTime` | Recomputed each reconcile; never persisted as a durable duration |
| `lastYieldTime` | Telemetry snapshot | Authoritative |
| `lastResumeTime` | Telemetry snapshot | See fallback logic above |
| `recentYieldCount` | Telemetry snapshot | Windowed; window size from policy's `yieldWindow` |
| `appliedPolicyRef` | RTJ `spec.priorityPolicyRef.name` | Tracks which policy is active |
| `basePriority` | Set by priority shaping controller | Not set by telemetry sync |
| `effectivePriority` | Set by priority shaping controller | Not set by telemetry sync |
| `preemptionState` | Set by priority shaping controller | Not set by telemetry sync |
| `preemptionStateReason` | Set by priority shaping controller | Not set by telemetry sync |
| `protectedUntil` | Set by priority shaping controller | Not set by telemetry sync |

## Data Source Details

### 1. RTJ Status Timestamps (Primary)

Most telemetry derives from `status.transitionTimestamps`, which the main
RTJ reconciler updates on every phase transition. These timestamps are:

- **Persisted in etcd** — survive operator restarts.
- **Set atomically** with the phase transition — no race conditions.
- **Authoritative** — the reconciler is the single writer.

The key timestamps for Phase 5 are:
- `startingAt` — when the current run attempt was created.
- `runningAt` — when the child JobSet was observed as active.
- `yieldRequestedAt` — when a stop was requested (manual or Kueue).
- `drainingAt` — when the drain flow began waiting for markers.
- `pausedAt` — when the drain completed and the child was removed.
- `restoringAt` — when a resume was initiated from a checkpoint.
- `restoreCompletedAt` — when the restored run became Running.
- `lastCheckpointCompletedAt` — when the drain observed a completed checkpoint.

### 2. RTJ Status Checkpoint Reference (Primary)

`status.lastCompletedCheckpoint` is set during the drain flow when
`ObservePause` returns a validated checkpoint. It includes:
- `id` — the checkpoint manifest ID.
- `completionTime` — the RFC3339 completion timestamp from the manifest.
- `manifestURI` — the S3 URI of the manifest.

This is the cheapest and most authoritative source for checkpoint
freshness. The telemetry collector prefers it over a catalog lookup.

### 3. Checkpoint Catalog (Fallback)

The `LatestCheckpointInfo()` method on the `Catalog` interface performs
a lightweight scan of the manifest prefix in the checkpoint store. It:
- Lists all manifest objects under `{storageRoot}/manifests/`.
- Reads and parses each manifest's `completionTimestamp`.
- Returns the one with the latest completion time.
- Does NOT perform full compatibility checking or artifact validation.

This is used only when `status.lastCompletedCheckpoint` is nil (e.g.,
after an operator restart before any drain has been observed, or for
a brand-new RTJ that has been saving periodic checkpoints but hasn't
yielded yet).

### 4. Yield History Annotation (Primary for yield counting)

The annotation `training.checkpoint.example.io/yield-history` stores a
JSON array of RFC3339 timestamps representing yield events:

```json
["2026-03-25T12:05:00Z","2026-03-25T12:12:00Z","2026-03-25T12:14:00Z"]
```

**Why an annotation instead of a status field?**
- `TransitionTimestamps.YieldRequestedAt` only records the *last* yield.
- The windowed count needs a *history* spanning multiple run attempts.
- Annotations persist in etcd and survive operator restarts.
- Annotations are writable from the main object (not just status subresource).

`RecordYieldEvent()` appends a timestamp and prunes entries older than the
configured `yieldWindow`. This keeps the annotation bounded in size.

## Idempotency Guarantees

### CollectTelemetry

`CollectTelemetry()` is a **pure read** operation. It does not mutate the
RTJ. Calling it multiple times with the same RTJ state produces the same
`TelemetrySnapshot`. This is critical because the priority shaping
controller may evaluate the same RTJ multiple times between status updates.

### SyncPriorityShapingTelemetry

`SyncPriorityShapingTelemetry()` is **idempotent**. When called with the
same snapshot and `now` value, it returns `changed=false` on the second
call. This prevents unnecessary status update API calls.

The one field that changes between reconciles even with the same checkpoint
is `checkpointAge`, which is recomputed from `now - lastCompletedCheckpointTime`.
This is intentional — the age should always reflect the current evaluation time.

### Operator Restart Resilience

After an operator restart:

1. **RTJ status is loaded from etcd** — all transition timestamps,
   checkpoint references, and priority shaping status survive.
2. **Yield history annotation is loaded from etcd** — windowed yield
   counting resumes correctly.
3. **The catalog fallback** covers the case where status fields were
   cleared or the operator was upgraded from a version that didn't
   record checkpoint references.
4. **No re-initialization** — `SyncPriorityShapingTelemetry` preserves
   existing `basePriority`, `effectivePriority`, and `preemptionState`
   that were set by the priority shaping controller before the restart.

## Phase 5 Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `checkpoint_native_operator_priority_evaluations_total` | Counter | Total priority evaluations performed |
| `checkpoint_native_operator_priority_penalties_applied_total` | Counter | Total times effective priority was lowered below base |
| `checkpoint_native_operator_priority_protection_window_active` | Gauge | Number of RTJs currently within protection window |
| `checkpoint_native_operator_priority_effective_value` | Gauge (per RTJ) | Current effective priority per RTJ |
| `checkpoint_native_operator_priority_telemetry_failures_total` | Counter | Total checkpoint telemetry retrieval failures |
| `checkpoint_native_operator_priority_driven_preemptions_total` | Counter | Total priority-shaping-influenced preemptions |

These metrics are registered in `internal/metrics/metrics.go` and exposed
via the standard controller-runtime metrics endpoint.

## Lifecycle Event Recording

The status helpers in `internal/controller/status_helpers.go` provide
Phase 5 hooks that are called from the main RTJ reconciler:

| Helper | Called When | Effect |
|--------|-----------|--------|
| `recordYieldForTelemetry()` | After `markStopRequested()` | Appends to yield history annotation, sets `lastYieldTime` |
| `recordResumeForTelemetry()` | After `markRunning()` when previous phase was Restoring | Sets `lastResumeTime`, records `appliedPolicyRef` |
| `clearPriorityShapingOnQueued()` | When RTJ transitions to Queued | Clears runtime-only fields, preserves historical timestamps, resets effective priority to base |

These helpers are no-ops when `spec.priorityPolicyRef` is nil, preserving
Phase 4 behavior exactly.

## Architectural Decision: Compute vs. Persist

**checkpointAge is computed, not persisted.** The age changes every second,
so persisting it would create unnecessary status update churn. Instead,
the telemetry sync recomputes it from `now - lastCompletedCheckpointTime`
on every evaluation.

**recentYieldCount uses an annotation, not a counter status field.** A
simple counter would not support windowed expiry. The JSON array of
timestamps in the annotation enables precise windowed counting and
graceful pruning.

**lastDrainDuration is approximate.** It measures the operator's observed
wall-clock time between `YieldRequestedAt` and `PausedAt`. This includes
control file propagation latency and reconcile intervals, so it slightly
overstates the trainer's actual drain time. This is acceptable for
priority shaping decisions.
