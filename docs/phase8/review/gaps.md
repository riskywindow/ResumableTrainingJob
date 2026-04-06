# Phase 8 -- Gaps Analysis

**Date**: 2026-04-05

---

## Classification

| Severity | Meaning |
|----------|---------|
| **P0** | Blocks signoff. Must fix before Phase 8 is complete. |
| **P1** | Should fix. Impacts observability or operational confidence but not correctness. |
| **P2** | Nice to have. Can be deferred to Phase 9 without risk. |

---

## G1: `observeDRAClaimStatus()` not wired into Reconcile loop

**Severity**: P1

**Description**: The function `observeDRAClaimStatus()` in
`internal/controller/dra_status.go` implements DRA claim allocation
observation (queries ResourceClaims, updates `status.devices` allocation
fields, sets/clears `DRAClaimAllocationFailed` condition). However, it is
never called from the main `Reconcile()` function in
`resumabletrainingjob_controller.go`.

**Impact**: RTJ `status.devices.claimAllocationState`,
`allocatedClaimCount`, `lastClaimFailureReason`, and the
`DRAClaimAllocationFailed` condition are never populated at runtime. The
fields remain at their zero values. This is an observability gap, not a
correctness issue -- Kueue admission and DRA allocation still function
correctly because Kueue and the kubelet DRA controller manage claims
independently.

**Evidence**: `grep -r "observeDRAClaimStatus" internal/controller/resumabletrainingjob_controller.go` returns no matches.

**Recommendation**: Wire `observeDRAClaimStatus()` into the Reconcile loop
after `reconcileDRATemplates()` and before launch gate evaluation. Requeue
on Pending allocation state. Set status changed flag. This is
straightforward (~15 lines of wiring code).

**Deferred to**: Phase 9 Session 1 (or late Phase 8 if a follow-up session
is scheduled).

---

## G2: `syncDeviceResumeFingerprint()` not wired into resume flow

**Severity**: P1

**Description**: The function `syncDeviceResumeFingerprint()` in
`internal/controller/status_helpers.go` records the device profile
fingerprint used during a resume attempt into
`status.devices.lastResumeDeviceProfileFingerprint`. It is defined but
never called from `resume_flow.go` or the main reconcile loop.

**Impact**: `status.devices.lastResumeDeviceProfileFingerprint` is never
populated. This is a telemetry/debugging gap -- the fingerprint used for
checkpoint compatibility is still correctly passed through
`ResumeRequest.CurrentDeviceProfileFingerprint` (populated from
`status.devices.currentDeviceProfileFingerprint`). The compatibility check
itself works correctly.

**Evidence**: `grep -rn "syncDeviceResumeFingerprint" internal/controller/` matches only the definition in status_helpers.go.

**Recommendation**: Call `syncDeviceResumeFingerprint()` in the resume path
after a compatible checkpoint is selected and before the child JobSet is
rendered. This is ~3 lines of wiring.

**Deferred to**: Phase 9 Session 1.

---

## G3: Phase 8 metric emission not wired into controller callsites

**Severity**: P1

**Description**: Eight Phase 8 metrics are defined in
`internal/metrics/metrics.go` with corresponding recorder methods
(`ObserveDeviceMode`, `IncDRATemplateReconcile`, `IncDRATemplateFailure`,
`IncDRAClaimsGenerated`, `IncDRAClaimAllocationSummary`,
`IncDRAResumeCompatibilityCheck`, `IncDRABackedLaunch`,
`IncDRALaunchFailure`). None of these are called from controller code.

**Impact**: All 8 Phase 8 DRA Prometheus metrics remain at zero. Operators
get no DRA-specific telemetry from the metrics endpoint. Phase 2-7 metrics
continue to function correctly.

**Evidence**: `grep -rn "IncDRA\|ObserveDeviceMode" internal/controller/` returns no matches.

**Recommendation**: Add metric calls at the following callsites:
- `ObserveDeviceMode()` in `observePhase()` (existing method, add DRA
  dimension)
- `IncDRATemplateReconcile()` in `reconcileDRATemplates()` on success
- `IncDRATemplateFailure()` in `reconcileDRATemplates()` on error
- `IncDRABackedLaunch()` in child JobSet creation when DRA is enabled
- `IncDRALaunchFailure()` in child JobSet creation failure when DRA is
  enabled
- Remaining metrics in `observeDRAClaimStatus()` (blocked by G1)

**Deferred to**: Phase 9 Session 1 (co-located with G1 and G2 wiring).

---

## G4: OQ9 -- DRA + ProvisioningRequest interaction e2e test

**Severity**: P2

**Description**: Open question OQ9 from Session 1 asked whether DRA
device requests interact correctly with Phase 7 ProvisioningRequest-based
capacity guarantees. No e2e test covers this combination.

**Impact**: The theoretical interaction is low-risk -- DRA device
allocation and ProvisioningRequest node provisioning are orthogonal Kueue
admission checks that compose naturally. However, the combination has not
been validated end-to-end.

**Recommendation**: Add an e2e test that submits a DRA-backed RTJ with
ProvisioningRequest configured. Verify both admission checks resolve and
the child JobSet launches with DRA claims on provisioned nodes.

**Deferred to**: Phase 9 (explicitly deferred in Session 1, carried through
all sessions).

---

## G5: Device failure recovery e2e test

**Severity**: P2

**Description**: Device-level failure detection is unit-tested in
`internal/dra/claims_test.go` (19 tests including failure conditions) and
`internal/controller/dra_status_test.go` (13 tests including failure
conditions and condition management). No e2e test exercises device failure
recovery.

**Impact**: Low risk. DRA driver-reported failures are an edge case that
depends on driver behavior. The unit test coverage is thorough. The fail
path (condition set, status updated) is well-covered.

**Recommendation**: Defer to Phase 9. Consider adding when a DRA driver
that can simulate failures is available in the dev environment.

**Deferred to**: Phase 9.

---

## G6: CEL selector validation at API level

**Severity**: P2

**Description**: Device request selectors are passed through as strings
without CEL syntax validation at the webhook level. Malformed CEL
expressions are only caught at DRA driver allocation time.

**Impact**: Users can submit RTJs with invalid CEL selectors that will
fail at allocation time rather than at admission time. The error is
surfaced through DRA claim status, not through webhook rejection.

**Recommendation**: Defer. CEL syntax validation at admission time would
require importing the CEL evaluation engine. The current fail-at-allocation
behavior is acceptable and consistent with how Kubernetes handles CEL
expressions in other contexts (e.g., ValidatingAdmissionPolicy).

**Deferred to**: Future phase (if requested).

---

## G7: Multi-device-class per claim

**Severity**: P2

**Description**: Phase 8 supports one DeviceClass per claim. Multiple
claims with different device classes are supported (e.g., GPU + RDMA), but
a single claim cannot express priority-ordered device class alternatives
(e.g., "prefer A100, fall back to H100").

**Impact**: None for Phase 8 scope. Multi-class per claim was explicitly
deferred in Session 1 Decision 6.

**Deferred to**: Future phase (per design).

---

## G8: Vague Kubernetes API version boundary

**Severity**: P1

**Description**: The Phase 8 index.md states "Kubernetes v1.33+ is assumed
for stable DRA support" and the implementation uses
`k8s.io/api/resource/v1beta1`. The note says "The pinned Kubernetes API
version must be validated against DRA GA status." However, no document
explicitly records whether `v1beta1` is the correct API version for the
pinned k8s.io/api@v0.34.2 or whether v1 is available and preferred.

**Impact**: Session 3 resolved this (OQ1: `v1beta1` chosen as stable beta
target matching Kubernetes 1.34), but the resolution is buried in the
session handoff. The index.md note still reads as unresolved.

**Recommendation**: Update the index.md DRA API version note to state the
resolution: `v1beta1` is the chosen API version for k8s.io/api@v0.34.2.
Mark the note as resolved.

---

## Summary

| Gap | Severity | Category | Deferred to |
|-----|----------|----------|-------------|
| G1: observeDRAClaimStatus wiring | P1 | Observation | Phase 9 S1 |
| G2: syncDeviceResumeFingerprint wiring | P1 | Telemetry | Phase 9 S1 |
| G3: Metric emission wiring | P1 | Observability | Phase 9 S1 |
| G4: DRA + ProvisioningRequest e2e | P2 | Test coverage | Phase 9 |
| G5: Device failure recovery e2e | P2 | Test coverage | Phase 9 |
| G6: CEL selector validation | P2 | Input validation | Future |
| G7: Multi-device-class per claim | P2 | Feature | Future |
| G8: API version boundary docs | P1 | Documentation | Fixable now |

**P0 gaps**: None. Phase 8 is signable.

**P1 gaps (3 wiring + 1 doc)**: G1, G2, G3 are co-located wiring tasks
that can be completed in a single follow-up session. G8 is a one-line doc
fix. None block signoff because the underlying logic is implemented and
unit-tested. The gaps affect runtime observability, not correctness or
safety.

**P2 gaps (4)**: All explicitly deferred by design. No action needed for
Phase 8 signoff.
