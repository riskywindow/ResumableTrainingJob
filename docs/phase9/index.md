# Phase 9 — Hybrid Elastic RTJ: Index

> **Status:** Design locked — implementation not started.

## Phase identity

| Field | Value |
|---|---|
| Phase number | 9 |
| Title | Hybrid Elastic RTJ |
| Depends on | Phases 0–8 (all invariants preserved) |
| Primary capability | Manual target-based worker-count resize with hybrid in-place / checkpoint-and-relaunch execution |
| Quota mechanism | `Workload.status.reclaimablePods` for surplus release |
| Trigger model | Manual `spec.elasticity.targetWorkerCount` patch (no metric-driven autoscaler) |

---

## Scope summary

Phase 9 introduces **deterministic, operator-initiated elasticity** for
ResumableTrainingJob.  An operator (or higher-level automation) sets a target
worker count on the RTJ.  The controller evaluates whether the delta can be
applied in-place (fast path) or requires a checkpoint-and-relaunch cycle
(safe fallback), then drives the transition.

### What ships

1. **`spec.elasticity` API extension** — `targetWorkerCount`, `mode`
   (Disabled / Manual), `inPlaceShrinkPolicy` (IfSupported / Never).
2. **In-place shrink** (fast path) — when the runtime and JobSet controller
   support live replica reduction, the operator patches the child JobSet
   directly and updates `Workload.status.reclaimablePods` so Kueue reclaims
   freed quota.
3. **Checkpoint-and-relaunch shrink** (fallback) — when in-place shrink is
   not supported, the controller initiates a graceful yield, writes a
   checkpoint, and relaunches at the smaller target size.
4. **Checkpoint-and-relaunch grow** — scale-up always requires a new Kueue
   admission at the larger size; the controller checkpoints, suspends the
   Workload, mutates the PodSet counts, and waits for re-admission.
5. **`Workload.status.reclaimablePods` integration** — surplus pods are
   declared reclaimable so Kueue can release quota without waiting for the
   full checkpoint cycle.
6. **Phase 6 / 7 / 8 backward compatibility** — multi-cluster dispatch,
   launch gating, and DRA device requests continue to work unchanged when
   elasticity is disabled or during resize transitions.

### What does NOT ship

- Automatic metric-driven autoscaling (HPA, custom metrics, etc.).
- Native Kueue Workload Slices as the core resize primitive.
- Custom scheduler or quota engine.
- In-place grow (requires upstream Kueue Workload resize — tracked as
  future work).

---

## Invariants (carried forward from Phases 0–8, extended)

| ID | Invariant | Origin |
|---|---|---|
| I-1 | RTJ is the only Kueue-managed admission object | Phase 2 |
| I-2 | Child JobSets are plain runtime resources | Phase 2 |
| I-3 | Kueue is sole authority for admission, preemption, and quota | Phase 2 |
| I-4 | RTJ operator is lifecycle owner for launch, yield, resize, checkpoint, and runtime rendering | Phase 2 (extended Phase 9) |
| I-5 | Checkpoint compatibility is fail-closed | Phase 0 |
| I-6 | Manager/worker split is transparent to single-cluster use | Phase 6 |
| I-7 | DRA disabled ≡ Phase 7 behavior | Phase 8 |
| I-8 | Resume uses latest-compatible-complete checkpoint | Phase 0 |
| **I-9** | **Elasticity disabled ≡ Phase 8 behavior** | **Phase 9 (new)** |
| **I-10** | **Scale-up always goes through checkpoint-and-relaunch** | **Phase 9 (new)** |
| **I-11** | **reclaimablePods is the only quota-release signal for in-place shrink** | **Phase 9 (new)** |

---

## Feature matrix

| Capability | Disabled | In-place shrink | C&R shrink | C&R grow |
|---|---|---|---|---|
| spec.elasticity.mode | Disabled | Manual | Manual | Manual |
| Runtime support required | — | Yes | No | No |
| Quota release via reclaimablePods | — | Yes | N/A (Workload suspended) | N/A |
| New Kueue admission required | — | No | Yes (relaunch) | Yes (larger PodSet) |
| Checkpoint written | — | No | Yes | Yes |
| DRA compatible | Yes | Yes (no device change) | Yes | Yes |
| MultiKueue compatible | Yes | Deferred (OQ-3) | Yes | Yes |

---

## API extension sketch

```yaml
apiVersion: training.io/v1alpha1
kind: ResumableTrainingJob
spec:
  elasticity:
    mode: Manual          # Disabled | Manual
    targetWorkerCount: 4  # desired worker count (must be ≥ parallelism.minCount)
    inPlaceShrinkPolicy: IfSupported  # IfSupported | Never
status:
  elasticity:
    currentWorkerCount: 8
    targetWorkerCount: 4
    resizePhase: InProgress  # Idle | InProgress | Blocked
    resizePath: InPlace      # InPlace | CheckpointAndRelaunch
    lastResizeTimestamp: "2026-04-05T00:00:00Z"
    reclaimablePods:
      - name: workers
        count: 4
```

---

## Documents

- [goals.md](goals.md) — must-ship, stretch, non-goals
- [architecture.md](architecture.md) — component & sequence diagrams
- [migration-from-phase8.md](migration-from-phase8.md) — backward compatibility
- [open-questions.md](open-questions.md) — unresolved design questions
- [adr/0001-hybrid-elastic-rtj.md](adr/0001-hybrid-elastic-rtj.md) — decision record
- [session-handoff.md](session-handoff.md) — session continuity log
