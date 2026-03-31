# Phase 7 -- Provisioning / Topology Observation Layer

## Purpose

The `internal/provisioning` package implements a read-only observation layer
that builds a compact **LaunchReadinessView** from a Kueue Workload's status.
The RTJ controller consumes this view to decide whether launching child runtime
is safe.

This layer does **not** create, mutate, or delete any Kubernetes resources.

## Architecture

```
                    ┌──────────────┐
                    │ Kueue        │
                    │ Workload     │
                    │ .status      │
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │  BuildView() │
                    └──────┬───────┘
                           │
            ┌──────────────▼──────────────┐
            │    LaunchReadinessView       │
            │  ┌────────────────────────┐  │
            │  │ QuotaReserved          │  │
            │  │ AdmissionChecks[]      │  │
            │  │ Provisioning state     │  │
            │  │ ProvisioningRequestRef │  │
            │  │ PodSetUpdates[]        │  │
            │  │ TopologyState          │  │
            │  │ AllChecksReady         │  │
            │  └────────────────────────┘  │
            └──────────────┬──────────────┘
                           │
                    ┌──────▼───────┐
                    │ RTJ operator │
                    │ (future      │
                    │  integration)│
                    └──────────────┘
```

## Kueue v0.15.1 Status Fields Relied On

### Quota Reservation

| Field | Type | Usage |
|---|---|---|
| `status.admission` | `*Admission` | Non-nil → quota reserved |

### Admission Checks

| Field | Type | Usage |
|---|---|---|
| `status.admissionChecks[].name` | `AdmissionCheckReference` | AC identifier |
| `status.admissionChecks[].state` | `CheckState` | Pending, Ready, Retry, Rejected |
| `status.admissionChecks[].message` | `string` | Human-readable status message |
| `status.admissionChecks[].retryCount` | `*int32` | Retry attempt counter |
| `status.admissionChecks[].podSetUpdates[].name` | `PodSetReference` | Target pod set |
| `status.admissionChecks[].podSetUpdates[].labels` | `map[string]string` | Labels to merge |
| `status.admissionChecks[].podSetUpdates[].annotations` | `map[string]string` | Annotations to merge |
| `status.admissionChecks[].podSetUpdates[].nodeSelector` | `map[string]string` | Node selector entries |
| `status.admissionChecks[].podSetUpdates[].tolerations` | `[]Toleration` | Tolerations to append |

### Topology Assignment

| Field | Type | Usage |
|---|---|---|
| `status.admission.podSetAssignments[].topologyAssignment` | `*TopologyAssignment` | Non-nil → topology assigned |
| `status.admission.podSetAssignments[].delayedTopologyRequest` | `*DelayedTopologyRequestState` | Pending or Ready |

### Kueue Type References (v0.15.1)

All types from `sigs.k8s.io/kueue/apis/kueue/v1beta2`:

- `Workload` -- top-level CRD
- `WorkloadStatus` -- status subresource
- `Admission` -- quota reservation with PodSetAssignments
- `PodSetAssignment` -- per-PodSet admission (Count, Flavors, TopologyAssignment, DelayedTopologyRequest)
- `AdmissionCheckState` -- per-AC state (Name, State, PodSetUpdates)
- `PodSetUpdate` -- per-PodSet mutations from an AC
- `TopologyAssignment` -- compressed topology assignment
- `CheckState` -- enum: Pending, Ready, Retry, Rejected
- `DelayedTopologyRequestState` -- enum: Pending, Ready

## Internal Types

### LaunchReadinessView (view.go)

The top-level snapshot consumed by the RTJ controller.

| Field | Type | Description |
|---|---|---|
| QuotaReserved | bool | Workload has non-nil admission |
| AdmissionChecks | []AdmissionCheckView | Per-AC state |
| ProvisioningRequestPresent | bool | A provisioning AC was detected |
| Provisioning | ProvisioningClassification | NotConfigured, Pending, Provisioned, Failed, Retry |
| ProvisioningRequestRef | *ProvisioningRequestRef | Derived PR resource reference |
| PodSetUpdates | []PodSetUpdateSet | Parsed podSetUpdates from all ACs |
| TopologyState | TopologyView | Topology assignment state |
| AllChecksReady | bool | All ACs in Ready state |

Methods:
- `IsLaunchReady()` -- true when quota + all checks + topology are satisfied
- `MergedPodSetUpdates()` -- merged updates by pod set name

### ProvisioningClassification (requests.go)

| Value | Kueue CheckState | Meaning |
|---|---|---|
| NotConfigured | n/a | No provisioning AC on workload |
| Pending | Pending | Backend processing |
| Provisioned | Ready | Physical capacity confirmed |
| Failed | Rejected | Backend rejected |
| Retry | Retry | Back-off before next attempt |

### ProvisioningRequest Reference Resolution (requests.go)

The PR reference is derived using Kueue v0.15.1's naming convention:

```
{workload-name}-{check-name}-{attempt}
```

Example: workload `wl-1` with check `provision-ac` → `wl-1-provision-ac-1`

This is a best-effort derivation. The RTJ controller should validate the
reference when integrating this layer.

### PodSetUpdateEntry (podsetupdates.go)

Parsed form of `kueuev1beta2.PodSetUpdate`:

| Field | Type | Source |
|---|---|---|
| Name | string | podSetUpdates[].name |
| Labels | map[string]string | podSetUpdates[].labels |
| Annotations | map[string]string | podSetUpdates[].annotations |
| NodeSelector | map[string]string | podSetUpdates[].nodeSelector |
| Tolerations | []Toleration | podSetUpdates[].tolerations |

All maps and slices are deep-copied on parse.

### TopologyView (topology.go)

| Field | Type | Description |
|---|---|---|
| Configured | bool | RTJ has topology enabled |
| Assigned | bool | At least one PodSet has TopologyAssignment |
| DelayedTopologyState | DelayedTopologyState | None, Pending, or Ready |
| SecondPassPending | bool | Topology configured but not yet assigned |
| PodSetStates | []PodSetTopologyState | Per-PodSet topology state |

## Phase 6 Backward Compatibility

When no provisioning AC names are configured (the default), the view
returns:

- `Provisioning: NotConfigured`
- `ProvisioningRequestPresent: false`
- `AllChecksReady: true` (when no ACs exist on the workload)
- `IsLaunchReady()` returns true when quota is reserved

This preserves Phase 6 behavior where the RTJ launches immediately after
Kueue admission without waiting for any provisioning gate.

## Test Coverage

### requests_test.go (15 tests)
- Classification for all CheckState values (Pending, Ready, Retry, Rejected)
- NotConfigured fallback for empty/nil/unmatched AC names
- Unknown state defaults to Pending
- Multiple ACs with first-match semantics
- PR reference derivation with attempt numbers
- Edge cases (empty workload name, empty check name)

### podsetupdates_test.go (14 tests)
- Nil/empty input handling
- Single and multi-PodSet parsing
- Deep copy independence verification
- Nil map preservation
- Multi-AC merge with conflict resolution
- HasPodSetUpdates edge cases

### topology_test.go (14 tests)
- Not configured with no assignments
- Configured but no assignments (SecondPassPending)
- TopologyAssignment present and assigned
- DelayedTopologyRequest Pending and Ready states
- Unknown delayed state defaults to Pending
- Multiple PodSets with mixed states
- IsTopologyReady for all combinations

### view_test.go (20 tests)
- Phase 6 fallback (no ACs, no provisioning)
- Provisioning readiness detection (pending, ready, failed, retry)
- Multiple ACs with mixed states
- podSetUpdates parsing through BuildView
- Topology assignment detection
- Delayed topology pending
- No quota reserved
- Nil receiver safety
- Full integration test (provisioning + topology + multiple ACs + podSetUpdates)
- CheckState normalization table test

Total: **63 tests**
