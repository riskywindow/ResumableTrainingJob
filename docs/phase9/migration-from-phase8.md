# Phase 9 — Migration from Phase 8

## What stays the same

Everything from Phase 8 is preserved when `spec.elasticity.mode` is
`Disabled` (the default).  Specifically:

| Component | Phase 8 behavior | Phase 9 with elasticity disabled |
|---|---|---|
| RTJ spec | No elasticity fields | Ignored (zero-value) |
| RTJ status | No elasticity section | Empty / absent |
| Workload | Created once, never mutated mid-run | Identical |
| Child JobSet | Created on launch, deleted on pause | Identical |
| Checkpoint flow | Yield → checkpoint → pause | Identical |
| Resume flow | Select checkpoint → relaunch | Identical |
| DRA | ResourceClaimTemplates, device compat | Identical |
| MultiKueue | Manager/worker dispatch | Identical |
| Launch gating | ProvisioningRequest, waitForPodsReady | Identical |
| Topology | TopologyRequest, TAS assignment | Identical |
| Priority | Checkpoint-aware effective priority | Identical |

**Invariant I-9**: Elasticity disabled ≡ Phase 8 behavior.

---

## What changes when elasticity is enabled

### 1. RTJ spec gains `spec.elasticity`

```yaml
spec:
  elasticity:
    mode: Manual
    targetWorkerCount: 4
    inPlaceShrinkPolicy: IfSupported  # or Never
```

- `mode: Disabled` (default): No change from Phase 8.
- `mode: Manual`: Enables target-based resize.
- `targetWorkerCount`: When set and different from the current admitted
  count, triggers resize evaluation.
- `inPlaceShrinkPolicy`:
  - `IfSupported`: Try in-place shrink first; fall back to
    checkpoint-and-relaunch if runtime doesn't support it.
  - `Never`: Always use checkpoint-and-relaunch for shrink.

### 2. RTJ status gains `status.elasticity`

```yaml
status:
  elasticity:
    currentWorkerCount: 8
    targetWorkerCount: 4
    resizePhase: InProgress   # Idle | InProgress | Blocked
    resizePath: InPlace       # InPlace | CheckpointAndRelaunch | ""
    lastResizeTimestamp: "2026-04-05T00:00:00Z"
    reclaimablePods:
      - name: workers
        count: 4
```

### 3. RTJ control semantics extended

The controller now has a third trigger for the yield flow (in addition to
manual pause and Kueue preemption): **resize-initiated checkpoint**.  The
flow is identical to a manual pause — graceful yield at step boundary, write
checkpoint, exit — but the intent is resume at a different size, not quiesce.

### 4. Workload lifecycle changes

| Scenario | Phase 8 | Phase 9 |
|---|---|---|
| Normal run | Workload created once | Same |
| Manual pause | Workload suspended | Same |
| Kueue preemption | Workload suspended by Kueue | Same |
| In-place shrink | N/A | Workload.status.reclaimablePods written |
| C&R shrink | N/A | Workload suspended → PodSets mutated → re-admitted |
| C&R grow | N/A | Workload suspended → PodSets mutated → re-admitted at larger size |

---

## How `Workload.status.reclaimablePods` is used

Kueue 0.9+ supports the `reclaimablePods` status field on Workload objects.
When an external job framework (the RTJ controller) writes:

```yaml
status:
  reclaimablePods:
    - name: "workers"
      count: 4
```

Kueue interprets this as: "4 pods from the 'workers' PodSet are being
released; reclaim their quota."  Kueue adjusts the ClusterQueue usage
accordingly, freeing quota for other workloads — without requiring the
Workload to be fully suspended.

### RTJ controller responsibilities

1. **Write**: After patching the child JobSet replicas down and observing
   surplus pods draining, write `reclaimablePods` to the Workload status.
2. **Wait**: Wait for surplus pods to terminate.
3. **Clear**: Once all surplus pods are gone and the Workload's admitted
   quota matches the actual usage, clear the `reclaimablePods` entry.

### What this does NOT do

- It does NOT trigger Kueue to preempt the RTJ's own pods.
- It does NOT change the Workload's `spec.podSets` — the Workload remains
  admitted at the original size, with the delta declared reclaimable.
- It does NOT interact with DRA claims (DRA claim release during in-place
  shrink is a stretch goal).

---

## Why native Kueue Workload Slices are NOT the core path

Workload Slices (Kueue's native mechanism for sub-dividing a Workload into
independently-admittable slices) are an experimental feature aimed at
gang-scheduling partial workloads.  For Phase 9:

1. **Not required for the manual resize use case.**  The RTJ controller can
   achieve deterministic resize via the suspend/mutate/re-admit cycle
   (checkpoint-and-relaunch) and via `reclaimablePods` (in-place shrink).
   Both mechanisms use stable Kueue API surface.

2. **Workload Slices add complexity.**  Slices require the controller to
   manage multiple admission lifecycles per RTJ and coordinate partial
   admission across slices.  This is unnecessary when the RTJ controller
   already owns the full lifecycle.

3. **reclaimablePods is simpler and sufficient.**  For quota release during
   in-place shrink, `reclaimablePods` provides a direct signal to Kueue
   without decomposing the Workload.

4. **Future compatibility.**  If Workload Slices stabilize and provide
   benefits (e.g., incremental grow without full checkpoint), Phase 9's
   architecture allows adopting them as an optimization without changing
   the RTJ API or the core resize flow.

---

## DRA-backed RTJs during resize

When `spec.devices.mode: DRA`:

- **Device profile is immutable during resize.**  The same device class,
  selectors, and count-per-pod apply.  Changing device requirements during
  a resize is not supported (requires a new RTJ).

- **ResourceClaimTemplates survive resize.**  Templates are owned by the RTJ
  (not the child JobSet) and are created once per RTJ lifetime.  They are
  reused across run attempts, including resize-triggered relaunches.

- **Checkpoint device-profile compatibility applies.**  On relaunch after
  resize, the controller validates that the selected checkpoint's device
  profile fingerprint matches the current spec.  This is the same fail-closed
  check from Phase 8.

- **In-place shrink with DRA.**  Surplus pods release their allocated
  ResourceClaims when they terminate.  The DRA driver deallocates the
  devices.  However, the RTJ controller does not proactively manage claim
  cleanup during in-place shrink — it relies on garbage collection of pods
  and their bound claims.  Proactive claim release is a stretch goal (G-8).

---

## MultiKueue-backed RTJs during resize

When `spec.managedBy: kueue.x-k8s.io/multikueue`:

- **Manager propagates `spec.elasticity` to the worker-side RTJ** via the
  MultiKueue adapter.  The adapter's existing spec-sync mechanism handles
  the new field.

- **Resize execution happens on the worker.**  The worker-side RTJ controller
  performs the same resize logic as in single-cluster mode.

- **Manager mirrors `status.elasticity`.**  The adapter's status-mirror
  mechanism reads the worker-side status and writes it to the manager-side
  RTJ status.

- **In-place shrink reclaimablePods on worker.**  The worker writes
  `reclaimablePods` to the worker-side Workload.  Whether the manager-side
  Workload reflects this is an open question (OQ-3) — the MultiKueue
  adapter may not mirror Workload status fields today.

- **Checkpoint-and-relaunch on worker.**  The suspend/mutate/re-admit cycle
  happens on the worker cluster against the worker-side Kueue.  The manager
  observes the transition via status mirroring.
