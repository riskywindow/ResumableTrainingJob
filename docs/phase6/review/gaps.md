# Phase 6 Gaps and Tightening Items

**Date:** 2026-03-30
**Scope:** Drift from locked Phase 6 design, vague wording, missing instrumentation, and hardening items.

---

## Gap Classification

- **G-DRIFT**: Design drift from locked contract (none found).
- **G-INSTRUMENT**: Metrics declared but not actively emitted.
- **G-HARDEN**: Code or docs that should be tightened but are functionally correct.
- **G-DOC**: Documentation that is vague or incomplete.

---

## G-INSTRUMENT-1: Phase 6 Metrics Not Actively Emitted

**Severity:** Low
**Status:** Accepted (deferred to Phase 7 active-tracking refactor)

Phase 6 defines nine Prometheus metrics (registered in `internal/metrics/metrics.go` lines 384-458) and their recorder methods. However, the `reconcileManagerIntent` controller path does not currently call most of these recorder methods on every reconciliation cycle:

| Metric | Recorder Method | Called From Controller? |
|--------|----------------|----------------------|
| `rtjs_by_execution_role` | `ObserveExecutionRole` | No |
| `remote_rtjs_by_cluster` | `ObserveRemoteCluster` | No |
| `manager_local_suppressions_total` | `IncManagerLocalSuppression` | No |
| `remote_status_sync_successes_total` | `IncRemoteStatusSyncSuccess` | No |
| `remote_status_sync_failures_total` | `IncRemoteStatusSyncFailure` | No |
| `remote_pause_events_total` | `IncRemotePauseEvent` | No |
| `remote_resume_events_total` | `IncRemoteResumeEvent` | No |
| `remote_checkpoint_observations_total` | `IncRemoteCheckpointObservation` | No |
| `shared_store_access_failures_total` | `IncSharedStoreAccessFailure` | No |

**Why this is acceptable for Phase 6 signoff:**
- The metrics infrastructure (registration, recorder types, helper methods) is complete.
- The controller status-update path functionally achieves the same observability via status fields (`multiCluster.dispatchPhase`, `multiCluster.remotePhase`, etc.).
- Wiring recorder calls into `reconcileManagerIntent` is a mechanical task that does not affect correctness.

**Recommendation for Phase 7:**
Add explicit `r.Metrics.IncManagerLocalSuppression()`, `r.Metrics.IncRemoteStatusSyncSuccess()`, etc. at the appropriate points in `reconcileManagerIntent`. This is a ~20-line change.

---

## G-HARDEN-1: Manager Reconcile Requeue Strategy

**Severity:** Low
**Status:** Acceptable

`reconcileManagerIntent` requeues with a fixed 5-second interval when a pause is requested but the remote is still active. This is functionally correct but uses a magic constant:

```go
return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
```

The 5-second interval matches `launchGateRequeueInterval` (used in Phase 4 launch gating) but is duplicated as a literal. Both should reference the same constant or a separate `managerRequeueInterval` constant.

**Impact:** None on correctness. Minor code hygiene.

**Recommendation:** Extract to a named constant `managerPollInterval` in a future cleanup pass.

---

## G-HARDEN-2: hasRemoteStatusSignal Heuristic

**Severity:** Low
**Status:** Acceptable with documentation

The `hasRemoteStatusSignal` function uses a heuristic to detect whether the adapter has mirrored remote status:

```go
func hasRemoteStatusSignal(job *...) bool {
    return job.Status.ActiveJobSetName != "" || job.Status.CurrentRunAttempt > 0
}
```

This works because:
1. The manager never sets `activeJobSetName` (it suppresses all runtime).
2. The manager never increments `currentRunAttempt`.
3. Therefore, non-zero values can only come from the adapter mirroring worker status.

**Risk:** If a future phase introduces a manager-side action that sets either field, this heuristic breaks. The function's doc comment (lines 119-125 of `remote_status.go`) explains the reasoning, which is sufficient.

**Recommendation:** No change needed. The heuristic is sound for all Phase 6 and foreseeable Phase 7 use cases. If Phase 7+ adds manager-side run attempts, this function must be revisited.

---

## G-HARDEN-3: Webhook managedBy Immutability Edge Case

**Severity:** Low
**Status:** Acceptable

The webhook rejects changes to `spec.managedBy` on update (line 105 of `resumabletrainingjob_webhook.go`):

```go
if oldCopy.Spec.ManagedBy != newCopy.Spec.ManagedBy {
    allErrs = append(allErrs, field.Invalid(...))
}
```

This correctly prevents both mutation and removal. However, it also prevents setting `managedBy` on an RTJ that was originally created without it. This is by design (the field is user-authored at creation time), but the error message says "immutable once set" which could confuse a user whose old value was empty.

**Recommendation:** Tighten the error message to distinguish the two cases:
- Old non-empty to different: "managedBy is immutable once set"
- Old empty to non-empty: "managedBy must be set at creation time; it cannot be added to an existing RTJ"

This is a UX improvement, not a correctness issue.

---

## G-DOC-1: Demo Doc Checkpoint Timing Assumptions

**Severity:** Low
**Status:** Acceptable

`docs/phase6/demo.md` Demo 3 (remote pause) instructs the user to "wait for at least one checkpoint" but provides no timeout guidance:

> Wait for the worker to report at least one checkpoint on the manager.

For demo purposes, the checkpoint interval in the sample RTJ is typically 30s. The doc should mention this expected wait time.

**Recommendation:** Add a note: "With the default sample RTJ, the first checkpoint completes within 60 seconds of reaching Running phase."

---

## G-DOC-2: Operations Doc Metrics Not Scraped by Default

**Severity:** Low
**Status:** Acceptable

`docs/phase6/operations.md` lists nine Phase 6 metrics but does not mention that the default metrics endpoint (`:8080/metrics`) must be scraped by a Prometheus instance. This is standard Kubernetes operator practice and not Phase 6-specific.

**Recommendation:** Add a one-line note referencing the controller-runtime metrics server bind address (`--metrics-bind-address`).

---

## G-DOC-3: Troubleshooting Gap - Stale Remote Status After Worker Restart

**Severity:** Low
**Status:** Acceptable

`docs/phase6/troubleshooting.md` covers six failure scenarios but does not cover the case where a worker cluster's operator restarts mid-run. In this scenario, the worker RTJ's status is preserved (it's in etcd), so the adapter continues mirroring the last-known state. The manager sees stale data until the worker operator re-reconciles.

**Impact:** Transient staleness only. The worker operator's restart triggers a re-reconcile which refreshes status. No data loss.

**Recommendation:** Add a brief note to troubleshooting: "If the worker operator restarts, manager-side remote status may be stale for up to one reconcile cycle (~10-30 seconds)."

---

## Summary

| ID | Classification | Severity | Action |
|----|---------------|----------|--------|
| G-INSTRUMENT-1 | Instrumentation | Low | Deferred to Phase 7 |
| G-HARDEN-1 | Code hygiene | Low | Future cleanup |
| G-HARDEN-2 | Heuristic documentation | Low | Sufficient as-is |
| G-HARDEN-3 | Webhook UX | Low | Optional improvement |
| G-DOC-1 | Demo documentation | Low | Optional improvement |
| G-DOC-2 | Operations documentation | Low | Optional improvement |
| G-DOC-3 | Troubleshooting coverage | Low | Optional improvement |

**No design drift (G-DRIFT) items were found.** The implementation is consistent with the locked Phase 6 design across all five goals and all key decisions. All gaps are low-severity and do not block signoff.
