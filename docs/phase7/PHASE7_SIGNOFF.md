# Phase 7 Signoff

**Date**: 2026-04-01
**Phase**: Capacity-Guaranteed Launch
**Status**: Signed off for Phase 8 handoff

---

## What Phase 7 Can Do

Phase 7 delivers capacity-guaranteed launch: RTJ child runtime is not created
until physical capacity is confirmed available, not just quota-reserved.

### Capabilities

1. **Provisioning-gated launch.** When a ProvisioningRequest AdmissionCheck is
   configured on the ClusterQueue, the RTJ operator holds child JobSet creation
   until the AC reaches Ready state. This ensures the underlying infrastructure
   (node pools, accelerators) has been provisioned before pods are created.

2. **Four-gate launch evaluation.** The launch gate checks, in order:
   - Quota reservation (Workload `status.admission` is present).
   - All AdmissionChecks ready (ProvisioningRequest, ResumeReadiness, any others).
   - Topology second-pass not pending (delayed topology request satisfied).
   - Topology assignment present (when topology is configured on the RTJ).

3. **Provisioning state observability.** `status.provisioning` surfaces the
   current provisioning lifecycle (NotConfigured, Pending, Provisioned, Failed)
   with the derived ProvisioningRequest name and attempt number.

4. **Launch gate status.** `status.launchGate` shows the aggregate gate state
   (Open, Blocked, Unknown) with a per-AdmissionCheck summary and human-readable
   reason/message.

5. **Capacity guarantee indicator.** `status.capacity.guaranteeActive` is true
   when the RTJ is running with provisioned capacity, distinguishing quota-only
   admission from capacity-guaranteed admission.

6. **waitForPodsReady startup/recovery detection.** When Kueue evicts a Workload
   due to waitForPodsReady timeout, the RTJ operator classifies the eviction as
   startup timeout (first run) or recovery timeout (subsequent run), records the
   classification in `status.startupRecovery`, and enters the existing graceful
   yield path.

7. **podSetUpdate application.** AdmissionCheck-suggested nodeSelectors,
   tolerations, labels, and annotations are applied additively to child JobSet
   pod templates. Conflicting values are detected before launch (fail-fast).

8. **Fake ProvisioningRequest backend.** A separate Deployment
   (`cmd/fake-provisioner/`) provides three deterministic classes for local dev:
   delayed success (10s), permanent failure, and booking expiry. No real
   cluster-autoscaler required.

9. **Multi-cluster compatibility.** Worker clusters run the full Phase 7 path.
   Manager clusters suppress local runtime. Phase 7 status is visible on the
   manager via adapter mirroring.

10. **Observability.** Nine Prometheus metrics cover launch gate states,
    provisioning observations, blocked launches, timeout events, and
    capacity-guaranteed launch count. Six inspection scripts and seven Makefile
    targets support demo and troubleshooting.

### Test Coverage

| Category | Count | Key Tests |
|----------|-------|-----------|
| Unit tests | 149 | Provisioning classification, topology parsing, podSetUpdate application, startup/recovery state machine, fake provisioner action computation |
| Integration tests | 12 | Launch gate controller, startup/recovery reconcile loops, multi-cluster status preservation |
| E2E tests | 5 | Capacity-guaranteed launch, provisioning failure, startup timeout, multi-cluster backward compat smoke, multi-cluster capacity gate smoke |
| **Total** | **166** | |

---

## What Remains Optional

These items are implemented but not required for Phase 7 correctness:

1. **Booking-expiry fake provisioner class.** The `booking-expiry.fake.dev`
   provisioning class is implemented (16 unit tests) but not e2e tested.
   Used only in advanced demo scenarios.

2. **Backoff behavior detail in status API.** Kueue Retry state is captured
   internally as `ProvisioningRetry` and mapped to `Pending` in the external
   status API. Operators can inspect Workload conditions for retry details.

3. **Phase 7 metrics on the manager.** Manager clusters log Phase 7 remote state
   but do not emit Phase 7 Prometheus metrics for worker-side provisioning.
   Metrics are worker-local.

---

## What Remains Deferred

These items are explicitly deferred to Phase 8 or beyond:

| Item | Reason | Prerequisite |
|------|--------|--------------|
| Booking-expiry e2e test | Requires long observation window or short-expiry tuning | Tunable fake backend parameters |
| Recovery timeout e2e test | Requires running job to checkpoint then fail on resume | Mature checkpoint fixtures |
| Topology + provisioning combined e2e | Requires Kueue TAS in dev profile | TAS-enabled kind environment |
| podSetUpdate materialization e2e | Requires fake backend that injects podSetUpdates | Enhanced fake provisioner |
| Worker-side Phase 7 provisioning e2e in multi-cluster | Requires ProvisioningRequest CRD + fake provisioner on worker clusters | Three-cluster Phase 7 profile |
| ProvisioningRequest cleanup verification (OQ4) | Kueue owns PR lifecycle | Upstream Kueue investigation |
| Feature gate formalization (OQ8) | CLI flag is sufficient for Phase 7 | Multi-tenant use case analysis |
| Backoff detail in status API (OQ7) | Retry maps to Pending externally | Operator feedback on retry visibility |

---

## Known Risks

### Risk 1: OQ3 -- Topology Assignment Timing (Medium)

**Description**: The interaction between ProvisioningRequest completion and
topology assignment delivery by Kueue has not been validated in a combined
e2e environment. If Kueue provides topology before the ProvisioningRequest AC
is Ready, the RTJ operator's gate ordering may cause unnecessary waiting.

**Mitigation**: The conservative gate (wait for both AC Ready AND topology
assigned) is correct in all cases. The worst outcome is a brief wait (one
reconcile cycle) if topology arrives before provisioning. No incorrect launches.

### Risk 2: Kueue Version Dependency (Low)

**Description**: Phase 7 depends on Kueue v0.15.1 behavior for ProvisioningRequest
naming convention (`{workload}-{check}-{attempt}`), AdmissionCheck states, and
Eviction condition format. Future Kueue versions may change these.

**Mitigation**: All Kueue-dependent behavior is isolated in `internal/provisioning/`
and `internal/controller/startup_recovery.go`. String constants are defined locally.
Upgrading Kueue requires updating these constants and re-running tests.

### Risk 3: Fake Provisioner in Production (Low)

**Description**: The fake provisioner (`cmd/fake-provisioner/`) is designed for
dev/test only. If accidentally deployed to a production cluster, it would auto-approve
or auto-reject ProvisioningRequests.

**Mitigation**: The fake provisioner uses `*.fake.dev` provisioning class names
that would not match production ProvisioningRequestConfig objects. Production
environments use cloud-specific provisioning backends (e.g., cluster-autoscaler).

### Risk 4: Status Section Size (Low)

**Description**: Four new status sections add to the RTJ status object size.
For clusters with many RTJs, this increases etcd storage and API server bandwidth.

**Mitigation**: Phase 7 status sections are small (< 500 bytes each) and nil when
Phase 7 is not configured. The total status size increase is comparable to Phase 6
(`multiCluster` status section).

---

## What Phase 8 Should Build Next

### Priority 1: Booking-Expiry and Capacity-Revocation Path

Complete the booking-expiry e2e test and verify the capacity revocation ->
requeue path works end-to-end. This validates the third ProvisioningRequest
lifecycle (success -> revocation) that maps to real-world scenarios where
reserved capacity expires or is reclaimed.

### Priority 2: Enhanced Fake Provisioner

Extend the fake provisioner to:
- Inject sample podSetUpdates (nodeSelector, tolerations) in the AC response.
- Support configurable retry behavior.
- Enable podSetUpdate materialization e2e testing.

### Priority 3: Topology + Provisioning Combined Validation

Set up a Kueue TAS-enabled kind environment and validate the interaction between
ProvisioningRequest completion and topology assignment. Resolve OQ3 conclusively.

### Priority 4: Recovery Timeout E2E

Build a test fixture where an RTJ successfully runs and checkpoints, then fails
on resume (e.g., image change, resource starvation), triggering a recovery timeout.
Verify checkpoint preservation and requeue behavior.

### Priority 5: Feature Gate Formalization

Evaluate whether `--provisioning-ac-names` should be promoted from a CLI flag to
a controller configuration CRD field or a formal feature gate. Consider multi-tenant
scenarios where different ClusterQueues have different provisioning AC configurations.

### Priority 6: Backoff Observability

Surface Kueue retry/backoff state in the RTJ status API. Consider adding
`provisioningAttempt` to track retry count and `lastRetryReason` for
operational visibility.

---

## Signoff Checklist

| Criterion | Status |
|-----------|--------|
| RTJ is the only Kueue-managed object | Verified |
| Child JobSets are plain runtime | Verified |
| Capacity-guaranteed launch gating works | Verified (unit + e2e) |
| ProvisioningRequest observation is correct | Verified (unit + e2e) |
| Topology second-pass awareness works | Verified (unit) |
| waitForPodsReady startup/recovery detection works | Verified (unit + integration + e2e) |
| podSetUpdate application with conflict detection works | Verified (unit) |
| Phase 6 behavior preserved when Phase 7 absent | Verified (unit + e2e) |
| Multi-cluster compatibility preserved | Verified (unit + integration + e2e smoke) |
| Status API is backward-compatible | Verified (unit + webhook tests) |
| Demo is documented end-to-end | Verified (demo.md, 3 scenarios) |
| Observability is operational | Verified (9 metrics, 6 scripts, 7 Makefile targets) |
| No new roadmap scope added | Verified |
| All deferred items documented | Verified (gaps.md) |

**Phase 7 is signed off.** Proceed to Phase 8.
