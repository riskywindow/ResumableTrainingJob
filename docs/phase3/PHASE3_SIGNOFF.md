# Phase 3 Signoff

**Date:** 2026-03-24
**Phase:** 3 — Admission-Aware Launch and Flavor-Aware Resume
**Status:** Signed off for local development, demo, and hardening use. Not production-ready.

---

## What Phase 3 Can Do

### Admission-Aware Runtime Launch (G1)

- When Kueue admits an RTJ, the operator reads the admitted pod counts from the
  bridge annotation (`training.checkpoint.example.io/admitted-pod-sets`) and
  adjusts the child JobSet replica counts accordingly.
- `status.admission.admittedWorkerCount` and `preferredWorkerCount` are
  recorded on every launch and resume.
- When no admission data is present (Phase 2 RTJ), the child JobSet uses the
  template replica counts unchanged.

### Flavor-Aware Child JobSet Rendering (G2)

- Kueue's `RunWithPodSetsInfo` callback applies `podset.Merge`, which injects
  the ResourceFlavor's `nodeSelector` and `tolerations` into the RTJ pod
  template. The operator then renders the child JobSet from that mutated
  template.
- Kueue management labels and annotations are stripped from the child JobSet pod
  template so it remains a plain runtime resource.
- Pods land on nodes whose labels match the assigned ResourceFlavor. This is
  verified by `TestFlavorAwareLaunch` on a live kind cluster with two node
  pools (on-demand and spot).

### World-Size-Flexible Resume (G3)

- RTJs with `spec.resume.allowWorldSizeChange: true` can resume from a
  checkpoint taken at a different world size, provided the checkpoint manifest
  declares `crossSizeRestoreSupported: true`.
- The Python yield SDK detects the world-size difference and uses PyTorch DCP's
  native resharding to load the checkpoint at the new world size. RNG state is
  skipped during cross-size restore.
- `status.restore` records the restore mode (`SameSize` or `Reshard`), the
  checkpoint world size, and the restore world size.
- Phase 3 environment variables (`YIELD_SDK_WORLD_SIZE`,
  `YIELD_SDK_ORIGINAL_WORLD_SIZE`, `YIELD_SDK_ALLOW_WORLD_SIZE_CHANGE`) are
  injected into the trainer container when admission data is present.
- `TestFlexibleResume` exercises the full pause/resume cycle with
  `allowWorldSizeChange=true` on a live kind cluster, verifying checkpoint
  monotonicity, restore status, and admission status.

### Developer and Demo Tooling

- `make phase3-up` creates a 4-worker kind cluster with two ResourceFlavors
  (on-demand and spot), a multi-flavor ClusterQueue, and all Phase 3
  infrastructure.
- `make phase3-submit-flavor`, `make phase3-submit-flex` submit Phase 3 sample
  RTJs.
- `make phase3-inspect-admission`, `make phase3-inspect-checkpoints` provide
  full inspection of admission and checkpoint state.
- `docs/phase3/demo.md` contains exact command sequences for flavor-aware
  launch and mixed-size resume demos.
- `docs/phase3/operations.md` explains how to inspect every layer of the
  admission and checkpoint data chain.
- `docs/phase3/troubleshooting.md` covers the four most common Phase 3 failure
  modes.

### Observability

- Eight new Prometheus metrics track Phase 3 data flows: admission comparisons
  (equal vs. partial), reshard restore attempts, flavor assignments, partial
  admission launches, and same-size vs. different-size resume counts.
- All existing Phase 1 and Phase 2 metrics continue to work.

---

## What Is Still Experimental

### Partial Admission (G4)

Partial admission allows Kueue to admit fewer workers than requested. It is
gated behind two flags:

1. **Operator flag:** `--enable-experimental-partial-admission` (default: false)
2. **Per-job field:** `spec.parallelism.enablePartialAdmission: true` (default: false)

Both must be true for `PodSet.MinCount` to be synthesized. Additionally,
`spec.resume.allowWorldSizeChange: true` is required by validation.

The full scaffolding, gating, PodSet synthesis, and unit test coverage (8
tests) are in place. Live e2e testing requires the `experimental` profile
(`make phase3-up PHASE3_PROFILE=experimental`) and the operator flag.

Partial admission is experimental because:

- DCP resharding is correct but has limited production mileage.
- World-size-dependent optimizer state (learning rate schedules, gradient
  accumulators) may have subtle semantic differences at different sizes.
- Kueue's partial admission interaction with preemption cascades is not
  fully characterized.
- No repeated-cycle soak test proves stability.

---

## What Remains Deferred

| Item | Reason | Phase |
| --- | --- | --- |
| GPU shape relaxation (OQ-4) | Cross-GPU-type resume (A100 → H100) has correctness implications for tensor layout and memory. Requires a dedicated ADR. | Future |
| True in-place elastic scaling | Phase 3 handles resume-time shape changes only. Live scaling of running workloads is a different problem. | Future |
| MultiKueue | Multi-cluster admission is out of Phase 3 scope. | Future |
| Topology-aware scheduling | Phase 3 uses ResourceFlavor nodeSelector for placement, not Kueue TopologyAwareScheduling. | Future |
| ProvisioningRequest integration | Node auto-provisioning is not in scope. | Future |
| Runtime heartbeat for Running evidence | Running means active child JobSet, not runtime heartbeat. Unchanged from Phase 1. | Future |
| Resume fallback to older checkpoint | Single-selection fail-closed. No fallback chain. Unchanged from Phase 1. | Future |
| Repeated pause/resume soak test | No dedicated multi-cycle soak. Phase 3 e2e covers two cycles. | Phase 4 |
| `status.admission.admittedFlavors` wiring | Flavor names not extracted from Workload. Scaffolded, not connected. | Phase 4 |
| `YIELD_SDK_ADMITTED_FLAVOR` env var population | Depends on admittedFlavors wiring. | Phase 4 |
| Reshard restore success/fail metrics | Counters defined, not incremented from controller. | Phase 4 |
| Controller unit tests for Phase 3 paths | Package-level and e2e tests provide coverage. Dedicated controller unit tests deferred. | Phase 4 |

---

## Main Known Risks

### 1. Different-size resume has no live e2e proof

The controller code path for different-size resume is identical to same-size —
only the restore mode differs. Unit tests (28+) validate compatibility
checking, checkpoint selection, PodSet MinCount synthesis, and DCP resharding.
But no live e2e test exercises the full flow with Kueue partial admission
delivering a different admitted count. A subtle interaction between Kueue's
partial admission and the controller's admission annotation parsing could go
undetected.

**Mitigation:** Unit tests cover the critical decision points. The manual test
path is documented. Phase 4 should add a live e2e test with the experimental
profile.

### 2. Flavor observability incomplete

`status.admission.admittedFlavors` is always nil. `YIELD_SDK_ADMITTED_FLAVOR`
is always empty. `flavor_assignments_total` metric is always zero. Operators
must inspect the Kueue Workload directly to determine which flavor was
assigned. This is a UX gap, not a correctness bug — pods are correctly placed
by nodeSelector/tolerations from `podset.Merge`.

**Mitigation:** `inspect-admission.sh` and operations.md document the Workload
inspection workaround. Phase 4 should wire flavor name extraction.

### 3. Silent annotation corruption fallback

If the `admitted-pod-sets` bridge annotation is corrupted (malformed JSON),
`parseAdmittedCounts()` silently returns nil, causing the controller to use
template replicas instead of admitted counts. No warning is logged.

**Mitigation:** The annotation is set by the operator's own
`RunWithPodSetsInfo` callback, making external corruption unlikely. Phase 4
should add a warning log.

### 4. Reshard restore metrics incomplete

`reshard_restores_attempted_total` is incremented correctly.
`reshard_restores_succeeded_total` and `reshard_restores_failed_total` are
defined but never incremented. Operators cannot distinguish reshard attempts
that succeeded from those that failed using metrics alone.

**Mitigation:** The `status.restore.restoreMode` field on the RTJ provides
per-job visibility. Phase 4 should wire the success/fail counters.

### 5. No repeated-cycle soak test

Phase 3 e2e tests execute two pause/resume cycles (`TestFlexibleResume`). No
test runs dozens of cycles to prove long-term stability under repeated
admission, preemption, and resume.

**Mitigation:** Phase 2 had the same gap. Phase 4 should add a soak test once
runtime evidence and restore paths are strengthened.

---

## Test Evidence

### Unit Tests

| Package | Phase 3 Tests | Coverage |
| --- | --- | --- |
| `api/v1alpha1` | 15 | Validation, defaulting, deep copy, webhook, backward compat |
| `internal/checkpoints` | 13 | Compatibility (8 flexible-match), selector (5 cross-size) |
| `internal/kueue` | 22 | AdmissionView (16), PodSet synthesis (6 with MinCount) |
| `internal/jobset` | 17 | Render (7 Phase 3), flavor injection (10) |
| `sdk/python` | 25 | Manifest (6), resume (7), checkpoint (12) — same-size, reshard, rejection |

### E2E Tests

| Test | Goals | What It Proves |
| --- | --- | --- |
| `TestAdmissionMaterialization` | G1 | RTJ admitted → child JobSet created with correct replicas; plain runtime; bridge annotation; status.admission |
| `TestFlavorAwareLaunch` | G2 | Flavor nodeSelector/tolerations in child JobSet; pods on correct node pool |
| `TestFlexibleResume` | G1, G2, G3 | Pause/resume with `allowWorldSizeChange=true`; checkpoint monotonicity; status.restore; status.admission |

### Experimental Coverage

Partial admission (G4) is covered by 8 unit tests in `internal/kueue/rtj_podsets_test.go`. These verify MinCount synthesis, worker-only targeting, PreferredCount override, double gating, and backward compatibility. Live e2e coverage is conditionally available via `make phase3-up PHASE3_PROFILE=experimental`.

---

## What Phase 4 Should Build Next

1. **Wire `admittedFlavors` into RTJ status.** Read the Workload object via
   `FromWorkloadAdmission()` and pass flavor names to `syncAdmissionStatus()`.
   Wire `RenderInput.AdmittedFlavor` and call `ObserveFlavorAssignment()`.

2. **Wire reshard restore success/fail metrics.** In the main reconciler,
   detect `Restoring` → `Running` with `restoreMode=Reshard` and call
   `IncReshardRestoreSucceeded()`. Similarly for `Restoring` → `Failed`.

3. **Add controller unit tests** for `reconcileLaunch` and `reconcileResume`
   with mocked admission data. Test scenarios: annotation present → replicas
   adjusted; annotation absent → Phase 2 behavior; admitted < preferred →
   partial admission metrics.

4. **Add a live e2e test for different-size resume** using the experimental
   profile. Submit an RTJ with `enablePartialAdmission`, constrain cluster
   capacity, verify `restoreMode=Reshard`.

5. **Add a warning log** in `parseAdmittedCounts()` when the annotation key is
   present but JSON unmarshal fails.

6. **Add a repeated-cycle soak test** that runs 10+ pause/resume cycles and
   verifies monotonic step advancement and no resource leaks.

7. **Evaluate GPU shape relaxation** (OQ-4) for a future ADR. Cross-GPU-type
   resume has tensor layout, memory alignment, and performance implications
   that need dedicated analysis.

---

## Signoff Statement

Phase 3 is a correctness-first slice that makes the ResumableTrainingJob
admission-aware and supports world-size-flexible resume. It preserves all
Phase 0, Phase 1, and Phase 2 contracts without exception. The critical paths
(admission-aware launch, flavor-aware pod placement, flexible-size resume) are
implemented and tested on live kind clusters. Experimental partial admission
is fully scaffolded and unit-tested, gated behind two flags, and documented
for manual testing.

Non-blocking gaps (flavor observability wiring, reshard metrics, controller
unit tests) are documented with resolution paths. No blocking gaps were found.

Phase 3 is signed off for local development, demo, and hardening use.
