# ADR 0001: Capacity-Guaranteed Launch

## Status

Accepted

## Date

2026-03-30

## Context

### Problem

In Phase 6, the RTJ operator launches the child JobSet as soon as Kueue
admits the workload (quota is reserved). Quota reservation does not
guarantee that physical nodes are available. When a cluster-autoscaler
must scale up nodes, pods sit in Pending state for minutes. This creates
two problems:

1. **Ambiguous startup failures**: If a startup timeout fires, the operator
   cannot distinguish "nodes are still scaling" from "the training image
   is broken." This delays debugging and wastes GPU-hours.

2. **Wasted scheduling work**: Pods that are created before nodes are
   ready consume scheduler bandwidth and create noise in monitoring.

### Options considered

**Option A: Custom capacity gate in RTJ operator**

The RTJ operator creates its own ProvisioningRequest-like resource and
watches it. This is rejected because:
- It duplicates Kueue's existing ProvisioningRequest AdmissionCheck.
- It creates a second source of truth for capacity state.
- It requires the RTJ operator to understand cloud-provider APIs.

**Option B: Kueue's built-in ProvisioningRequest AdmissionCheck**

Configure Kueue's existing AdmissionCheck mechanism to gate admission on
a ProvisioningRequest. The RTJ operator reads the Workload's AdmissionCheck
state and only launches when all checks pass. This is selected because:
- It uses existing Kueue infrastructure.
- It keeps RTJ out of the provisioning business.
- It is testable with a fake backend.
- It is compatible with multiple real backends (GKE NAP, Karpenter).

**Option C: External webhook gate**

An external webhook blocks admission until capacity is confirmed. Rejected
because it adds operational complexity and Kueue already provides the
needed mechanism.

## Decision

### Phase 7 uses Kueue's built-in ProvisioningRequest AdmissionCheck

The RTJ operator does not create, manage, or directly interact with
ProvisioningRequest resources. Instead:

1. **Cluster admin configures** an AdmissionCheck of type
   `kueue.x-k8s.io/provisioning-request` on the ClusterQueue.
2. **Kueue creates** ProvisioningRequest resources for pending Workloads.
3. **A backend** (fake or real) satisfies the ProvisioningRequest.
4. **Kueue updates** the Workload's AdmissionCheck state to Ready.
5. **RTJ operator reads** the Workload state and opens the launch gate
   only when ALL AdmissionChecks are Ready.

### Launch gate contract

The launch gate opens when ALL of the following are true:

| Input | Source | Required |
|---|---|---|
| Workload admitted | `Workload.status.admission != nil` | Always |
| All ACs Ready | `Workload.status.admissionChecks[*].state == Ready` | When ACs configured |
| Topology assigned | `Workload.status.admission.podSetAssignments[*].topologyAssignment != nil` | When topology configured |
| RTJ not suspended | `RTJ.spec.suspend == false` | Always |

When no AdmissionChecks are configured on the ClusterQueue, the AC check
is trivially satisfied. **This preserves Phase 6 behavior exactly.**

### Fail-safe behavior when provisioning state is unknown

If the Workload is admitted but AdmissionCheck state is not present in
the Workload status (e.g., Kueue version mismatch, race condition), the
RTJ operator treats this as **all checks passed**. Rationale:

- Kueue versions without AdmissionCheck support do not populate the field.
- Blocking launch on a missing field would break backward compatibility.
- The fail-open behavior matches Phase 6 semantics.

If a specific AdmissionCheck is present but in an unknown state (not
Ready, not Pending, not Failed), the RTJ operator treats it as
**not ready** and does not open the launch gate. Rationale:

- An unknown state suggests an in-progress check.
- Fail-closed on unknown state prevents premature launch.

Summary:
- **No AC state field at all** → fail-open (Phase 6 compat).
- **AC state field present, state = Ready** → pass.
- **AC state field present, state = anything else** → block.

### Must-ship Phase 7 demo

The must-ship demo proves the end-to-end flow:

1. Deploy RTJ operator (Phase 7 build).
2. Deploy fake ProvisioningRequest backend with 10-second delay.
3. Configure ClusterQueue with ProvisioningRequest AdmissionCheck.
4. Create an RTJ with topology = Required.
5. Observe:
   - RTJ enters Queued → Admitted.
   - RTJ stays in Admitted for ~10 seconds (provisioning pending).
   - After fake approves, RTJ transitions to Starting.
   - Child JobSet appears with topology annotations.
   - Pods reach Ready; RTJ transitions to Running.
6. Repeat with fake in reject mode:
   - RTJ enters Admitted but provisioning fails.
   - Workload is requeued.
   - RTJ transitions to Paused with a clear failure condition.
7. Demonstrate waitForPodsReady timeout:
   - Configure `waitForPodsReady.timeout: 30s`.
   - Create RTJ with a broken image (pods never Ready).
   - After 30s, Kueue evicts. RTJ transitions to Paused with
     `StartupTimeoutEvicted` condition.

### What remains optional or deferred

| Item | Status | Rationale |
|---|---|---|
| Real cloud ProvisioningRequest backend (GKE NAP, Karpenter) | Optional | Not required for local success; documented config only |
| Multi-cluster provisioning status mirroring | Deferred (should-ship) | Worker phase implicitly reflects provisioning state |
| Backoff state surfacing in RTJ status | Deferred (should-ship) | Kueue handles backoff; RTJ visibility is UX improvement |
| Prometheus metrics for provisioning gate latency | Deferred (should-ship) | Useful but not required for correctness |
| DRA integration | Deferred (future phase) | Separate concern |
| Elastic workloads | Out of scope | Phase 0 ADR 0001 |

## Consequences

### Positive

1. **No custom capacity logic**: RTJ operator stays out of provisioning.
   Kueue's existing mechanism is reused.
2. **Testable locally**: Fake backend enables full e2e without cloud.
3. **Backward compatible**: No behavior change without explicit config.
4. **Coherent timeouts**: waitForPodsReady gives meaningful startup/recovery
   timeouts that RTJ surfaces in status.
5. **Multi-backend support**: Any ProvisioningRequest-compatible backend
   works (GKE NAP, Karpenter, custom).

### Negative

1. **Kueue version coupling**: The feature depends on Kueue v0.15.1's
   ProvisioningRequest AC implementation. If the pinned version has bugs
   or missing features, we must work around them.
2. **Two-pass topology**: The topology second-pass adds a small delay to
   launch. In practice this is one reconciliation cycle.
3. **Global waitForPodsReady**: The timeout is configured globally in
   Kueue, not per-RTJ. All workloads get the same timeout.

### Risks

1. **Kueue v0.15.1 divergence**: The pinned version may not match upstream
   docs. Mitigated by source audit in Session 2 (OQ1).
2. **Fake backend fidelity**: The fake backend may not exercise all code
   paths that a real backend would. Mitigated by documenting the real-cloud
   path and testing it in a staging environment.

## Verification

| Criterion | Test |
|---|---|
| Launch gate blocks until ProvisioningRequest AC ready | e2e with fake backend + delay |
| Launch gate blocks until topology assigned | e2e with topology + provisioning |
| Phase 6 behavior preserved without ProvisioningRequest AC | Phase 6 e2e suite regression |
| Startup timeout eviction triggers yield | e2e with short timeout + broken image |
| Fake backend approves after delay | e2e with configurable delay |
| Fake backend rejects on config | e2e with reject mode |
| Fail-open when no AC state field | Unit test |
| Fail-closed on unknown AC state | Unit test |
