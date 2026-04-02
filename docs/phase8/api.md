# Phase 8 -- DRA Device API Reference

## Overview

Phase 8 adds an optional `spec.devices` section to the ResumableTrainingJob
(RTJ) spec. When configured with `mode: DRA`, the operator materializes
companion ResourceClaimTemplate objects from user-authored DRA request
fragments and renders DRA claim references in the child JobSet pod templates.

When `spec.devices` is absent or `mode: Disabled`, the RTJ follows the
Phase 7 path unchanged. No Phase 7 behavior changes.

---

## Spec additions

### `spec.devices` (optional)

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `mode` | `DeviceMode` | Yes (when devices is set) | `Disabled` | `Disabled` or `DRA`. Controls whether DRA claims are generated. |
| `claims` | `[]DeviceClaimSpec` | When mode=DRA | - | List of per-worker ResourceClaimTemplate specs. Each produces a companion ResourceClaimTemplate owned by the RTJ. |

### `DeviceClaimSpec`

Each entry in `spec.devices.claims` describes a single ResourceClaimTemplate
to be materialized by the operator. The generated template is named
`<rtj-name>-<claim-name>`.

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | `string` | Yes | Unique identifier within the RTJ. DNS subdomain fragment, max 63 chars. Pattern: `^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`. |
| `containers` | `[]string` | Yes (min 1) | Container names within the worker pod template that receive this claim via `resources.claims[]`. |
| `request` | `DeviceRequestSpec` | Yes | Constrained DRA device request fragment. |

### `DeviceRequestSpec`

A constrained subset of a DRA DeviceRequest. The operator copies these
fields into the generated ResourceClaimTemplate's
`spec.spec.devices.requests[]` entry.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `deviceClassName` | `string` | Yes | - | DRA DeviceClass name. Must match a DeviceClass installed in the cluster and configured in Kueue's `deviceClassMappings`. |
| `count` | `int32` | No | `1` | Number of devices per worker pod for this claim. Must be >= 1. |
| `selectors` | `[]string` | No | - | CEL-expression selectors for device attributes. All selectors must match. |

**What is exposed directly**: `deviceClassName`, `count`, and `selectors`
are the only DRA DeviceRequest fields exposed. These map directly to the
upstream DRA `DeviceRequest` type's `deviceClassName`, `count`, and
`selectors[].cel.expression` fields.

**What is NOT exposed**: `allocationMode`, `adminAccess`, `tolerations`,
and other advanced DRA fields are not supported. Attempting to use
unsupported DRA features must go through the embedded JobSet template's
raw spec (not recommended for Phase 8).

---

## What the operator materializes

For each `DeviceClaimSpec` entry, the operator creates a
ResourceClaimTemplate:

```yaml
apiVersion: resource.k8s.io/v1beta2
kind: ResourceClaimTemplate
metadata:
  name: <rtj-name>-<claim-name>
  namespace: <rtj-namespace>
  ownerReferences:
    - apiVersion: training.checkpoint.example.io/v1alpha1
      kind: ResumableTrainingJob
      name: <rtj-name>
      uid: <rtj-uid>
      controller: true
      blockOwnerDeletion: true
spec:
  spec:
    devices:
      requests:
        - name: <claim-name>
          deviceClassName: <request.deviceClassName>
          count: <request.count>
          selectors:
            - cel:
                expression: "<selector[0]>"
            - cel:
                expression: "<selector[1]>"
```

The child JobSet worker pod template is extended with:

```yaml
spec:
  resourceClaims:
    - name: <claim-name>
      resourceClaimTemplateName: <rtj-name>-<claim-name>
  containers:
    - name: <container from claim.containers>
      resources:
        claims:
          - name: <claim-name>
```

---

## Status additions

### `status.devices` (controller-owned)

All fields are controller-owned. Users must not write to this section.
Nil when `spec.devices` is absent (Phase 7 behavior).

| Field | Type | Description |
|---|---|---|
| `deviceMode` | `DeviceMode` | Observed device mode from `spec.devices.mode`. |
| `requestedDeviceClasses` | `[]string` | Deduplicated, sorted summary of DeviceClass names across all claims. |
| `currentDeviceProfileFingerprint` | `string` | SHA256 hash of the current device profile (sorted classes + selectors). Used for checkpoint compatibility. |
| `resourceClaimTemplateRefs` | `[]ResourceClaimTemplateReference` | Maps claim names to generated ResourceClaimTemplate object names. |
| `claimAllocationState` | `ClaimAllocationState` | Aggregate allocation state: `Pending`, `Allocated`, `Failed`, `Unknown`. |
| `allocatedClaimCount` | `int32` | Number of claims successfully allocated. |
| `lastClaimFailureReason` | `string` | Machine-readable reason for the most recent claim failure. |
| `lastClaimFailureTime` | `metav1.Time` | When the most recent claim failure occurred. |
| `lastCheckpointDeviceProfileFingerprint` | `string` | Device fingerprint from the most recent completed checkpoint. |
| `lastResumeDeviceProfileFingerprint` | `string` | Device fingerprint active during the most recent resume. |

### `ResourceClaimTemplateReference`

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Kubernetes object name of the generated ResourceClaimTemplate. |
| `claimName` | `string` | `DeviceClaimSpec.Name` that this template was generated from. |

---

## Validation rules

### When `spec.devices` is nil

No validation. Phase 7 behavior preserved.

### When `spec.devices` is set

| Rule | Error |
|---|---|
| `mode` must be `Disabled` or `DRA` | `spec.devices.mode: Unsupported value` |
| When `mode=DRA`, `claims` must be non-empty | `spec.devices.claims: Required` |
| When `mode=Disabled`, `claims` must be empty | `spec.devices.claims: Forbidden` |
| Each claim `name` must be non-empty, max 63 chars, DNS-safe | `spec.devices.claims[i].name: Required/TooLong` |
| Claim names must be unique across all claims | `spec.devices.claims[i].name: Duplicate` |
| Each claim `containers` must have at least one entry | `spec.devices.claims[i].containers: Required` |
| Container names must be non-empty and unique within a claim | `spec.devices.claims[i].containers[j]: Required/Duplicate` |
| `request.deviceClassName` must be non-empty | `spec.devices.claims[i].request.deviceClassName: Required` |
| `request.count` must be >= 1 (defaulted to 1 if 0) | `spec.devices.claims[i].request.count: Invalid` |
| Each selector must be non-empty | `spec.devices.claims[i].request.selectors[k]: Required` |

### Defaulting

| Condition | Default |
|---|---|
| `spec.devices` is set but `mode` is empty | `mode = Disabled` |
| `spec.devices.claims[i].request.count` is 0 | `count = 1` |

### Immutability

The `spec.devices` field is **mutable** between run attempts. The operator
will update or recreate ResourceClaimTemplates when the device spec changes.

---

## How Phase 7 specs continue to behave unchanged

1. When `spec.devices` is nil (the Phase 7 default), the webhook does not
   inject any device defaults or status.
2. The `spec.devices` field is fully optional; omitting it produces
   identical behavior to Phase 7.
3. All Phase 7 status fields (`launchGate`, `provisioning`,
   `startupRecovery`, `capacity`, `multiCluster`) are preserved unchanged.
4. The `spec.identity.gpuShape` field remains required and is independent
   of `spec.devices.claims[].request.deviceClassName`. Both fields coexist
   for Phase 0 compatibility.

---

## Example: minimal DRA RTJ

```yaml
apiVersion: training.checkpoint.example.io/v1alpha1
kind: ResumableTrainingJob
metadata:
  name: llm-train
spec:
  queueName: gpu-training
  workloadPriorityClassName: batch-medium
  identity:
    image: registry.example.com/training/llm:v1
    codeVersion: "git:abc123"
    worldSize: 4
    gpuShape: "nvidia-a100-80gb"
  devices:
    mode: DRA
    claims:
      - name: gpu
        containers: ["worker"]
        request:
          deviceClassName: gpu.example.com
          count: 8
          selectors:
            - 'device.attributes["memory"].compareTo(quantity("80Gi")) >= 0'
  runtime:
    mode: FSDP
    optimizerMode: adamw
    shardingMode: full
    template:
      spec:
        replicatedJobs:
          - name: trainer
            replicas: 4
  checkpoint:
    storageURI: s3://checkpoints/llm-train/
    interval: 5m
    freshnessBudget: 10m
    maxDrainTime: 15m
  resume:
    sourcePolicy: LatestCompatibleComplete
    maxResumeRetries: 3
```

## Example: Phase 7 RTJ (no devices)

```yaml
apiVersion: training.checkpoint.example.io/v1alpha1
kind: ResumableTrainingJob
metadata:
  name: llm-train
spec:
  queueName: gpu-training
  workloadPriorityClassName: batch-medium
  identity:
    image: registry.example.com/training/llm:v1
    codeVersion: "git:abc123"
    worldSize: 4
    gpuShape: "nvidia-a100-80gb"
  # devices: not set -- Phase 7 behavior
  runtime:
    mode: FSDP
    optimizerMode: adamw
    shardingMode: full
    template:
      spec:
        replicatedJobs:
          - name: trainer
            replicas: 4
  checkpoint:
    storageURI: s3://checkpoints/llm-train/
    interval: 5m
    freshnessBudget: 10m
    maxDrainTime: 15m
  resume:
    sourcePolicy: LatestCompatibleComplete
    maxResumeRetries: 3
```
