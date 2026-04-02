# ADR 0001: DRA Accelerator-Native RTJ

## Status

Accepted

## Date

2026-04-01

## Context

### Problem

In Phase 7, RTJ declares GPU/accelerator requirements in two disconnected
ways:

1. **`spec.identity.gpuShape`**: An opaque string (e.g., `"nvidia-a100-80gb"`)
   used for resume-compatibility checks. It has no relationship to
   Kubernetes resource modeling.

2. **Extended-resource requests in the embedded JobSet template**: Container
   resource requests like `nvidia.com/gpu: 8`. Kueue accounts for these
   through standard resource-flavor quotas.

This approach has several limitations:

- **No structured device selection**: Extended resources are flat counters.
  There is no way to select devices by attributes (memory size, compute
  capability, interconnect type) without vendor-specific labels and node
  selectors.

- **No device lifecycle management**: Extended resources are claimed
  atomically at pod scheduling time. There is no pre-allocation, no
  claim reuse, and no claim lifecycle visible to the operator.

- **gpuShape is disconnected**: The operator cannot validate that the
  claimed devices match the gpuShape string. Resume compatibility relies
  on an honor-system string comparison.

- **Kueue quota is count-based**: Kueue accounts for extended resources
  as integer counts per flavor. DRA enables class-based and attribute-
  based accounting through `deviceClassMappings`.

Kubernetes Dynamic Resource Allocation (DRA) provides a structured
alternative: DeviceClasses, ResourceClaimTemplates, ResourceClaims, and
CEL-expression device selectors. Kueue already supports DRA device
accounting through `deviceClassMappings`.

### Options considered

**Option A: DRA extended-resource bridge**

Use the DRA extended-resource bridge to translate DRA device requests into
legacy extended-resource requests. Kueue accounts for them through existing
resource-flavor quotas.

Rejected because:
- The bridge is an alpha feature; stability risk.
- It loses structured device information (selectors, attributes).
- Kueue already has native DRA support; the bridge is unnecessary.
- It would require maintaining two parallel paths.

**Option B: Custom device abstraction in RTJ**

Invent a new RTJ-specific device abstraction layer that wraps both DRA
and extended resources. The operator translates the abstraction to the
appropriate Kubernetes mechanism.

Rejected because:
- It adds unnecessary indirection.
- It duplicates work that DRA already does.
- It creates a private API that must be maintained.
- Violates the hard boundary: "do NOT add speculative device APIs."

**Option C: Native DRA ResourceClaimTemplates (selected)**

The RTJ spec gains an optional `spec.devices` section using the Kubernetes
DRA vocabulary. The operator creates companion ResourceClaimTemplates.
Kueue uses `deviceClassMappings` for quota/accounting.

Selected because:
- Uses the Kubernetes-native DRA framework directly.
- Kueue already supports `deviceClassMappings`.
- Preserves structured device information (class, selectors, attributes).
- ResourceClaimTemplates are simple, namespace-scoped, ownable objects.
- Testable with an example DRA driver (no real hardware).
- Additive to Phase 7 (no Phase 7 behavior changes).

## Decision

### Phase 8 uses native DRA ResourceClaimTemplates

The RTJ operator creates a ResourceClaimTemplate when `spec.devices` is
present. The child JobSet's pod templates reference the template. Kubelet
creates per-pod ResourceClaims from the template. The DRA driver allocates
devices to each claim.

### Ownership split

| Object | Created by | Owned by | Managed by | Kueue-managed? |
|---|---|---|---|---|
| RTJ | User | User | RTJ Operator | **Yes** |
| Workload | RTJ Operator | RTJ | Kueue | No |
| ResourceClaimTemplate | RTJ Operator | RTJ | RTJ Operator | **No** |
| Child JobSet | RTJ Operator | RTJ | RTJ Operator | **No** |
| ResourceClaim (per-pod) | Kubelet | Pod | Kubelet + DRA driver | No |
| DeviceClass | Cluster admin | N/A | DRA driver | No |
| ResourceSlice | DRA driver | DRA driver | DRA driver | No |

**Key ownership rules:**

1. **RTJ is the ONLY Kueue-managed object.** This is unchanged from Phase 2.
2. **ResourceClaimTemplates are helper runtime objects.** They are owned
   by the RTJ via ownerReference and garbage-collected on deletion. They
   are NOT Kueue-managed workloads.
3. **ResourceClaims are per-pod objects** created by the kubelet from the
   template. They are owned by the pod and cleaned up when the pod is
   deleted.
4. **Kueue accounts for DRA devices** through `deviceClassMappings` on
   ClusterQueue ResourceGroups, not by watching claims or templates.

### Conservative compatibility rules for checkpoint resume

The Phase 0 ADR 0003 resume-compatibility contract is extended with two
new dimensions:

| Dimension | Match rule | Source |
|---|---|---|
| DeviceClass | Exact string match | `spec.devices.deviceClassName` |
| DeviceFingerprint | Exact hash match | `SHA256(sorted(spec.devices.selectors))` |

**Compatibility matrix:**

| Checkpoint devices | Resume devices | Result |
|---|---|---|
| None (Phase 7) | None (Phase 7) | Compatible (Phase 7 behavior) |
| None (Phase 7) | Present | Incompatible |
| Present | None (Phase 7) | Incompatible |
| Present (class=X, fp=Y) | Present (class=X, fp=Y) | Compatible |
| Present (class=X, fp=Y) | Present (class=X, fp=Z) | Incompatible |
| Present (class=X, fp=Y) | Present (class=W, fp=Y) | Incompatible |

**Rationale:**

- Device class determines the physical device type. Different classes may
  have different memory layouts, compute capabilities, or instruction sets.
  Training state (optimizer, gradient accumulator) may be incompatible
  across device classes.

- Device selectors constrain which devices within a class are used. A
  selector change (e.g., dropping a memory requirement) could result in a
  device with different characteristics, making the checkpoint unsafe.

- The fail-closed rule (Phase 0 ADR 0003) applies: when in doubt, reject
  the resume. This prevents silent data corruption or training divergence.

### Must-ship Phase 8 demo

The must-ship demo proves the end-to-end DRA flow:

1. Deploy RTJ operator (Phase 8 build).
2. Deploy example DRA driver with simulated devices.
3. Create DeviceClass resource for the example driver.
4. Configure ClusterQueue with `deviceClassMappings`.
5. Create an RTJ with `spec.devices`.
6. Observe:
   - RTJ operator creates ResourceClaimTemplate.
   - Kueue admits RTJ (deviceClassMappings quota check).
   - Launch gate opens (Phase 7 gate unchanged).
   - Child JobSet created with DRA claim references.
   - Worker pods scheduled; kubelet creates ResourceClaims.
   - DRA driver allocates simulated devices.
   - Pods reach Ready; RTJ transitions to Running.
7. Pause the RTJ:
   - Checkpoint written with device fingerprint.
   - Child JobSet deleted; ResourceClaimTemplate preserved.
   - RTJ transitions to Paused.
8. Resume the RTJ (same device spec):
   - Checkpoint compatibility verified (device class + fingerprint match).
   - Child JobSet recreated; claims re-allocated.
   - Trainer restores from checkpoint.
   - RTJ transitions to Running.
9. Demonstrate incompatible resume:
   - Modify `spec.devices.deviceClassName` while paused.
   - Resume attempt rejects the existing checkpoint.
   - RTJ either starts fresh or fails (depending on retry state).

### What is deferred or optional

| Item | Status | Rationale |
|---|---|---|
| Multi-device-class per RTJ | Deferred | Single class is sufficient for Phase 8 |
| Shared/manual ResourceClaims | Deferred | Per-pod templates are simpler and sufficient |
| DRA extended-resource bridge | Deferred/experimental | Alpha, not required when native claims work |
| Real vendor DRA drivers (NVIDIA, AMD) | Optional | Not required for local success; doc only |
| Device topology/affinity via DRA | Deferred | Kueue TAS (Phase 4) handles topology |
| Prioritized device alternatives | Deferred | Single class per RTJ for now |
| Automatic gpuShape derivation from deviceClassName | Deferred | Keep both fields independent for Phase 0 compat |
| ResourceClaim reuse across run attempts | Deferred | Clean claim per attempt is safer |
| DRA metrics (allocation latency, claim lifecycle) | Should-ship | Useful but not required for correctness |
| Device attribute capture in RTJ status | Should-ship | Useful for debugging |

## Consequences

### Positive

1. **Kubernetes-native device model**: RTJ uses the standard DRA framework
   instead of opaque resource counters.
2. **Structured device selection**: CEL selectors enable attribute-based
   device selection (memory, compute capability, interconnect).
3. **Kueue-native accounting**: `deviceClassMappings` provides DRA quota
   without a custom engine.
4. **Testable locally**: Example DRA driver enables full e2e without
   real hardware.
5. **Fail-closed resume safety**: Device fingerprint extends the Phase 0
   compatibility contract with no unsafe resume paths.
6. **Additive**: Phase 7 behavior is completely preserved when
   `spec.devices` is absent.

### Negative

1. **DRA maturity**: DRA is newer than extended resources. Some Kubernetes
   versions may have bugs or incomplete DRA support.
2. **Kubernetes version requirement**: DRA requires Kubernetes 1.31+ (beta)
   or 1.33+ (GA). Older clusters cannot use Phase 8.
3. **Dual path during migration**: Until extended resources are fully
   deprecated, RTJs may use either path. This creates documentation and
   support complexity.
4. **Example driver fidelity**: The example DRA driver may not exercise
   all code paths that a real vendor driver would.

### Risks

1. **Kueue deviceClassMappings availability**: If Kueue v0.15.1 does not
   support `deviceClassMappings`, Phase 8 is blocked until Kueue is
   upgraded. Mitigated by OQ2 audit.
2. **DRA API version churn**: The DRA API may change between beta and GA.
   Mitigated by OQ1 audit and pinning to a specific API version.
3. **Example DRA driver stability**: The upstream example driver may not
   be actively maintained. Mitigated by selecting a well-known driver
   and having a fallback plan.

## Verification

| Criterion | Test |
|---|---|
| ResourceClaimTemplate created with correct spec | Unit test |
| ResourceClaimTemplate owned by RTJ (ownerReference) | Unit test |
| Child JobSet pods reference ResourceClaimTemplate | Unit test |
| Kueue admits RTJ with deviceClassMappings quota | e2e test |
| Pods run with allocated DRA devices | e2e test with example driver |
| Checkpoint includes device fingerprint | Unit test |
| Resume with same device profile succeeds | Unit test + e2e |
| Resume with different device class rejected | Unit test |
| Resume with different selectors rejected | Unit test |
| Phase 7 RTJ (no devices) unchanged | e2e regression test |
| Manager-mode RTJ does not create RCT | Unit test |
| RTJ deletion garbage-collects RCT | e2e test |
