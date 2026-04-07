# Phase 9 — Hybrid Elastic RTJ: Index

> **Status:** Implementation complete — signed off 2026-04-06.

## Phase identity

| Field | Value |
|---|---|
| Phase number | 9 |
| Title | Hybrid Elastic RTJ |
| Depends on | Phases 0-8 (all invariants preserved) |
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

1. **`spec.elasticity` API extension** -- `targetWorkerCount`, `mode`
   (Disabled / Manual), `inPlaceShrinkPolicy` (IfSupported / Never),
   `reclaimMode` (ReclaimablePods).
2. **In-place shrink** (fast path) -- when the runtime advertises in-place
   shrink support, the controller publishes `Workload.status.reclaimablePods`
   via server-side apply so Kueue reclaims freed quota. The RTJ remains
   Running with no checkpoint or eviction.
3. **Checkpoint-and-relaunch shrink** (fallback) -- when in-place shrink is
   not supported or the policy says `Never`, the controller initiates a
   graceful yield, writes a checkpoint, and relaunches at the smaller target
   size via DCP resharding.
4. **Checkpoint-and-relaunch grow** -- scale-up always requires a new Kueue
   admission at the larger size; the controller checkpoints, suspends the
   Workload, and waits for re-admission at the new worker count.
5. **`Workload.status.reclaimablePods` integration** -- surplus pods are
   declared reclaimable so Kueue can release quota without waiting for the
   full checkpoint cycle. SSA field manager `rtj-elastic-reclaim` avoids
   clobbering Kueue-owned status fields.
6. **Phase 6 / 7 / 8 backward compatibility** -- multi-cluster dispatch,
   launch gating, and DRA device requests continue to work unchanged when
   elasticity is disabled or during resize transitions.

### What does NOT ship

- Automatic metric-driven autoscaling (HPA, custom metrics, etc.).
- Native Kueue Workload Slices as the core resize primitive.
- Custom scheduler or quota engine.
- In-place grow (requires upstream Kueue Workload resize support).

---

## Invariants (carried forward from Phases 0-8, extended)

| ID | Invariant | Origin |
|---|---|---|
| I-1 | RTJ is the only Kueue-managed admission object | Phase 2 |
| I-2 | Child JobSets are plain runtime resources | Phase 2 |
| I-3 | Kueue is sole authority for admission, preemption, and quota | Phase 2 |
| I-4 | RTJ operator is lifecycle owner for launch, yield, resize, checkpoint, and runtime rendering | Phase 2 (extended Phase 9) |
| I-5 | Checkpoint compatibility is fail-closed | Phase 0 |
| I-6 | Manager/worker split is transparent to single-cluster use | Phase 6 |
| I-7 | DRA disabled = Phase 7 behavior | Phase 8 |
| I-8 | Resume uses latest-compatible-complete checkpoint | Phase 0 |
| **I-9** | **Elasticity disabled = Phase 8 behavior** | **Phase 9** |
| **I-10** | **Scale-up always goes through checkpoint-and-relaunch** | **Phase 9** |
| **I-11** | **reclaimablePods is the only quota-release signal for in-place shrink** | **Phase 9** |
| **I-12** | **Manager never evaluates elastic plans for remote RTJs** | **Phase 9** |
| **I-13** | **reclaimablePods published only on executing worker-side Workload** | **Phase 9** |
| **I-14** | **Manager never creates reclaim helper state for remote RTJs** | **Phase 9** |

---

## Feature matrix

| Capability | Disabled | In-place shrink | C&R shrink | C&R grow |
|---|---|---|---|---|
| spec.elasticity.mode | Disabled | Manual | Manual | Manual |
| Runtime support required | -- | Yes | No | No |
| Quota release via reclaimablePods | -- | Yes | N/A (Workload suspended) | N/A |
| New Kueue admission required | -- | No | Yes (relaunch) | Yes (larger PodSet) |
| Checkpoint written | -- | No | Yes | Yes |
| DRA compatible | Yes | Yes (no device change) | Yes | Yes |
| MultiKueue compatible | Yes | Deferred (OQ-3) | Yes | Yes |

---

## API extension

```yaml
apiVersion: training.checkpoint.example.io/v1alpha1
kind: ResumableTrainingJob
spec:
  elasticity:
    mode: Manual                    # Disabled | Manual
    targetWorkerCount: 4            # desired worker count (>= parallelism.minCount)
    inPlaceShrinkPolicy: IfSupported  # IfSupported | Never
    reclaimMode: ReclaimablePods    # ReclaimablePods (only value)
status:
  elasticity:
    resizeState: Idle               # Idle | Pending | InProgress | Blocked | Completed | Failed
    resizePath: ""                  # InPlace | CheckpointAndRelaunch
    resizeReason: ""                # machine-readable reason code
    lastResizeEvent: ""             # human-readable description
    admittedWorkerCount: 4          # from Workload admission
    currentWorkerCount: 4           # running pods
    targetWorkerCount: 4            # from spec
    reclaimablePodsPublished: false  # whether reclaimablePods SSA patch is active
    inPlaceShrinkSupported: false   # runtime capability advertisement
```

---

## Resize path decision tree

```
targetWorkerCount changed?
  +-- target == current -> no-op (NoResize)
  +-- target < current (SHRINK)
  |   +-- inPlaceShrinkPolicy == Never? -> checkpoint-and-relaunch shrink
  |   +-- runtime supports in-place?
  |   |   +-- YES -> in-place shrink (reclaimablePods)
  |   |   +-- NO  -> checkpoint-and-relaunch shrink
  |   +-- target < minCount? -> REJECT (webhook validation)
  +-- target > current (GROW) -> checkpoint-and-relaunch grow
  +-- preemption in progress? -> ResizeBlocked (target preserved for later)
```

---

## Documents

### Design
- [goals.md](goals.md) -- must-ship, stretch, non-goals
- [architecture.md](architecture.md) -- component and sequence diagrams
- [migration-from-phase8.md](migration-from-phase8.md) -- backward compatibility
- [open-questions.md](open-questions.md) -- design questions and resolutions
- [adr/0001-hybrid-elastic-rtj.md](adr/0001-hybrid-elastic-rtj.md) -- hybrid elastic ADR
- [adr/0002-elasticity-api.md](adr/0002-elasticity-api.md) -- API design ADR

### Implementation
- [api.md](api.md) -- API reference: spec/status fields, validation, authorship
- [runtime-elasticity.md](runtime-elasticity.md) -- SDK elasticity protocol
- [elastic-planning.md](elastic-planning.md) -- pure-function planning model
- [resize-execution.md](resize-execution.md) -- execution engine design
- [multicluster-compatibility.md](multicluster-compatibility.md) -- manager/worker behavior
- [dev-environment.md](dev-environment.md) -- Phase 9 dev profile

### Operations
- [demo.md](demo.md) -- end-to-end demo command sequence
- [operations.md](operations.md) -- inspection procedures, metrics reference
- [troubleshooting.md](troubleshooting.md) -- 6 failure scenarios with diagnostics
- [e2e.md](e2e.md) -- E2E test documentation

### Review
- [review/consistency-audit.md](review/consistency-audit.md) -- invariant and design compliance
- [review/gaps.md](review/gaps.md) -- gaps analysis with severity and recommendations
- [PHASE9_SIGNOFF.md](PHASE9_SIGNOFF.md) -- signoff summary

### Session
- [session-handoff.md](session-handoff.md) -- session continuity log
