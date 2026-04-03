# Phase 8: Multi-Cluster DRA Compatibility

## Overview

Phase 8 introduces DRA (Dynamic Resource Allocation) support for RTJ via
companion `ResourceClaimTemplate` objects, DRA-aware child JobSet rendering,
and device-profile-aware checkpoint compatibility. This document explains
how Phase 8 integrates with the Phase 6 manager/worker multi-cluster
architecture.

**Key invariant**: DRA helper objects (ResourceClaimTemplates, ResourceClaims)
are created **only on the executing worker cluster**. The manager cluster
never creates these objects for remote RTJs.

## What Phase 8 changes on workers

When an RTJ with `spec.devices.mode=DRA` is reconciled on a **worker**
cluster (either single-cluster or as a MultiKueue mirror copy), the worker
operator performs the full Phase 8 path:

1. **Template reconciliation** (`reconcileDRATemplates`): creates one
   `ResourceClaimTemplate` per `spec.devices.claims[]` entry, owned by the
   RTJ with `controller=true`. Templates are named
   `<rtj-name>-<claim-name>`.

2. **Device status sync**: populates `status.devices` with the device
   profile fingerprint, requested device classes, and template references.

3. **Template readiness gate**: defers child JobSet creation until all
   templates exist and match the spec.

4. **Claim injection**: injects DRA claim references into the rendered
   child JobSet pod templates (container-scoped).

5. **Claim status observation** (`observeDRAClaimStatus`): tracks claim
   allocation state and surfaces failures via `status.devices` and
   conditions.

6. **Checkpoint compatibility**: device profile fingerprints are included
   in checkpoint manifests and checked on resume (fail-closed).

All of this is **identical** to the single-cluster Phase 8 path. The
worker-side RTJ (created by the MultiKueue adapter) goes through the
same `Reconcile()` path as any local RTJ because the adapter strips
`spec.managedBy` before creating the mirror copy.

## What remains the same on manager

The manager cluster behavior is **unchanged** from Phase 6/7 with one
additive observability enhancement:

1. **Runtime suppression preserved**: `ShouldSuppressRuntime()` returns
   `true` for MultiKueue-managed RTJs, causing `reconcileManagerIntent()`
   to run instead of the full reconciliation path. This return happens
   **before** `reconcileDRATemplates()` in the `Reconcile()` function,
   so the manager never reaches the DRA template code.

2. **No local ResourceClaimTemplates**: the manager does not create
   `ResourceClaimTemplate` objects for remote RTJs. There is no exception
   to this rule.

3. **No local child JobSets**: unchanged from Phase 6.

4. **No local ResourceClaims**: follows from (2) — claims are created by
   the kubelet from templates, and since no templates exist on the manager,
   no claims are created.

5. **Remote DRA status surfaced**: when the Kueue adapter mirrors the
   worker's full `.status` to the manager-side RTJ, Phase 8
   `status.devices` fields are included in the mirror. The manager
   controller logs these fields for observability via `buildRemoteDRASummary()`.
   This is the same pattern used for Phase 7 launch status
   (`buildRemoteLaunchSummary()`).

6. **No status.devices on manager-created RTJs**: the manager's
   `reconcileManagerIntent()` does not call `reconcileDRATemplates()` or
   `syncDeviceStatus()`, so `status.devices` is only populated if the
   Kueue adapter mirrors it from the worker.

## DRA helper object lifecycle in multi-cluster

```
Manager cluster                 Worker cluster (selected by MultiKueue)
─────────────────               ────────────────────────────────────────
RTJ created by user             RTJ mirror created by Kueue adapter
  spec.managedBy=multikueue       spec.managedBy="" (stripped)
  spec.devices.mode=DRA            spec.devices.mode=DRA
                                   ↓
ShouldSuppressRuntime=true      ShouldSuppressRuntime=false
  ↓                                ↓
reconcileManagerIntent()        reconcileDRATemplates()
  - no templates created           - creates ResourceClaimTemplate(s)
  - no child JobSet                - syncs status.devices
  - logs remote DRA status         ↓
  - surfaces multiCluster.*     evaluateLaunchGates() → reconcileLaunch()
                                   - creates child JobSet with DRA claims
                                   - kubelet creates ResourceClaims
                                   ↓
Kueue adapter mirrors           Worker status.devices populated:
worker .status to manager         - deviceMode: DRA
  ↓                               - fingerprint: sha256:...
Manager sees remote devices       - claimAllocationState: Allocated
via status.devices mirror
```

## Test coverage

### Unit tests (deterministic, no cluster needed)

| Test | What it proves |
|------|----------------|
| `TestBuildRemoteDRASummaryFullState` | DRA status fields correctly extracted from mirrored worker status |
| `TestBuildRemoteDRASummaryEmptyStatus` | Zero values for non-DRA workers (backward compat) |
| `TestBuildRemoteDRASummaryPendingClaims` | Pending allocation state correctly extracted |
| `TestHasPhase8RemoteStatus` (4 subtests) | Phase 8 detection: DRA active, Disabled, empty, nil |
| `TestManagerModeReflectsPhase8WorkerDRAStatus` | Manager preserves worker DRA status fields after reconcile |
| `TestManagerModePhase7WorkerHasNoPhase8DRAFields` | Manager works correctly with Phase 7 workers (no DRA) |
| `TestManagerModeDoesNotCreateResourceClaimTemplates` | Manager never creates templates even when spec.devices is DRA |

### E2e smoke test (requires Phase 6 multi-cluster environment)

| Test | What it proves |
|------|----------------|
| `TestMultiClusterDRASmoke` | Full multi-cluster DRA flow: manager suppression, worker DRA execution |

The e2e test verifies:
1. Manager does not create ResourceClaimTemplates (checked twice: before and after worker launch)
2. Manager does not create child JobSets (checked twice)
3. Manager reports `localExecutionSuppressed=true`
4. Worker-1 receives the mirror RTJ (adapter strips `spec.managedBy`)
5. Worker-1 creates ResourceClaimTemplates (if DRA CRD present)
6. Worker-1 creates child JobSet
7. Manager reflects remote execution cluster

### What is NOT covered by local tests

1. **Kueue deviceClassMappings across clusters**: requires worker-specific
   Kueue configuration. Covered by single-cluster Phase 8 e2e tests
   (`TestDRAQuotaAndAllocation`).

2. **Actual device allocation on worker**: requires real DRA driver or
   kubelet-level simulation. The e2e test verifies template creation but
   actual allocation depends on the cluster's DRA infrastructure.

3. **DRA + ProvisioningRequest interaction in multi-cluster** (OQ9):
   deferred. The interaction between Phase 7 provisioning and Phase 8 DRA
   in a multi-cluster setup is not tested. Single-cluster testing is the
   prerequisite.

4. **Cross-cluster DRA scheduling policy**: explicitly out of scope.
   MultiKueue cluster selection does not consider device availability.
   This would be a future phase concern.

5. **Device profile mismatch across workers**: if an RTJ is paused on
   worker-1 with device profile A and resumed on worker-2 with device
   profile B, the checkpoint compatibility check runs on the new worker.
   This is covered by single-cluster tests (`TestDRAIncompatibleResumeRejection`)
   but not in a multi-cluster setup.

## Design decisions

1. **No manager-local templates (Decision 12 from Session 1)**:
   ResourceClaimTemplates are worker-local. The manager cluster may not
   even have the DeviceClass or DRA driver installed. Creating templates
   on the manager would be wasteful and potentially invalid.

2. **Observability via status mirror**: rather than adding a new cross-
   cluster RPC for DRA status, we reuse the existing Kueue adapter
   status mirror. The adapter copies the entire `.status` from worker
   to manager, which naturally includes `status.devices`.

3. **No new MultiClusterStatus fields for DRA**: rather than adding
   `RemoteDeviceSummary` to `MultiClusterStatus` (which would require
   API changes and deep copy updates), we rely on the already-mirrored
   `status.devices` being present on the manager-side RTJ. Structured
   logging extracts the key fields for operator visibility.

4. **Guard is structural, not conditional**: the manager skips DRA
   template reconciliation because `ShouldSuppressRuntime()` returns
   before that code runs. This is a structural guarantee (code ordering)
   rather than a conditional check inside `reconcileDRATemplates()`.
   This design prevents accidental DRA object creation if someone
   reorders the reconcile loop.
