# Phase 4 Signoff

- Date: 2026-03-25
- Phase: 4 — Topology-Aware Admission Pipeline
- Status: **SIGNED OFF**

---

## What Phase 4 Can Do

Phase 4 extends the checkpoint-native preemption controller with topology-aware
scheduling and custom admission-check gating. When Phase 4 features are enabled,
the operator:

1. **Synthesizes topology-aware Workloads.** When `spec.topology` is set on an
   RTJ, the operator includes `TopologyRequest` fields on PodSets in the
   synthesized Kueue Workload. Kueue TAS uses these to compute topology-aware
   PodSetAssignments. Supported modes: Required, Preferred, Unconstrained.
   Leader-worker co-location is supported via `PodSetGroupName`.

2. **Gates launch on admission-check readiness.** A custom `ResumeReadiness`
   AdmissionCheck controller evaluates checkpoint completeness, age, and
   compatibility before clearing the admission gate. The evaluator is stateless
   and re-validates on every admission cycle (safe across preemption boundaries).
   Configurable via `ResumeReadinessPolicy` CRD with failure policy (FailOpen /
   FailClosed) and initial-launch bypass.

3. **Materializes topology assignments into child JobSet constraints.** The
   operator parses Kueue's compressed `TopologyAssignment` format and injects
   topology domain labels as `nodeSelector` entries on child JobSet pod templates.
   Supports single-domain and homogeneous multi-domain assignments. Refuses to
   launch with clear status condition when assignments are not representable.

4. **Preserves topology across pause/resume.** When an RTJ is paused and resumed,
   the operator re-evaluates topology from the current Workload admission and
   applies it to the new child JobSet. Global step monotonicity is maintained.

5. **Reports launch state in status fields.** Three new status structs provide
   visibility: `status.launchReadiness` (gate state), `status.topology`
   (admitted domains), `status.effectiveLaunchShape` (worker count, world size,
   resume mode, selected checkpoint).

6. **Observes Phase 4 operations.** 8 Prometheus metrics track gate outcomes,
   topology launches, assignment waits, resume attempts, and unsupported shapes.
   Shell scripts and Makefile targets support inspection and demo workflows.

---

## What Remains Optional

1. **ProvisioningRequest cloud profile (G5).** The operator does not implement
   ProvisioningRequest logic. PodSet synthesis is compatible with Kueue's
   built-in ProvisioningRequest AdmissionCheck — no operator changes needed.
   Documented as optional; the local dev profile works without cloud resources.

2. **Preferred topology mode.** Implemented and unit-tested, but not separately
   e2e-tested because kind cluster behavior is identical to Required mode at
   the capacity used. Works in production where Kueue TAS distinguishes
   Required from Preferred scheduling.

3. **Leader-worker co-location.** Implemented and unit-tested (PodSetGroupName
   grouping). No deploy sample or e2e test yet. Works for multi-PodSet JobSet
   templates.

---

## What Remains Deferred

1. **Per-pod topology placement via scheduling gates.** The operator uses
   nodeSelector injection, which supports a subset of Kueue TAS assignments.
   Full per-pod placement (heterogeneous multi-domain) requires scheduling-gate
   support on the child JobSet, which conflicts with the plain-runtime contract.
   Deferred to a future phase that may relax this constraint.

2. **Preemption + re-admission e2e test.** The evaluator is stateless and
   unit-tested for resume-after-preemption. No e2e test exercises the full
   preemption -> re-queue -> re-gate -> re-launch cycle. Deferred pending
   reliable preemption triggers in kind.

3. **Resharding combined with topology.** Phase 3 world-size-flexible resume
   (DCP resharding) and Phase 4 topology are both implemented but not tested
   in combination. No known conflict but not verified end-to-end.

4. **Partial admission combined with topology.** Phase 3 partial admission
   (experimental) and Phase 4 topology are both implemented but not tested
   in combination. No known conflict but not verified end-to-end.

5. **Elastic Workloads.** Deferred since Phase 3. Phase 4 handles topology-aware
   placement, not live in-place scaling.

6. **Multi-cluster / MultiKueue.** Deferred since Phase 0. No multi-cluster
   topology semantics defined.

---

## Main Known Risks

1. **AdmissionCheck identification by name pattern.** The launch gate identifies
   the resume-readiness AdmissionCheck by matching the check name against the
   controller name constant. If the AdmissionCheck is named without the expected
   pattern, the gate may be skipped. Mitigated by: the operator and its
   AdmissionCheck are deployed together; all deploy configs and e2e tests use
   the expected naming. Improvement: look up by `spec.controllerName` instead
   of name string.

2. **Heterogeneous topology shapes not representable.** When Kueue TAS assigns
   pods to domains with different higher-level labels, the operator cannot
   express this in a single nodeSelector. The operator fails with a clear status
   condition and metric (`unsupported_topology_shape_failures_total`).
   Improvement: scheduling-gate support (deferred).

3. **Kueue TAS availability.** The Topology CRD may not be present in all Kueue
   installations. The smoke test and e2e helpers gracefully handle missing TAS.
   When TAS is absent, topology features degrade to Phase 3 behavior. The
   operator does not crash or block.

4. **OQ-6 unresolved.** The interaction between ProvisioningRequest and TAS
   admission checks on the same ClusterQueue is not fully verified. This
   affects only the optional cloud path (G5) and does not impact local
   Phase 4 functionality.

---

## What Phase 5 Should Build Next

Based on the gaps and deferral list, recommended Phase 5 priorities:

1. **Scheduling-gate support for full per-pod topology.** Unlock heterogeneous
   multi-domain assignments by allowing the operator to set scheduling gates
   on individual pods. Requires reconsidering the plain-runtime child JobSet
   contract — the operator would need limited post-creation mutation rights.

2. **Preemption-cycle integration test.** Build a reliable preemption trigger
   in kind and test the full preemption -> re-queue -> re-admission ->
   re-gate -> re-launch cycle with both topology and readiness gates.

3. **Resharding + topology combination test.** Verify that world-size-flexible
   resume works correctly when topology constraints change across attempts.

4. **Metrics dashboards and alerting guidance.** Provide Grafana dashboard
   templates and alert threshold recommendations for Phase 4 metrics.

5. **ProvisioningRequest cloud profile (G5).** If cloud environments are in
   scope, verify PodSet synthesis compatibility with ProvisioningRequest
   admission checks and document the combined TAS + ProvisioningRequest
   ClusterQueue configuration.

6. **Elastic Workloads.** If live scaling enters scope, design the interaction
   between topology constraints and dynamic pod count changes.

---

## Test Coverage Summary

### Unit Tests

| Area | Test Count | Verdict |
|------|-----------|---------|
| RTJ topology API validation | 14 | Pass |
| ResumeReadinessPolicy webhook | 9 | Pass |
| Topology request synthesis | 15 | Pass |
| AdmissionCheck controller | 10 | Pass |
| Readiness evaluator | 15 | Pass |
| Workload reconciler | 9 | Pass |
| Topology assignment parser | 17 | Pass |
| Topology injection | 7 | Pass |
| Controller (Phase 4) | 7 | Pass |
| Render (Phase 4) | 5 | Pass |
| **Total** | **108** | **All pass** |

### E2E Tests

| Test | What It Proves |
|------|---------------|
| `TestResumeReadinessGate` | Held queue blocks launch; readiness gate clears on release; child JobSet is plain runtime; status fields populated |
| `TestTopologyAwareLaunch` | Topology synthesized on Workload; child JobSet has nodeSelector; pods land on correct topology domain |
| `TestTopologyAwareResume` | Topology persists across pause/resume; global step monotonicity maintained; effectiveLaunchShape has checkpoint ID |

---

## Contract Compliance

All 11 audited contracts are **COMPLIANT**. See
[consistency-audit.md](review/consistency-audit.md) for detailed evidence.

No blocking gaps found. See [gaps.md](review/gaps.md) for hardening
opportunities and deferred items.

---

## Phase 4 Goal Completion

| Goal | Description | Status |
|------|-------------|--------|
| G1 | Topology-aware Workload synthesis | Complete |
| G2 | Topology-aware runtime materialization | Complete |
| G3 | ResumeReadiness AdmissionCheck controller | Complete |
| G4 | Admission-gated launch and resume | Complete |
| G5 | Optional ProvisioningRequest cloud profile | Deferred (optional) |
