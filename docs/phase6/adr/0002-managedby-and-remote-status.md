# ADR 0002: ManagedBy Field and Remote Status for MultiKueue

- **Status:** Accepted
- **Date:** 2026-03-30
- **Context:** Phase 6 RTJ API extension for MultiKueue external-framework
  support.

## Decision

Extend the RTJ API with `spec.managedBy` and `status.multiCluster` to
support MultiKueue external-framework dispatch without adding a separate
user-facing mode switch.

## Context

Phase 6 Goal G1 requires RTJ integration with Kueue's MultiKueue
external-framework for multi-cluster dispatch. The API must:

1. Signal to MultiKueue that an RTJ is eligible for dispatch.
2. Surface remote worker execution status on the manager-side RTJ.
3. Preserve single-cluster Phase 5 semantics as the default.
4. Avoid speculative fields for future custom dispatch policies.

Two API design patterns were considered.

## Alternatives Considered

### Alternative A: Separate Mode Field

Add a `spec.multiCluster.mode` field (e.g., `Disabled`, `MultiKueue`)
alongside `spec.managedBy`.

**Pros:**
- Explicit user-facing toggle for multi-cluster behavior.
- Could extend to custom dispatch modes in future phases.

**Rejected because:**
- Introduces a second signal alongside `managedBy`, creating ambiguity
  when they disagree.
- `managedBy` is the standard Kubernetes convention for external
  controller ownership (used by batch/v1 Job, Kueue's jobframework).
- Violates the hard boundary: "do NOT add speculative fields for later
  custom dispatch policy."
- The `managedBy` value already fully determines behavior: empty means
  Phase 5, `kueue.x-k8s.io/multikueue` means MultiKueue dispatch.

### Alternative B: ManagedBy Only (Selected)

Use `spec.managedBy` as the sole user-facing signal, following the
Kubernetes `managedBy` convention. Derive all behavioral differences
from this single field.

**Pros:**
- Smallest API surface.
- Follows established Kubernetes patterns.
- No ambiguity: one field, one source of truth.
- Forward-compatible: new controllers can be supported by new
  `managedBy` values without API changes.

## Decisions

### D1: Use `spec.managedBy` as the Sole MultiKueue Signal

The `spec.managedBy` field is the only user-authored field that controls
multi-cluster behavior. No separate mode switch, feature flag, or
annotation is needed.

When `spec.managedBy` is empty or absent, the RTJ follows the Phase 5
single-cluster path. When set to `kueue.x-k8s.io/multikueue`, the RTJ
is eligible for MultiKueue dispatch.

### D2: ManagedBy Is Immutable

Once set at creation time, `spec.managedBy` cannot be changed or removed.
This prevents runtime ambiguity about which controller owns the Workload
lifecycle.

**Rationale:** Changing the management controller mid-lifecycle would
require coordinated handoff between controllers (e.g., cleaning up
remote copies, re-creating local child JobSets). This is complex and
error-prone. Immutability eliminates this class of bugs.

### D3: ManagedBy Validation Requires Domain Prefix

When non-empty, `managedBy` must contain at least one `/` character,
following the Kubernetes convention of domain-prefixed controller names
(e.g., `kueue.x-k8s.io/multikueue`). Maximum length is 256 characters.

The validation does not restrict to a whitelist of known values. This
allows forward-compatible support for new external controllers without
API changes.

### D4: Remote Status Lives Under `status.multiCluster`

All manager-side remote execution status is grouped under a single
`status.multiCluster` struct. This struct is:
- **Nil in single-cluster mode:** No allocation, no confusion.
- **Controller-owned:** Users must not write to it.
- **Self-contained:** All multi-cluster observability in one place.

Fields:

| Field | Purpose |
| --- | --- |
| `dispatchPhase` | High-level dispatch lifecycle (Pending/Dispatched/Active) |
| `nominatedClusters` | Clusters considered for dispatch |
| `executionCluster` | Current worker cluster |
| `remoteObjectRef` | Reference to remote RTJ copy |
| `remotePhase` | Mirror of worker's `.status.phase` |
| `remoteCheckpoint` | Summary of worker's latest checkpoint |
| `remoteObservedGeneration` | Sync marker for spec propagation |
| `localExecutionSuppressed` | Boolean: local execution suppressed |

### D5: Remote Checkpoint Is a Summary, Not a Full Copy

`status.multiCluster.remoteCheckpoint` contains only the checkpoint ID,
completion time, and storage URI. It does not duplicate the full
`CheckpointReference` struct (which includes compatibility state,
manifest URI, source run attempt, etc.).

**Rationale:** The manager does not perform checkpoint I/O (ADR-0001 D5).
It only needs enough information to display status and detect progress.
The full checkpoint metadata lives on the worker's RTJ status. If
detailed inspection is needed, the operator can query the worker directly.

### D6: `localExecutionSuppressed` Is a Derived Indicator

The `localExecutionSuppressed` boolean is always `true` when
`spec.managedBy` is set to the MultiKueue value and the operator runs
in manager mode. It provides an explicit, easily queryable signal that
the manager is NOT creating local child JobSets for this RTJ.

This is technically derivable from `spec.managedBy` + operator mode, but
exposing it as a status field:
- Makes kubectl/dashboard queries simpler (`status.multiCluster.localExecutionSuppressed=true`).
- Documents the runtime decision explicitly in the API.
- Avoids requiring external tools to know the operator's startup mode.

### D7: Dispatch Phase Is Separate from Training Phase

`status.multiCluster.dispatchPhase` (Pending/Dispatched/Active) tracks
the multi-cluster dispatch lifecycle. `status.multiCluster.remotePhase`
mirrors the training lifecycle from the worker.

These are independent dimensions:
- Dispatch can be `Active` while training is `Paused`.
- Dispatch can be `Dispatched` while training is still `Pending`.

Collapsing them into a single phase would lose information.

## Consequences

### Positive

- Smallest possible API surface for MultiKueue support.
- Follows Kubernetes `managedBy` convention -- familiar to Kueue contributors.
- All Phase 5 behavior preserved by default (zero-value semantics).
- No speculative fields: each field has a concrete Phase 6 use case.
- Clean separation of dispatch and training lifecycle phases.

### Negative

- `managedBy` immutability prevents runtime re-homing of an RTJ from
  single-cluster to MultiKueue (must delete and recreate).
- `remoteCheckpoint` summary requires querying the worker for full
  checkpoint details.

### Neutral

- CRD schema grows by one spec field and one status section. Impact on
  etcd storage is minimal (status is only populated for MultiKueue-managed
  RTJs).
- No new required fields. No new printer columns.
