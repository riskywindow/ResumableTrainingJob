# Phase 7 -- API Reference

## Overview

Phase 7 extends the RTJ status surface with controller-owned fields for
launch gating, provisioning visibility, and startup/recovery status.

**No new spec fields are added.** All Phase 7 additions are status-only,
controller-owned fields. See [ADR 0002](adr/0002-launch-gate-status-api.md)
for the rationale.

## Backward compatibility

- All new status sections are optional (`+optional`, `omitempty`).
- A Phase 6 RTJ manifest is accepted unchanged.
- A Phase 6 RTJ status (with nil Phase 7 sections) is valid.
- The webhook does not inject or validate Phase 7 status fields.
- Phase 7 status fields are populated only by the controller.

---

## New enum types

### LaunchGateState

Describes the aggregate launch gate evaluation state.

| Value | Meaning |
|---|---|
| `Open` | All prerequisites satisfied; child JobSet may be rendered |
| `Blocked` | One or more prerequisites not satisfied |
| `Unknown` | Gate state could not be determined |

### ProvisioningState

Describes the state of the ProvisioningRequest AdmissionCheck gate.

| Value | Meaning |
|---|---|
| `NotConfigured` | No ProvisioningRequest AC on the ClusterQueue (Phase 6 behavior) |
| `Pending` | ProvisioningRequest created, backend processing |
| `Provisioned` | Backend confirmed physical capacity available |
| `Failed` | Backend rejected the provisioning request |

### StartupState

Describes the startup/recovery lifecycle of the child runtime.

| Value | Meaning |
|---|---|
| `NotStarted` | No child runtime launched yet |
| `Starting` | Child runtime launched, pods not yet Ready |
| `Running` | Child runtime pods are Ready |
| `StartupTimedOut` | Pods did not reach Ready within waitForPodsReady timeout |
| `RecoveryTimedOut` | Pods lost Ready state and did not recover |
| `Evicted` | Workload evicted by Kueue |

### PodsReadyState

Derived pod readiness indicator.

| Value | Meaning |
|---|---|
| `Unknown` | Pod readiness not determined |
| `PodsReady` | All expected worker pods Ready |
| `PodsNotReady` | One or more expected worker pods not Ready |
| `NoRuntime` | No active child runtime to evaluate |

### TopologyGateState

Whether topology assignment is satisfied.

| Value | Meaning |
|---|---|
| `NotConfigured` | Topology not enabled (Phase 3 behavior) |
| `Pending` | Topology configured, assignment not yet present |
| `Assigned` | Topology assignment present and ready |

### AdmissionCheckState

State of a single admission check.

| Value | Meaning |
|---|---|
| `Pending` | Check not yet evaluated |
| `Ready` | Check passed |
| `Retry` | Check failed but may be retried |
| `Rejected` | Check permanently failed |

---

## New status sections

All new status sections are under `status.*` and are controller-owned.
Users must not write to these fields.

### `status.launchGate` (LaunchGateStatus)

Captures the aggregate launch gate evaluation state. The launch gate is
the decision point where the controller transitions from Admitted to
Starting and renders the child JobSet.

| Field | Type | Description |
|---|---|---|
| `launchGateState` | `LaunchGateState` | Aggregate gate state |
| `launchGateReason` | `string` | Machine-readable reason (e.g., "AllChecksPassed", "AdmissionCheckPending") |
| `message` | `string` | Human-readable explanation |
| `admissionCheckSummary` | `map[string]AdmissionCheckState` | Per-check state summary. Empty when no ACs configured |
| `topologyGateState` | `TopologyGateState` | Topology assignment prerequisite state |
| `lastTransitionTime` | `*metav1.Time` | When the gate state last changed |

**Launch gate opens when ALL of:**
1. Workload admitted (`Workload.status.admission != nil`)
2. All AdmissionChecks Ready (or none configured)
3. Topology assigned (or topology disabled)
4. RTJ not suspended

### `status.provisioning` (ProvisioningStatus)

Captures the state of the ProvisioningRequest AdmissionCheck gate.
Derived from the Workload's AdmissionCheck state.

| Field | Type | Description |
|---|---|---|
| `provisioningState` | `ProvisioningState` | Current provisioning gate state |
| `provisioningRequestRef` | `*ProvisioningRequestReference` | Reference to Kueue-created ProvisioningRequest |
| `provisioningAttempt` | `int32` | Number of provisioning attempts |
| `reason` | `string` | Machine-readable reason |
| `message` | `string` | Human-readable explanation |
| `provisioningLastTransitionTime` | `*metav1.Time` | When state last changed |

**ProvisioningRequestReference:**

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Name of the ProvisioningRequest |
| `namespace` | `string` | Namespace of the ProvisioningRequest |

### `status.startupRecovery` (StartupRecoveryStatus)

Captures the startup and recovery lifecycle of the child runtime.
Integrates with Kueue's waitForPodsReady eviction signals.

| Field | Type | Description |
|---|---|---|
| `startupState` | `StartupState` | Current startup/recovery state |
| `podsReadyState` | `PodsReadyState` | Derived pod readiness indicator |
| `lastLaunchFailureReason` | `string` | Reason for last launch failure |
| `lastEvictionReason` | `string` | Reason for last Kueue eviction |
| `lastRequeueReason` | `string` | Reason for last requeue |
| `lastTransitionTime` | `*metav1.Time` | When state last changed |

### `status.capacity` (CapacityStatus)

Derived indicator of whether a physical capacity guarantee is active.

| Field | Type | Description |
|---|---|---|
| `guaranteeActive` | `bool` | True when physical capacity confirmed via ProvisioningRequest |
| `reason` | `string` | Machine-readable reason (e.g., "ProvisioningSatisfied", "QuotaOnlyAdmission") |

---

## Mapping to requirements

| Requirement | Status field | Section |
|---|---|---|
| launchGateState | `status.launchGate.launchGateState` | LaunchGateStatus |
| launchGateReason | `status.launchGate.launchGateReason` | LaunchGateStatus |
| admissionCheckSummary | `status.launchGate.admissionCheckSummary` | LaunchGateStatus |
| provisioningRequestRef | `status.provisioning.provisioningRequestRef` | ProvisioningStatus |
| provisioningState | `status.provisioning.provisioningState` | ProvisioningStatus |
| provisioningAttempt | `status.provisioning.provisioningAttempt` | ProvisioningStatus |
| provisioningLastTransitionTime | `status.provisioning.provisioningLastTransitionTime` | ProvisioningStatus |
| topologyGateState | `status.launchGate.topologyGateState` | LaunchGateStatus |
| startupState | `status.startupRecovery.startupState` | StartupRecoveryStatus |
| lastLaunchFailureReason | `status.startupRecovery.lastLaunchFailureReason` | StartupRecoveryStatus |
| lastEvictionReason | `status.startupRecovery.lastEvictionReason` | StartupRecoveryStatus |
| lastRequeueReason | `status.startupRecovery.lastRequeueReason` | StartupRecoveryStatus |
| podsReadyState | `status.startupRecovery.podsReadyState` | StartupRecoveryStatus |
| capacity guarantee active | `status.capacity.guaranteeActive` | CapacityStatus |

---

## Controller ownership

All Phase 7 status fields are **controller-owned**. The controller is the
sole writer. The API server (webhook) does not validate, default, or
reject these fields.

This follows the existing pattern used by:
- `status.launchReadiness` (Phase 4)
- `status.priorityShaping` (Phase 5)
- `status.multiCluster` (Phase 6)

---

## Fail-safe behavior

| Scenario | Behavior | Rationale |
|---|---|---|
| No AC state field on Workload | Fail-open (launch allowed) | Phase 6 compat |
| AC state = Ready | Pass | Normal operation |
| AC state = anything else | Block launch | Fail-closed on unknown |
| Phase 7 status sections nil | Valid (Phase 6 compat) | All new sections optional |
| Topology not configured | `topologyGateState = NotConfigured` | Gate trivially satisfied |
| ProvisioningRequest AC not configured | `provisioningState = NotConfigured` | Gate trivially satisfied |
