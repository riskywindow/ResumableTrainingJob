# Phase 3 Gaps Register

**Date:** 2026-03-24
**Basis:** consistency-audit.md findings, code review of all Phase 3 implementation files, test coverage audit.

Each gap is classified:

- **Blocking:** Must resolve before Phase 3 signoff.
- **Non-blocking:** Acceptable for signoff; documented for Phase 4.
- **Deferred by design:** Intentionally excluded from Phase 3 scope.

---

## Wiring Gaps

### GAP-1: `status.admission.admittedFlavors` always nil

**Severity:** Non-blocking
**Location:** `internal/controller/resume_flow.go` lines 48, 117
**Design reference:** api.md, architecture.md, admission-materialization.md

`syncAdmissionStatus()` is called with `nil` for the `admittedFlavors` parameter in both `reconcileLaunch()` and `reconcileResume()`. The design specifies that flavor names should be extracted from the Workload's `PodSetAssignments[].flavors` map via `FromWorkloadAdmission()`. The `AdmissionView` abstraction and `FlavorsByPodSet()` method exist in `internal/kueue/admission_view.go` but are not called by the controller.

**Impact:** RTJ status does not report which ResourceFlavor was assigned. Operators must inspect the Kueue Workload directly. The `operations.md` and `inspect-admission.sh` documents work around this by querying the Workload.

**Resolution path:** Read the Workload object in `reconcileLaunch`/`reconcileResume`, call `FromWorkloadAdmission()`, pass `FlavorsByPodSet()` to `syncAdmissionStatus()`.

### GAP-2: `RenderInput.AdmittedFlavor` always empty

**Severity:** Non-blocking
**Location:** `internal/controller/resume_flow.go` lines 170-181
**Design reference:** flavor-aware-rendering.md

The `RenderInput.AdmittedFlavor` field is defined and the renderer injects it as `YIELD_SDK_ADMITTED_FLAVOR`, but the controller never populates it. This means the trainer container always sees an empty `YIELD_SDK_ADMITTED_FLAVOR` env var (when Phase 3 env vars are injected at all).

**Impact:** Trainer code cannot distinguish which flavor it was assigned to. For Phase 3 this is cosmetic — the trainer does not branch on flavor. The nodeSelector/tolerations from `podset.Merge` correctly place pods regardless of whether the trainer knows the flavor name.

**Resolution path:** Same as GAP-1. Once the Workload is read, extract the worker pod set's flavor name and pass it to `RenderInput.AdmittedFlavor`.

### GAP-3: Reshard restore success/fail metrics not incremented

**Severity:** Non-blocking
**Location:** `internal/metrics/metrics.go` lines 355-371
**Design reference:** Session 9 metrics design

`IncReshardRestoreSucceeded()` and `IncReshardRestoreFailed()` are defined but never called. `IncReshardRestoreAttempted()` is called via `ObserveResumeWorldSize()` when world sizes differ. The success/failure counters require the controller to detect when a reshard restore transitions to Running (success) or Failed (failure), which happens in the main reconcile loop — not in the resume flow where the reshard is initiated.

**Impact:** The `reshard_restores_succeeded_total` and `reshard_restores_failed_total` Prometheus counters always read 0. The `reshard_restores_attempted_total` counter works correctly.

**Resolution path:** In the main reconciler, when transitioning from `Restoring` to `Running`, check if `status.restore.restoreMode == Reshard` and call `IncReshardRestoreSucceeded()`. Similarly for `Restoring` → `Failed`.

### GAP-4: `ObserveFlavorAssignment` metric never called

**Severity:** Non-blocking
**Location:** `internal/metrics/metrics.go` lines 373-378

Depends on GAP-1. Once flavor names are extracted, `ObserveFlavorAssignment(flavor)` should be called to populate `flavor_assignments_total{flavor="..."}`.

### GAP-5: `status.admission.activeWorkerCount` never populated

**Severity:** Non-blocking
**Location:** `internal/controller/status_helpers.go` line 372

The `AdmissionStatus.ActiveWorkerCount` field exists in the API types but `syncAdmissionStatus()` does not set it. It would require counting running pods in the active child JobSet, which adds a List call per reconcile.

**Impact:** Operators cannot see actual running pod count in RTJ status. They can inspect pods directly with `kubectl get pods`.

**Resolution path:** Optional. Add a pod count query in the main reconciler when the phase is Running. Low priority — adding API calls to the hot path has cost.

---

## Test Coverage Gaps

### GAP-6: No controller unit tests for Phase 3 admission paths

**Severity:** Non-blocking (unit tests exist at package level; e2e covers integration)
**Location:** `internal/controller/` — no Phase 3-specific test file

The session-handoff.md recommended adding controller unit tests for:
- Launch with admission annotation → replicas adjusted, Phase 3 env vars injected.
- Resume with different world size → `AllowWorldSizeChange` respected.
- Phase 2 backward compat → no annotation → original behavior.
- Partial admission → admitted count < preferred.

These scenarios are covered indirectly:
- `internal/jobset/render_test.go` (7 Phase 3 tests) covers rendering with admitted counts.
- `internal/jobset/flavor_injection_test.go` (10 tests) covers replica adjustment.
- `internal/checkpoints/compatibility_test.go` (8 tests) covers world-size decisions.
- `test/e2e/` (3 tests) covers live integration.

But there are no unit tests that exercise `reconcileLaunch()` or `reconcileResume()` with mocked admission data.

**Impact:** A subtle wiring bug between `parseAdmittedCounts()` and `RenderInput` population could go undetected by package-level tests. The e2e tests would catch it on a live cluster.

### GAP-7: Different-size resume not covered by e2e

**Severity:** Non-blocking (deferred by design)
**Location:** `test/e2e/flexible_resume_test.go`
**Design reference:** e2e.md section "World-Size Parity and Partial Admission"

`TestFlexibleResume` exercises the same-size path (admitted world size == requested world size). The different-size path requires:
1. Kueue partial admission (PodSet.MinCount) to admit fewer pods.
2. Preemption or resource pressure to force partial admission.
3. Resume at a different world size.

This is validated by unit tests across 4 packages (28+ tests). The e2e.md documents the experimental manual test path.

**Impact:** The live integration proof for reshard mode exists only in unit tests. A subtle interaction between Kueue partial admission and the controller could go undetected.

### GAP-8: PodSetName validation sparse in webhook tests

**Severity:** Non-blocking
**Location:** `api/v1alpha1/resumabletrainingjob_webhook_test.go`

Webhook tests cover Phase 3 acceptance and rejection for `enablePartialAdmission`, but do not test `PodSetName` targeting an invalid replicatedJob name. The `validateParallelism()` function validates other constraints but not pod set name existence (this would require access to the runtime template at validation time, which may not be available).

**Impact:** An RTJ with an invalid `PodSetName` would be accepted by the webhook. At reconcile time, `resolveWorkerPodSetName()` defaults to the first replicatedJob if the name doesn't match, which is a safe fallback.

### GAP-9: Silent JSON unmarshal error in parseAdmittedCounts

**Severity:** Non-blocking
**Location:** `internal/controller/resume_flow.go` line 208-211

`parseAdmittedCounts()` returns nil when JSON unmarshal fails, silently falling back to Phase 2 behavior. If the bridge annotation is corrupted, the controller would launch with the original template replicas instead of the admitted count. No warning is logged.

**Impact:** Annotation corruption would be difficult to diagnose. The controller would appear to work but the child JobSet would have the wrong replica count.

**Resolution path:** Add a warning log when unmarshal fails but the annotation key is present.

---

## Documentation Gaps

### GAP-10: architecture.md references `admittedFlavors` in status as populated

**Severity:** Non-blocking
**Location:** `docs/phase3/architecture.md`

The architecture document describes `admittedFlavors` as part of the status flow, but it is not populated (GAP-1). The document should note that this field is scaffolded but not yet wired.

### GAP-11: demo.md does not mention the `YIELD_SDK_ADMITTED_FLAVOR` limitation

**Severity:** Non-blocking
**Location:** `docs/phase3/demo.md`

Demo 1 (Flavor-Aware Launch) instructs inspecting admission status but does not note that `admittedFlavors` in RTJ status will be empty. The workaround (inspect Workload directly) is documented in operations.md.

---

## Deferred by Design

These are explicitly out of Phase 3 scope per goals.md and ADR 0001.

| Item | Phase 3 Rule | Phase 4 Candidate |
| --- | --- | --- |
| GPU shape relaxation | Strict exact match (OQ-4) | Future ADR |
| True in-place elastic scaling | Resume-time shape changes only | Not Phase 4 |
| MultiKueue | Out of scope | Future phase |
| Topology-aware scheduling | Out of scope | Future phase |
| ProvisioningRequest integration | Out of scope | Future phase |
| Runtime heartbeat for Running evidence | Running means active child JobSet | Future phase |
| Resume fallback to older checkpoint | Single-selection, fail-closed | Future phase |
| Repeated pause/resume soak test | Not added | Phase 4 |
| Durable metrics/monitoring | Process-local Prometheus | Future phase |

---

## Summary

| Category | Blocking | Non-blocking | Deferred |
| --- | --- | --- | --- |
| Wiring gaps | 0 | 5 (GAP-1 through GAP-5) | 0 |
| Test coverage | 0 | 4 (GAP-6 through GAP-9) | 0 |
| Documentation | 0 | 2 (GAP-10, GAP-11) | 0 |
| By design | 0 | 0 | 9 |
| **Total** | **0** | **11** | **9** |

**No blocking gaps.** All non-blocking gaps are documented with resolution paths and tracked for future work.
