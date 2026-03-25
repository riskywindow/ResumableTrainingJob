# Migration from Phase 2

## What Stays the Same

### Kueue Authority Model

RTJ remains the **only** Kueue-managed admission object. Child JobSets remain
**plain runtime resources** with no Kueue management metadata. The external
`jobframework` integration path, the `RTJGenericJob` adapter, and the Kueue
generic reconciler are unchanged in their overall structure.

### Lifecycle State Machine

All lifecycle phases are unchanged:

```
Pending → Queued → Admitted → Starting → Running
    → YieldRequested → Draining → Queued (Kueue re-queue)
    → Restoring → Running
    → Succeeded | Failed
```

Phase 3 does not add new phases. The `Admitted` → `Starting` transition now
carries richer admission context (flavor info, admitted counts), but the
phase names and allowed transitions are identical.

### Suspend Semantics

- `spec.suspend` remains the Kueue-facing admission gate.
- `spec.control.desiredState` remains the user-facing manual hold surface.
- These two fields are not aliases. Their semantics and interaction rules
  are unchanged from Phase 2.

### Checkpoint Contract

The checkpoint storage layout, manifest schema, manifest-last publication
semantics, yield-marker contract, and checkpoint completeness/validity rules
are **unchanged**.

Checkpoint manifests already record `worldSize` in their metadata. Phase 3
does not change the manifest schema; it changes how the compatibility checker
interprets the recorded `worldSize` when `worldSizePolicy=Flexible`.

### Graceful Yield and Drain

The graceful yield protocol is unchanged:
- Control ConfigMap signals `desiredState=Paused` to the trainer.
- Trainer finishes the in-flight step, writes DCP checkpoint, publishes
  yield marker and manifest.
- Controller polls for evidence, then deletes the child JobSet.
- Bounded timer: `maxDrainTime` enforced; fail-closed on timeout.

### Resume Selection

The `LatestCompatibleComplete` source policy remains the only supported
policy. The selection algorithm (enumerate → discard incomplete → discard
invalid → evaluate compatibility → sort by completionTimestamp → select
newest) is unchanged.

What changes is the compatibility evaluation for the `worldSize` dimension
when `worldSizePolicy=Flexible`. See below.

### Child JobSet Rendering Basics

The `RenderChildJobSet` function continues to:
- Parse the embedded JobSet template from `spec.runtime.template.spec`.
- Strip Kueue management labels and annotations.
- Inject control ConfigMap volume, staging volume, and environment variables.
- Set RTJ ownership labels.

Phase 3 adds admission-aware adjustments on top of this existing rendering.

### Existing Environment Variables

All Phase 1/2 environment variables remain supported and unchanged:

| Variable | Status |
| --- | --- |
| `YIELD_SDK_STORAGE_URI` | Unchanged |
| `YIELD_SDK_CONTROL_FILE` | Unchanged |
| `YIELD_SDK_RUN_ATTEMPT` | Unchanged |
| `YIELD_SDK_RESTORE_MANIFEST_URI` | Unchanged |
| `YIELD_SDK_RTJ_IDENTITY` | Unchanged |
| `YIELD_SDK_CLUSTER_IDENTITY` | Unchanged |
| `YIELD_SDK_RUNTIME_MODE` | Unchanged |
| `YIELD_SDK_GPU_SHAPE` | Unchanged |
| `YIELD_SDK_IMAGE_IDENTITY` | Unchanged |
| `YIELD_SDK_CODE_VERSION` | Unchanged |
| `YIELD_SDK_OPTIMIZER_MODE` | Unchanged |
| `YIELD_SDK_SHARDING_MODE` | Unchanged |
| `YIELD_SDK_STAGING_ROOT` | Unchanged |
| `YIELD_SDK_RESTORE_ROOT` | Unchanged |
| `YIELD_SDK_YIELD_MARKER_PATH` | Unchanged |
| `YIELD_SDK_YIELD_MARKER_URI` | Unchanged |

## What Changes in Launch Planning

### Admission-Aware Child JobSet Shape

**Phase 2 behavior:** `RunWithPodSetsInfo` applies `nodeSelector`,
`tolerations`, and labels from the admitted `PodSetInfo` onto the RTJ's
embedded JobSet template via `podset.Merge`. The controller then renders the
child JobSet from the mutated template. However, Phase 2 does **not** adjust
child JobSet replica counts based on admitted counts, and does **not** surface
the admitted flavor in RTJ status.

**Phase 3 behavior:** In addition to the Phase 2 mutations, the controller:

1. **Reads the admitted count** for each pod set from the mutated template
   (carried through by `podset.Merge`'s count handling or read from status).
2. **Adjusts child JobSet replica counts** to match the admitted counts.
3. **Computes the effective admitted world size** as the sum of admitted pods
   across all pod sets.
4. **Records `status.admittedWorldSize`** and **`status.admittedFlavors`**.
5. **Passes `YIELD_SDK_WORLD_SIZE`** as the admitted world size (not the
   requested world size, when they differ).
6. **Passes `YIELD_SDK_ORIGINAL_WORLD_SIZE`** when restoring from a checkpoint
   with a different world size, so the trainer can configure DCP resharding.
7. **Passes `YIELD_SDK_ADMITTED_FLAVOR`** for observability.

### Compatibility Checker: Flexible World-Size Mode

**Phase 2 behavior:** `CheckManifestCompatibility` requires
`manifest.WorldSize == request.WorldSize`. Any mismatch rejects the
checkpoint.

**Phase 3 behavior:** A new `WorldSizePolicy` field on `ResumeRequest`
controls world-size checking:

- `WorldSizePolicyFixed` (default): identical to Phase 2. Exact match
  required.
- `WorldSizePolicyFlexible`: the world-size check is skipped. All other
  compatibility dimensions remain strict.

The `ResumeRequest` struct gains a `WorldSizePolicy` field:

```go
type ResumeRequest struct {
    // ... existing fields ...
    WorldSizePolicy string // "Fixed" or "Flexible"
}
```

### PodSet MinCount (Experimental)

**Phase 2 behavior:** `PodSetsFromRTJTemplate` sets `Count` but never sets
`MinCount`. Kueue treats this as "admit all or nothing."

**Phase 3 behavior:** When the `PartialAdmission` feature gate is enabled
and `spec.resume.minWorldSize` is specified, `PodSetsFromRTJTemplate` also
sets `MinCount` proportionally. Kueue may then admit a count between
`MinCount` and `Count`.

## How Fixed-Size Mode Continues to Work

When Phase 3 features are left at their defaults:

- `spec.resume.worldSizePolicy` defaults to `Fixed`.
- `spec.resume.minWorldSize` is nil.
- The `PartialAdmission` feature gate is disabled.

In this configuration:

1. **PodSets:** `MinCount` is not set. Kueue admits all-or-nothing.
2. **Admitted count:** Always equals requested count.
3. **Admitted world size:** Always equals `spec.identity.worldSize`.
4. **Compatibility check:** Requires exact world-size match (Phase 2 rule).
5. **Child JobSet replicas:** Unchanged from Phase 2.
6. **Environment variables:** `YIELD_SDK_WORLD_SIZE` equals
   `spec.identity.worldSize`. `YIELD_SDK_ORIGINAL_WORLD_SIZE` is never set.
   `YIELD_SDK_ADMITTED_FLAVOR` is set for observability but has no behavioral
   effect.

**The only behavioral difference from Phase 2** in fixed-size mode is:
- `status.admittedWorldSize` is populated (informational).
- `status.admittedFlavors` is populated (informational).

No admission, scheduling, compatibility, or runtime behavior changes.

## Why Partial Admission Is Experimental

### Technical Reasons

1. **DCP resharding maturity.** PyTorch DCP resharding works for standard
   model/optimizer state, but edge cases exist for custom stateful objects,
   non-standard sharding, or very large models. Phase 3 validates the path
   for DDP and FSDP with standard DCP payloads.

2. **World-size-dependent training semantics.** Changing world size affects
   effective batch size, learning rate, gradient accumulation, and
   convergence. The RTJ controller does not adjust hyperparameters; the user
   is responsible for training code that handles varying world sizes correctly.

3. **Kueue partial-admission interaction.** The `MinCount` → admitted count
   flow through the Kueue generic reconciler has specific semantics in
   Kueue v0.15.1 that need validation. If the pinned Kueue version exposes a
   different surface than expected, we follow the pinned version and document
   divergence.

4. **Preemption cascades.** Partially admitted workloads interact with Kueue's
   preemption algorithm differently than all-or-nothing workloads. The
   preemption behavior under partial admission is Kueue's responsibility, but
   the RTJ controller must handle the resulting shape changes correctly.

### Process Reasons

5. **Incremental validation.** Flavor-aware launch (G1, G2) and flexible
   resume (G3) are independently valuable without partial admission. Shipping
   them as stable and partial admission as experimental reduces risk.

6. **User expectations.** Partial admission may cause training to proceed at
   a suboptimal world size. Users must explicitly opt in and understand the
   tradeoffs.

7. **Phase 0 compatibility.** Phase 0 locked strict world-size match as a v1
   rule. Flexible mode is a controlled relaxation; partial admission is a
   further relaxation that should prove itself before becoming a default.

## Upgrade Path

### From Phase 2 to Phase 3 (No Feature Changes)

1. Deploy the Phase 3 operator.
2. Existing RTJs with no `worldSizePolicy` or `minWorldSize` fields continue
   to work identically.
3. `status.admittedWorldSize` and `status.admittedFlavors` are populated on
   the next reconcile (informational only).

### Enabling Flexible Resume

1. Set `spec.resume.worldSizePolicy: Flexible` on the RTJ.
2. Ensure the training code handles `YIELD_SDK_ORIGINAL_WORLD_SIZE` and
   invokes DCP resharding when it differs from `YIELD_SDK_WORLD_SIZE`.
3. Resume from checkpoints saved at a different world size is now allowed.

### Enabling Partial Admission (Experimental)

1. Enable the `PartialAdmission` feature gate in the operator deployment.
2. Set `spec.resume.worldSizePolicy: Flexible` (required).
3. Set `spec.resume.minWorldSize` to the minimum acceptable world size.
4. Ensure the training code handles varying world sizes correctly.
