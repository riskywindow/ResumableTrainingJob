# Phase 8 Operations Guide

This document explains how to inspect and operate DRA-backed
ResumableTrainingJobs in a Phase 8 environment.

## Inspecting RTJ DRA Status

The RTJ `status.devices` section contains all DRA-related state.

```bash
# Quick summary.
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.devices}' | jq .

# Or use the inspect script.
make phase8-inspect-dra PHASE8_RTJ_NAME=<name>
```

Key fields:

| Field | Description |
|-------|-------------|
| `deviceMode` | `DRA` when DRA is active, `Disabled` otherwise |
| `currentDeviceProfileFingerprint` | SHA256 hash of device classes + selectors |
| `requestedDeviceClasses` | Sorted list of DeviceClass names |
| `resourceClaimTemplateRefs` | Name → ClaimName mapping for each claim |
| `claimAllocationState` | `Pending`, `Allocated`, `Failed`, or `Unknown` |
| `allocatedClaimCount` | Number of claims with active allocations |
| `lastClaimFailureReason` | Reason for the most recent claim failure |
| `lastClaimFailureTime` | Timestamp of the most recent claim failure |
| `lastCheckpointDeviceProfileFingerprint` | Fingerprint from the last committed checkpoint |
| `lastResumeDeviceProfileFingerprint` | Fingerprint used in the last resume attempt |

## Inspecting ResourceClaimTemplates

ResourceClaimTemplates are owned by the RTJ and survive child JobSet
deletion (they are not owned by the JobSet).

```bash
# List templates for an RTJ.
kubectl -n checkpoint-dev get resourceclaimtemplates \
  -l training.checkpoint.example.io/rtj-name=<name>

# Show template spec (device request details).
kubectl -n checkpoint-dev get resourceclaimtemplate <rtj-name>-<claim-name> -o yaml
```

Template naming convention: `<rtj-name>-<claim-name>`

Example: RTJ `demo` with claim `gpu` produces template `demo-gpu`.

Labels on each template:

| Label | Value |
|-------|-------|
| `training.checkpoint.example.io/rtj-name` | RTJ name |
| `training.checkpoint.example.io/claim-name` | Claim name from spec |
| `training.checkpoint.example.io/managed-by` | `rtj-operator` |

## Inspecting Generated ResourceClaims

ResourceClaims are created by the Kubernetes scheduler when pods from
the child JobSet are bound to nodes. They are not directly created by
the RTJ operator.

```bash
# List claims for an RTJ.
kubectl -n checkpoint-dev get resourceclaims \
  -l training.checkpoint.example.io/rtj-name=<name>

# Show claim allocation details.
kubectl -n checkpoint-dev get resourceclaim <claim-name> -o yaml
```

The claim's `status.allocation` section shows which devices were
assigned and on which nodes.

## Inspecting DeviceClass and ResourceSlices

DeviceClass defines the pool of devices available for allocation.
ResourceSlices are published by DRA drivers to advertise available
devices.

```bash
# List DeviceClasses.
kubectl get deviceclasses.resource.k8s.io

# Show DeviceClass selector (which driver it targets).
kubectl get deviceclasses.resource.k8s.io example-gpu \
  -o jsonpath='{.spec.selectors[0].cel.expression}'

# List ResourceSlices (published by the DRA driver).
kubectl get resourceslices -l app.kubernetes.io/managed-by=dra-example-driver

# Show devices in a ResourceSlice.
kubectl get resourceslice <name> -o jsonpath='{.spec.devices}' | jq .
```

In the dev environment, each node publishes a ResourceSlice with 4
simulated GPUs (gpu-0 through gpu-3), each with attributes:

- `model`: Example-GPU-v1
- `memory`: 80Gi
- `index`: 0-3

## Inspecting Kueue Logical-Resource Accounting

Kueue resolves DRA device requests to logical resources via
`deviceClassMappings`. In the Phase 8 dev profile, `example-gpu`
maps to `example.dev/gpu`.

```bash
# Show ClusterQueue resource usage.
kubectl get clusterqueues.kueue.x-k8s.io phase8-cq -o yaml

# Quick usage summary.
make phase8-inspect-kueue

# Show deviceClassMappings from Kueue config.
kubectl -n kueue-system get configmap kueue-manager-config \
  -o jsonpath='{.data.controller_manager_config\.yaml}' | grep -A5 deviceClassMappings
```

The mapping chain:

1. RTJ `spec.devices.claims[].request.deviceClassName` = `example-gpu`
2. Kueue `deviceClassMappings`: `example-gpu` → `example.dev/gpu`
3. ClusterQueue `resourceGroups[].coveredResources` includes `example.dev/gpu`
4. ClusterQueue `flavors[].resources[].nominalQuota` = 8 (4 GPUs × 2 nodes)

## Inspecting Checkpoint Device-Profile Metadata

The checkpoint manifest includes a `deviceProfileFingerprint` field
when DRA is active. This is used for fail-closed compatibility
checking during resume.

```bash
# Inspect checkpoint device profile metadata.
make phase8-inspect-checkpoints PHASE8_RTJ_NAME=<name>

# Direct status query.
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.devices.currentDeviceProfileFingerprint}'
```

The fingerprint is a SHA256 hash of the canonical form:

```
class=<className>;selectors=<sorted,semicolon-joined>;count=<count>
```

Multiple claims are sorted and joined with newlines before hashing.

### Compatibility rules

| Current RTJ | Checkpoint | Result |
|-------------|-----------|--------|
| DRA active, fingerprint X | fingerprint X | Compatible |
| DRA active, fingerprint X | fingerprint Y | **Incompatible** (rejected) |
| DRA active, fingerprint X | no fingerprint | **Incompatible** (rejected, fail-closed) |
| No DRA | fingerprint X | Compatible (downgrade allowed) |
| No DRA | no fingerprint | Compatible (Phase 7 path) |

## Operator Metrics

Phase 8 adds the following Prometheus metrics under
`checkpoint_native_operator_`:

| Metric | Type | Description |
|--------|------|-------------|
| `rtjs_by_device_mode` | GaugeVec (mode) | Current RTJs by device mode |
| `dra_template_reconciles_total` | Counter | Successful template reconcile ops |
| `dra_template_failures_total` | Counter | Failed template reconcile attempts |
| `dra_claims_generated_total` | Counter | ResourceClaims created |
| `dra_claim_allocation_summary_total` | CounterVec (state) | Claim allocation observations |
| `dra_resume_compatibility_checks_total` | CounterVec (result) | Resume compatibility checks |
| `dra_backed_launches_total` | Counter | DRA-backed child JobSet launches |
| `dra_launch_failures_total` | Counter | DRA-backed launch failures |

Access via the metrics endpoint:

```bash
kubectl -n checkpoint-dev port-forward deploy/rtj-operator 8080:8080
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_dra
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_rtjs_by_device_mode
```
