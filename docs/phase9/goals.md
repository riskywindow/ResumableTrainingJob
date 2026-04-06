# Phase 9 — Goals

## Must-ship (P0)

### G-1: Manual target-based resize API

The RTJ spec gains `spec.elasticity` with:

- `mode`: `Disabled` (default, ≡ Phase 8) or `Manual`.
- `targetWorkerCount`: The desired worker count.  Must satisfy
  `parallelism.minCount ≤ targetWorkerCount`.  When omitted or equal to the
  current admitted count, no resize occurs.
- `inPlaceShrinkPolicy`: `IfSupported` (try in-place, fall back to
  checkpoint-and-relaunch) or `Never` (always checkpoint-and-relaunch for
  shrink).

The RTJ status gains `status.elasticity` reporting current/target counts,
resize phase, chosen resize path, and timestamps.

### G-2: In-place shrink (fast path)

When the operator detects `targetWorkerCount < currentWorkerCount` and the
runtime supports live replica reduction:

1. Controller patches child JobSet to reduce replica count.
2. Controller writes `reclaimablePods` entries to
   `Workload.status.reclaimablePods` so Kueue releases freed quota.
3. Controller waits for surplus pods to terminate.
4. Controller updates `status.elasticity` to reflect the new size.

No checkpoint is required.  No new Kueue admission is required.

**Definition of "runtime supports it"**: The child JobSet controller and the
training framework (e.g., PyTorch Elastic / TorchElastic) can handle a
replica-count reduction on a live JobSet without data loss or hang.  Phase 9
ships with a conservative default: `inPlaceShrinkPolicy: IfSupported` checks
a runtime capability annotation on the JobSet
(`training.io/supports-in-place-shrink: "true"`).  If the annotation is
absent, the controller falls back to checkpoint-and-relaunch.

### G-3: Checkpoint-and-relaunch shrink (fallback)

When in-place shrink is not supported or `inPlaceShrinkPolicy: Never`:

1. Controller sets `spec.control.desiredState = Paused` (triggers graceful
   yield at the next step boundary).
2. Trainer writes checkpoint and exits.
3. Controller records completed checkpoint.
4. Controller updates Workload PodSet counts to the smaller target.
5. Controller sets `spec.suspend = false` and waits for Kueue re-admission.
6. On admission, controller relaunches child JobSet at the new size, restoring
   from the checkpoint (with DCP resharding if world-size changed).

### G-4: Checkpoint-and-relaunch grow

Scale-up always requires additional quota, so it always goes through
checkpoint-and-relaunch:

1. Controller sets `spec.control.desiredState = Paused`.
2. Trainer writes checkpoint and exits.
3. Controller records completed checkpoint.
4. Controller updates Workload PodSet counts to the larger target.
5. Controller sets `spec.suspend = true` (request re-admission at larger size).
6. Kueue evaluates admission; if quota available, admits at new size.
7. Controller relaunches child JobSet at the new size, restoring from
   checkpoint.

### G-5: `Workload.status.reclaimablePods` integration

For in-place shrink, the controller writes:

```yaml
status:
  reclaimablePods:
    - name: "workers"     # matches the PodSet name
      count: <delta>      # number of pods being released
```

Kueue reads this field and releases the corresponding quota.  The controller
clears the entry once the surplus pods are confirmed terminated and the resize
is complete.

### G-6: Phase 6 / 7 / 8 backward compatibility

- **Elasticity disabled** (default): Behavior is identical to Phase 8.
- **MultiKueue (Phase 6)**: Manager propagates `spec.elasticity` to the
  worker-side RTJ.  Resize execution happens on the worker.  Manager mirrors
  `status.elasticity` from worker.
- **Launch gating (Phase 7)**: Launch gates apply to the relaunch step of
  checkpoint-and-relaunch resize, just as they apply to normal resume.
- **DRA (Phase 8)**: Device profile does not change during resize (same
  devices, different worker count).  ResourceClaimTemplates are not
  modified during resize.  Checkpoint device-profile compatibility check
  applies to the relaunch step.

### G-7: Phase 9 demo

A single-cluster demo that:

1. Submits an RTJ with 8 workers.
2. Patches `targetWorkerCount = 4` (shrink).
3. Observes either in-place shrink or checkpoint-and-relaunch shrink
   (depending on runtime annotation).
4. Observes `reclaimablePods` quota release (in-place path).
5. Patches `targetWorkerCount = 6` (grow).
6. Observes checkpoint-and-relaunch grow with re-admission.
7. Verifies training continuity (no data loss, correct step resume).

---

## Stretch (P1)

### G-8: In-place shrink with live DRA claim release

When shrinking in-place, release the DRA ResourceClaims associated with the
terminated pods.  Requires coordination with the DRA driver to deallocate
devices.  Deferred because DRA claim lifecycle during live resize is not yet
well-defined upstream.

### G-9: MultiKueue in-place shrink propagation

In-place shrink on a worker cluster with reclaimablePods status mirrored to
the manager.  Deferred because the MultiKueue adapter's status-mirror path
does not yet propagate `Workload.status.reclaimablePods` fields.

### G-10: Resize history and observability

Emit Kubernetes events and Prometheus metrics for each resize transition:
resize requested, resize path chosen, resize completed, resize failed.
Track cumulative resize count, duration, and checkpoint overhead.

---

## Non-goals (explicitly out of scope)

| ID | Non-goal | Reason |
|---|---|---|
| NG-1 | Automatic metric-driven autoscaling | Core milestone is manual; autoscaler is a future layer on top |
| NG-2 | Native Kueue Workload Slices as core path | Workload Slices are experimental and not required for manual resize |
| NG-3 | Custom scheduler or quota engine | Kueue remains the admission authority |
| NG-4 | In-place grow | Requires upstream Kueue Workload resize support (KEP pending) |
| NG-5 | Cross-device-profile resize | Device profile is immutable within a resize; changing devices requires a new RTJ |
| NG-6 | Resize during Draining phase | Resize is rejected while a yield/drain is in progress |
