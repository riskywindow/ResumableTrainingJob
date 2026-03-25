# ADR 0002: Parallelism and Resume Contract

- **Status:** Accepted
- **Date:** 2026-03-23
- **Scope:** Phase 3 API surface for adaptive parallelism, world-size-flexible resume, and partial admission

## Context

Phase 2 established RTJ as the native Kueue-managed admission object. The API
carries `spec.identity.worldSize` as a strict compatibility dimension: resume
requires exact world-size match, and Kueue admits all-or-nothing.

Phase 3 needs to:

1. Allow the controller to faithfully materialize Kueue admission decisions
   (flavors, node selectors, tolerations, and admitted counts) into child
   JobSets.
2. Support resuming from checkpoints saved at a different world size using
   PyTorch DCP resharding.
3. Optionally allow Kueue to admit fewer workers than requested (partial
   admission) for better cluster utilization.

These features require API extensions that preserve backward compatibility
with Phase 2 specs.

## Decision

### 1. Worker Parallelism in `spec.parallelism`

A new optional `spec.parallelism` section separates worker scaling policy
from identity:

```go
type ParallelismSpec struct {
    PreferredCount         int32   `json:"preferredCount,omitempty"`
    MinCount               *int32  `json:"minCount,omitempty"`
    PodSetName             string  `json:"podSetName,omitempty"`
    EnablePartialAdmission bool    `json:"enablePartialAdmission,omitempty"`
}
```

**Rationale:** `spec.identity.worldSize` is a Phase 0-locked compatibility
dimension. Adding scaling knobs to a separate section keeps the identity
contract intact and makes the new functionality opt-in.

**Backward compatibility:** When `spec.parallelism` is nil, the controller
uses `spec.identity.worldSize` as the effective preferred count and does not
set `PodSet.MinCount`. This degenerates to Phase 2 behavior.

**Leader vs. worker:** `PodSetName` identifies the scalable worker pod set.
All other replicatedJobs (e.g., a launcher) keep their replica count fixed.
Most training jobs have a single replicatedJob, so the default (first) is
correct in the common case.

### 2. World-Size-Flexible Resume in `spec.resume.allowWorldSizeChange`

A new boolean field on `ResumePolicy`:

```go
type ResumePolicy struct {
    // ... existing fields ...
    AllowWorldSizeChange bool `json:"allowWorldSizeChange,omitempty"`
}
```

**Rationale:** This is the simplest possible opt-in that preserves Phase 2
exact-match semantics by default. A boolean is sufficient because the only
dimension being relaxed is world size; all other compatibility checks remain
strict.

**DCP resharding contract:** When `allowWorldSizeChange=true` and the
checkpoint world size differs from the admitted world size, the controller
passes both values to the trainer via environment variables:

- `YIELD_SDK_WORLD_SIZE` = admitted (current) world size
- `YIELD_SDK_ORIGINAL_WORLD_SIZE` = checkpoint world size

The trainer is responsible for invoking DCP's resharding-capable load.

### 3. Partial Admission as Per-Job Experimental Opt-In

`spec.parallelism.enablePartialAdmission` is a per-job switch (not a global
feature gate) because:

- Different RTJs in the same cluster may have different tolerance for
  world-size changes.
- The user controls whether their training code handles varying world sizes.
- Per-job granularity avoids a cluster-wide blast radius.

**Validation guards:**

- `enablePartialAdmission=true` requires `allowWorldSizeChange=true`
  (partial admission changes world size).
- `enablePartialAdmission=true` requires `minCount` to be set (Kueue needs
  a floor).

**Kueue integration:** Verified that Kueue v0.15.1 exposes `PodSet.MinCount`
(`*int32`, optional, alpha). When `enablePartialAdmission=true` and
`minCount` is set, the controller sets `PodSet.MinCount` on the worker pod
set. Kueue may then admit a count between `MinCount` and `Count`.

### 4. Admitted-Shape Status

Two new status sub-objects:

```go
type AdmissionStatus struct {
    AdmittedWorkerCount  int32             `json:"admittedWorkerCount,omitempty"`
    PreferredWorkerCount int32             `json:"preferredWorkerCount,omitempty"`
    ActiveWorkerCount    int32             `json:"activeWorkerCount,omitempty"`
    AdmittedFlavors      map[string]string `json:"admittedFlavors,omitempty"`
}

type RestoreStatus struct {
    LastCheckpointWorldSize int32       `json:"lastCheckpointWorldSize,omitempty"`
    LastRestoreWorldSize    int32       `json:"lastRestoreWorldSize,omitempty"`
    RestoreMode             RestoreMode `json:"restoreMode,omitempty"`
}
```

**Rationale:** These fields give operators clear visibility into the admitted
shape and restore behavior without requiring them to inspect Kueue Workload
objects or checkpoint manifests directly.

### 5. Checkpoint WorldSize in CheckpointReference

`CheckpointReference` gains a `worldSize` field so the status carries the
world size from the checkpoint manifest alongside the checkpoint ID and URI.
This makes world-size-flexible resume observable from RTJ status alone.

## Consequences

### Positive

- **Zero-cost backward compatibility.** Phase 2 specs (no `parallelism`, no
  `allowWorldSizeChange`) produce identical behavior. No migration required.
- **Incremental adoption.** Users can enable `allowWorldSizeChange` without
  partial admission, or enable both together.
- **Per-job control.** Different jobs in the same cluster can have different
  flexibility policies.
- **Auditable.** Status surfaces the admitted shape and restore mode.

### Negative

- **API surface grows.** Three new spec fields and two new status sub-objects.
  Mitigated by making everything optional with zero-value defaults.
- **Trainer SDK must handle resharding.** This is real work in the Python SDK,
  but DCP resharding is a native PyTorch capability.
- **Partial admission is experimental.** The interaction between MinCount and
  Kueue's preemption algorithm needs operational experience.

### Alternatives Considered

**A. Global feature gate instead of per-job switch.** Rejected because it
forces cluster-wide policy on a per-workload concern. The per-job switch
is more granular and doesn't require operator deployment changes.

**B. Separate `WorldSizePolicy` enum instead of boolean.** The original
Phase 3 design doc proposed `Fixed | Flexible`. A boolean is simpler and
sufficient since there are only two states. If more policies emerge later,
the boolean can be replaced with an enum in a backward-compatible way (add
a new field, deprecate the boolean).

**C. Putting minCount on ResumePolicy instead of ParallelismSpec.** Rejected
because `minCount` is a Kueue admission concept (PodSet.MinCount), not a
resume concept. Grouping it with `preferredCount` and `podSetName` in
`ParallelismSpec` is more coherent.

**D. Separate placement mode field.** Not needed because the controller
always materializes Kueue admission faithfully. The "mode" is implicit in
whether `enablePartialAdmission` and `allowWorldSizeChange` are set. A
separate field would be redundant.

## Kueue v0.15.1 API Verification

Verified in the Go module cache:

- `kueuev1beta2.PodSet` has `MinCount *int32` (alpha, requires PartialAdmission
  feature gate in Kueue).
- `podset.PodSetInfo` has `Count int32` (admitted count flows through this).
- `podset.Merge` applies `NodeSelector`, `Tolerations`, `Labels`,
  `Annotations` to pod templates.

No Kueue version bump is required for Phase 3.

## Test Coverage

Unit tests added for:

- Backward-compatible decoding (Phase 2 spec with no parallelism section)
- `EffectivePreferredCount` fallback to `identity.worldSize`
- `EffectiveMinCount` nil when partial admission disabled
- Validation: minCount <= preferredCount
- Validation: partial admission requires allowWorldSizeChange
- Validation: partial admission requires minCount
- Deep copy independence for all new types
- Webhook integration: Phase 2 spec passes, Phase 3 spec passes, invalid
  Phase 3 spec rejected
