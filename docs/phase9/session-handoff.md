# Phase 9 — Session Handoff

## Session: 2026-04-05 (Design Lock)

### Decisions made

1. **Phase 9 scope locked**: Hybrid elastic RTJ with manual target-based
   resize, in-place shrink (when runtime supports it), checkpoint-and-relaunch
   for grow and unsupported shrink, reclaimablePods quota release.

2. **RTJ remains the only Kueue-managed object** — no change to the
   fundamental ownership model.

3. **Native Kueue Workload Slices are NOT the core path** — resize uses
   suspend/mutate/re-admit and reclaimablePods.

4. **In-place shrink is gated on runtime annotation**
   (`training.io/supports-in-place-shrink: "true"`).  Conservative default:
   absent annotation → checkpoint-and-relaunch fallback.

5. **Scale-up always requires checkpoint-and-relaunch** — in-place grow is
   out of scope (requires upstream Kueue Workload resize support).

6. **Workload PodSet mutation uses suspend/mutate/re-admit cycle** — no
   in-flight Workload spec mutation required.

7. **reclaimablePods is the quota release mechanism** for in-place shrink.
   Kueue reads it and releases quota.

8. **Phase 6/7/8 backward compatibility preserved** — elasticity disabled is
   identical to Phase 8.

9. **Automatic metric-driven autoscaling is out of scope** — manual target
   changes only.

### Files created

| File | Purpose |
|---|---|
| `docs/phase9/README.md` | Phase overview and quick orientation |
| `docs/phase9/index.md` | Scope, invariants, feature matrix, API sketch |
| `docs/phase9/goals.md` | Must-ship (P0), stretch (P1), non-goals |
| `docs/phase9/architecture.md` | Component diagram, decision tree, 4 sequence diagrams |
| `docs/phase9/migration-from-phase8.md` | What stays, what changes, reclaimablePods, DRA/MultiKueue compat |
| `docs/phase9/open-questions.md` | 8 open questions with status and leanings |
| `docs/phase9/session-handoff.md` | This file |
| `docs/phase9/adr/0001-hybrid-elastic-rtj.md` | Architectural decision record |

### Files changed

None (design-only session; no code changes).

### Tests run

None (design-only session).

### Open issues

See [open-questions.md](open-questions.md) for the full list.  Key blockers
for implementation:

- **OQ-1**: JobSet live replica reduction behavior unknown — mitigated by
  annotation gate.
- **OQ-3**: MultiKueue reclaimablePods mirror — deferred to stretch.
- **OQ-4**: Preemption/resize race — must be handled in implementation.
- **OQ-6**: Grow admission failure — MVP waits indefinitely.

---

## Session: 2026-04-05 (API Implementation)

### Decisions made

1. **`spec.elasticity` is a separate top-level section** — not folded into
   `spec.parallelism`.  Parallelism controls admission-time shape; elasticity
   controls runtime resize.

2. **`ElasticityMode` has exactly two values**: `Disabled` and `Manual`.
   No speculative `Auto` placeholder.

3. **`targetWorkerCount` is bounded by Phase 3 fields**: lower bound is
   `parallelism.minCount` (or 1), upper bound is `parallelism.preferredCount`
   (or `identity.worldSize`).

4. **`reclaimMode` is narrow** with a single value (`ReclaimablePods`) to
   document the mechanism and provide an extension point.

5. **Elasticity mode changes require suspension** — same pattern as queue name.

6. **`allowWorldSizeChange: true` is required for Manual mode** — every resize
   changes effective world size.

7. **`status.elasticity` is a flat struct** with resize state, path, reason,
   timestamps, checkpoint reference, reclaimable counts, and runtime capability.

### Files created

| File | Purpose |
|---|---|
| `docs/phase9/api.md` | API reference: spec/status fields, field mapping, authorship, how-to |
| `docs/phase9/adr/0002-elasticity-api.md` | ADR for API design decisions |

### Files changed

| File | Changes |
|---|---|
| `api/v1alpha1/resumabletrainingjob_types.go` | Added ElasticitySpec, ElasticityStatus, 6 new enums, 5 new constants, validation, defaulting, helper methods |
| `api/v1alpha1/resumabletrainingjob_webhook.go` | Added elasticity mode immutability check on update (requires suspension) |
| `api/v1alpha1/resumabletrainingjob_webhook_test.go` | Added 25 new tests for Phase 9: backward compat, defaulting, validation, status preservation, deep copy, helper functions |
| `api/v1alpha1/zz_generated.deepcopy.go` | Added DeepCopyInto for ElasticitySpec and ElasticityStatus; updated Spec/Status DeepCopyInto |
| `config/crd/bases/training.checkpoint.example.io_resumabletrainingjobs.yaml` | Added spec.elasticity and status.elasticity sections |

### Tests run

- `go test ./api/v1alpha1/ -count=1` — **all pass** (existing + 25 new)
- `go build ./...` — **clean**

### New types added

| Type | Kind | Values |
|---|---|---|
| `ElasticityMode` | Enum | `Disabled`, `Manual` |
| `InPlaceShrinkPolicy` | Enum | `IfSupported`, `Never` |
| `ReclaimMode` | Enum | `ReclaimablePods` |
| `ResizeState` | Enum | `Idle`, `Pending`, `InProgress`, `Blocked`, `Completed`, `Failed` |
| `ResizePath` | Enum | `InPlace`, `CheckpointAndRelaunch` |
| `ExecutionMode` | Enum | `Fixed`, `Elastic` |
| `ElasticitySpec` | Struct | mode, targetWorkerCount, inPlaceShrinkPolicy, reclaimMode |
| `ElasticityStatus` | Struct | 15 fields covering resize lifecycle observability |

### New helper methods

| Method | Returns | Description |
|---|---|---|
| `IsElasticityEnabled()` | `bool` | True when elasticity mode != Disabled |
| `EffectiveTargetWorkerCount()` | `int32` | Target from elasticity or fallback to preferred count |
| `EffectiveElasticityMinCount()` | `int32` | Lower bound for target (minCount or 1) |

### Validation rules added

| Rule | Error |
|---|---|
| Manual mode requires `allowWorldSizeChange: true` | Forbidden on `spec.elasticity.mode` |
| `targetWorkerCount` must not be set when mode is Disabled | Forbidden on `targetWorkerCount` |
| `targetWorkerCount < effectiveMinCount` | Invalid on `targetWorkerCount` |
| `targetWorkerCount > effectivePreferredCount` | Invalid on `targetWorkerCount` |
| Mode change while unsuspended | Forbidden on `spec.elasticity.mode` |

### What was NOT done (deliberately)

- **No controller logic**: Resize decision logic, reclaimablePods write/clear,
  Workload PodSet mutation — all deferred to next session.
- **No automatic autoscaling fields**: No `Auto` mode, no metric sources.
- **No Workload Slices**: Not the core resize path.
- **No in-place grow**: Requires upstream Kueue support.

### Recommended next prompt

See Session: 2026-04-05 (Runtime Elasticity Protocol) below.

---

## Session: 2026-04-05 (Runtime Elasticity Protocol)

### Decisions made

1. **Elasticity protocol is trigger-based**: a single trigger (target ≠ current
   worker count) produces exactly one of three outcomes (no resize, in-place
   shrink success, checkpoint-and-relaunch fallback).

2. **`evaluate_resize()` is the core decision function**: a pure function from
   `ElasticConfig → ResizeOutcome`.  Deterministic, no side effects.

3. **Control file carries `targetWorkerCount`**: highest-priority source for
   target, allows runtime mutation without relaunch.  Backward compatible —
   Phase 1-8 control files without the field parse normally.

4. **Manifest resize metadata is optional**: five new fields, all `None` when
   no resize is active.  Phase 3-8 manifests decode cleanly.

5. **Resize signal file is the runtime→controller communication channel**:
   `resize-signal.json` written to a configurable directory.  Controller reads
   it to determine path and checkpoint reference.

6. **DDP fixture always reports `supports_in_place_shrink=false`**: DDP
   requires process group reinitialization.  Every resize produces a checkpoint.

7. **Fixture adds deterministic e2e knobs**: `--warmup-steps`,
   `--resize-check-every`, `--shrink-barrier-timeout`, `--resize-signal-dir`.

### Files created

| File | Purpose |
|---|---|
| `sdk/python/yield_sdk/elastic.py` | Core elasticity protocol: ElasticConfig, ResizeDirection, evaluate_resize(), signal I/O |
| `sdk/python/tests/test_elastic.py` | 27 tests: config detection, resize evaluation, serialization, signals, backward compat |
| `docs/phase9/runtime-elasticity.md` | Runtime elasticity protocol design document |

### Files changed

| File | Changes |
|---|---|
| `sdk/python/yield_sdk/control.py` | Added `target_worker_count` and `resize_request_id` to ControlRecord; parser extracts them from control file |
| `sdk/python/yield_sdk/runtime.py` | Added `elasticity_mode`, `target_worker_count`, `supports_in_place_shrink` fields; `from_env()` reads new env vars |
| `sdk/python/yield_sdk/manifest.py` | Added 5 Phase 9 fields: `resize_active_worker_count`, `resize_target_worker_count`, `resize_direction`, `resize_reason`, `resize_in_place_shrink_supported`; updated `to_dict()`, `from_dict()` |
| `sdk/python/yield_sdk/__init__.py` | Re-exported all new elastic module types |
| `sdk/python/tests/test_control.py` | Added 7 Phase 9 tests: target parsing, snake_case, backward compat, metadata isolation |
| `sdk/python/tests/test_manifest.py` | Added 7 Phase 9 tests: resize field round-trip, serialization, omission, cross-phase coexistence |
| `sdk/python/tests/test_resume.py` | Added 3 backward compat tests: non-elastic checkpoint has no resize fields, restore works, RuntimeConfig defaults |
| `fixtures/pytorch_ddp_counter/train.py` | Added elastic resize loop: `_build_elastic_config()`, resize detection, checkpoint with resize metadata, signal file writing, new CLI args |
| `fixtures/pytorch_ddp_counter/README.md` | Updated for Phase 9: elastic resize docs, knobs table, example commands |

### Tests run

- `python -m pytest sdk/python/tests/ -v` — **82 passed**, 0 failures
  - `test_elastic.py`: 27 passed
  - `test_control.py`: 10 passed
  - `test_manifest.py`: 21 passed
  - `test_resume.py`: 10 passed
  - `test_checkpoint.py`: 12 passed
  - `test_storage.py`: 2 passed
- `go build ./...` — **clean**

### New SDK types

| Type | Module | Kind | Description |
|---|---|---|---|
| `ElasticityMode` | `elastic` | Enum | `Disabled`, `Manual` |
| `ResizeDirection` | `elastic` | Enum | `None`, `Shrink`, `Grow` |
| `ShrinkOutcome` | `elastic` | Enum | `Success`, `FallbackRequired`, `NotRequested` |
| `ElasticConfig` | `elastic` | Frozen dataclass | Runtime-visible elasticity configuration |
| `ResizeOutcome` | `elastic` | Frozen dataclass | Deterministic resize evaluation result |
| `ResizeCheckpointContext` | `elastic` | Frozen dataclass | Manifest metadata for resize checkpoints |

### New environment variables

| Variable | Default | Description |
|---|---|---|
| `YIELD_SDK_ELASTICITY_MODE` | `Disabled` | `Disabled` or `Manual` |
| `YIELD_SDK_TARGET_WORKER_COUNT` | current world size | Target worker count |
| `YIELD_SDK_SUPPORTS_IN_PLACE_SHRINK` | `false` | Runtime annotation gate |
| `YIELD_SDK_SHRINK_BARRIER_TIMEOUT` | `30.0` | Barrier timeout in seconds |
| `YIELD_SDK_RESIZE_SIGNAL_DIR` | unset | Directory for resize signal files |

### What was NOT done (deliberately)

- **No controller-side reclaim logic**: reclaimablePods lifecycle, Workload
  PodSet mutation, suspend/mutate/re-admit cycle — all deferred.
- **No automatic autoscaling**: No `Auto` mode, no metric sources.
- **No in-place grow**: Requires upstream Kueue support.
- **No Workload Slices**: Not the core resize path.

### Recommended next prompt

```
You are working on Phase 9 only for the checkpoint-native preemption controller repo.

Mission: Implement Phase 9 resize controller logic.

Read docs/phase9/index.md, docs/phase9/architecture.md, docs/phase9/api.md,
docs/phase9/runtime-elasticity.md, and docs/phase9/open-questions.md.

Step 1: Add the resize decision logic to the controller
(internal/controller/resumabletrainingjob_controller.go):
- Detect targetWorkerCount delta from spec.elasticity vs status.elasticity.
- Choose resize path (in-place shrink vs. checkpoint-and-relaunch).
- Implement in-place shrink: patch JobSet replicas, write reclaimablePods to
  Workload.status, update status.elasticity.
- Implement checkpoint-and-relaunch: pause → checkpoint → suspend Workload →
  mutate PodSets → re-admit → relaunch.

Step 2: Wire reclaimablePods lifecycle:
- Write reclaimablePods entry after patching JobSet replicas down.
- Wait for surplus pods to terminate.
- Clear reclaimablePods once resize is confirmed complete.

Step 3: Inject YIELD_SDK_ELASTICITY_MODE, YIELD_SDK_TARGET_WORKER_COUNT,
YIELD_SDK_SUPPORTS_IN_PLACE_SHRINK, and YIELD_SDK_RESIZE_SIGNAL_DIR into
the JobSet pod template env when elasticity is enabled.

Step 4: Write unit tests for:
- Resize path decision logic (in-place vs C&R, shrink vs grow).
- reclaimablePods write/clear lifecycle.
- Checkpoint-and-relaunch state transitions.
- Elasticity disabled ≡ Phase 8 behavior.
- Target within/outside bounds.
- Preemption/resize race handling (OQ-4).

Step 5: Update docs/phase9/session-handoff.md.

Hard boundaries:
- Preserve Phase 8 behavior when elasticity is disabled.
- Do NOT add native Kueue Workload Slices as core path.
- Do NOT add automatic metric-driven autoscaling.
- Do NOT break MultiKueue manager/worker ownership.
- Use the existing spec.elasticity and status.elasticity API surface.
- Use the existing runtime-side protocol from sdk/python/yield_sdk/elastic.py.
```
