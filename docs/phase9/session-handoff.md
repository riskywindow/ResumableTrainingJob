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

See Session: 2026-04-05 (Elastic Planning Model) below.

---

## Session: 2026-04-05 (Elastic Planning Model)

### Decisions made

1. **Elastic planning is a pure-function model**: `EvaluatePlan()` takes a
   `PlanInput` snapshot and returns a deterministic `PlanOutput`.  No side
   effects, no Kubernetes client dependencies.

2. **Seven discrete plan kinds**: `NoResize`, `ShrinkInPlace`,
   `ShrinkViaRelaunch`, `GrowViaRelaunch`, `ResizeBlocked`,
   `ResizeInProgress`, `ReclaimPublished`.

3. **SSA with dedicated field manager for reclaimablePods**: Field manager
   `rtj-elastic-reclaim` writes only `reclaimablePods` entries via
   server-side apply.  This avoids clobbering Kueue-owned status fields
   (admission, conditions, admissionChecks, requeueState).

4. **SSA chosen over merge-patch**: Eliminates read-modify-write races with
   Kueue's concurrent status writes.  `reclaimablePods` has `+listType=map`
   with `+listMapKey=name`, so SSA treats each PodSet entry as independently
   owned.

5. **Preemption/resize coalesce (OQ-4 resolved)**: When preemption is in
   progress, the planner returns `ResizeBlocked`.  The resize target is
   preserved in spec and evaluated after re-admission.

6. **MaxWorkerCount=0 means unbounded**: Consistent with Go zero-value
   semantics.  Allows grow targets beyond preferred count when upper bound
   is not explicitly set.

7. **NeedsReclaimUpdate() idempotency guard**: Prevents unnecessary SSA
   patches when the desired state matches the current Workload state.

8. **Controller integration via buildElasticPlanInput()**: Extracts all
   plan inputs from RTJ spec, status, and Workload admission state.
   syncElasticityStatus() writes plan outputs back to status.elasticity.

### Files created

| File | Purpose |
|---|---|
| `internal/elastic/types.go` | PlanKind enum, PlanInput, PlanOutput, ReclaimDelta types |
| `internal/elastic/plan.go` | EvaluatePlan() pure function with decision tree |
| `internal/elastic/plan_test.go` | 22 tests: shrink, grow, no-op, blocked, idempotency |
| `internal/elastic/reclaim.go` | ComputeReclaimDelta, BuildReclaimablePods, NeedsReclaimUpdate |
| `internal/elastic/reclaim_test.go` | 13 tests: delta calculation, build, clear, idempotency |
| `internal/controller/elastic_plan.go` | buildElasticPlanInput, evaluateElasticPlan, syncElasticityStatus |
| `internal/controller/elastic_plan_test.go` | 17 tests: input building, plan evaluation, status sync |
| `internal/controller/workload_status_patch.go` | SSA patch for Workload.status.reclaimablePods |
| `internal/controller/workload_status_patch_test.go` | 10 tests: patch safety, field manager, idempotency |
| `docs/phase9/elastic-planning.md` | Planning model design document |

### Files changed

| File | Changes |
|---|---|
| `docs/phase9/session-handoff.md` | Added this session entry |

### Tests run

- `go test ./internal/elastic/... -v -count=1` — **35 passed** (22 plan + 13 reclaim)
- `go test ./internal/controller/... -run 'Elastic|SSA|Reclaim|Field|PlanKind|Sync' -v -count=1` — **27 passed** (17 elastic_plan + 10 workload_status_patch)
- `go test ./internal/... -count=1` — **all pass** (no regressions)
- `go build ./...` — **clean**

### New Go types

| Type | Package | Kind | Description |
|---|---|---|---|
| `PlanKind` | `elastic` | Enum (7 values) | Discrete plan action |
| `PlanInput` | `elastic` | Struct (16 fields) | Read-only planner input |
| `PlanOutput` | `elastic` | Struct (7 fields) | Deterministic plan result |
| `ReclaimDelta` | `elastic` | Struct (2 fields) | reclaimablePods patch descriptor |
| `ElasticPlanResult` | `controller` | Struct | Controller plan evaluation result |
| `WorkloadReclaimPatchResult` | `controller` | Struct | SSA patch outcome |

### New functions

| Function | Package | Description |
|---|---|---|
| `EvaluatePlan()` | `elastic` | Core pure-function planner |
| `ComputeReclaimDelta()` | `elastic` | Plan → ReclaimDelta |
| `BuildReclaimablePods()` | `elastic` | ReclaimDelta → Kueue ReclaimablePod slice |
| `ClearReclaimablePods()` | `elastic` | Returns nil (clear signal) |
| `ReclaimDeltaFromExisting()` | `elastic` | Extract delta from existing Workload |
| `NeedsReclaimUpdate()` | `elastic` | Idempotency guard |
| `buildElasticPlanInput()` | `controller` | RTJ → PlanInput |
| `evaluateElasticPlan()` | `controller` | RTJ × Workload state → ElasticPlanResult |
| `syncElasticityStatus()` | `controller` | PlanOutput → status.elasticity fields |
| `patchWorkloadReclaimablePods()` | `controller` | SSA patch reclaimablePods |
| `clearWorkloadReclaimablePods()` | `controller` | Convenience clear wrapper |
| `buildReclaimablePodsSSAPatch()` | `controller` | JSON SSA patch builder |
| `planKindToResizeState()` | `controller` | PlanKind → ResizeState mapping |
| `planKindToResizePath()` | `controller` | PlanKind → ResizePath mapping |

### Open questions resolved

| OQ | Resolution |
|---|---|
| OQ-4 (Preemption/resize race) | **Resolved**: Planner returns ResizeBlocked when preemption is in progress. Target preserved in spec, evaluated after re-admission. |

### What was NOT done (deliberately)

- **No reconcile loop wiring**: The planning model and patch strategy are
  implemented but not yet called from the main Reconcile() method.
  Wiring in the elastic plan evaluation and reclaimablePods lifecycle is
  the next step.
- **No JobSet replica patching**: The in-place shrink execution (patch
  child JobSet replicas) is not yet implemented.
- **No checkpoint-and-relaunch orchestration**: The C&R execution path
  (pause → checkpoint → suspend Workload → mutate PodSets → re-admit →
  relaunch) is not yet implemented.
- **No environment variable injection**: YIELD_SDK_* env vars are not
  yet injected into JobSet pod templates.
- **No state machine transitions**: The resize state machine (Pending →
  InProgress → Completed/Failed) is not yet wired.

### Recommended next prompt

See Session: 2026-04-05 (Resize Execution) below.

---

## Session: 2026-04-05 (Resize Execution)

### Decisions made

1. **Resize execution is integrated into the main reconcile loop**: When an
   RTJ is Running with an active JobSet and elasticity is enabled, the
   controller evaluates the elastic plan and executes the appropriate
   resize action on every reconcile.

2. **In-place shrink executes via SSA reclaimablePods patch**: The execution
   engine computes the reclaim delta, builds the Kueue ReclaimablePod slice,
   applies the SSA patch, and marks `reclaimablePodsPublished=true`. The
   RTJ remains Running and the Workload remains admitted.

3. **Grow and shrink-fallback use checkpoint-and-relaunch**: The execution
   engine marks `resizeState=InProgress`, sets the `ResizeCheckpointing`
   condition, and signals `TriggerStopFlow`. The main reconciler enters
   the existing drain flow using `stopSourceResize` as the stop source.

4. **Resize stop source is a new stop source variant**: `stopSourceResize`
   uses the existing `reconcileStopFlow()` machinery, keeping the drain,
   checkpoint, and cleanup flow identical to manual pause and Kueue
   preemption flows.

5. **Resize completion is detected on relaunch**: When the RTJ transitions
   from Restoring to Running and `isResizeTriggeredStop()` is true,
   `completeResizeAfterRelaunch()` sets `resizeState=Completed` and clears
   all execution conditions.

6. **Seven resize conditions with mutual exclusion**: Exactly one resize
   condition is active at a time, progressing through: ResizePending ->
   ShrinkingInPlace/ShrinkReclaimPublished or ResizeCheckpointing ->
   RelaunchingForResize -> (cleared on completion).

7. **YIELD_SDK_TARGET_WORKER_COUNT env var injected**: The render path
   injects the elastic target worker count into all containers via the
   new `ElasticTargetWorkerCount` field on `RenderInput`.

8. **Launch gate result carries resize context**: `LaunchGateResult.ResizeRelaunch`
   is set when the launch is triggered by a resize flow, enabling the
   `RelaunchingForResize` condition on the launch/resume path.

### Files created

| File | Purpose |
|---|---|
| `internal/controller/elastic_execute.go` | Core resize execution engine: executeElasticPlan(), executeShrinkInPlace(), executeRelaunchResize(), clearStaleResizeState(), isResizeTriggeredStop(), completeResizeAfterRelaunch(), markResizeRelaunchingCondition() |
| `internal/controller/elastic_execute_test.go` | 25+ tests: condition lifecycle, shrink/grow execution, DRA coherency, idempotency, fallback |
| `docs/phase9/resize-execution.md` | Design document for resize execution |

### Files changed

| File | Changes |
|---|---|
| `internal/controller/resumabletrainingjob_controller.go` | Integrated elastic plan evaluation and execution into the active-JobSet reconcile path; added resize completion detection on relaunch |
| `internal/controller/status_helpers.go` | Added 7 resize condition types, 13 resize reason constants, condition setter/clearer helpers, syncResizeConditions(), clearAllResizeConditions() |
| `internal/controller/launch_gate.go` | Added ResizeRelaunch flag to LaunchGateResult; detect resize relaunch in evaluateLaunchGates() |
| `internal/controller/launch_plan.go` | Inject ElasticTargetWorkerCount into RenderInput |
| `internal/controller/resume_flow.go` | Set RelaunchingForResize condition on resize-triggered launches/resumes; inject ElasticTargetWorkerCount in non-plan render path |
| `internal/controller/suspend_flow.go` | Added stopSourceResize; added reconcileResizeStopFlow() |
| `internal/jobset/render.go` | Added ElasticTargetWorkerCount to RenderInput; inject YIELD_SDK_TARGET_WORKER_COUNT env var |
| `internal/jobset/names.go` | Added EnvTargetWorkerCount constant |
| `internal/jobset/render_test.go` | Added 4 Phase 9 render tests: target injection, zero omission, DRA coexistence, admission coexistence |
| `docs/phase9/session-handoff.md` | Added this session entry |

### Tests run

- `go test ./...` — **all pass** (no regressions across all packages)
- `go build ./...` — **clean**

### New functions

| Function | Package | Description |
|---|---|---|
| `executeElasticPlan()` | `controller` | Main resize execution dispatcher |
| `executeShrinkInPlace()` | `controller` | In-place shrink: SSA patch + status update |
| `executeRelaunchResize()` | `controller` | Checkpoint-and-relaunch: mark state + trigger stop |
| `clearStaleResizeState()` | `controller` | Clear leftover resize state on NoResize |
| `isResizeTriggeredStop()` | `controller` | Detect resize-triggered drain |
| `completeResizeAfterRelaunch()` | `controller` | Mark resize completed after relaunch |
| `markResizeRelaunchingCondition()` | `controller` | Transition from checkpointing to relaunching |
| `reconcileResizeStopFlow()` | `controller` | Resize-specific stop flow entry point |
| `syncResizeConditions()` | `controller` | Set/clear conditions based on plan kind |
| `clearAllResizeConditions()` | `controller` | Clear all 7 resize conditions |

### New condition types

| Condition | Description |
|---|---|
| `ResizePending` | Resize planned but not yet started |
| `ShrinkingInPlace` | In-place shrink executing |
| `ShrinkReclaimPublished` | reclaimablePods written to Workload |
| `ResizeCheckpointing` | Drain flow active for resize |
| `RelaunchingForResize` | Post-drain relaunch in progress |
| `ResizeBlocked` | Resize cannot proceed |
| `ResizeFailed` | Execution error |

### What was NOT done (deliberately)

- **No child JobSet replica patching**: In-place shrink uses reclaimablePods
  only. Direct replica mutation on the child JobSet is not needed because
  Kueue interprets reclaimablePods and the runtime handles worker removal.
- **No automatic reclaim completion detection**: The controller does not yet
  detect when surplus pods have terminated to clear reclaimablePods. This
  requires observing pod counts on the active JobSet.
- **No Workload PodSet spec mutation**: The grow path uses the existing
  suspend/re-admit cycle (new Workload for new admission), not in-flight
  Workload spec mutation.

### Recommended next prompt

```
You are working on Phase 9 only for the checkpoint-native preemption controller repo.

Mission: Implement reclaim completion detection and resize lifecycle finalization.

Read docs/phase9/resize-execution.md for the execution model.
Read internal/controller/elastic_execute.go for the current implementation.

Step 1: Detect in-place shrink completion:
- When the RTJ is Running and ReclaimPublished, observe the active JobSet's
  worker pod count.
- When the active pod count matches the target worker count, clear
  reclaimablePods on the Workload and mark the resize as Completed.

Step 2: Add elapsed time tracking for resize operations:
- Track resize start time and completion time.
- Add metrics for resize duration.

Step 3: Handle resize target changes during in-progress operations:
- If the user changes targetWorkerCount while a resize is in progress,
  determine whether to abort-and-restart or queue the new target.

Step 4: E2E resize scenario tests with the DDP fixture.
```

---

## Session: 2026-04-06 (Dev Environment Profile)

### Decisions made

1. **Single ClusterQueue is sufficient for dynamic reclaim**: reclaimablePods
   quota release is per-Workload within the same queue. No cohort or
   borrowing configuration is needed.

2. **Quota sized at 1250m CPU / 1280Mi memory**: Enough for one 4-worker RTJ
   (1000m) but not two. After shrink 4→2, released 500m admits a second
   2-worker RTJ. This creates a deterministic contention scenario.

3. **No special Kueue feature gates required**: reclaimablePods is part of
   the standard Workload Status API (Kueue v0.15.1+). SSA field ownership
   is a standard Kubernetes API server feature (GA since v1.22). The
   `rtj-elastic-reclaim` field manager works without configuration.

4. **Kueue config based on Phase 7 baseline** (with waitForPodsReady) but
   without DRA or provisioning additions. waitForPodsReady is useful for
   detecting resize-triggered relaunch failures.

5. **Three sample RTJs cover the feature matrix**:
   - Elastic shrink: 4 workers, shrink to 2 (demonstrates reclaimablePods)
   - Elastic grow: 2 workers, grow to 4 (demonstrates checkpoint-and-relaunch)
   - Non-elastic: 2 workers, no elasticity (backward compatibility)

6. **Fixture knobs threaded through manifests**: YIELD_SDK_ELASTICITY_MODE,
   YIELD_SDK_SUPPORTS_IN_PLACE_SHRINK, YIELD_SDK_RESIZE_SIGNAL_DIR, plus
   resize-signal emptyDir volume. Non-elastic sample correctly omits these.

7. **Smoke test covers 17+ checks**: CRD fields, Kueue config, queues,
   quota values, sample dry-run, fixture knobs, reclaimablePods schema
   availability, and non-elastic sample omission verification.

### Files created

| File | Purpose |
|---|---|
| `deploy/dev/phase9/kueue/controller_manager_config.phase9.yaml` | Kueue config: RTJ external framework + waitForPodsReady |
| `deploy/dev/phase9/queues/00-resource-flavor.yaml` | Phase 9 ResourceFlavor |
| `deploy/dev/phase9/queues/10-cluster-queue.yaml` | ClusterQueue with dynamic reclaim quota |
| `deploy/dev/phase9/queues/20-local-queue.yaml` | LocalQueue for phase9-cq |
| `deploy/dev/phase9/samples/rtj-elastic-shrink.yaml` | 4-worker elastic RTJ (shrink demo) |
| `deploy/dev/phase9/samples/rtj-elastic-grow.yaml` | 2-worker elastic RTJ (grow demo) |
| `deploy/dev/phase9/samples/rtj-non-elastic.yaml` | Fixed-size RTJ (backward compat) |
| `hack/dev/install-phase9-profile.sh` | Profile installation script |
| `hack/dev/phase9-profile.sh` | Profile wrapper |
| `hack/dev/phase9-smoke.sh` | Infrastructure smoke test (17+ checks) |
| `docs/phase9/dev-environment.md` | Dev environment documentation |

### Files changed

| File | Changes |
|---|---|
| `Makefile` | Added Phase 9 variables and targets: phase9-up/down/status/load-images/smoke/profile |
| `docs/phase9/session-handoff.md` | Added this session entry |

### Tests run

- `go build ./...` — **clean** (no code changes, manifest-only)
- Smoke test structure verified by inspection (17+ independent checks)

### Makefile targets added

| Target | Description |
|---|---|
| `make phase9-up` | Create kind cluster + base stack + Phase 9 profile |
| `make phase9-down` | Delete kind cluster |
| `make phase9-status` | Show queues, quota, workloads, RTJs |
| `make phase9-load-images` | Load images into kind |
| `make phase9-smoke` | Run 17+ infrastructure validation checks |
| `make phase9-profile` | Apply/re-apply Phase 9 profile |

### What was NOT done (deliberately)

- **No e2e test suite**: Deferred per task boundary. Infrastructure only.
- **No demo/inspect scripts**: Phase 9 demo scripts (submit, patch, inspect
  resize state) are a natural follow-up but not required for the profile.
- **No reclaim completion detection**: Controller-side reclaimablePods
  lifecycle (clear on pod termination) is the next implementation step.
- **No DRA integration in Phase 9 profile**: Phase 9 focuses on elastic
  resize. DRA can be layered on top if needed (Phase 8 profile provides
  the DRA baseline).

### Recommended next prompt

See Session: 2026-04-06 (E2E Test Coverage) below.

---

## Session: 2026-04-06 (E2E Test Coverage)

### Decisions made

1. **Three strong deterministic tests over many shallow ones**: Each test
   exercises one complete resize path end-to-end with explicit assertions
   at every lifecycle stage.

2. **In-place shrink tested via status fixture knob**: Since the production
   controller's runtime-detection mechanism for `inPlaceShrinkSupported` is
   not yet wired to the reconcile loop (reads from status circularly), the
   shrink/reclaim test pre-patches the RTJ status subresource to set
   `inPlaceShrinkSupported=true`. This is a legitimate fixture approach
   that tests the in-place shrink codepath end-to-end.

3. **Fallback test uses default DDP behavior**: `inPlaceShrinkSupported=false`
   (the zero-value default) demonstrates that the controller correctly
   falls back to checkpoint-and-relaunch when in-place is not available.

4. **RTJ is the only Kueue-managed object**: All tests assert child JobSets
   are plain runtime (Phase 2 invariant preserved).

5. **Manual patches as core trigger**: All resize triggers use explicit
   `kubectl patch` on spec (targetWorkerCount) and status (fixture knobs)
   rather than sleeps, timers, or metric-driven autoscaling.

### Files created

| File | Purpose |
|---|---|
| `test/e2e/phase9_helpers_test.go` | Phase 9 view types, env setup, wait/get/patch helpers |
| `test/e2e/elastic_shrink_reclaim_test.go` | `TestElasticShrinkDynamicReclaim` — in-place shrink + reclaimablePods + quota reclaim |
| `test/e2e/elastic_grow_relaunch_test.go` | `TestElasticGrowViaRelaunch` — grow via checkpoint-and-relaunch |
| `test/e2e/elastic_fallback_test.go` | `TestElasticFallbackShrinkViaRelaunch` — fallback shrink when in-place unsupported |
| `test/e2e/testdata/phase9/rtj-elastic-shrink-4w.yaml` | 4-worker RTJ for shrink/reclaim test |
| `test/e2e/testdata/phase9/rtj-elastic-queued-2w.yaml` | 2-worker RTJ (queued, admitted after reclaim) |
| `test/e2e/testdata/phase9/rtj-elastic-grow-2w.yaml` | 2-worker RTJ for grow test |
| `test/e2e/testdata/phase9/rtj-elastic-fallback-4w.yaml` | 4-worker RTJ for fallback test |
| `docs/phase9/e2e.md` | E2E test documentation: what each test proves, deferred items |

### Files changed

| File | Changes |
|---|---|
| `Makefile` | Added `e2e-phase9` target and `.PHONY` entry |
| `docs/phase9/session-handoff.md` | Added this session entry |

### Tests run

- `go vet ./test/e2e/...` — **clean** (all Phase 9 e2e files compile)
- `go build ./...` — **clean**

### New test functions

| Function | File | Description |
|---|---|---|
| `TestElasticShrinkDynamicReclaim` | `elastic_shrink_reclaim_test.go` | In-place shrink → reclaimablePods → RTJ B admitted |
| `TestElasticGrowViaRelaunch` | `elastic_grow_relaunch_test.go` | 2→4 workers via checkpoint-and-relaunch |
| `TestElasticFallbackShrinkViaRelaunch` | `elastic_fallback_test.go` | 4→2 workers via relaunch when in-place unsupported |

### New helper functions

| Function | File | Description |
|---|---|---|
| `setupPhase9Env()` | `phase9_helpers_test.go` | Phase 9 env setup (cluster, minio, operator) |
| `getPhase9RTJ()` | `phase9_helpers_test.go` | Get Phase 9 RTJ view |
| `waitForPhase9RTJState()` | `phase9_helpers_test.go` | Wait for RTJ predicate |
| `waitForPhase9Phase()` | `phase9_helpers_test.go` | Wait for specific phase |
| `getPhase9Workload()` | `phase9_helpers_test.go` | Get Workload with reclaimablePods |
| `findPhase9WorkloadOwnedBy()` | `phase9_helpers_test.go` | Find Workload by owner |
| `waitForPhase9WorkloadAdmitted()` | `phase9_helpers_test.go` | Wait for admitted Workload |
| `waitForPhase9WorkloadReclaimablePods()` | `phase9_helpers_test.go` | Wait for reclaimablePods |
| `cleanupPhase9RTJ()` | `phase9_helpers_test.go` | Cleanup RTJ and child JobSets |
| `hasPhase9Condition()` | `phase9_helpers_test.go` | Check condition presence |
| `findPhase9Condition()` | `phase9_helpers_test.go` | Find condition by type |
| `patchPhase9RTJStatus()` | `phase9_helpers_test.go` | Patch status subresource |
| `patchPhase9RTJSpec()` | `phase9_helpers_test.go` | Patch spec |

### Makefile targets added

| Target | Description |
|---|---|
| `make e2e-phase9` | Run Phase 9 e2e tests |

### What was NOT done (deliberately)

- **No reclaim completion detection**: Tests verify reclaimablePods publish
  but not surplus pod termination or reclaimablePods clearing.
- **No multi-cluster resize tests**: Per hard boundary, single-cluster only.
- **No DRA + elasticity coexistence test**: Phase 8 DRA tests remain
  independent.
- **No resize target change mid-operation test**: Deferred until the
  controller handles this case.
- **No elapsed time tracking test**: Deferred until metrics are implemented.
- **No runtime detection wiring**: The `inPlaceShrinkSupported` detection
  from runtime signals/annotations is not yet wired into the production
  reconciler. The e2e test uses a status patch as fixture knob.

### Recommended next prompt

See Session: 2026-04-06 (MultiCluster Compatibility) below.

---

## Session: 2026-04-06 (MultiCluster Compatibility)

### Decisions made

1. **Manager suppression path already covers Phase 9**: The existing
   `ShouldSuppressRuntime()` gate returns before the elastic plan evaluation
   block, so no new suppression logic is needed. The manager NEVER evaluates
   elastic plans, executes resize operations, publishes reclaimablePods, or
   creates reclaim helper state for remote RTJs.

2. **Elasticity status surfacing follows Phase 7/8 pattern**: A new
   `remoteElasticitySummary` struct and `buildRemoteElasticitySummary()`
   function extract worker-side elasticity state for structured logging on
   the manager. `hasPhase9RemoteStatus()` detects Phase 9 fields using the
   same pattern as `hasPhase7RemoteStatus()` and `hasPhase8RemoteStatus()`.

3. **Worker mode is unchanged**: The full Phase 9 elastic resize path runs
   identically in worker mode whether or not the RTJ was dispatched by a
   manager. reclaimablePods is published on the worker-local Workload only.

4. **Manager-initiated resize target propagation is deferred**: The adapter's
   spec-propagation mechanism handles `spec.elasticity.targetWorkerCount`
   changes initiated on the manager side. Full testing of this flow is
   deferred to the integration milestone.

5. **Multi-cluster reclaimablePods mirroring remains deferred (OQ-3)**: The
   reclaimablePods written by the worker to its local Workload are not
   mirrored to the manager-side Workload. This is stretch work per the
   original Phase 9 design.

6. **Unit/integration tests use fake client for deterministic coverage**:
   Nine new tests in `remote_status_test.go` cover Phase 9 remote status
   building, detection, manager-mode integration with elastic worker status,
   backward compatibility, and state transition reflection.

7. **E2E smoke test requires Phase 6 infrastructure**: The multicluster
   elastic smoke test (`TestMultiClusterElasticSmokeManagerSuppression`)
   uses the existing Phase 6 multi-cluster setup to verify manager
   suppression for elastic RTJs. Full resize execution over MultiKueue
   is deferred.

### Files created

| File | Purpose |
|---|---|
| `docs/phase9/multicluster-compatibility.md` | What changes on workers, what stays the same on manager, test coverage, invariants, ownership table |
| `test/e2e/multicluster_elastic_smoke_test.go` | `TestMultiClusterElasticSmokeManagerSuppression` — manager suppression for elastic RTJs via MultiKueue |
| `test/e2e/testdata/phase9/rtj-multicluster-elastic-smoke.yaml` | Elastic RTJ template for multicluster smoke test |

### Files changed

| File | Changes |
|---|---|
| `internal/controller/remote_status.go` | Added `remoteElasticitySummary`, `buildRemoteElasticitySummary()`, `hasPhase9RemoteStatus()` |
| `internal/controller/remote_status_test.go` | Added 9 tests: 3 unit tests for summary building/detection, 6 integration tests for manager mode with Phase 9 worker status |
| `internal/controller/resumabletrainingjob_controller.go` | Added Phase 9 multicluster compatibility comment block; added Phase 9 elasticity logging in `reconcileManagerIntent()` |
| `docs/phase9/session-handoff.md` | Added this session entry |

### Tests run

- `go build ./...` — **clean**
- `go vet ./test/e2e/...` — **clean**
- `go test ./internal/controller/... -count=1` — **all pass** (no regressions)
- `go test ./internal/controller/... -run 'Phase9|Elastic|RemoteElasticity' -v` — **all 36+ tests pass**

### New types

| Type | Package | Description |
|---|---|---|
| `remoteElasticitySummary` | `controller` | Phase 9 elasticity state for manager-side observability (8 fields) |

### New functions

| Function | Package | Description |
|---|---|---|
| `buildRemoteElasticitySummary()` | `controller` | Extract Phase 9 summary from mirrored status |
| `hasPhase9RemoteStatus()` | `controller` | Detect Phase 9 fields in mirrored status |

### New test functions

| Function | File | Description |
|---|---|---|
| `TestBuildRemoteElasticitySummaryFullState` | `remote_status_test.go` | All Phase 9 fields populated |
| `TestBuildRemoteElasticitySummaryEmptyStatus` | `remote_status_test.go` | No Phase 9 fields (Phase 8 worker) |
| `TestBuildRemoteElasticitySummaryCheckpointAndRelaunch` | `remote_status_test.go` | Grow via C&R scenario |
| `TestHasPhase9RemoteStatus` | `remote_status_test.go` | Table-driven detection test |
| `TestManagerModeReflectsPhase9WorkerElasticStatus` | `remote_status_test.go` | In-place shrink state from worker |
| `TestManagerModeReflectsPhase9WorkerGrowResize` | `remote_status_test.go` | Grow-via-relaunch state from worker |
| `TestManagerModePhase8WorkerHasNoPhase9Fields` | `remote_status_test.go` | Backward compat |
| `TestManagerModeDoesNotExecuteElasticResize` | `remote_status_test.go` | Elastic spec on manager does not trigger resize |
| `TestManagerModeResizeStateTransitionReflected` | `remote_status_test.go` | State transitions reflected correctly |
| `TestMultiClusterElasticSmokeManagerSuppression` | `multicluster_elastic_smoke_test.go` | E2E: elastic RTJ via MultiKueue |

### New invariants

| ID | Invariant |
|---|---|
| I-12 | Manager never evaluates elastic plans for remote RTJs |
| I-13 | reclaimablePods is published only on the executing worker-side Workload |
| I-14 | Manager never creates reclaim helper state for remote RTJs |

### What was NOT done (deliberately)

- **No full multi-cluster resize execution test**: Requires combined Phase 6 +
  Phase 9 infrastructure setup. The unit/integration tests verify the
  controller behavior deterministically; the e2e smoke test verifies dispatch
  and suppression.
- **No multi-cluster reclaimablePods mirroring (OQ-3)**: Deferred per original
  Phase 9 design. reclaimablePods are worker-local.
- **No manager-initiated resize propagation test**: The adapter handles spec
  propagation; testing the full round-trip requires a running adapter.
- **No cross-cluster resize failover**: Worker switch during resize is an
  advanced scenario requiring future work.
- **No combined DRA + elasticity multicluster test**: Each is independently
  compatible; combined testing is a future milestone.

### Recommended next prompt

```
You are working on Phase 9 only for the checkpoint-native preemption controller repo.

Mission: Wire runtime in-place shrink detection and implement reclaim
completion detection.

Read docs/phase9/e2e.md for what the e2e tests cover and what is deferred.
Read internal/controller/elastic_plan.go for the current detection gap.
Read internal/controller/elastic_execute.go for the execution model.

Step 1: Wire inPlaceShrinkSupported detection from the child JobSet annotation
  or resize signal file into the main reconcile loop.

Step 2: Detect in-place shrink completion: when ReclaimPublished and the
  active JobSet's worker pod count matches the target, clear reclaimablePods
  on the Workload and mark resizeState=Completed.

Step 3: Add elapsed time tracking for resize operations.

Step 4: Handle resize target changes during in-progress operations.
```
