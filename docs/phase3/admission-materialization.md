# Admission Materialization

## Overview

Phase 3 introduces an **admission-aware** path for ResumableTrainingJob. When
Kueue admits an RTJ, the Workload's `PodSetAssignments` carry per-pod-set
admission decisions: the admitted `count`, assigned `ResourceFlavor` (with
`nodeSelector` and `tolerations`), and resource usage. Phase 3 materializes
these decisions faithfully into the child JobSet launch shape and checkpoint
selection logic.

This document covers:

1. The `AdmissionView` abstraction for reading Kueue admission state.
2. World-size-aware checkpoint compatibility and selection.
3. Admitted counts and flavors surfaced in RTJ status.
4. Restore-mode tracking for same-size vs. reshard resumes.

## AdmissionView Abstraction

### Location

`internal/kueue/admission_view.go`

### Purpose

`AdmissionView` is a read-only internal snapshot of the Kueue admission state
for an RTJ. It decouples the admission data extraction from the consumers
(controller, renderer) so that:

- The controller can compute admitted world size and populate status.
- The renderer can apply nodeSelector, tolerations, and replica counts to
  the child JobSet (Phase 3 G2, not yet wired).
- Checkpoint selection can target the admitted count, not the original
  preferred count.

### Construction Paths

```
┌─────────────────────────────┐
│ Kueue RunWithPodSetsInfo()  │
│ (receives []PodSetInfo)     │
└────────────┬────────────────┘
             │ FromPodSetsInfo(names, infos)
             v
    ┌─────────────────┐
    │  AdmissionView   │
    │  - PodSets[]     │
    │    - Name        │
    │    - Count       │
    │    - NodeSelector│
    │    - Tolerations │
    └─────────────────┘

┌─────────────────────────────────────┐
│ Workload.Status.Admission           │
│ (has PodSetAssignments with Flavors)│
└────────────┬────────────────────────┘
             │ FromWorkloadAdmission(admission)
             v
    ┌─────────────────┐
    │  AdmissionView   │
    │  - PodSets[]     │
    │    - Name        │
    │    - Count       │
    │    - Flavors     │
    │  - ClusterQueue  │
    └─────────────────┘
```

`FromPodSetsInfo` is used during `RunWithPodSetsInfo` to capture counts,
nodeSelector, and tolerations. Flavor names are NOT available through
`PodSetInfo`; they are only available through the Workload admission status.

`FromWorkloadAdmission` is used when the controller reads the Workload object
directly to extract flavor names and counts.

### Key Methods

| Method | Returns | Purpose |
| --- | --- | --- |
| `TotalAdmittedCount()` | `int32` | Sum of all pod set counts |
| `FlavorsByPodSet()` | `map[string]string` | Pod set name → comma-separated flavor names |
| `PodSetByName(name)` | `(PodSetAdmission, bool)` | Lookup a single pod set |
| `IsEmpty()` | `bool` | True when no assignments are present |

### Data Independence

Both constructors deep-copy input data. Mutating the original `PodSetInfo`
or `Admission` after construction does not affect the `AdmissionView`.

## World-Size-Aware Checkpoint Compatibility

### Location

`internal/checkpoints/compatibility.go`

### Changes

`ResumeRequest` gains a new field:

```go
AllowWorldSizeChange bool
```

When `AllowWorldSizeChange` is `false` (the default, Phase 2 behavior), the
compatibility checker requires exact world-size match.

When `AllowWorldSizeChange` is `true` and the world sizes differ, the checker
requires the manifest to declare `CrossSizeRestoreSupported = true`. This
ensures that:

- Phase 2 manifests (where `CrossSizeRestoreSupported` is `nil`) are never
  selected for cross-size restore.
- Only Phase 3 checkpoints that explicitly declare DCP resharding support
  can be used for different-size resume.

All other compatibility dimensions remain strict and unchanged:

- Cluster identity
- RTJ lineage identity
- Runtime mode
- GPU shape
- Image identity
- Code version identity
- Format version
- Optimizer mode
- Sharding mode

### CheckpointManifest Phase 3 Field

`internal/checkpoints/types.go` gains:

```go
CrossSizeRestoreSupported *bool `json:"crossSizeRestoreSupported,omitempty"`
```

`nil` is treated as `false` (Phase 2 backward compatibility).

`CheckpointReference()` now includes `WorldSize` in the returned reference
for observability and restore tracking.

### Decision Matrix

| `AllowWorldSizeChange` | Manifest WorldSize == Request WorldSize | `CrossSizeRestoreSupported` | Result |
| --- | --- | --- | --- |
| `false` | yes | any | Compatible |
| `false` | no | any | Rejected: "world size mismatch" |
| `true` | yes | any | Compatible |
| `true` | no | `true` | Compatible (cross-size) |
| `true` | no | `nil` or `false` | Rejected: "checkpoint does not support cross-size restore" |

## Checkpoint Selection

### Location

`internal/checkpoints/selector.go`

### Behavior

The selector (`SelectLatestCompatible`) is unchanged in its algorithm. It:

1. Sorts candidates by completion timestamp (latest first).
2. Iterates and calls `CheckManifestCompatibility` for each.
3. Returns the first compatible match.

Phase 3 changes flow through `CheckManifestCompatibility`:

- When `AllowWorldSizeChange` is set in the `ResumeRequest`, checkpoints
  at different world sizes become compatible (if they declare cross-size
  restore support).
- The caller sets `ResumeRequest.WorldSize` to the **admitted** count,
  not the original preferred count.

## Status Helpers

### Location

`internal/controller/status_helpers.go`

### New Helpers

**`syncAdmissionStatus`**: Updates `status.admission` with:
- `admittedWorkerCount`: the Kueue-admitted pod count for the worker pod set.
- `preferredWorkerCount`: the original preferred count from the RTJ spec.
- `admittedFlavors`: pod set name → flavor name map.

**`clearAdmissionStatus`**: Resets `status.admission` to nil when not admitted.

**`syncRestoreStatus`**: Records restore details in `status.restore`:
- `lastCheckpointWorldSize`: world size from the checkpoint manifest.
- `lastRestoreWorldSize`: admitted world size at restore time.
- `restoreMode`: `SameSize` or `Reshard` (auto-computed from the world sizes).

### Idempotency

All helpers are idempotent: they return `false` when the current status already
matches the desired state, avoiding unnecessary status updates.

## Test Coverage

### Admission View Tests (16 tests)

| Test | What It Verifies |
| --- | --- |
| `TestFromPodSetsInfoBuildsPodSetAdmissions` | Correct parsing of pod set names, counts, nodeSelector, tolerations |
| `TestFromPodSetsInfoReturnsNilOnMismatchedLengths` | Nil return when names/infos don't align |
| `TestFromPodSetsInfoReturnsNilOnEmptyInput` | Nil return for nil/empty input |
| `TestFromWorkloadAdmissionExtractsCountsAndFlavors` | Correct extraction of counts and flavor names from Admission |
| `TestFromWorkloadAdmissionReturnsNilForNilInput` | Nil return for nil Admission |
| `TestFromWorkloadAdmissionReturnsNilForEmptyAssignments` | Nil return for empty assignments |
| `TestFromWorkloadAdmissionMultiplePodSets` | Multiple pod set extraction |
| `TestTotalAdmittedCount` | Sum of pod set counts |
| `TestTotalAdmittedCountNilView` | Zero for nil view |
| `TestFlavorsByPodSet` | Flavor name extraction |
| `TestFlavorsByPodSetMultipleResources` | Sorted comma-separated flavor names |
| `TestFlavorsByPodSetNilView` | Nil for nil view |
| `TestPodSetByName` | Lookup by name |
| `TestPodSetByNameNilView` | False for nil view |
| `TestIsEmpty` | Empty checks |
| `TestFromPodSetsInfoCopiesDataIndependently` | Deep copy independence |

### Compatibility Tests (11 tests)

| Test | What It Verifies |
| --- | --- |
| `TestCheckManifestCompatibilityAcceptsExactMatch` | Phase 2 same-size match |
| `TestCheckManifestCompatibilityRejectsWorldSizeMismatch` | Phase 2 strict rejection |
| `TestCheckManifestCompatibilityAllowsWorldSizeChangeWithCrossSizeSupport` | Phase 3 flexible match |
| `TestCheckManifestCompatibilityRejectsCrossSizeWhenManifestDoesNotSupport` | Phase 2 manifest rejection under flexible mode |
| `TestCheckManifestCompatibilityRejectsCrossSizeWhenExplicitlyFalse` | Explicit false rejection |
| `TestCheckManifestCompatibilitySameSizeWithAllowChangeStillWorks` | Same-size works even with AllowChange=true |
| `TestCheckManifestCompatibilityWorldSizeMismatchWithoutAllowChange` | AllowChange=false rejects even with CrossSizeSupport=true |
| `TestCheckManifestCompatibilityPreservesStrictDimensionChecks` | 4 subtests: cluster, RTJ, GPU shape, image identity strict checks |

### Selector Tests (8 tests)

| Test | What It Verifies |
| --- | --- |
| `TestSelectLatestCompatibleSkipsNewerIncompatibleManifest` | Skips world-size-incompatible newer checkpoint |
| `TestSelectLatestCompatibleRejectsIncompleteManifest` | Rejects missing completion timestamp |
| `TestSelectLatestCompatiblePrefersCompletedTimestampOverInvalidTimestamp` | Valid timestamp wins |
| `TestSelectLatestCompatibleDifferentSizeWithAllowance` | Cross-size selection with AllowWorldSizeChange |
| `TestSelectLatestCompatibleDifferentSizeRejectsWithoutCrossSizeSupport` | Rejection of Phase 2 manifest in flexible mode |
| `TestSelectLatestCompatiblePrefersLatestAmongMultipleCrossSizeCompatible` | Latest-first among multiple cross-size-compatible |
| `TestSelectLatestCompatibleSkipsCrossSizeIncompatibleAndPicksOlder` | Falls back to older cross-size-compatible |
| `TestSelectLatestCompatibleSameSizeStillSelectedWithoutCrossSizeField` | Phase 2 backward compatibility |
| `TestSelectLatestCompatibleEmptyCandidates` | Empty input returns nothing |

## What This Session Does NOT Change

- The child JobSet renderer (`internal/jobset/render.go`) is not updated to
  consume the admission view. That is the next step (G2 rendering).
- The controller reconciliation loop is not updated to build `ResumeRequest`
  with admitted counts or to call `syncAdmissionStatus`. The helpers are
  ready for the next session to wire in.
- The PodSet synthesis (`internal/kueue/rtj_podsets.go`) is not updated to
  use `EffectivePreferredCount()` or `EffectiveMinCount()` yet.

## Phase 2 Backward Compatibility

When Phase 3 fields are not set:

- `AllowWorldSizeChange` defaults to `false` → strict world-size match.
- `CrossSizeRestoreSupported` is `nil` on Phase 2 manifests → treated as `false`.
- `AdmissionView` returns `nil` when no admission data is present.
- Status helpers are no-ops when admission is nil.
- All existing tests pass unchanged.
