# Phase 9 — Gaps Analysis

> Audited: 2026-04-06
> Scope: Implementation completeness against locked Phase 9 design.

---

## 1. Functional Gaps

### G-1: Metrics not wired into reconcile loop

**Severity: Medium (observability only, no functional impact)**

All 12 Phase 9 metrics are registered with Prometheus and the `Recorder` struct
has mutation methods for each. However, none are called from the reconcile path.
The following call sites are missing:

| Missing call | Where it should go |
|---|---|
| `ObserveResizeState(name, state)` | `syncElasticityStatus()` in `elastic_plan.go` |
| `ObserveElasticWorkers(name, active, target, reclaimable)` | `syncElasticityStatus()` |
| `IncReclaimablePodsPublications()` | `executeShrinkInPlace()` after SSA patch success |
| `IncShrinkInPlaceSuccess()` | `executeShrinkInPlace()` on plan completion |
| `IncShrinkInPlaceFailure()` | `executeShrinkInPlace()` on SSA patch failure |
| `IncGrowViaRelaunch()` | `executeRelaunchResize()` for grow path |
| `IncResizeFallbackRelaunch()` | `executeRelaunchResize()` for shrink fallback |
| `IncResizeCheckpointCreation()` | After checkpoint write in resize stop flow |
| `IncWorkloadStatusPatchFailure()` | SSA patch error handling |
| `IncResizePlanEvaluation(kind)` | `evaluateElasticPlan()` after plan evaluation |

**Impact**: No production metrics for resize operations. Prometheus queries in
`operations.md` would return zero.

**Recommendation**: Wire in Phase 10 or as a hardening follow-up. Low risk —
the infrastructure is correct, just not called.

---

### G-2: Runtime in-place shrink detection not wired

**Severity: Medium (conservative fallback is safe)**

The design specifies that `inPlaceShrinkSupported` should be detected from a
runtime annotation on the child JobSet or from the resize signal file. Currently:

- The `ElasticityStatus.InPlaceShrinkSupported` field exists and is used by the planner.
- `buildElasticPlanInput()` reads it from `job.Status.Elasticity.InPlaceShrinkSupported`.
- But nothing in the reconcile loop writes this field from runtime signals.
- E2E tests work around this by patching the status subresource directly.

**Impact**: In production, `InPlaceShrinkSupported` defaults to `false` (zero value),
so the controller always falls back to checkpoint-and-relaunch for shrink operations.
This is safe but means in-place shrink is effectively disabled without manual status patching.

**Recommendation**: Wire detection from either:
1. Child JobSet annotation (preferred — runtime capability is static per JobSet)
2. Resize signal file contents (already written by fixture)
3. An explicit `spec.elasticity.runtimeCapabilities.inPlaceShrink: true` field

---

### G-3: Reclaim completion detection not implemented

**Severity: Low (reclaimablePods persists harmlessly)**

After an in-place shrink, `reclaimablePods` is published and Kueue releases quota.
But the controller does not detect when the surplus pods have actually terminated
to clear the `reclaimablePods` field and mark the resize as `Completed`.

**Impact**: After in-place shrink:
- `resizeState` stays at a transitional state rather than moving to `Completed`
- `reclaimablePodsPublished` stays `true` indefinitely
- Kueue has already released the quota, so no functional impact on scheduling
- The stale `reclaimablePods` on the Workload is harmless (Kueue treats it idempotently)

**Recommendation**: Observe active JobSet pod count. When `runningPods == targetWorkerCount`,
clear `reclaimablePods` via SSA and set `resizeState=Completed`.

---

### G-4: Mid-resize target changes not handled

**Severity: Low (edge case, not in P0 scope)**

If a user patches `targetWorkerCount` while a resize operation is already in progress:
- For in-place shrink: the planner returns `ReclaimPublished` (idempotent) and does
  not re-evaluate.
- For checkpoint-and-relaunch: the drain flow is already active and cannot be interrupted.
- The new target is evaluated on the next reconcile after the current resize completes.

**Impact**: Brief period where the target does not match the executing resize.
The new target will be picked up after completion. No data loss or invariant violation.

**Recommendation**: Document this behavior explicitly. Optionally add a `ResizeQueued`
state for future enhancement.

---

### G-5: Resize elapsed time tracking not implemented

**Severity: Low (observability enhancement only)**

The design mentions tracking resize start/completion timestamps and duration metrics.
Neither `resizeStartTimestamp` nor `resizeCompletionTimestamp` fields exist in the
current `ElasticityStatus`.

**Impact**: Operators cannot measure resize duration from status alone. Must correlate
log timestamps.

**Recommendation**: Add timestamp fields to `ElasticityStatus` when metrics are wired.

---

## 2. Test Coverage Gaps

### T-1: No unit tests for metrics recording

**Status: Expected gap (metrics not wired yet)**

When metrics are wired, unit tests should verify that each recorder method is called
at the correct point in the resize lifecycle.

### T-2: No e2e test for DRA + elasticity coexistence

**Status: Acceptable gap (each independently tested)**

Phase 8 e2e tests cover DRA. Phase 9 e2e tests cover elasticity. Combined testing
is a future milestone. Unit tests in `elastic_execute_test.go` cover DRA-aware
resize coherency.

### T-3: No e2e test for mid-resize target change

**Status: Acceptable gap (matches G-4, not in scope)**

### T-4: No integration test for full multi-cluster resize execution

**Status: Acceptable gap (smoke test covers suppression)**

Full multi-cluster resize requires combined Phase 6 + Phase 9 infrastructure.
Unit/integration tests deterministically cover the controller behavior.
E2E smoke verifies manager suppression.

---

## 3. Documentation Gaps

### D-1: `index.md` status is stale

The status line says "Design locked -- implementation not started."
Should say "Implementation complete -- hardening in progress."

### D-2: `index.md` document links incomplete

Missing links to:
- `api.md`
- `runtime-elasticity.md`
- `elastic-planning.md`
- `resize-execution.md`
- `dev-environment.md`
- `e2e.md`
- `demo.md`
- `operations.md`
- `troubleshooting.md`
- `multicluster-compatibility.md`
- `adr/0002-elasticity-api.md`
- `review/consistency-audit.md`
- `review/gaps.md`
- `PHASE9_SIGNOFF.md`

### D-3: API extension sketch in `index.md` uses old field names

The sketch uses `resizePhase` (should be `resizeState`) and uses `training.io/v1alpha1`
instead of `training.checkpoint.example.io/v1alpha1`. These are cosmetic but could
confuse readers.

---

## 4. Gap Priority Matrix

| Gap | Severity | Blocks signoff? | Recommended timing |
|---|---|---|---|
| G-1 Metrics wiring | Medium | No | Phase 10 or hardening follow-up |
| G-2 Runtime detection | Medium | No | Phase 10 (conservative fallback is safe) |
| G-3 Reclaim completion | Low | No | Phase 10 |
| G-4 Mid-resize target | Low | No | Phase 10 (document current behavior) |
| G-5 Elapsed time | Low | No | Phase 10 (with metrics wiring) |
| D-1 Stale index.md | Low | Yes (fixed in this pass) | This session |
| D-2 Missing links | Low | Yes (fixed in this pass) | This session |
| D-3 Old field names | Low | Yes (fixed in this pass) | This session |

No gap blocks Phase 9 signoff. All functional gaps have safe defaults or
conservative fallbacks. Documentation gaps are fixed in this signoff pass.
