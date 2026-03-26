# Phase 4 Consistency Audit

- Date: 2026-03-25
- Scope: Verify Phase 4 implementation against locked contracts from Phases 0-4

## Methodology

Audited all Phase 4 source files, tests, CRDs, RBAC, deploy configs, and
documentation against the contracts established in:
- Phase 0: v1 Contract Pack (authority model, fail-closed resume)
- Phase 1: Manual pause/resume vertical slice (operator-trainer protocol)
- Phase 2: Native Kueue integration (RTJ as only Kueue-managed object, plain child JobSet)
- Phase 3: Admission-aware launch, flavor-aware resume, partial admission
- Phase 4: ADRs 0001-0003, goals G1-G5, architecture, API reference

---

## Contract 1: RTJ as Only Kueue-Managed Admission Object

**Origin:** Phase 2 (locked), reinforced in Phase 3 and Phase 4 ADR 0001 D1.

**Verdict: COMPLIANT.**

Evidence:
- `internal/kueue/rtj_podsets.go` synthesizes PodSets for the RTJ Workload. No
  second Workload is created for the child JobSet.
- `internal/kueue/rtj_topology.go` adds TopologyRequest to RTJ-level PodSets,
  not to child JobSet objects.
- `internal/controller/launch_gate.go` reads admission state from the RTJ's
  Workload, not from any child-level Workload.
- `config/rbac/role.yaml` grants workloads access for the RTJ controller only.
- E2E tests (`phase4_helpers_test.go:assertChildJobSetPlainRuntime`) assert no
  Kueue management labels on child JobSets.
- No code path creates a `kueue.x-k8s.io/queue-name` label on the child JobSet.

---

## Contract 2: Child JobSet Remains Plain Runtime

**Origin:** Phase 2 (locked), reinforced in Phase 4 ADR 0001 D2.

**Verdict: COMPLIANT.**

Evidence:
- `internal/jobset/render.go:stripKueuePodTemplateLabels` explicitly removes
  `kueue.x-k8s.io/` prefixed labels and annotations from pod templates.
- `internal/jobset/render_test.go` tests confirm queue-name and workload-priority
  labels are stripped, non-Kueue labels are preserved.
- `internal/jobset/topology_injection.go` adds only `nodeSelector` entries (topology
  domain labels) to pod templates. No Kueue management metadata.
- Controller test `TestReconcileChildJobSetHasNoKueueManagementLabels` verifies
  no Kueue labels leak.
- All three e2e tests verify child JobSet is plain runtime via
  `assertChildJobSetPlainRuntime`.

---

## Contract 3: Topology-Aware Workload Synthesis (G1)

**Origin:** Phase 4 ADR 0001 D3, ADR 0002 D1-D4.

**Verdict: COMPLIANT.**

Evidence:
- `internal/kueue/rtj_topology.go:applyTopologyRequests` maps TopologyMode to
  Kueue PodSetTopologyRequest fields:
  - Required -> `TopologyRequest.Required = &topologyLevel`
  - Preferred -> `TopologyRequest.Preferred = &topologyLevel`
  - Unconstrained -> `TopologyRequest.Unconstrained = ptr.To(true)`
  - Disabled/nil -> no TopologyRequest
- Sub-group metadata always populated when topology active: PodIndexLabel,
  SubGroupIndexLabel, SubGroupCount.
- LeaderWorkerColocation uses PodSetGroupName for same-domain co-location.
- 15 unit tests cover all modes, colocation, worker resolution, Phase 3 compat.
- E2E `TestTopologyAwareLaunch` verifies Workload carries TopologyRequest.

---

## Contract 4: Custom AdmissionCheck Gating (G3)

**Origin:** Phase 4 ADR 0001 D4, ADR 0003 D1-D8.

**Verdict: COMPLIANT.**

Evidence:
- Controller name: `training.checkpoint.example.io/resume-readiness` (ADR 0003 D1).
- Wired into single operator binary (ADR 0003 D2).
- `ResumeReadinessPolicy` is cluster-scoped CRD (ADR 0003 D3).
- Four policy fields implemented: `requireCompleteCheckpoint`, `maxCheckpointAge`,
  `failurePolicy`, `allowInitialLaunchWithoutCheckpoint` (ADR 0003 D4-D6).
- Active condition based on policy existence (ADR 0003 D8).
- Evaluator is a pure function with 5 decision paths mapped to 3 Kueue states
  (Ready, Retry, Rejected).
- 15 evaluator tests + 9 reconciler tests cover all decision paths.
- E2E `TestResumeReadinessGate` verifies held queue blocks launch.

---

## Contract 5: Topology-Aware Runtime Materialization (G2)

**Origin:** Phase 4 ADR 0001 D8, Session 6 divergence note.

**Verdict: COMPLIANT with documented limitation.**

Evidence:
- `internal/topology/assignment.go` parses Kueue compressed TopologyAssignment
  format (Universal/Individual values, prefix/suffix, multi-slice).
- Representability checks enforce:
  - Single-domain: always representable (all level labels -> nodeSelector).
  - Multi-domain, homogeneous higher levels: representable (common labels -> nodeSelector).
  - Multi-domain, heterogeneous higher levels: NOT representable (fails with clear condition).
  - Multi-domain, single level: NOT representable (no OR in nodeSelector).
- `internal/jobset/topology_injection.go` merges topology labels into existing
  nodeSelector without overwriting user-specified labels.
- 17 topology parser tests + 7 injection tests cover all scenarios.

**Documented limitation:** NodeSelector injection is a subset of Kueue's built-in
scheduling-gate approach. Kueue-managed JobSets get per-pod placement via
scheduling gates; RTJ child JobSets get per-template placement via nodeSelector.
This is the correct trade-off given the plain-runtime child JobSet contract.

---

## Contract 6: Admission-Gated Launch Pipeline (G4)

**Origin:** Phase 4 ADR 0001 D4, topology-aware-launch.md.

**Verdict: COMPLIANT.**

Evidence:
- `internal/controller/launch_gate.go:evaluateLaunchGates` evaluates sequential
  gates: ResumeReadiness check state, then topology assignment validation.
- `internal/controller/launch_plan.go:buildLaunchPlan` synthesizes all launch
  parameters (counts, flavors, topology) into a single LaunchPlan.
- Controller integration: gates evaluated only when topology enabled OR
  WorkloadReference present. Phase 3 path skipped entirely otherwise.
- Status fields populated: `status.launchReadiness`, `status.topology`,
  `status.effectiveLaunchShape`.
- 7 controller tests cover gate blocking, topology launch, flavor coexistence,
  non-representable failure, Phase 3 preservation, label containment.
- All 3 e2e tests verify status field population.

---

## Contract 7: Phase 3 Backward Compatibility

**Origin:** Phase 4 ADR 0001 D6, migration-from-phase3.md.

**Verdict: COMPLIANT.**

Evidence:
- When `spec.topology` is nil:
  - No defaulting occurs (webhook test: `TestWebhookDefaultPreservesPhase3SpecWithoutTopology`).
  - No validation runs on topology fields.
  - All new status fields stay nil.
  - No gate evaluation in controller.
  - Phase 3 reconcile path followed exactly.
- When no ResumeReadiness AdmissionCheck is on the ClusterQueue:
  - Gate evaluation skips readiness check.
  - Phase 3 behavior preserved.
- Controller test: `TestReconcilePhase3BehaviorPreservedWithoutTopology`.
- Topology tests: `TestPodSetsTopologyDisabledPreservesPhase3ExactBehavior`.
- Phase 3 pool labels maintained alongside Phase 4 topology labels in dev env.

---

## Contract 8: Fail-Closed Resume Semantics

**Origin:** Phase 0 (locked), reinforced through all phases.

**Verdict: COMPLIANT.**

Evidence:
- Evaluator defaults: `failurePolicy=FailClosed`, `requireCompleteCheckpoint=true`.
- Storage errors with FailClosed -> Retry (wait, don't launch with stale data).
- Non-representable topology -> controller refuses to launch (fails with status condition).
- No fallback to older checkpoints.
- Resume still uses only latest compatible complete checkpoint.

---

## Contract 9: Stateless Re-Validation (Preemption Safety)

**Origin:** Phase 4 ADR 0001 D7, OQ-5 resolution.

**Verdict: COMPLIANT.**

Evidence:
- Evaluator is a pure function with no cached state.
- Workload reconciler re-runs full evaluation on every reconcile.
- No state persists across preemption/re-admission boundaries.
- Evaluator tests cover resume-after-preemption scenario.

---

## Contract 10: ProvisioningRequest Compatibility (G5)

**Origin:** Phase 4 ADR 0001 D5.

**Verdict: COMPLIANT (deferred, not implemented).**

Evidence:
- No ProvisioningRequest operator code landed.
- PodSet synthesis follows Kueue conventions (standard PodSet fields, TopologyRequest).
- The operator does not prevent Kueue from attaching a ProvisioningRequest
  AdmissionCheck alongside the ResumeReadiness check.
- OQ-6 remains open (ProvisioningRequest + TAS interaction not fully verified).
- Documented as optional in goals.md, session-handoff.md, and this audit.

---

## Contract 11: Pinned Dependency Versions

**Origin:** Phase 2 (locked), Phase 4 Session 1 Decision 8.

**Verdict: COMPLIANT.**

Evidence:
- All Kueue API types used (`PodSetTopologyRequest`, `TopologyAssignment`,
  `AdmissionCheck`, `AdmissionCheckState`) confirmed present in Kueue v0.15.1
  (OQ-1, OQ-2, OQ-3 resolved in Sessions 3-4).
- No new dependency versions introduced in Phase 4.

---

## Cross-Cutting Concerns

### RBAC
- `config/rbac/role.yaml` includes Phase 4 rules: admissionchecks (get/list/watch),
  admissionchecks/status (get/update/patch), resumereadinesspolicies
  (get/list/watch), resumereadinesspolicies/status (get/update/patch).
- No over-privileging detected.

### Metrics
- 8 new metrics follow existing namespace/subsystem conventions.
- Emitted from correct code paths (gate evaluation, topology launch, resume).
- No metrics leak to Phase 3 code paths when features disabled.

### CRDs
- `ResumableTrainingJob` CRD includes all Phase 4 spec/status fields.
- `ResumeReadinessPolicy` CRD is cluster-scoped with correct fields and defaults.
- Both CRDs have proper validation (enum, required fields, forbidden combos).

---

## Summary

| Contract | Verdict |
|----------|---------|
| RTJ as only Kueue-managed object | COMPLIANT |
| Child JobSet plain runtime | COMPLIANT |
| Topology-aware Workload synthesis | COMPLIANT |
| Custom AdmissionCheck gating | COMPLIANT |
| Topology-aware materialization | COMPLIANT (documented limitation) |
| Admission-gated launch pipeline | COMPLIANT |
| Phase 3 backward compatibility | COMPLIANT |
| Fail-closed resume semantics | COMPLIANT |
| Stateless re-validation | COMPLIANT |
| ProvisioningRequest compat | COMPLIANT (deferred) |
| Pinned versions | COMPLIANT |

**Overall: All locked contracts are maintained. No drift from Phase 4 design.**
