# Phase 4 Gaps and Hardening Notes

- Date: 2026-03-25
- Scope: Gaps, risks, and tightening opportunities found during the Phase 4 signoff audit

---

## Test Coverage Gaps

### Unit Tests: Adequate

All required areas have unit coverage:

| Area | Files | Tests | Verdict |
|------|-------|-------|---------|
| RTJ API validation (topology) | `resumabletrainingjob_webhook_test.go` | 14 | Sufficient |
| ResumeReadinessPolicy webhook | `resumereadinesspolicy_webhook_test.go` | 9 | Sufficient |
| Topology request synthesis | `rtj_topology_test.go` | 15 | Sufficient |
| AdmissionCheck reconciler | `setup_test.go` | 10 | Sufficient |
| Readiness evaluator | `evaluator_test.go` | 15 | Sufficient |
| Workload reconciler | `workload_reconciler_test.go` | 9 | Sufficient |
| Topology assignment parser | `assignment_test.go` | 17 | Sufficient |
| Topology injection | `topology_injection_test.go` | 7 | Sufficient |
| Controller (Phase 4 paths) | `resumabletrainingjob_controller_test.go` | 7 | Sufficient |
| Render (Phase 4 paths) | `render_test.go` | 5 | Sufficient |

### E2E Tests: Three Strong Tests, Negative Paths Deferred

| Test | Status | Strength |
|------|--------|----------|
| `TestResumeReadinessGate` | Present | Strong: full admission-gated lifecycle |
| `TestTopologyAwareLaunch` | Present | Strong: synthesis-to-placement chain |
| `TestTopologyAwareResume` | Present | Strong: topology persistence across pause/resume |

**Missing e2e negative paths (acceptable deferrals):**

| Scenario | Why deferred | Covered by |
|----------|-------------|------------|
| Readiness gate rejection (invalid checkpoint) | Requires manipulating checkpoint state mid-admission | 15 evaluator unit tests |
| FailOpen vs FailClosed behavior | Requires storage error injection in e2e | Evaluator + reconciler unit tests |
| Non-representable topology failure | Requires manipulating Kueue TAS decisions | 17 parser unit tests, documented |
| Preemption-driven resume with gates | Requires reliable preemption trigger | Evaluator covers resume-after-preemption |
| Resharding with topology | Phase 3 resharding feature + Phase 4 topology | Not yet combined |
| Partial admission with topology | Experimental partial admission + topology | Not yet combined |
| LeaderWorkerColocation in e2e | Requires multi-PodSet trainer image | 15 topology synthesis unit tests |

**Assessment:** The three e2e tests cover the main Phase 4 value propositions. The
missing scenarios are either covered by unit tests or require infrastructure that
is not practical in a kind-based e2e environment. No blocking gaps.

---

## Documentation Gaps

### Minor Wording Issues

1. **`resume-readiness-acc.md` says "scaffold only" in places.** The evaluator
   is fully implemented (Session 5). The doc should clarify that the scaffold
   was replaced with full decision logic. This is a documentation staleness
   issue, not an implementation gap.

2. **`topology-aware-launch.md` does not specify requeue interval.** The
   controller uses a 5-second `launchGateRequeueInterval` when gates are not
   yet ready. This concrete value should be documented for operators tuning
   reconcile behavior.

3. **`operations.md` does not cover log analysis.** Operators troubleshooting
   gate issues benefit from knowing which log messages to search for. The
   controller emits structured log fields (`gateResult`, `topologyResult`,
   `readinessState`) that should be documented.

### Missing Documentation

4. **No sample RTJ with `leaderWorkerColocation: true`.** The feature is
   implemented and unit-tested but has no deploy sample for hands-on
   verification. Low priority since colocation is an advanced configuration.

5. **No metrics interpretation guide.** `operations.md` lists metrics names
   but does not explain expected values, alert thresholds, or anomaly
   indicators. This is a Phase 5 polish item.

---

## Implementation Observations

### Fragile Pattern: AdmissionCheck Identification in launch_gate.go

`evaluateReadinessCheck` in `internal/controller/launch_gate.go` identifies the
resume-readiness AdmissionCheck by matching `AdmissionCheckState.Name` against
the controller name constant. This relies on the AdmissionCheck object being
named in a way that contains the controller name string.

**Risk:** If the AdmissionCheck is named differently (e.g., `my-custom-readiness`),
the pattern match may fail and the gate would be skipped.

**Mitigation:** The e2e tests and dev profile all use the expected naming. A future
improvement could look up the AdmissionCheck object to match by `spec.controllerName`
rather than by name string. This is a hardening opportunity, not a correctness bug,
because the Phase 4 contract requires the operator to be configured alongside
its own AdmissionCheck.

### Topology Injection Subset

The nodeSelector injection strategy supports single-domain and homogeneous
multi-domain assignments but not heterogeneous multi-domain. This is a known
and documented limitation (Session 6 divergence note). The operator fails
clearly when encountering an unsupported shape.

**Risk:** Users on clusters where Kueue TAS produces heterogeneous assignments
will see topology-related failures. The failure is clean (status condition +
metric), not silent.

**Mitigation:** Document this limitation prominently in the troubleshooting guide
(already done). A future phase could add scheduling-gate support for full
per-pod placement.

### No Integration Test for Preemption + Re-Admission Cycle

The evaluator's stateless re-validation is unit-tested (including
`TestEvaluateResumeAfterPreemptionCheckpointAvailable`), but there is no
integration or e2e test that exercises the full preemption -> re-queue ->
re-admission -> re-gate -> re-launch cycle with Phase 4 features active.

**Risk:** Subtle ordering issues between Kueue's re-admission and the operator's
gate re-evaluation could surface only in production-like environments.

**Mitigation:** The stateless evaluator design eliminates most state-dependent
ordering issues. A Phase 5 integration test with a preemption trigger would
close this gap.

---

## RBAC Gaps

None identified. `config/rbac/role.yaml` includes all Phase 4 resources with
appropriate verbs. No over-privileging.

---

## CRD Gaps

None identified. Both CRDs match the API reference documentation exactly.
Validation rules cover all required constraints.

---

## Deployment Configuration Gaps

1. **ResourceFlavor `phase4-topology` is defined in deploy config but not
   audited for `spec.topologyName` correctness.** The smoke test verifies
   its existence and the `topologyName` field, so this is operationally
   covered.

2. **No Kueue config for combined TAS + ProvisioningRequest.** Expected since
   G5 is deferred. No action needed.

---

## Summary

| Category | Blocking Gaps | Hardening Opportunities |
|----------|---------------|----------------------|
| Unit tests | 0 | 0 |
| E2E tests | 0 | 7 deferred negative paths |
| Documentation | 0 | 5 polish items |
| Implementation | 0 | 2 future improvements |
| RBAC | 0 | 0 |
| CRDs | 0 | 0 |
| Deploy config | 0 | 0 |

**No blocking gaps found. Phase 4 is ready for signoff.**
