# Phase 7 -- Goals and Acceptance Criteria

## Mission

Ensure RTJ child runtime is never launched until physical capacity is
confirmed available, using Kueue's built-in ProvisioningRequest
AdmissionCheck as the capacity-truth source.

Make startup timeout and recovery timeout visible and coherent in RTJ
status.

Provide a fake ProvisioningRequest backend so the full flow is testable
locally without a real cluster-autoscaler.

---

## Core goals

### G1 -- ProvisioningRequest AdmissionCheck integration

Configure Kueue's built-in ProvisioningRequest AdmissionCheck so that RTJ
workloads are not admitted until a ProvisioningRequest confirms physical
capacity. RTJ does not create or manage ProvisioningRequests directly;
Kueue's AdmissionCheck controller does.

### G2 -- Provisioning-aware launch gating

The RTJ operator must not render the child JobSet until **all** of the
following are true:

1. The Kueue workload is Admitted (quota reserved).
2. All configured AdmissionChecks report success -- including the
   ProvisioningRequest check.
3. Topology assignment is available (if topology is configured).

This is the **launch gate**. Until all inputs are satisfied, the RTJ
remains in the `Admitted` phase and does not transition to `Starting`.

### G3 -- Topology-second-pass-aware launch rendering

Topology assignment may arrive in a second Kueue reconciliation pass,
_after_ the ProvisioningRequest is satisfied. The launch gate must wait
for topology assignment (when configured) before rendering the child
JobSet with topology annotations.

### G4 -- waitForPodsReady startup/recovery integration

Kueue's `waitForPodsReady` feature provides:

- **Startup timeout**: if pods don't reach Ready within a configured
  window after launch, Kueue evicts the workload (sets it back to
  suspended). RTJ must detect this eviction and transition to `Paused`
  via the existing yield path.
- **Recovery timeout**: if a running workload's pods become NotReady and
  don't recover within a configured window, Kueue evicts. RTJ treats
  this identically to a startup timeout eviction.

RTJ status must surface:

- Whether the workload is under startup timeout observation.
- Whether a timeout eviction occurred (and map it to a clear condition).
- The configured timeout values for operator visibility.

### G5 -- Fake ProvisioningRequest backend for local dev/e2e

A lightweight controller that watches ProvisioningRequest resources and
auto-approves them after a configurable delay. This enables:

- Full end-to-end testing of the provisioning-gated launch flow locally.
- Simulating provisioning failures by configuring the fake to reject.
- Simulating slow provisioning by configuring longer delays.

The fake backend runs as a separate Deployment or in-process toggle,
gated by a feature flag.

### G6 -- Optional real cloud/autoscaler profile

Document the configuration for real ProvisioningRequest controllers
(GKE NAP, Karpenter integration) without requiring them for local
success. Provide a sample AdmissionCheck + ClusterQueue config.

---

## Non-goals (explicit out-of-scope)

| ID | Non-goal | Reason |
|---|---|---|
| NG1 | Custom ProvisioningRequest controller | Kueue's built-in one is sufficient |
| NG2 | DRA (Dynamic Resource Allocation) | Separate concern, future phase |
| NG3 | Elastic workloads | Out of v1 scope (Phase 0 ADR 0001) |
| NG4 | Custom scheduling or scheduler plugins | Kueue + default scheduler is the contract |
| NG5 | Transparent CUDA/container snapshots | Out of v1 scope (Phase 0 ADR 0001) |
| NG6 | Replacing Kueue admission/preemption | Kueue is the authority (Phase 2 ADR 0001) |
| NG7 | Node-level failure detection/fencing | Kubernetes and Kueue handle node failures |
| NG8 | Multi-cluster ProvisioningRequest | Phase 6 manager dispatches; worker handles local provisioning |
| NG9 | Live migration between nodes | Out of scope (Phase 6 non-goal, still applies) |

---

## Must-ship success criteria

| ID | Criterion | Verification |
|---|---|---|
| MS1 | RTJ does not launch child JobSet until ProvisioningRequest AdmissionCheck passes | e2e test: fake backend with delay; child pods appear only after approval |
| MS2 | RTJ does not launch child JobSet until topology assignment arrives (when configured) | e2e test: topology + provisioning; verify launch waits for both |
| MS3 | RTJ status shows provisioning gate state (pending/satisfied/failed) | e2e test: status field inspection during gated phase |
| MS4 | Startup timeout eviction triggers RTJ yield path | e2e test: configure short timeout, observe RTJ → Paused |
| MS5 | Recovery timeout eviction triggers RTJ yield path | e2e test: kill pod after Running, observe timeout → Paused |
| MS6 | Fake ProvisioningRequest backend auto-approves with configurable delay | e2e test: deploy fake, observe approval after delay |
| MS7 | Fake ProvisioningRequest backend supports configurable rejection | e2e test: deploy fake in reject mode, observe workload requeue |
| MS8 | Phase 6 single-cluster path works unchanged without Phase 7 config | e2e test: run Phase 6 suite with no provisioning config |
| MS9 | Phase 6 manager/worker path works unchanged without Phase 7 config | e2e test: run Phase 6 multi-cluster suite with no provisioning config |

## Should-ship success criteria

| ID | Criterion | Verification |
|---|---|---|
| SS1 | Sample real-cloud AdmissionCheck config for GKE NAP | Doc + dry-run validation |
| SS2 | Prometheus metrics for provisioning gate latency | Unit test + manual verification |
| SS3 | RTJ status shows waitForPodsReady timeout values | e2e test: inspect status during startup window |
| SS4 | Documentation for Karpenter-based ProvisioningRequest | Doc only |

## Exit criteria

1. All MS* criteria pass in CI.
2. Phase 6 e2e suite passes without regression.
3. Docs pack complete (this directory).
4. session-handoff.md updated with final state.
