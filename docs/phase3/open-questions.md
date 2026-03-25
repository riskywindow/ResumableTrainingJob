# Phase 3 Open Questions

## OQ-1: Admitted Count Propagation Through podset.Merge [RESOLVED]

**Question:** Does `podset.Merge` (from Kueue v0.15.1) propagate the admitted
`Count` from `PodSetInfo` into the RTJ template in a way that the RTJ
controller can read back?

**Resolution:** Yes. `podset.PodSetInfo` has `Count int32`. `podset.Merge`
applies `NodeSelector`, `Tolerations`, `Labels`, `Annotations` to pod
templates. The admitted count flows through `PodSetInfo.Count` and is
available to `RunWithPodSetsInfo`. Verified in Kueue v0.15.1 Go module cache.

## OQ-2: PodSet.MinCount Availability in Kueue v0.15.1 [RESOLVED]

**Question:** Does `kueuev1beta2.PodSet` in Kueue v0.15.1 expose a `MinCount`
field for partial admission?

**Resolution:** Yes. `kueuev1beta2.PodSet` has `MinCount *int32` (alpha,
requires `PartialAdmission` feature gate in Kueue). No Kueue version bump
is required for Phase 3. Verified in Go module cache.

## OQ-3: Proportional Scaling of MinCount Across Multiple ReplicatedJobs [RESOLVED]

**Question:** When an RTJ has multiple `replicatedJobs` (e.g., a `launcher`
and `workers`), how should `minCount` be distributed across pod sets for
`MinCount`?

**Resolution:** `spec.parallelism.podSetName` identifies the single scalable
worker pod set. Only that pod set gets `MinCount`. All other replicatedJobs
keep their replica count fixed. Defaults to the first replicatedJob. See
ADR 0002 for rationale.

## OQ-4: GPU Shape Compatibility Under Flavor Change

**Question:** When an RTJ is re-admitted to a different ResourceFlavor (e.g.,
from `a100-80gb` to `h100-80gb`), should the GPU shape compatibility check
pass or fail?

**Context:** Phase 0 locks `gpuShape` as a strict compatibility dimension.
If a checkpoint is saved on A100 nodes and the workload is re-admitted to
H100 nodes, the `gpuShape` in the manifest will not match the new identity.
The user would need to update `spec.identity.gpuShape` to match the new
flavor, which breaks the immutability of identity fields.

**Impact:** Determines whether cross-flavor resume is possible without user
intervention. If `gpuShape` must match, re-admission to a different GPU type
forces a fresh start (no checkpoint resume).

**Resolution plan:** Phase 3 does NOT relax GPU shape compatibility. This
preserves the Phase 0 contract. Re-admission to a different GPU type with
the same `gpuShape` string (e.g., both flavors use `gpuShape: "8xA100"`) is
supported because flavors may differ in node pool without differing in GPU
type. Cross-GPU-type resume (A100 → H100) requires a future ADR to relax
the `gpuShape` check, potentially in the same manner as `worldSizePolicy`.

## OQ-5: Feature Gate Infrastructure [RESOLVED]

**Question:** What is the preferred feature gate implementation?

**Resolution:** Per-job `spec.parallelism.enablePartialAdmission` replaces
the global feature gate entirely. No env var infrastructure needed. Each RTJ
opts in independently. This is more granular than a cluster-wide gate and
doesn't require operator deployment changes. See ADR 0002 for rationale.

## OQ-6: Trainer SDK Changes for Resharding [RESOLVED]

**Question:** What changes are needed in the Python yield SDK to support DCP
resharding when `YIELD_SDK_ORIGINAL_WORLD_SIZE` differs from
`YIELD_SDK_WORLD_SIZE`?

**Resolution:** SDK reads `YIELD_SDK_ALLOW_WORLD_SIZE_CHANGE` and
`YIELD_SDK_ORIGINAL_WORLD_SIZE` env vars via `RuntimeConfig`. When world
size differs and `allow_world_size_change=True`, validation allows it if
`manifest.cross_size_restore_supported=True`. DCP's `load()` handles
resharding natively — no special options needed. RNG state is skipped during
cross-size restore (it is per-rank and world-size-specific). 32 tests
pass including same-size, different-size, and incompatible rejection cases.

## OQ-7: Status Surfacing of Admitted Flavor Details [RESOLVED]

**Question:** Should `status.admittedFlavors` record just the flavor name,
or also the flavor's resource details (e.g., GPU count, memory)?

**Resolution:** Flavor name only. `status.admission.admittedFlavors` is a
`map[string]string` mapping pod set name to ResourceFlavor name. Operators
can use `kubectl get resourceflavor <name>` for details. See ADR 0002.
