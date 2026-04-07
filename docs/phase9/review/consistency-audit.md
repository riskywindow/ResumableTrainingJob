# Phase 9 — Consistency Audit

> Audited: 2026-04-06
> Scope: Implementation and docs vs. locked Phase 9 design and Phases 0-8 contracts.

---

## 1. Invariant Compliance

### I-1: RTJ is the only Kueue-managed admission object (Phase 2)

**Status: PASS**

- `resumabletrainingjob_webhook.go` integrates with `jobframework` for Kueue admission.
- Child JobSets have no Kueue labels or queue-name annotations.
- E2E tests (`elastic_shrink_reclaim_test.go`, `elastic_grow_relaunch_test.go`) assert child
  JobSets are plain runtime resources.
- Workload is created for the RTJ, not for child JobSets.

### I-2: Child JobSets are plain runtime resources (Phase 2)

**Status: PASS**

- `render.go` creates JobSets without Kueue admission labels.
- No `kueue.x-k8s.io/queue-name` annotation on rendered JobSets.
- E2E tests verify this explicitly.

### I-3: Kueue is sole authority for admission, preemption, and quota (Phase 2)

**Status: PASS**

- Elastic resize does NOT bypass Kueue:
  - In-place shrink: controller publishes `reclaimablePods` — Kueue reads and releases quota.
  - Grow and relaunch shrink: controller suspends Workload — Kueue re-admits at new size.
- No direct quota manipulation outside Kueue.

### I-4: RTJ operator is lifecycle owner (Phase 2, extended Phase 9)

**Status: PASS**

- `elastic_execute.go` owns the resize lifecycle: plan evaluation, execution dispatch,
  condition management, and completion detection.
- `elastic_plan.go` evaluates plans as a pure function with no Kueue client dependency.
- `reclaim.go` computes reclaim deltas without side effects.

### I-5: Checkpoint compatibility is fail-closed (Phase 0)

**Status: PASS**

- Grow and relaunch-shrink paths use the existing checkpoint selection and restore
  flow, which fail-closed on incompatible checkpoints.
- DRA device profile fingerprint matching preserved (Phase 8).
- `elastic_execute_test.go` includes DRA-aware resize coherency tests.

### I-6: Manager/worker split transparent to single-cluster (Phase 6)

**Status: PASS**

- `remote_status.go` adds `remoteElasticitySummary` following the Phase 7/8 pattern.
- `ShouldSuppressRuntime()` gate prevents elastic plan evaluation for remote RTJs.
- Single-cluster mode runs the full elastic path without any manager/worker awareness.

### I-7: DRA disabled = Phase 7 behavior (Phase 8)

**Status: PASS**

- No Phase 9 code modifies DRA-related paths.
- `elastic_execute_test.go` includes DRA coexistence tests.
- Render tests verify DRA and elasticity env vars coexist cleanly.

### I-8: Resume uses latest-compatible-complete checkpoint (Phase 0)

**Status: PASS**

- Grow and relaunch-shrink paths checkpoint, then use the existing restore flow
  which selects latest-compatible-complete checkpoint.
- DCP resharding (Phase 3) enables world-size-flexible restore after resize.

### I-9: Elasticity disabled = Phase 8 behavior (Phase 9)

**Status: PASS**

- `IsElasticityEnabled()` returns false when mode is `Disabled` or empty.
- Main reconcile loop skips elastic plan evaluation when disabled.
- Non-elastic sample RTJ (`rtj-non-elastic.yaml`) exercises this path.
- Webhook test `TestPhase9BackwardCompat_ElasticityDisabled` validates.
- No elasticity env vars injected when elasticity is disabled (render test covers zero-value omission).

### I-10: Scale-up always checkpoint-and-relaunch (Phase 9)

**Status: PASS**

- `plan.go:EvaluatePlan()` returns `GrowViaRelaunch` for all grow cases.
- No in-place grow code path exists anywhere.
- E2E test `TestElasticGrowViaRelaunch` validates the full cycle.

### I-11: reclaimablePods is only quota-release signal for in-place shrink (Phase 9)

**Status: PASS**

- `reclaim.go` writes `reclaimablePods` via SSA with field manager `rtj-elastic-reclaim`.
- No other quota-release mechanism exists for in-place shrink.
- `workload_status_patch.go` builds the SSA patch targeting only `reclaimablePods`.

### I-12: Manager never evaluates elastic plans for remote RTJs (Phase 9)

**Status: PASS**

- `ShouldSuppressRuntime()` returns before elastic plan evaluation block.
- `remote_status_test.go:TestManagerModeDoesNotExecuteElasticResize` validates.
- E2E smoke: `TestMultiClusterElasticSmokeManagerSuppression`.

### I-13: reclaimablePods published only on worker-side Workload (Phase 9)

**Status: PASS**

- Manager suppression prevents reclaimablePods publication for remote RTJs.
- Worker mode publishes on its local Workload only.
- No mirroring to manager-side Workload (deferred per OQ-3).

### I-14: Manager never creates reclaim helper state for remote RTJs (Phase 9)

**Status: PASS**

- Manager suppression prevents any reclaim delta computation or SSA patching.
- E2E smoke test verifies no local child JobSets or helper state for dispatched RTJ.

---

## 2. Design-vs-Implementation Consistency

### 2.1 API Surface

| Design field | Implemented | File | Notes |
|---|---|---|---|
| `spec.elasticity.mode` | Yes | `types.go:ElasticitySpec` | `Disabled`, `Manual` |
| `spec.elasticity.targetWorkerCount` | Yes | `types.go:ElasticitySpec` | Bounded by min/preferred |
| `spec.elasticity.inPlaceShrinkPolicy` | Yes | `types.go:ElasticitySpec` | `IfSupported`, `Never` |
| `spec.elasticity.reclaimMode` | Yes | `types.go:ElasticitySpec` | `ReclaimablePods` only |
| `status.elasticity.resizeState` | Yes | `types.go:ElasticityStatus` | 6 states |
| `status.elasticity.resizePath` | Yes | `types.go:ElasticityStatus` | `InPlace`, `CheckpointAndRelaunch` |
| `status.elasticity.resizeReason` | Yes | `types.go:ElasticityStatus` | Machine-readable |
| `status.elasticity.lastResizeEvent` | Yes | `types.go:ElasticityStatus` | Human-readable |
| `status.elasticity.admittedWorkerCount` | Yes | `types.go:ElasticityStatus` | From Workload |
| `status.elasticity.currentWorkerCount` | Yes | `types.go:ElasticityStatus` | Running pods |
| `status.elasticity.targetWorkerCount` | Yes | `types.go:ElasticityStatus` | From spec |
| `status.elasticity.reclaimablePodsPublished` | Yes | `types.go:ElasticityStatus` | Boolean |
| `status.elasticity.inPlaceShrinkSupported` | Yes | `types.go:ElasticityStatus` | Runtime capability |

### 2.2 Resize Path Decision Tree

| Design path | Implemented | Verified by |
|---|---|---|
| target == current -> no-op | Yes (`NoResize`) | `plan_test.go` |
| target < current, policy=Never -> C&R shrink | Yes (`ShrinkViaRelaunch`) | `plan_test.go`, e2e fallback |
| target < current, runtime supports in-place -> in-place | Yes (`ShrinkInPlace`) | `plan_test.go`, e2e shrink |
| target < current, runtime no in-place -> C&R shrink | Yes (`ShrinkViaRelaunch`) | `plan_test.go`, e2e fallback |
| target < minCount -> REJECT | Yes (webhook validation) | `webhook_test.go` |
| target > current -> C&R grow | Yes (`GrowViaRelaunch`) | `plan_test.go`, e2e grow |
| Preemption in progress -> blocked | Yes (`ResizeBlocked`) | `plan_test.go` |

### 2.3 Execution Paths

| Path | Implemented | Entry point | Notes |
|---|---|---|---|
| In-place shrink execution | Yes | `executeShrinkInPlace()` | SSA patch + status |
| Relaunch resize execution | Yes | `executeRelaunchResize()` | Mark + trigger stop |
| Resize completion detection | Yes | `completeResizeAfterRelaunch()` | On restore success |
| Stale state cleanup | Yes | `clearStaleResizeState()` | On NoResize plan |
| Stop flow integration | Yes | `stopSourceResize` | Reuses drain machinery |

### 2.4 Metrics

| Design metric | Registered | Wired into reconcile | Notes |
|---|---|---|---|
| `rtjs_by_resize_state` | Yes | **No** | Infrastructure only |
| `elastic_active_workers` | Yes | **No** | Infrastructure only |
| `elastic_target_workers` | Yes | **No** | Infrastructure only |
| `elastic_reclaimable_workers` | Yes | **No** | Infrastructure only |
| `reclaimable_pods_publications_total` | Yes | **No** | Infrastructure only |
| `shrink_in_place_successes_total` | Yes | **No** | Infrastructure only |
| `shrink_in_place_failures_total` | Yes | **No** | Infrastructure only |
| `grow_via_relaunch_total` | Yes | **No** | Infrastructure only |
| `resize_fallback_relaunch_total` | Yes | **No** | Infrastructure only |
| `resize_checkpoint_creations_total` | Yes | **No** | Infrastructure only |
| `workload_status_patch_failures_total` | Yes | **No** | Infrastructure only |
| `resize_plan_evaluations_total` | Yes | **No** | Infrastructure only |

**Finding: All 12 Phase 9 metrics are registered with Prometheus but none are called
from the reconcile loop.** The recorder methods exist (`ObserveResizeState`,
`IncShrinkInPlaceSuccess`, etc.) but are not invoked at the appropriate points in
`elastic_execute.go` or `elastic_plan.go`. This is documented as deliberate in
the session handoff.

### 2.5 Runtime Elasticity Protocol

| Design element | Implemented | Notes |
|---|---|---|
| `ElasticConfig` dataclass | Yes | `elastic.py` |
| `evaluate_resize()` pure function | Yes | `elastic.py` |
| `resize-signal.json` file output | Yes | `elastic.py` + fixture |
| YIELD_SDK env var injection | Yes | `render.go` + `names.go` |
| Control file `targetWorkerCount` | Yes | `control.py` |
| Manifest resize metadata | Yes | `manifest.py` (5 fields) |
| DDP fixture integration | Yes | `train.py` |

### 2.6 Index.md Freshness

**Finding: `docs/phase9/index.md` says "Design locked -- implementation not started".**
This is stale. The implementation is substantially complete. The status field and
document links need updating.

---

## 3. Cross-Phase Consistency

### Phase 3: World-Size-Flexible Resume

- Grow path triggers DCP resharding at the new world size. Confirmed via the relaunch
  cycle which creates a new JobSet attempt with updated replica count.
- `allowWorldSizeChange: true` is required for Manual elasticity mode (webhook validates).

### Phase 4: Topology-Aware Admission

- No Phase 9 code interferes with topology-aware placement.
- TopologyRequest synthesis continues to work for elastic RTJs.
- Launch gate evaluation preserved for resize-triggered relaunches.

### Phase 5: Priority Shaping

- Priority reconciliation runs independently of elastic plan evaluation.
- Both can be active simultaneously on the same RTJ (no mutex).

### Phase 6: Multi-Cluster

- Manager suppression fully covers Phase 9.
- Remote elasticity status surfaced via `remoteElasticitySummary`.
- Worker-side elasticity runs identically whether or not dispatched.

### Phase 7: Capacity Guarantee

- Launch gates apply to resize-triggered relaunches.
- `waitForPodsReady` detects relaunch failures.
- `LaunchGateResult.ResizeRelaunch` flag threads context correctly.

### Phase 8: DRA

- DRA-aware resize coherency tested in `elastic_execute_test.go`.
- Device profile fingerprint matching preserved for resize relaunches.
- No new DRA code needed; existing paths handle it.

---

## 4. Summary of Findings

| # | Severity | Finding |
|---|---|---|
| C-1 | Low | `index.md` status says "implementation not started" — stale |
| C-2 | Info | Metrics registered but not wired (documented, deliberate) |
| C-3 | Info | `inPlaceShrinkSupported` detection from runtime not wired to reconcile loop; tests use status patch as fixture knob (documented, deliberate) |
| C-4 | Info | Reclaim completion detection (clearing reclaimablePods after pod termination) not implemented (documented, deliberate) |
| C-5 | Info | Resize target changes during in-progress operations not handled (documented, deliberate) |
| C-6 | Info | MultiKueue reclaimablePods mirroring deferred (OQ-3, documented) |

All findings are either cosmetic (C-1) or explicitly documented as deliberate deferrals.
No invariant violations. No drift from the locked Phase 9 design.
