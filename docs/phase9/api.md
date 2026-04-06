# Phase 9 — Elasticity API Reference

> **Status:** Implemented — controller logic not yet wired.

## Overview

Phase 9 extends the RTJ API with a narrow `spec.elasticity` / `status.elasticity`
surface for manual target-based worker-count resize.  When elasticity is disabled
(the default), all behavior is identical to Phase 8.

---

## Spec fields

### `spec.elasticity` (ElasticitySpec)

| Field | Type | Default | Mutable | Owner | Description |
|---|---|---|---|---|---|
| `mode` | `Disabled` \| `Manual` | `Disabled` | While suspended | User | Controls whether elasticity is active. |
| `targetWorkerCount` | `*int32` | nil | Yes | User | Desired worker count. Must be ≥ `minCount` and ≤ `preferredCount`. Only meaningful when mode is `Manual`. |
| `inPlaceShrinkPolicy` | `IfSupported` \| `Never` | `IfSupported` | Yes | User | Controls in-place shrink behavior. `IfSupported` tries in-place first, falls back to C&R. `Never` always uses C&R. |
| `reclaimMode` | `ReclaimablePods` | `ReclaimablePods` | Yes | User | Controls quota release mechanism. Only `ReclaimablePods` is supported. |

**Constraints:**

- `mode: Manual` requires `spec.resume.allowWorldSizeChange: true`.
- `targetWorkerCount` must not be set when `mode: Disabled`.
- `targetWorkerCount` must satisfy: `effectiveMinCount ≤ target ≤ effectivePreferredCount`.
- `mode` can only be changed while the RTJ is suspended (`spec.suspend: true`).

---

## Status fields

### `status.elasticity` (ElasticityStatus)

All fields are **controller-owned**. Users must not write to this section.

| Field | Type | Description |
|---|---|---|
| `desiredWorkerCount` | `int32` | Effective preferred count from `spec.parallelism.preferredCount` or `spec.identity.worldSize`. |
| `targetWorkerCount` | `int32` | Mirrors `spec.elasticity.targetWorkerCount` for observability. |
| `activeWorkerCount` | `int32` | Observed number of running worker pods. |
| `admittedWorkerCount` | `int32` | Number of worker pods admitted by Kueue. |
| `resizeState` | `Idle` \| `Pending` \| `InProgress` \| `Blocked` \| `Completed` \| `Failed` | Current state of the resize operation. |
| `resizeReason` | `string` | Machine-readable reason for the current resize state. |
| `currentExecutionMode` | `Fixed` \| `Elastic` | Whether the RTJ is in fixed or elastic execution mode. |
| `resizePath` | `InPlace` \| `CheckpointAndRelaunch` | Resize execution path chosen for current/most recent resize. |
| `reclaimableWorkerCount` | `int32` | Workers declared reclaimable via `reclaimablePods`. |
| `reclaimablePodsPublished` | `bool` | Whether `reclaimablePods` has been written to the Workload. |
| `inPlaceShrinkSupported` | `bool` | Whether the runtime advertises in-place shrink support. |
| `lastResizeEvent` | `string` | Description of the most recent resize event. |
| `lastResizeCheckpoint` | `*CheckpointReference` | Checkpoint from the most recent C&R resize. |
| `lastResizeFailureReason` | `string` | Reason for the most recent resize failure. |
| `lastElasticTransitionTime` | `*metav1.Time` | When the elasticity state last changed. |
| `lastResizeCompletedTime` | `*metav1.Time` | When the most recent resize completed. |

---

## How Phase 3/8 parallelism fields map into Phase 9

| Phase 3/8 field | Phase 9 role | Notes |
|---|---|---|
| `spec.parallelism.preferredCount` | Upper bound for `targetWorkerCount` | Also the "desired" count that `status.elasticity.desiredWorkerCount` mirrors. |
| `spec.parallelism.minCount` | Lower bound for `targetWorkerCount` | Defaults to 1 when not set. |
| `spec.parallelism.enablePartialAdmission` | Orthogonal | Partial admission governs initial admission shape; elasticity governs runtime resize. |
| `spec.identity.worldSize` | Fallback for `preferredCount` when parallelism is nil | Used as upper bound when `preferredCount` is unset. |
| `spec.resume.allowWorldSizeChange` | **Required** for elasticity Manual mode | Resize always changes world size → DCP resharding is needed. |
| `status.admission.admittedWorkerCount` | Feeds `status.elasticity.admittedWorkerCount` | The admitted count is the baseline from which resize delta is computed. |
| `status.admission.activeWorkerCount` | Feeds `status.elasticity.activeWorkerCount` | Running pod count during resize transitions. |

---

## Field authorship

| Field | Author | When set |
|---|---|---|
| `spec.elasticity.mode` | User | At creation or while suspended |
| `spec.elasticity.targetWorkerCount` | User (operator/automation) | At any time when mode is Manual |
| `spec.elasticity.inPlaceShrinkPolicy` | User | At creation or update |
| `spec.elasticity.reclaimMode` | User | At creation or update |
| `status.elasticity.*` | Controller | During reconciliation |

---

## How manual resize is requested

1. **Enable elasticity** (one-time, while suspended):
   ```yaml
   spec:
     resume:
       allowWorldSizeChange: true
     elasticity:
       mode: Manual
   ```

2. **Request a resize** (at any time, live):
   ```bash
   kubectl patch rtj my-training --type=merge -p '
     {"spec":{"elasticity":{"targetWorkerCount": 4}}}'
   ```

3. **Controller evaluates** the delta:
   - If `target < current` and in-place supported → in-place shrink.
   - If `target < current` and in-place not supported → C&R shrink.
   - If `target > current` → C&R grow.
   - If `target == current` → no-op.

4. **Observe progress** via `status.elasticity.resizeState`:
   - `Idle` → `Pending` → `InProgress` → `Completed` (happy path).
   - `InProgress` → `Blocked` (e.g., waiting for quota during grow).
   - `InProgress` → `Failed` (resize could not complete).

---

## Backward compatibility

- **Phase 8 RTJ without `spec.elasticity`**: All Phase 9 code paths are dormant.
  `status.elasticity` remains nil. Behavior is identical to Phase 8.
- **Phase 8 RTJ with `spec.elasticity.mode: Disabled`**: Same as above.
- **Existing `spec.parallelism` semantics**: Unchanged. Phase 9 adds a new
  dimension (runtime resize) orthogonal to admission-time parallelism.
- **`spec.suspend` semantics**: Unchanged. Elasticity mode changes require
  the RTJ to be suspended, same as queue name changes.

---

## Enums added in Phase 9

| Enum | Values | Description |
|---|---|---|
| `ElasticityMode` | `Disabled`, `Manual` | Controls elasticity activation. |
| `InPlaceShrinkPolicy` | `IfSupported`, `Never` | Controls shrink path selection. |
| `ReclaimMode` | `ReclaimablePods` | Controls quota release mechanism. |
| `ResizeState` | `Idle`, `Pending`, `InProgress`, `Blocked`, `Completed`, `Failed` | Resize operation lifecycle. |
| `ResizePath` | `InPlace`, `CheckpointAndRelaunch` | Resize execution path. |
| `ExecutionMode` | `Fixed`, `Elastic` | Current execution mode. |
