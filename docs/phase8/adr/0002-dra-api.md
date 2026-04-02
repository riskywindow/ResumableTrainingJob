# ADR 0002: DRA API Shape for RTJ

## Status

Accepted

## Date

2026-04-01

## Context

### Problem

Phase 8 adds native DRA device requests to the RTJ API. The core design
question is: what API shape should the RTJ use to carry DRA device
configuration, and how does the operator translate it into Kubernetes DRA
objects?

ADR 0001 decided that the RTJ would use native DRA ResourceClaimTemplates
(Option C). This ADR decides the specific API shape within the RTJ spec.

### Design constraints

From the task requirements:

1. The core path must support devices disabled vs enabled.
2. The core path must support a DRA mode.
3. The core path must support one or more worker claim-template specs.
4. Each claim must have container attachment targets.
5. Each claim must have a per-claim name.
6. The RTJ spec must carry a constrained DRA request fragment, not the
   full DRA API surface.
7. The operator materializes companion ResourceClaimTemplate objects
   from the user-authored fragments.
8. Phase 7 behavior must be preserved when devices are not configured.

### Options considered

**Option A: Single flat DeviceSpec**

```go
type DeviceSpec struct {
    DeviceClassName string   `json:"deviceClassName"`
    Selectors       []string `json:"selectors,omitempty"`
    Count           int32    `json:"count,omitempty"`
    ClaimName       string   `json:"claimName,omitempty"`
}
```

This was the Session 1 design from the architecture doc. Rejected because:
- Supports only a single device class per RTJ.
- No container attachment targets.
- No support for multiple claims (e.g., GPU + RDMA).
- Cannot evolve to multi-claim without breaking changes.

**Option B: Full DRA ResourceClaimTemplateSpec embedding**

```go
type DeviceSpec struct {
    Mode     DeviceMode                         `json:"mode"`
    Template resourcev1beta2.ResourceClaimTemplateSpec `json:"template"`
}
```

Embed the full upstream DRA type. Rejected because:
- Exposes the entire DRA API surface, most of which is not relevant.
- Creates a maintenance burden tracking upstream DRA API changes.
- Violates the constraint to use a "constrained subset."
- Makes validation harder (must reject unsupported fields).

**Option C: Structured claims with constrained request fragments (selected)**

```go
type DeviceSpec struct {
    Mode   DeviceMode        `json:"mode"`
    Claims []DeviceClaimSpec `json:"claims,omitempty"`
}

type DeviceClaimSpec struct {
    Name       string            `json:"name"`
    Containers []string          `json:"containers"`
    Request    DeviceRequestSpec `json:"request"`
}

type DeviceRequestSpec struct {
    DeviceClassName string   `json:"deviceClassName"`
    Count           int32    `json:"count,omitempty"`
    Selectors       []string `json:"selectors,omitempty"`
}
```

Selected because:
- Supports one or more claims per RTJ.
- Each claim has explicit container attachment targets.
- Each claim has a unique name used for ResourceClaimTemplate naming.
- The `DeviceRequestSpec` is a constrained subset of DRA DeviceRequest
  exposing only `deviceClassName`, `count`, and CEL `selectors`.
- The `mode` field provides an explicit disabled/enabled toggle.
- The design naturally evolves: new DRA fields can be added to
  `DeviceRequestSpec` without breaking changes.

**Option D: Reuse upstream DRA types via type aliases**

Use Go type aliases to reference upstream DRA request types. Rejected
because:
- Upstream DRA types may not be available in the pinned k8s client-go
  version at the exact field path needed.
- Type aliases create implicit coupling to upstream API changes.
- A constrained subset with explicit fields is safer for validation.

## Decision

### Option C: structured claims with constrained request fragments

The RTJ spec gains `spec.devices` with:

1. **`mode`** (`DeviceMode`): `Disabled` (default) or `DRA`. Explicit
   toggle for DRA functionality.

2. **`claims`** (`[]DeviceClaimSpec`): List of claim templates. Each
   produces one ResourceClaimTemplate owned by the RTJ.

3. **`DeviceClaimSpec`**: Per-claim configuration with `name` (unique
   per RTJ), `containers` (attachment targets), and `request` (DRA
   fragment).

4. **`DeviceRequestSpec`**: Constrained subset of DRA DeviceRequest
   with `deviceClassName`, `count`, and `selectors`. These three fields
   are sufficient for Phase 8's core use case of structured accelerator
   requests.

### What part of DRA is exposed directly

| DRA concept | RTJ field | Exposed? |
|---|---|---|
| DeviceClass name | `request.deviceClassName` | Yes |
| Device count | `request.count` | Yes |
| CEL selectors | `request.selectors[]` | Yes (as string expressions) |
| Allocation mode | - | No (uses DRA default) |
| Admin access | - | No |
| Tolerations | - | No |
| Config references | - | No |
| Constraints | - | No |

### What the operator materializes

For each `DeviceClaimSpec`, the operator creates:
- A `ResourceClaimTemplate` named `<rtj-name>-<claim-name>` with
  ownerReference to the RTJ.
- The template's `spec.spec.devices.requests[]` contains one entry
  populated from the `DeviceRequestSpec`.

### Controller-owned status

`status.devices` is entirely controller-owned and includes:
- `deviceMode`: observed mode
- `requestedDeviceClasses`: summary of referenced classes
- `currentDeviceProfileFingerprint`: SHA256 for checkpoint compatibility
- `resourceClaimTemplateRefs`: materialized template references
- `claimAllocationState`: aggregate allocation state
- `allocatedClaimCount`, `lastClaimFailureReason`, `lastClaimFailureTime`
- `lastCheckpointDeviceProfileFingerprint`, `lastResumeDeviceProfileFingerprint`

### How Phase 7 specs continue to behave unchanged

When `spec.devices` is nil:
- No device defaults are injected.
- No device status is populated.
- No ResourceClaimTemplates are created.
- All Phase 7 features work identically.
- `spec.identity.gpuShape` remains the sole accelerator identity field.

## Consequences

### Positive

1. **Multi-claim support from day one**: The array-based design supports
   GPU + RDMA or GPU + NIC claims without API changes.
2. **Explicit container targeting**: Each claim declares which containers
   receive it, making the pod template injection deterministic.
3. **Constrained DRA surface**: Only three DRA fields are exposed,
   making validation straightforward and the API stable across DRA
   versions.
4. **Clean disabled path**: `mode: Disabled` explicitly opts out of DRA
   without requiring the field to be nil.
5. **Backward compatible**: Phase 7 manifests work unchanged.

### Negative

1. **Not a direct DRA type embed**: Users familiar with DRA must learn
   the RTJ-specific field names, though they map 1:1 to DRA concepts.
2. **CEL selectors are strings**: No compile-time validation of CEL
   expressions. Runtime validation deferred to DRA driver.
3. **Single request per claim**: Each `DeviceClaimSpec` has one
   `DeviceRequestSpec`. Multi-request claims would require API extension.

### Risks

1. **DRA API evolution**: If upstream DRA adds required fields, the
   constrained subset may become insufficient. Mitigated by the design's
   extensibility (new fields can be added to `DeviceRequestSpec`).
2. **CEL expression correctness**: Invalid CEL expressions are only
   caught at claim allocation time, not at RTJ creation. Mitigated by
   conservative validation of non-empty strings.

## Verification

| Criterion | Test |
|---|---|
| Phase 7 spec without devices passes validation | `TestWebhookValidateCreateAcceptsPhase7ManifestForPhase8` |
| DRA mode with valid claims passes validation | `TestWebhookValidateCreateAcceptsDRADeviceSpec` |
| Multiple claims with unique names pass validation | `TestWebhookValidateCreateAcceptsMultipleClaims` |
| DRA mode without claims rejected | `TestWebhookValidateCreateRejectsDRAWithoutClaims` |
| Disabled mode with claims rejected | `TestWebhookValidateCreateRejectsDisabledWithClaims` |
| Duplicate claim names rejected | `TestWebhookValidateCreateRejectsDuplicateClaimNames` |
| Empty containers rejected | `TestWebhookValidateCreateRejectsEmptyContainers` |
| Missing deviceClassName rejected | `TestWebhookValidateCreateRejectsMissingDeviceClassName` |
| Empty selectors rejected | `TestWebhookValidateCreateRejectsEmptySelector` |
| Deep copy independence verified | `TestWebhookDeepCopyIndependenceForDeviceSpec` |
| Status deep copy independence verified | `TestWebhookDeepCopyIndependenceForDeviceStatus` |
| Defaulting does not inject device status | `TestWebhookDefaultDoesNotInjectPhase8Status` |
| Status preserved through webhook updates | `TestWebhookValidateUpdatePreservesPhase8StatusFields` |
