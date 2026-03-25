# Phase 3 API Reference

## Overview

Phase 3 extends the `v1alpha1` ResumableTrainingJob API with:

1. **Worker parallelism controls** (`spec.parallelism`) for preferred count, minimum count, pod set targeting, and per-job partial-admission opt-in.
2. **World-size-flexible resume** (`spec.resume.allowWorldSizeChange`) to allow DCP resharding across world sizes.
3. **Admitted-shape status** (`status.admission`, `status.restore`) for observability of Kueue admission decisions and checkpoint restore details.

All new fields are optional and default to values that preserve Phase 2 behavior.

## Spec Changes

### `spec.parallelism` (new, optional)

Configures the scalable worker group and partial admission. When nil, the
controller derives worker count from `spec.identity.worldSize` with no
partial admission (Phase 2 behavior).

```yaml
spec:
  parallelism:
    # Desired worker pod count for Kueue admission.
    # Defaults to spec.identity.worldSize when zero or unset.
    preferredCount: 8

    # Minimum acceptable worker pods (experimental, partial admission).
    # Only effective when enablePartialAdmission is true.
    # Must be >= 1 and <= preferredCount.
    minCount: 4

    # Which replicatedJob is the scalable worker group.
    # Defaults to the first replicatedJob. Others stay fixed-size.
    podSetName: "workers"

    # Per-job opt-in for Kueue partial admission (EXPERIMENTAL).
    # Requires spec.resume.allowWorldSizeChange=true.
    enablePartialAdmission: true
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `preferredCount` | int32 | `spec.identity.worldSize` | Desired worker count for Kueue PodSet.Count |
| `minCount` | *int32 | nil | Minimum worker count for Kueue PodSet.MinCount |
| `podSetName` | string | first replicatedJob | Scalable worker pod set name |
| `enablePartialAdmission` | bool | false | EXPERIMENTAL: enable partial admission |

### `spec.resume.allowWorldSizeChange` (new, optional)

```yaml
spec:
  resume:
    sourcePolicy: LatestCompatibleComplete
    maxResumeRetries: 3
    # NEW: permit resuming from a checkpoint at a different world size.
    # Requires DCP resharding in the trainer.
    allowWorldSizeChange: false  # default
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `allowWorldSizeChange` | bool | false | When true, world-size compatibility check is skipped on resume |

### `spec.identity.worldSize` (unchanged)

Remains the nominal world size for the training lineage. Compatibility checks
use this as the reference when `allowWorldSizeChange=false`. When
`spec.parallelism.preferredCount` is unset, this value is used for Kueue
pod set accounting.

## Status Changes

### `status.admission` (new, optional)

Captures the admitted shape from Kueue for the current or most recent admission.

```yaml
status:
  admission:
    admittedWorkerCount: 4
    preferredWorkerCount: 8
    activeWorkerCount: 4
    admittedFlavors:
      workers: "a100-80gb"
```

| Field | Type | Description |
| --- | --- | --- |
| `admittedWorkerCount` | int32 | Workers admitted by Kueue (0 when not admitted) |
| `preferredWorkerCount` | int32 | Effective preferred count at admission time |
| `activeWorkerCount` | int32 | Currently running worker pods (0 when no runtime) |
| `admittedFlavors` | map[string]string | Pod set name to ResourceFlavor name |

### `status.restore` (new, optional)

Captures details of the most recent checkpoint restore.

```yaml
status:
  restore:
    lastCheckpointWorldSize: 8
    lastRestoreWorldSize: 4
    restoreMode: Reshard
```

| Field | Type | Description |
| --- | --- | --- |
| `lastCheckpointWorldSize` | int32 | World size in the restored checkpoint |
| `lastRestoreWorldSize` | int32 | Effective world size at restore launch |
| `restoreMode` | RestoreMode | `SameSize` or `Reshard` |

### `status.selectedCheckpoint.worldSize` (new, optional)

The `CheckpointReference` type gains a `worldSize` field recording the world
size from the checkpoint manifest.

### Existing status fields (unchanged)

All existing status fields remain unchanged:

- `phase`, `conditions` - lifecycle state
- `workloadReference` - Kueue Workload reference
- `admittedClusterQueue` - admitted cluster queue name
- `currentSuspension` - suspension source and reason
- `currentRunAttempt`, `pauseRequestID` - run tracking
- `activeJobSetName`, `activeControlConfigMapName` - runtime resources
- `selectedCheckpoint`, `lastCompletedCheckpoint` - checkpoint references
- `transitionTimestamps` - lifecycle timestamps
- `reason`, `message`, `observedGeneration` - diagnostics

## Validation Rules

### Existing rules (preserved)

All Phase 2 validation rules remain unchanged and are applied first.

### New Phase 3 rules

| Rule | Field | Condition |
| --- | --- | --- |
| minCount <= preferredCount | `spec.parallelism.minCount` | When set, must be <= effective preferred count |
| minCount >= 1 | `spec.parallelism.minCount` | When set, must be positive |
| partial admission requires allowWorldSizeChange | `spec.parallelism.enablePartialAdmission` | Rejected if `spec.resume.allowWorldSizeChange=false` |
| partial admission requires minCount | `spec.parallelism.enablePartialAdmission` | Rejected if `spec.parallelism.minCount` is nil |

## Defaulting Rules

### Existing defaults (preserved)

All Phase 2 defaults remain unchanged.

### New Phase 3 defaults

| Field | Default | Notes |
| --- | --- | --- |
| `spec.parallelism` | nil | No parallelism section = Phase 2 behavior |
| `spec.resume.allowWorldSizeChange` | false | Phase 2 exact-match semantics |
| `spec.parallelism.preferredCount` | 0 (= `spec.identity.worldSize`) | Zero triggers fallback to worldSize |
| `spec.parallelism.enablePartialAdmission` | false | Experimental, off by default |

## Phase 2 Backward Compatibility

A Phase 2 spec with no `parallelism` section and no `allowWorldSizeChange` field:

```yaml
spec:
  identity:
    worldSize: 8
  resume:
    sourcePolicy: LatestCompatibleComplete
    maxResumeRetries: 3
  # no parallelism section
```

Maps to Phase 3 semantics as:

| Phase 3 Concept | Effective Value | Source |
| --- | --- | --- |
| Preferred worker count | 8 | `spec.identity.worldSize` |
| Min worker count | nil (all-or-nothing) | No `parallelism.minCount` |
| Partial admission | disabled | Default `enablePartialAdmission=false` |
| World-size change | disallowed | Default `allowWorldSizeChange=false` |
| Scalable pod set | first replicatedJob | Default |

Result: identical admission, scheduling, compatibility, and runtime behavior to Phase 2.

## Helper Methods

Three helper methods on `ResumableTrainingJob` make the effective values
accessible to controllers and renderers:

```go
// EffectivePreferredCount returns parallelism.preferredCount
// or falls back to identity.worldSize.
func (r *ResumableTrainingJob) EffectivePreferredCount() int32

// EffectiveMinCount returns parallelism.minCount when
// enablePartialAdmission is true, else nil.
func (r *ResumableTrainingJob) EffectiveMinCount() *int32

// EffectivePodSetName returns parallelism.podSetName
// or empty (controller treats empty as "first replicatedJob").
func (r *ResumableTrainingJob) EffectivePodSetName() string
```

## New Types Summary

```go
// RestoreMode: "SameSize" | "Reshard"
type RestoreMode string

// ParallelismSpec: worker scaling and partial admission config
type ParallelismSpec struct {
    PreferredCount         int32   // desired workers
    MinCount               *int32  // minimum workers (experimental)
    PodSetName             string  // scalable pod set
    EnablePartialAdmission bool    // EXPERIMENTAL opt-in
}

// AdmissionStatus: admitted shape from Kueue
type AdmissionStatus struct {
    AdmittedWorkerCount  int32
    PreferredWorkerCount int32
    ActiveWorkerCount    int32
    AdmittedFlavors      map[string]string
}

// RestoreStatus: last checkpoint restore details
type RestoreStatus struct {
    LastCheckpointWorldSize int32
    LastRestoreWorldSize    int32
    RestoreMode             RestoreMode
}
```
