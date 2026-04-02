# DRA ResourceClaimTemplate Lifecycle

## Overview

Phase 8 introduces companion ResourceClaimTemplate objects that the RTJ
operator creates and manages alongside the RTJ. These templates declare
the DRA device requirements for worker pods and are referenced by the
child JobSet's pod templates.

This document describes the ownership model, reconciliation semantics,
and design decisions for the ResourceClaimTemplate lifecycle.

---

## Ownership model

**Decision: one stable ResourceClaimTemplate per RTJ claim spec.**

Each `DeviceClaimSpec` in `spec.devices.claims[]` produces exactly one
ResourceClaimTemplate object named `<rtj-name>-<claim-name>`. The
template is:

- **Owned by the RTJ** via `ownerReference` with `controller=true` and
  `blockOwnerDeletion=true`.
- **Not owned by the child JobSet.** Templates survive child JobSet
  deletion (pause/resume cycles). This is essential because the same
  template is reused across run attempts without recreation.
- **Namespace-scoped.** Templates live in the same namespace as the RTJ.
- **Labeled** with `training.checkpoint.example.io/rtj-name`,
  `training.checkpoint.example.io/claim-name`, and
  `training.checkpoint.example.io/managed-by=rtj-operator` for
  discovery and orphan cleanup.

### Why per-RTJ, not per-run?

Per-run templates (one template per run attempt) would provide stronger
isolation between run attempts but introduce unnecessary churn:

| Factor | Per-RTJ (chosen) | Per-run |
|--------|-------------------|---------|
| Template count | 1 per claim | N per claim (one per attempt) |
| Orphan cleanup | Simple (label-based) | Complex (must track attempts) |
| Pause/resume | Template survives | Must recreate on resume |
| GC on RTJ delete | Automatic via ownerRef | Same |
| Spec drift | Delete + recreate | New template per run |

Per-RTJ is materially simpler and sufficient for the Phase 8 scope.
The template's spec is immutable in practice (the kubelet creates
ResourceClaims from the template), so the operator detects spec drift
and recreates when necessary.

---

## Reconciliation semantics

### Entry point

`reconcileDRATemplates(ctx, job, now)` is the single entry point for
DRA template reconciliation. It should be called early in the reconcile
loop, before rendering the child JobSet.

### Algorithm

```
1. If devices not configured (nil spec or mode=Disabled):
   - Clear status.devices
   - Return (templates ready)

2. Build device profile fingerprint from spec.devices

3. Build desired template set from spec.devices.claims[]

4. For each desired template:
   a. GET existing template by name
   b. If not found → CREATE
   c. If found and spec matches → no-op (ready)
   d. If found and spec drifts → DELETE + CREATE (recreate)

5. List all templates with RTJ label in namespace
   - DELETE any template not in the desired set (orphan cleanup)

6. Sync status.devices:
   - deviceMode = DRA
   - requestedDeviceClasses = sorted, deduplicated class list
   - currentDeviceProfileFingerprint = SHA256 hash
   - resourceClaimTemplateRefs = sorted ref list
   - claimAllocationState = Pending (initial)
```

### Idempotency

The reconciliation is fully idempotent:

- **Create**: uses `apierrors.IsAlreadyExists` to handle races.
- **Get + compare**: `TemplateSpecMatches()` compares only the
  operator-controlled fields (DeviceClassName, Count, Selectors,
  AllocationMode).
- **Status sync**: `syncDeviceStatus()` compares the desired status
  with the current status and only writes when changed.
- **Fingerprint**: `BuildProfile()` produces a deterministic SHA256
  hash that is order-independent (claims and selectors are sorted).

### Spec drift handling

When the user modifies `spec.devices.claims[]` (changing DeviceClassName,
Count, or Selectors), the operator:

1. Detects the mismatch via `TemplateSpecMatches()`.
2. Deletes the existing template.
3. Creates a new template with the updated spec.
4. Updates `status.devices.currentDeviceProfileFingerprint`.

This is the "recreate" strategy (OQ7 resolution). In-place update is
not safe because:

- Active ResourceClaims created from the template reference the
  template's spec. Changing the template under active claims could
  cause undefined behavior.
- Delete + create ensures a clean break between device configurations.

### Orphan cleanup

When a claim is removed from `spec.devices.claims[]`, the operator:

1. Lists all ResourceClaimTemplates with the RTJ label.
2. Identifies templates not in the desired set.
3. Verifies ownership via `ownerReference` (defensive).
4. Deletes orphaned templates.

This handles the case where claims are added, removed, or renamed.

### Garbage collection

On RTJ deletion, Kubernetes garbage collection automatically deletes
all ResourceClaimTemplates via the `ownerReference` with
`blockOwnerDeletion=true`. No explicit cleanup is needed.

---

## Device profile fingerprint

The device profile fingerprint is a SHA256 hash of the canonical
representation of all DRA device requirements. It is:

- **Stable**: the same spec always produces the same fingerprint.
- **Order-independent**: claim order and selector order do not affect
  the fingerprint.
- **Sensitive to hardware changes**: different DeviceClassName, Count,
  or Selectors produce different fingerprints.
- **Insensitive to container targets**: which containers receive the
  claim does not affect the fingerprint (container binding is a
  rendering concern, not a hardware requirement).

### Canonical form

```
class=<deviceClassName>;selectors=<sorted,comma-joined selectors>;count=<count>
```

Multiple claims are joined with newlines and sorted. The SHA256 hash
of this string is the fingerprint.

### Checkpoint compatibility

The fingerprint is stored in `status.devices.currentDeviceProfileFingerprint`
and will be recorded in checkpoint manifests (future session). On resume,
the operator will compare the current fingerprint with the checkpoint's
fingerprint:

- **Match**: compatible, resume proceeds.
- **Mismatch**: incompatible, fail-closed (same as Phase 0 gpuShape
  mismatch).
- **Empty-to-empty**: compatible (Phase 7 behavior).
- **Empty-to-non-empty or non-empty-to-empty**: incompatible.

---

## Status fields

The reconciler populates `status.devices` with:

| Field | Source | Description |
|-------|--------|-------------|
| `deviceMode` | `spec.devices.mode` | Always `DRA` when templates exist |
| `requestedDeviceClasses` | Sorted, deduplicated from claims | Summary of requested hardware |
| `currentDeviceProfileFingerprint` | SHA256 of canonical form | For checkpoint compatibility |
| `resourceClaimTemplateRefs` | From desired templates | Maps claim names to template names |
| `claimAllocationState` | Initial: `Pending` | Tracks allocation progress |

Allocation-tracking fields (`allocatedClaimCount`, `lastClaimFailureReason`,
etc.) are preserved across status syncs and will be updated by future
allocation observation logic.

---

## Interaction with other phases

### Phase 7 backward compatibility

When `spec.devices` is nil or `mode=Disabled`:
- No ResourceClaimTemplates are created.
- `status.devices` is nil.
- All Phase 7 behavior is preserved unchanged.

### Manager mode (Phase 6)

Manager-mode RTJs do not create ResourceClaimTemplates. DRA claims are
worker-local, matching the Phase 6 manager/worker split. The
`reconcileDRATemplates` function should be gated on worker mode
(not called when `ShouldSuppressRuntime` returns true).

### Child JobSet rendering (future session)

The child JobSet renderer will use `status.devices.resourceClaimTemplateRefs`
to inject `spec.resourceClaims` and `container.resources.claims` into
the worker pod template. This is deferred to a future session.

### Kueue PodSet synthesis (future session)

The Workload PodSet synthesizer will include DRA device requests for
`deviceClassMappings`-based quota accounting. This is deferred to a
future session.
