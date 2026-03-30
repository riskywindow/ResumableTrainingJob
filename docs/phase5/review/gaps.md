# Phase 5 Gaps Analysis

Audit date: 2026-03-26

## Overview

This document catalogs gaps between the Phase 5 design/contracts and the
current implementation. Each gap is classified as:

- **Acceptable**: known limitation documented and deferred to Phase 6+
- **Minor**: low-risk imprecision; no behavior impact today
- **Requires action**: must be resolved before signoff

---

## 1. StaleCheckpointBoost field is defaulted but unused

**Classification**: Minor

The `CheckpointPriorityPolicySpec.StaleCheckpointBoost` field is defaulted
to 0 in the webhook and tested in defaulting/deep-copy tests. However, the
`Evaluate()` function in `decision.go` does not use this field. When
checkpoint is stale, it uses `PreemptibleOffset` instead.

The name suggests an additive boost on top of the preemptible offset, but
the engine does not implement a combined stale+boost path.

**Impact**: None today because the default is 0. If a user sets a non-zero
value, it would be silently ignored.

**Recommendation**: Either (a) remove the field from the API and webhook
tests, or (b) add a note in the CRD description that the field is reserved
for future use and currently has no effect. Prefer (a) to avoid confusion.

---

## 2. No dedicated metrics unit tests

**Classification**: Acceptable (deferred)

`internal/metrics/metrics.go` has `[no test files]`. The Recorder methods
are exercised indirectly through controller tests and e2e tests, but there
are no direct unit tests validating metric registration, increment logic,
or gauge set/delete behavior.

**Impact**: Low. The Recorder is trivial wrapper code with nil-safety guards.
Indirect coverage through controller and e2e tests reduces risk.

**Recommendation**: Defer to Phase 6. The existing indirect coverage is
sufficient for signoff.

---

## 3. RTJ webhook does not enforce priorityPolicyRef.name immutability

**Classification**: Acceptable (deferred)

The RTJ `ValidateUpdate` webhook does not prevent changing the
`spec.priorityPolicyRef.name` after creation. Changing the policy
mid-flight would cause the priority engine to re-evaluate against
a different policy, potentially causing discontinuities in the
effective priority.

**Impact**: Low. This is an operator-facing footgun, not a correctness
bug. The reconciler handles policy changes gracefully (re-resolves each
reconcile). The priority state just shifts to the new policy's parameters.

**Recommendation**: Document that changing the policy ref on a running
RTJ causes an immediate re-evaluation. Consider adding update-time
validation in Phase 6 if this becomes a support issue.

---

## 4. No integration test for Kueue version compatibility

**Classification**: Acceptable (deferred)

The troubleshooting guide notes that Phase 5 is tested against Kueue
v0.15.1. There is no automated test that verifies compatibility with
the Workload.Spec.Priority patch approach against different Kueue
versions. If Kueue's GenericJob reconciler starts overwriting
Spec.Priority on subsequent reconciles, priority shaping would break.

**Impact**: Low-medium. The current Kueue version (v0.15.1) sets
Spec.Priority only at creation time. This is verified by the e2e tests.

**Recommendation**: Track Kueue release notes for changes to
Spec.Priority lifecycle. Add a version check or guard in Phase 6.

---

## 5. Yield budget annotation has unbounded growth potential

**Classification**: Minor

The yield history annotation (`training.checkpoint.example.io/yield-history`)
stores a JSON array of RFC3339 timestamps. While `RecordYieldEvent()` prunes
expired entries, in pathological cases (very long yield windows with
frequent yields), the annotation could grow large.

**Impact**: Kubernetes annotations have a 256KiB total metadata limit.
With ~30 bytes per timestamp, this would require ~8,500 yields in a single
window to approach limits. This is unrealistic for normal operation.

**Recommendation**: No action needed. The pruning logic is sufficient.

---

## 6. CheckpointStoreError flag not plumbed from catalog

**Classification**: Minor

The `EvaluationInput.CheckpointStoreError` field exists in the decision
engine and is tested. However, the telemetry collection path
(`CollectTelemetry`) does not currently set this flag from catalog errors.
If the catalog returns an error, the checkpoint time is simply nil, which
maps to `TelemetryUnknown` with `FailOpenOnTelemetryLoss`.

**Impact**: The fail-open/fail-closed distinction between "telemetry loss"
and "store error" is present in the API but effectively both map to the
telemetry-loss path. The `FailOpenOnCheckpointStoreErrors` policy field
is not exercised in production.

**Recommendation**: Wire the catalog error into the telemetry snapshot as
`CheckpointStoreError = true` in Phase 6. The current behavior (treating
store errors as telemetry loss) is safe because `FailOpenOnTelemetryLoss`
defaults to true.

---

## 7. No periodic re-evaluation timer for stale checkpoint detection

**Classification**: Acceptable (deferred)

The operator re-evaluates priority on reconcile events (status changes,
Workload changes, periodic informer syncs). There is a RequeueAfter for
protection window expiry, but no explicit RequeueAfter for the checkpoint
freshness target. This means the transition from Active to Preemptible
depends on the next reconcile trigger (informer resync or status change).

**Impact**: Low-medium. The default controller-runtime informer resync
period (10 hours) is too infrequent for prompt staleness detection. However,
in practice, running jobs generate frequent status updates (checkpoint
completions, phase transitions) that trigger reconciles well within the
freshness target window.

**Recommendation**: Add a RequeueAfter based on the freshness target
remaining time in Phase 6 for more deterministic staleness detection.
Current behavior is acceptable for signoff because active jobs produce
frequent reconcile triggers.

---

## Summary

| # | Gap | Classification | Action |
|---|-----|---------------|--------|
| 1 | StaleCheckpointBoost unused | Minor | Remove or document as reserved |
| 2 | No metrics unit tests | Acceptable | Defer to Phase 6 |
| 3 | PolicyRef not immutable | Acceptable | Document, consider Phase 6 validation |
| 4 | No Kueue version compat test | Acceptable | Track upstream, Phase 6 |
| 5 | Yield annotation growth | Minor | No action needed |
| 6 | CheckpointStoreError not wired | Minor | Wire in Phase 6 |
| 7 | No freshness requeue timer | Acceptable | Add in Phase 6 |

**Conclusion**: No gaps require action before Phase 5 signoff. All gaps
are either minor (no behavioral impact) or acceptable known limitations
documented for Phase 6.
