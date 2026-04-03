# Phase 8: DRA-Aware Rendering and Kueue Accounting

## Overview

Phase 8 Session 4 integrates DRA (Dynamic Resource Allocation) into two
critical paths:

1. **Kueue Workload PodSet synthesis** -- so Kueue can account DRA
   device classes via `deviceClassMappings`.
2. **Child JobSet rendering** -- so worker pods reference the correct
   ResourceClaimTemplate objects and containers declare their device
   claims.

Both paths are additive: when `spec.devices` is nil or `mode=Disabled`,
no DRA fields appear, preserving exact Phase 7 behavior.

## Architecture

### Kueue PodSet Synthesis (Admission/Accounting)

When `PodSetsFromRTJTemplate()` is called by the Kueue generic adapter:

1. The RTJ's `spec.devices.claims[]` are iterated.
2. For each claim, a `PodResourceClaim` is added to the PodSet's pod
   template with `ResourceClaimTemplateName` pointing at the
   deterministically-named companion template (`<rtj-name>-<claim-name>`).
3. Container `Resources.Claims[]` entries are added for targeted
   containers only.
4. PodSets whose pod template has no matching containers are skipped.

This enables Kueue's `deviceClassMappings` to resolve the device class
from the ResourceClaimTemplate and account it against ClusterQueue
capacity.

### Child JobSet Rendering (Launch)

When `RenderChildJobSet()` produces the child JobSet:

1. The new `DRAClaims` field on `RenderInput` carries the claim
   injections built from the RTJ's spec + status.
2. `InjectDRAClaims()` runs as the **last** injection step (after
   topology and podSetUpdates) to compose cleanly.
3. For each replicatedJob, claims are injected only when at least one
   container matches the claim's target container list.
4. `PodResourceClaim` entries use `ResourceClaimTemplateName` pointing at
   the actual template names from `status.devices.resourceClaimTemplateRefs`.
5. Container `Resources.Claims[]` entries attach claims to targeted
   containers.

### Controller Integration

The main reconcile loop calls `reconcileDRATemplates()` early (after
manager-mode check, before launch gate evaluation). This ensures:

- ResourceClaimTemplate objects exist before Kueue evaluates the
  Workload for admission.
- `status.devices` is populated with template refs before rendering.
- The launch is gated on `TemplatesReady` -- the controller will not
  create a child JobSet until all companion templates exist.

### Materialization Order

```
1. RTJ created with spec.devices
2. reconcileDRATemplates() creates ResourceClaimTemplate objects
   and populates status.devices
3. Kueue creates Workload from PodSets (includes DRA pod spec fields)
4. Kueue admits Workload (accounts DRA devices via deviceClassMappings)
5. RTJ controller clears suspend, evaluates launch gates
6. Child JobSet rendered with DRA claim references
7. Kubelet creates ResourceClaims from templates, allocates devices
```

## Container Targeting

Each `DeviceClaimSpec` includes a `containers[]` field specifying which
containers receive the claim:

- **Pod-level**: A `PodResourceClaim` is added to `pod.spec.resourceClaims`
  only if at least one container in the pod matches.
- **Container-level**: `container.resources.claims[]` entries are added
  only to matching containers.
- **Empty targets**: When `containers` is empty, all containers in the
  pod receive the claim (defensive; validation normally requires at least
  one container).

This prevents over-allocation: if a driver/leader pod has no matching
containers, no DRA claims are injected into that PodSet or replicatedJob.

## Idempotency

Both `InjectDRAClaims()` and `injectDRAIntoPodSets()` are idempotent:
- Duplicate `PodResourceClaim` entries are detected by name and skipped.
- Duplicate `ResourceClaim` entries on containers are detected and skipped.

## Backward Compatibility

When `spec.devices` is nil or `mode=Disabled`:
- `PodSetsFromRTJTemplate()` does not inject any DRA fields.
- `RenderChildJobSet()` does not call `InjectDRAClaims()`.
- `reconcileDRATemplates()` returns `TemplatesReady=true` immediately.
- No ResourceClaimTemplate objects are created.
- All Phase 7 features work identically.

## Files

| File | Role |
|------|------|
| `internal/jobset/dra_render.go` | DRA claim injection logic, `BuildDRAClaimInjections()`, `InjectDRAClaims()` |
| `internal/jobset/render.go` | Updated `RenderInput` and `RenderChildJobSet()` pipeline |
| `internal/kueue/rtj_podsets.go` | Updated `PodSetsFromRTJTemplate()` with DRA injection |
| `internal/controller/resumabletrainingjob_controller.go` | Wired `reconcileDRATemplates()`, RBAC marker, launch gate |
| `internal/controller/resume_flow.go` | Populates `DRAClaims` in simple launch path |
| `internal/controller/launch_plan.go` | Populates `DRAClaims` in plan-based launch path |

## Tests

### DRA Render Tests (`dra_render_test.go`)
- Single claim injection
- Multiple claim injection
- Targeted container attachment
- Non-matching container skip
- Empty claims no-op
- Idempotent injection
- Multi-replicatedJob targeting
- All-containers-when-empty behavior
- BuildDRAClaimInjections from spec+status
- Disabled/NoStatus/MissingRef edge cases

### Render Integration Tests (`render_test.go`)
- DRA claims injected in rendered child JobSet
- No DRA when not configured
- Kueue management labels still stripped with DRA active
- DRA and topology coexist

### PodSet Synthesis Tests (`rtj_podsets_test.go`)
- DRA claims injected into PodSets
- No DRA when disabled/nil
- Container targeting
- Non-matching container skip
- Template name format verification
- Multiple claims
- Phase 3 partial admission preserved with DRA
