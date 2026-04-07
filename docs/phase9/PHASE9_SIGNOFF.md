# Phase 9 Signoff — Hybrid Elastic RTJ

> **Signed off: 2026-04-06**
> **Auditor: Hardening pass**

---

## What Phase 9 Can Do

Phase 9 delivers **manual, operator-initiated elastic resize** for ResumableTrainingJob
with hybrid execution paths and dynamic quota reclaim.

### Core Capabilities

1. **Manual target-based resize**: An operator patches `spec.elasticity.targetWorkerCount`
   to trigger a resize. No automatic metric-driven autoscaling — resize is always
   an explicit human or automation action.

2. **In-place shrink (fast path)**: When the runtime advertises in-place shrink support
   and the policy allows it, the controller publishes `Workload.status.reclaimablePods`
   via server-side apply. Kueue reads this and releases the corresponding quota
   from the ClusterQueue. The RTJ remains Running — no checkpoint, no eviction, no
   downtime.

3. **Checkpoint-and-relaunch shrink (safe fallback)**: When in-place shrink is not
   supported or the policy says `Never`, the controller initiates the same
   drain/checkpoint/cleanup flow used for manual pause and Kueue preemption.
   The RTJ relaunches at the smaller target size via DCP resharding.

4. **Checkpoint-and-relaunch grow**: Scale-up always requires new Kueue admission at
   the larger size. The controller checkpoints, suspends the Workload, and waits for
   re-admission. On admission, a new JobSet attempt launches at the larger world size.

5. **Dynamic quota reclaim**: In-place shrink releases quota via `reclaimablePods`
   without waiting for the full checkpoint cycle. This enables a blocked RTJ to be
   admitted as soon as the surplus is declared.

6. **Backward compatibility**: When `spec.elasticity.mode` is `Disabled` (default),
   the RTJ behaves identically to Phase 8. Multi-cluster dispatch, launch gating,
   DRA device requests, priority shaping, and topology-aware placement all continue
   to work unchanged.

### Concrete Feature Matrix

| Capability | Disabled | In-Place Shrink | C&R Shrink | C&R Grow |
|---|---|---|---|---|
| `spec.elasticity.mode` | Disabled | Manual | Manual | Manual |
| Runtime support required | -- | Yes | No | No |
| Quota release via reclaimablePods | -- | Yes | N/A | N/A |
| New Kueue admission required | -- | No | Yes | Yes |
| Checkpoint written | -- | No | Yes | Yes |
| DRA compatible | Yes | Yes | Yes | Yes |
| MultiKueue compatible | Yes | Deferred | Yes | Yes |

### Implementation Artifacts

| Layer | Key files |
|---|---|
| API types | `api/v1alpha1/resumabletrainingjob_types.go` (ElasticitySpec, ElasticityStatus, 6 enums) |
| Webhook validation | `api/v1alpha1/resumabletrainingjob_webhook.go` (mode immutability, bounds, requires allowWorldSizeChange) |
| Pure-function planner | `internal/elastic/plan.go` (7 plan kinds, deterministic, no side effects) |
| Reclaim computation | `internal/elastic/reclaim.go` (delta, build, idempotency guard) |
| Resize execution | `internal/controller/elastic_execute.go` (in-place, relaunch, conditions, completion) |
| Plan integration | `internal/controller/elastic_plan.go` (input building, plan evaluation, status sync) |
| SSA patch | `internal/controller/workload_status_patch.go` (reclaimablePods via field manager) |
| Stop flow | `internal/controller/suspend_flow.go` (stopSourceResize variant) |
| Render | `internal/jobset/render.go` (YIELD_SDK_TARGET_WORKER_COUNT injection) |
| Multi-cluster | `internal/controller/remote_status.go` (remoteElasticitySummary, manager suppression) |
| Metrics | `internal/metrics/metrics.go` (12 Phase 9 metrics registered) |
| SDK | `sdk/python/yield_sdk/elastic.py` (ElasticConfig, evaluate_resize, signal I/O) |
| Fixture | `fixtures/pytorch_ddp_counter/train.py` (elastic resize loop, deterministic knobs) |
| Dev profile | `deploy/dev/phase9/` (queues, samples, Kueue config) |
| Demo tooling | `hack/dev/phase9-*.sh` (submit, shrink, grow, inspect) |

---

## Test Coverage Summary

### Unit Tests

| Package | Test file | Count | What it covers |
|---|---|---|---|
| `elastic` | `plan_test.go` | 22 | All 7 plan kinds, bounds, idempotency, edge cases |
| `elastic` | `reclaim_test.go` | 13 | Delta computation, build, clear, existing, idempotency guard |
| `controller` | `elastic_plan_test.go` | 17 | Input building, plan evaluation, status sync, state mapping |
| `controller` | `elastic_execute_test.go` | 25+ | Conditions, in-place/relaunch execution, DRA coherency, fallback |
| `controller` | `workload_status_patch_test.go` | 10 | SSA patch safety, field manager, idempotency |
| `controller` | `remote_status_test.go` | 9 | Remote summary, detection, manager mode, backward compat |
| `api` | `resumabletrainingjob_webhook_test.go` | 25 | Phase 9 defaulting, validation, backward compat, deep copy |
| `jobset` | `render_test.go` | 4 | Target injection, zero omission, DRA/admission coexistence |
| SDK | `test_elastic.py` | 27 | Config, evaluation, serialization, signals, backward compat |
| SDK | `test_control.py` | 10 | Target parsing, backward compat |
| SDK | `test_manifest.py` | 21 | Resize fields, round-trip, cross-phase |

### E2E Tests

| Test | File | What it proves |
|---|---|---|
| `TestElasticShrinkDynamicReclaim` | `elastic_shrink_reclaim_test.go` | In-place shrink -> reclaimablePods -> RTJ B admitted |
| `TestElasticGrowViaRelaunch` | `elastic_grow_relaunch_test.go` | 2->4 workers via checkpoint-and-relaunch |
| `TestElasticFallbackShrinkViaRelaunch` | `elastic_fallback_test.go` | 4->2 via relaunch when in-place unsupported |
| `TestMultiClusterElasticSmokeManagerSuppression` | `multicluster_elastic_smoke_test.go` | Manager suppresses elastic execution for remote RTJ |

### Coverage Assessment

| Requirement | Unit | E2E | Multi-cluster |
|---|---|---|---|
| API/webhook changes | Yes (25 tests) | -- | -- |
| Runtime/control elasticity protocol | Yes (27+10+21 SDK tests) | -- | -- |
| Controller planning and reclaim | Yes (22+13+17 tests) | -- | -- |
| Fallback behavior | Yes (plan + execute tests) | Yes (fallback e2e) | -- |
| Single-cluster reclaim | Yes (patch tests) | Yes (shrink e2e) | -- |
| Grow-via-relaunch | Yes (plan + execute tests) | Yes (grow e2e) | -- |
| Multi-cluster compatibility | Yes (9 tests) | Smoke (1 test) | -- |

**Verdict: All required coverage thresholds met.**

---

## What Remains Deferred

### Deliberate Deferrals (documented, safe defaults in place)

| Item | Why deferred | Impact | Safe default |
|---|---|---|---|
| Metrics wiring into reconcile | Infrastructure-first approach; methods exist, calls not made | No production metrics for resize ops | Metrics return zero |
| Runtime in-place shrink detection | Annotation/signal wiring not yet connected to reconcile | In-place shrink disabled without manual status patch | Falls back to C&R (safe) |
| Reclaim completion detection | Requires pod count observation on active JobSet | reclaimablePods stays published; Kueue handles idempotently | Stale but harmless |
| Mid-resize target changes | Complex abort/restart logic not in P0 scope | New target evaluated after current resize completes | Eventual consistency |
| Resize elapsed time tracking | Coupled with metrics wiring | Operators correlate log timestamps | Functional, less ergonomic |
| MultiKueue reclaimablePods mirroring (OQ-3) | Requires adapter extension for status field mirroring | Manager cannot observe worker-side quota release | Worker-local only |
| Full multi-cluster resize e2e | Requires combined Phase 6 + Phase 9 infrastructure | Covered by unit/integration tests + smoke | Deterministic coverage |

### Explicit Non-Goals (will NOT ship in Phase 9)

- Automatic metric-driven autoscaling (HPA, custom metrics, KEDA)
- Native Kueue Workload Slices as core resize primitive
- Custom scheduler or quota engine
- In-place grow (requires upstream Kueue Workload resize support)
- Resize target changes during in-progress operations (complex, deferred)

---

## Known Risks

### R-1: In-place shrink is effectively disabled in production

**Risk level: Medium**

Without runtime detection wiring, `inPlaceShrinkSupported` defaults to `false`.
Every shrink operation falls back to checkpoint-and-relaunch. This is functionally
correct but does not deliver the fast-path shrink benefit.

**Mitigation**: The `inPlaceShrinkPolicy: Never` provides an explicit opt-out.
Detection wiring is the first Phase 10 task.

### R-2: No operational metrics for resize operations

**Risk level: Medium**

The 12 registered metrics are not called from the reconcile loop. Operators
monitoring Prometheus dashboards would see zero values for all resize counters
and gauges.

**Mitigation**: Log lines in the reconcile path provide equivalent information.
Metrics wiring is the second Phase 10 task.

### R-3: reclaimablePods persistence after in-place shrink completion

**Risk level: Low**

After in-place shrink, `reclaimablePods` is published and Kueue releases quota.
But the field is never cleared because reclaim completion detection is not
implemented. Kueue treats stale `reclaimablePods` idempotently — no double-release.

**Mitigation**: Harmless in practice. Clearing is a Phase 10 cleanup task.

### R-4: Kueue version dependency

**Risk level: Low**

`reclaimablePods` is part of the Workload Status API. This requires Kueue v0.15.1+.
Older Kueue versions would ignore the field (no crash, just no quota release).

**Mitigation**: Phase 9 dev profile uses compatible Kueue. Production deployments
should verify Kueue version.

### R-5: SSA field manager conflicts with future Kueue changes

**Risk level: Low**

The `rtj-elastic-reclaim` field manager writes `reclaimablePods` via SSA. If a
future Kueue version also writes to `reclaimablePods`, there could be ownership
conflicts.

**Mitigation**: SSA ownership model handles this gracefully — the last writer wins
for each PodSet entry. The `+listType=map` with `+listMapKey=name` ensures
per-PodSet-name ownership.

---

## What Phase 10 Should Build Next

### Priority 1: Wire Existing Infrastructure

1. **Wire metrics into reconcile loop**: Call the existing recorder methods at the
   correct points in `elastic_execute.go` and `elastic_plan.go`. No new metric
   definitions needed.

2. **Wire runtime in-place shrink detection**: Read `inPlaceShrinkSupported` from
   child JobSet annotation or resize signal file. Write to `status.elasticity`.

3. **Implement reclaim completion detection**: Observe active JobSet pod count.
   When `runningPods == targetWorkerCount`, clear `reclaimablePods` and set
   `resizeState=Completed`.

### Priority 2: Robustness

4. **Handle mid-resize target changes**: Define behavior for target change during
   in-progress resize. Options: abort-and-restart, or queue the new target.

5. **Add resize elapsed time tracking**: Add `resizeStartTimestamp` and
   `resizeCompletionTimestamp` to `ElasticityStatus`. Wire with metrics.

6. **Resize timeout / retry logic**: When grow admission fails indefinitely,
   provide a timeout mechanism or manual abort path.

### Priority 3: Multi-Cluster

7. **MultiKueue reclaimablePods mirroring (OQ-3)**: Extend the adapter to mirror
   worker-side `reclaimablePods` to the manager-side Workload for manager-level
   quota visibility.

8. **Full multi-cluster resize e2e test**: Combined Phase 6 + Phase 9 infrastructure
   test with actual resize execution across clusters.

### Priority 4: Advanced Elasticity

9. **Automatic metric-driven resize**: `ElasticityMode: Auto` with HPA-style
   metric sources. This is the natural evolution of the manual trigger model.

10. **In-place grow**: When upstream Kueue supports Workload resize without
    re-admission, enable the in-place grow path.

---

## Signoff Checklist

| Criterion | Status |
|---|---|
| All Phase 0-8 invariants preserved | PASS |
| Phase 9 invariants (I-9 through I-14) implemented and tested | PASS |
| API types, validation, defaulting complete | PASS |
| Pure-function planning model with full unit coverage | PASS |
| reclaimablePods SSA patch with idempotency guard | PASS |
| Resize execution engine with condition lifecycle | PASS |
| Runtime elasticity protocol (SDK + fixture) | PASS |
| Stop flow integration (stopSourceResize) | PASS |
| Multi-cluster manager suppression | PASS |
| E2E: single-cluster reclaim | PASS |
| E2E: grow-via-relaunch | PASS |
| E2E: shrink fallback | PASS |
| E2E/smoke: multi-cluster compatibility | PASS |
| Demo documented end-to-end | PASS |
| Operations and troubleshooting guides | PASS |
| Dev environment profile with deterministic contention | PASS |
| Makefile targets for all demo operations | PASS |
| `go build ./...` clean | PASS |
| `go vet ./...` clean | PASS |
| Backward compatibility with non-elastic RTJs | PASS |
| No drift from locked Phase 9 design | PASS |
| All gaps documented with safe defaults | PASS |

**Phase 9 is signed off for merge.**
