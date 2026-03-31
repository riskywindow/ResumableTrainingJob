# Phase 7: Provisioning-Aware Launch Gating

## Overview

Phase 7 Session 4 integrates the provisioning observation layer (Session 3)
into the RTJ controller's launch gate and render path. The controller now
evaluates provisioning readiness, topology second-pass state, and
AdmissionCheck-suggested podSetUpdates before creating child JobSets.

## Launch Gate Decision Tree

The launch gate evaluates prerequisites in order:

```
1. Quota reserved?
   └─ NO  → Blocked: QuotaNotReserved
   └─ YES ↓

2. All AdmissionChecks Ready?
   └─ NO  → Check provisioning state:
            ├─ Provisioning Pending/Retry → Blocked: ProvisioningInProgress
            ├─ Provisioning Failed        → Blocked: ProvisioningFailed
            ├─ ResumeReadiness Pending    → Blocked: WaitingForReadinessGate
            ├─ ResumeReadiness Rejected   → Blocked: ReadinessGateRejected
            └─ Other AC Pending           → Blocked: AdmissionCheckPending
   └─ YES ↓

3. Topology second-pass pending? (if topology configured)
   └─ YES → Blocked: TopologyPendingSecondPass
   └─ NO  ↓

4. Topology assignment present? (if topology configured)
   └─ NO  → Blocked: WaitingForTopologyAssignment
   └─ YES ↓

5. podSetUpdate conflicts? (dry-run apply)
   └─ YES → Failed: LaunchBlockedByConflictingPodSetUpdate
   └─ NO  ↓

6. All gates passed → LaunchReady
```

## Phase 6 Backward Compatibility

When none of these conditions are true, the launch gate is skipped entirely
and the controller proceeds directly to the Phase 3 launch path:

- No topology enabled (`spec.topology.mode` is Disabled or nil)
- No WorkloadReference in status
- No ProvisioningACNames configured on the reconciler

This preserves exact Phase 6 behavior: the controller creates child JobSets
immediately after admission without any gate checks.

## podSetUpdate Application

AdmissionCheck-suggested podSetUpdates are applied to the rendered child
JobSet **additively**:

| Field | Application Rule | Conflict Detection |
|-------|-----------------|-------------------|
| Labels | New keys added to pod metadata | Existing key with different value |
| Annotations | New keys added to pod metadata | Existing key with different value |
| NodeSelector | New keys added to pod nodeSelector | Existing key with different value |
| Tolerations | Appended (deduplicated) | No conflicts (always additive) |

### Conflict Handling

When a podSetUpdate contains a key that already exists in the rendered
template with a **different** value, the launch is blocked:

- The RTJ transitions to `PhaseFailed`
- The `LaunchBlockedByConflictingPodSetUpdate` condition is set
- A clear error message identifies the conflicting field, key, existing
  value, and update value

When the update value **matches** the existing value, it is not a conflict
(idempotent merge).

## Status Sections

### status.launchGate

| Field | Description |
|-------|------------|
| `launchGateState` | Open, Blocked, or Unknown |
| `launchGateReason` | Machine-readable reason (e.g., ProvisioningInProgress) |
| `message` | Human-readable explanation |
| `admissionCheckSummary` | Per-AC name → state (Pending/Ready/Retry/Rejected) |
| `topologyGateState` | NotConfigured, Pending, or Assigned |
| `lastTransitionTime` | When the gate state last changed |

### status.provisioning

| Field | Description |
|-------|------------|
| `provisioningState` | NotConfigured, Pending, Provisioned, or Failed |
| `provisioningRequestRef` | Name/Namespace of the derived ProvisioningRequest |
| `provisioningAttempt` | Attempt counter |
| `reason` | Machine-readable reason |
| `message` | Human-readable explanation |

### status.capacity

| Field | Description |
|-------|------------|
| `guaranteeActive` | true when provisioning satisfied + quota reserved |
| `reason` | ProvisioningSatisfied, QuotaOnlyAdmission, ProvisioningPending, NotAdmitted |

## Conditions

| Condition | When Set |
|-----------|---------|
| `CapacityPending` | ProvisioningRequest AC exists and not yet Ready |
| `ProvisioningInProgress` | Provisioning AC in Pending or Retry state |
| `ProvisioningFailed` | Provisioning AC rejected |
| `TopologyPendingSecondPass` | Topology configured but second-pass assignment pending |
| `LaunchReady` | All gates satisfied |
| `LaunchBlockedByConflictingPodSetUpdate` | podSetUpdate conflicts with rendered state |

## Architecture

```
                    ┌─────────────────────┐
                    │  Kueue Workload      │
                    │  status.admission    │
                    │  status.admChecks    │
                    └────────┬────────────┘
                             │
                    ┌────────▼────────────┐
                    │ provisioning.        │
                    │ BuildView()          │
                    │ (Session 3)          │
                    └────────┬────────────┘
                             │
                    ┌────────▼────────────┐
                    │ evaluateLaunchGates  │
                    │ (Session 4)          │
                    │                      │
                    │ Gate 1: Quota        │
                    │ Gate 2: All ACs      │
                    │ Gate 3: Topology 2nd │
                    │ Gate 4: Topo assign  │
                    └────────┬────────────┘
                             │
                    ┌────────▼────────────┐
                    │ buildLaunchPlan      │
                    │ + PodSetUpdates      │
                    │ + conflict check     │
                    └────────┬────────────┘
                             │
                    ┌────────▼────────────┐
                    │ RenderChildJobSet    │
                    │ + topology injection │
                    │ + podSetUpdates      │
                    └─────────────────────┘
```

## Configuration

The reconciler accepts `ProvisioningACNames` (a `map[string]bool`) to
identify which Workload AdmissionCheck names are ProvisioningRequest checks.
When empty, provisioning is not configured and Phase 6 behavior is preserved.

This is typically configured from:
- ClusterQueue AdmissionCheck configuration
- Operator flags or environment variables
