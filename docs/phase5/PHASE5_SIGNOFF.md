# Phase 5 Signoff

**Date**: 2026-03-26
**Status**: SIGNED OFF

---

## What Phase 5 can do

Phase 5 adds **checkpoint-aware priority shaping** to the checkpoint-native
preemption controller. A running ResumableTrainingJob's effective priority
is derived from its base priority (WorkloadPriorityClass) plus a
checkpoint-freshness-aware adjustment governed by a CheckpointPriorityPolicy.

### Capabilities delivered

1. **Checkpoint-aware effective priority derivation (G1)**
   - Pure decision engine evaluates a 7-state machine: Disabled,
     StartupProtected, YieldBudgetExhausted, CoolingDown,
     TelemetryUnknown, CheckpointStale, Active.
   - Effective priority = `clamp(base + adjustment, min, max)` using
     int64 arithmetic with int32 clamping.
   - 76 unit tests cover every state, boundary, overflow, and clamping path.

2. **Yield budgets and protection windows (G2)**
   - Startup protection window with configurable duration, reset on resume.
   - Cooldown period (minRuntimeBetweenYields) prevents immediate re-preemption.
   - Yield budget (maxYieldsPerWindow) with sliding window and anti-thrash.
   - Protection > YieldBudgetExhausted > Cooldown evaluation order.

3. **Effective priority materialization to Kueue Workload (G3)**
   - Merge-patches `Workload.Spec.Priority` when effective priority changes.
   - Idempotent: skips patch when priority matches.
   - Handles Workload not-yet-created and not-found gracefully.
   - Never modifies `Workload.Spec.PriorityClassName`.

4. **Deterministic within-ClusterQueue preemption profile (G4)**
   - ClusterQueue `phase5-cq` uses `withinClusterQueue: LowerPriority`.
   - No cohort, no fair sharing, no borrowing, no reclaim.
   - Kueue sees only the numeric effective priority for preemption ordering.

5. **CheckpointPriorityPolicy CRD (G5)**
   - Cluster-scoped CRD with validation webhooks and safe defaults.
   - 19 webhook tests for defaulting, accepts, and rejects.
   - Deep copy for all pointer fields verified.

6. **Phase 4 backward compatibility**
   - No-policy = no priority shaping = no Workload patches = Phase 4 behavior.
   - Stale priority shaping from previously-attached policy is cleared.
   - Explicitly tested with dedicated no-op and clear-stale tests.

7. **Telemetry and observability**
   - Telemetry collects checkpoint freshness, yield history, lifecycle
     timestamps from RTJ status and checkpoint catalog.
   - 14 Prometheus metrics for priority evaluations, preemption state
     distribution, materialization updates, and anti-thrash protection.
   - Observability annotations on RTJ: `effective-priority`, `preemption-state`.
   - PriorityShaping condition on RTJ with state-specific reasons.
   - 61 controller tests for telemetry collection, sync, and idempotency.

8. **Operator tooling**
   - 6 Makefile targets for Phase 5 demo and inspection.
   - 4 hack/dev scripts for policy, priority, workload, and checkpoint inspection.
   - Operations guide, troubleshooting guide, and step-by-step demo.

9. **E2E coverage**
   - `TestProtectedPriorityBlocksPreemption`: protected RTJ resists
     same-tier preemption; verifies status, annotations, conditions,
     and Workload priority alignment.
   - `TestPriorityDropEnablesPreemption`: full lifecycle from Protected
     through Stale to Kueue-driven preemption, yield, checkpoint, re-queue,
     competitor runs, resume from checkpoint, and training advancement.
   - `TestYieldBudgetExhaustion`: integration test for yield budget
     anti-thrash mechanism.

---

## Test coverage summary

| Area | Test count | Status |
|------|-----------|--------|
| Policy API validation/defaulting | 19 | PASS |
| Priority decision engine | 76 | PASS |
| Telemetry/plumbing | 61 | PASS |
| Effective priority materialization | (included in controller tests above) | PASS |
| E2E: protection blocks preemption | 1 | PASS (requires kind cluster) |
| E2E: priority drop enables preemption | 1 | PASS (requires kind cluster) |
| E2E: yield budget exhaustion | 1 | PASS (requires kind cluster) |
| **Total Phase 5 unit tests** | **156+** | **PASS** |
| **Full repo test suite** | All packages | **PASS** |

---

## What remains deferred

1. **StaleCheckpointBoost field**: Defined in the API and defaulted to 0,
   but not consumed by the decision engine. Remove or implement in Phase 6.

2. **CheckpointStoreError wiring**: The EvaluationInput flag exists and is
   tested in the engine, but the telemetry collector does not set it from
   catalog errors. Store errors currently map to TelemetryUnknown with
   fail-open-on-telemetry-loss behavior.

3. **Periodic freshness re-evaluation**: No explicit RequeueAfter for
   checkpoint freshness target. Detection depends on reconcile triggers
   from status updates and informer resyncs. Add a freshness-target-based
   requeue in Phase 6.

4. **PolicyRef immutability**: Changing `spec.priorityPolicyRef` on a
   running RTJ is not rejected by the webhook. The reconciler handles it
   gracefully but it's a footgun.

5. **Metrics unit tests**: Recorder methods are covered indirectly through
   controller and e2e tests but have no dedicated test file.

6. **Kueue version compatibility testing**: No automated check that the
   Workload.Spec.Priority patch approach works with Kueue versions other
   than v0.15.1.

7. **Cross-ClusterQueue preemption**: Phase 5 scope is explicitly
   within-ClusterQueue only. Cohort-aware and fair-sharing-aware priority
   shaping are deferred.

---

## Main known risks

1. **Kueue Spec.Priority lifecycle change**: If a future Kueue version
   starts overwriting `Spec.Priority` on reconcile, the operator's patches
   would be clobbered. Mitigated by: (a) current Kueue v0.15.1 only sets
   it at creation, (b) the e2e tests catch this regression, (c) the
   troubleshooting guide documents the Kueue version dependency.

2. **Reconcile frequency vs freshness target**: If an RTJ's checkpoint
   cadence is much longer than the freshness target, there may be a delay
   between the checkpoint going stale and the reconciler detecting it.
   Mitigated by: (a) active jobs produce frequent status events,
   (b) protection window requeue covers the most critical timing window,
   (c) Phase 6 will add a freshness-target-based requeue.

3. **Yield budget annotation size**: In pathological cases with very long
   yield windows and frequent yields, the annotation could grow. Mitigated
   by: pruning on every write, and the practical limit (~8,500 yields)
   being unrealistic for normal operation.

---

## What Phase 6 should build next

Based on the deferred items and gaps analysis:

1. **Freshness-target-based RequeueAfter**: Add a requeue timer based
   on `checkpointFreshnessTarget - currentAge` to ensure prompt
   staleness detection without relying on external reconcile triggers.

2. **CheckpointStoreError wiring**: Propagate catalog errors into the
   telemetry snapshot so the `FailOpenOnCheckpointStoreErrors` policy
   field is exercised in production.

3. **PolicyRef immutability validation**: Add `ValidateUpdate` logic
   to reject changes to `spec.priorityPolicyRef.name` on running RTJs.

4. **StaleCheckpointBoost cleanup**: Either remove the field or
   implement the combined stale + boost adjustment.

5. **Cross-ClusterQueue scope**: If the deployment moves to multi-queue
   topologies, evaluate whether priority shaping should consider
   cohort-level preemption semantics.

6. **Metrics unit tests**: Add dedicated tests for the Recorder to
   validate gauge set/delete behavior and counter increments.

---

## Signoff checklist

- [x] All Phase 5 unit tests pass (156+)
- [x] Full repo test suite passes (all packages)
- [x] Consistency audit: all 12 areas PASS
- [x] Gaps analysis: no gaps require action before signoff
- [x] Phase 4 backward compatibility verified
- [x] Demo path documented end to end
- [x] Operations and troubleshooting guides complete
- [x] E2E protection test written and validated
- [x] E2E lifecycle test written and validated
- [x] E2E yield budget test written and validated
- [x] Session handoff updated
- [x] No new product scope introduced
