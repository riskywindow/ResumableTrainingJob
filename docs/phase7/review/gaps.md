# Phase 7 Gaps Analysis

**Date**: 2026-04-01
**Scope**: Identify drift from the locked Phase 7 design, open gaps, and
areas requiring tightening.

---

## Drift Assessment

### No Significant Drift Detected

The implementation closely follows the locked design from Session 1. All ten
Session 1 decisions are implemented as specified. The four-gate launch sequence,
five-state provisioning classification, topology second-pass handling, and
waitForPodsReady eviction detection all match the design documents.

Minor deviations documented below are expected implementation refinements, not
design drift.

---

## Open Questions Status

| OQ | Topic | Status | Assessment |
|----|-------|--------|------------|
| OQ1 | Kueue v0.15.1 ProvisioningRequest AC maturity | **Resolved** (Session 4) | Types confirmed, controller integration complete. |
| OQ2 | waitForPodsReady eviction condition format | **Resolved** (Session 5) | `Evicted` condition with `reason: PodsReadyTimeout` confirmed. |
| OQ3 | Topology assignment timing with ProvisioningRequest | **Open** | Conservative gate handles both cases. Needs combined topology+provisioning e2e. |
| OQ4 | ProvisioningRequest cleanup on yield/preemption | **Open** | Kueue likely handles cleanup since it owns PR lifecycle. Needs verification. |
| OQ5 | Fake backend scope | **Resolved** (Session 1) | Separate Deployment. |
| OQ6 | Multi-cluster provisioning status mirroring | **Partially resolved** (Session 8) | Adapter mirror works. Full multi-cluster provisioning e2e deferred. |
| OQ7 | Backoff behavior surfacing | **Open** | Retry state captured internally. Not surfaced in status API beyond Pending mapping. |
| OQ8 | Feature gate naming | **Open** | `ProvisioningACNames` is a reconciler field, not a formal feature gate. |

### Assessment of Open Items

**OQ3** is low risk: the conservative gate (wait for both AC Ready AND topology
assigned) is correct regardless of Kueue's actual behavior. If Kueue provides
topology in the same pass as AC readiness, the gate passes immediately. If Kueue
provides topology in a second pass, the gate waits correctly.

**OQ4** is low risk: Kueue owns the ProvisioningRequest lifecycle. When Kueue
suspends a Workload (yield/preemption), it handles AC cleanup. The RTJ operator
does not create or delete ProvisioningRequests. Worst case: orphaned PRs in dev
environments (cleaned up by namespace deletion).

**OQ7** is cosmetic: Retry maps to Pending in the external API. Operators see
`provisioningState: Pending` during retries. Adding a `retryCount` or
`lastRetryReason` field would improve observability but is not required for
correctness.

**OQ8** is a naming decision: `--provisioning-ac-names` is a CLI flag, which is
sufficient for Phase 7. Promoting to a formal feature gate (CRD field or
controller config) is a Phase 8 consideration if multi-tenant AC name management
becomes necessary.

---

## Gaps by Category

### Gap 1: Booking-Expiry E2E Test (Deferred)

**Severity**: Low
**Impact**: The `booking-expiry.fake.dev` provisioning class is implemented
in the fake provisioner (16 unit tests cover the action computation) but has no
e2e test.

**Why deferred**: Requires either a long observation window (default 60s expiry)
or short-expiry tuning. The unit tests cover the state machine completely. The
delayed-success and permanent-failure e2e tests cover the two primary production
paths.

**Recommendation**: Add in Phase 8 with short-expiry tuning parameter. Not
required for Phase 7 signoff.

---

### Gap 2: Recovery Timeout E2E Test (Deferred)

**Severity**: Low
**Impact**: The recovery timeout path (pods reach Ready, then lose readiness on
a subsequent run) is covered by 9 integration tests but has no e2e test.

**Why deferred**: Requires a running job to successfully checkpoint, then fail
on resume with a different image or resource starvation. More complex fixture
setup than the startup timeout test.

**Recommendation**: Add in Phase 8 when checkpoint infrastructure is more mature.
The integration tests (including `TestReconcileRecoveryTimeoutClassification` and
`TestReconcileCheckpointPreservedOnRecoveryTimeout`) provide strong coverage.

---

### Gap 3: Topology + Provisioning Combined E2E (Deferred)

**Severity**: Medium
**Impact**: OQ3 cannot be fully validated without an environment that runs both
Kueue TAS (Topology-Aware Scheduling) and ProvisioningRequest simultaneously.

**Why deferred**: Requires Kueue TAS support in the kind environment, which is
a separate infrastructure investment. The conservative launch gate handles both
possible Kueue behaviors correctly.

**Recommendation**: Add when Kueue TAS is available in the dev profile. The
unit tests in `topology_test.go` and `view_test.go` cover all topology state
transitions.

---

### Gap 4: podSetUpdate Materialization E2E (Deferred)

**Severity**: Low
**Impact**: podSetUpdates are applied in unit tests (17 tests) but the e2e tests
do not explicitly verify that AC-suggested nodeSelectors or tolerations appear
on the child JobSet pods.

**Why deferred**: The fake provisioner does not inject podSetUpdates in its
current implementation. A more sophisticated fake backend or a real provisioning
backend would be needed.

**Recommendation**: Extend fake provisioner to inject sample podSetUpdates in a
future session. Unit coverage is strong.

---

### Gap 5: ProvisioningRequest Cleanup Verification (OQ4)

**Severity**: Low
**Impact**: The RTJ operator does not create or delete ProvisioningRequests.
Kueue owns the PR lifecycle. Whether Kueue properly cleans up PRs on
yield/preemption is not verified.

**Why low risk**: This is Kueue's responsibility, not the RTJ operator's. The
observation-only design (Session 3) explicitly avoids PR mutations. In the worst
case, orphaned PRs are harmless in dev and cleaned up by namespace deletion.

**Recommendation**: Document as a Kueue dependency. Verify in integration with
upstream Kueue if ProvisioningRequest cleanup matters for production.

---

### Gap 6: Enum Count Discrepancy

**Severity**: Cosmetic
**Impact**: Session 2 documents "7 new enum types" but the implementation has
6 distinct enum types in the API (`LaunchGateState`, `ProvisioningState`,
`StartupState`, `PodsReadyState`, `TopologyGateState`, `AdmissionCheckState`).
The seventh may be an internal-only type or a documentation overcount.

**Recommendation**: Update Session 2 documentation to clarify the actual count.
No code change needed.

---

### Gap 7: Backoff Behavior Not Surfaced in Status API (OQ7)

**Severity**: Low
**Impact**: When Kueue retries a ProvisioningRequest (Retry state), the RTJ
status shows `provisioningState: Pending` with no indication that a retry
occurred. Operators must inspect Workload conditions directly to see retry
history.

**Recommendation**: Consider adding `provisioningAttempt` or `lastRetryReason`
to `ProvisioningStatus` in Phase 8. Not required for Phase 7 correctness.

---

### Gap 8: Feature Gate Formalization (OQ8)

**Severity**: Low
**Impact**: Phase 7 activation is controlled by the `--provisioning-ac-names`
CLI flag. There is no formal feature gate CRD field or controller configuration
resource. This is sufficient for single-operator deployments but may be
insufficient for multi-tenant environments.

**Recommendation**: Defer to Phase 8. The CLI flag is the simplest correct
mechanism and matches the Kueue model (ClusterQueue-level AC configuration).

---

## Wording Tightened

The following areas had vague wording in the design docs that the implementation
has now concretized:

1. **"Launch gate checks all ACs"** is now precisely: "Gate 2 iterates
   `workload.Status.AdmissionChecks` and returns blocked if any check has
   `State != Ready`. When `AdmissionChecks` is empty, the gate passes (Phase 6
   fail-open behavior)."

2. **"Topology second-pass handling"** is now precisely: "`SecondPassPending`
   is true when `TopologyEnabled && !anyPodSetHasTopologyAssignment &&
   (anyDelayedTopologyIsPending || noDelayedTopologyExists)`."

3. **"Conflict detection is fail-fast"** is now precisely: "For each key in
   `podSetUpdate.NodeSelector`, if the rendered template already has that key
   with a different value, `ApplyPodSetUpdates` returns `Applied: false` with
   a message identifying the field, key, existing value, and update value.
   Same-value overwrites are allowed. Labels and annotations are always merged
   (no conflict detection). Tolerations are appended and deduplicated."

4. **"waitForPodsReady eviction reuses yield path"** is now precisely: "When
   `stopSource == stopSourceKueue` and `job.Status.WorkloadReference != nil`,
   `detectAndRecordEviction()` is called before entering the stop flow. The
   eviction is classified via `ClassifyEviction()` which reads
   `Evicted=True, reason=PodsReadyTimeout` from Workload conditions. The
   existing yield path is entered unchanged."

5. **"Phase 6 backward compatibility is unconditional"** is now precisely:
   "The launch gate evaluation path is entered only when
   `job.IsTopologyEnabled() || job.Status.WorkloadReference != nil ||
   len(r.ProvisioningACNames) > 0`. When `ProvisioningACNames` is empty,
   `BuildView()` returns `ProvisioningNotConfigured` and `AllChecksReady: true`.
   Phase 7 status sections remain nil."
