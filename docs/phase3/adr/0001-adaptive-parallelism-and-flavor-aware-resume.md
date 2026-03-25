# ADR 0001: Adaptive Parallelism and Flavor-Aware Resume

- **Status:** Proposed
- **Date:** 2026-03-23
- **Scope:** Phase 3 end-to-end contract and must-ship demo

## Context

Phase 2 established RTJ as the native Kueue-managed admission object. When
Kueue admits an RTJ, it sets `PodSetAssignments` on the Workload that carry
ResourceFlavor assignments, `nodeSelector`, `tolerations`, and admitted
`count`. The Kueue generic reconciler calls `RunWithPodSetsInfo` which merges
these admission mutations into the RTJ's embedded JobSet template.

However, Phase 2 has three gaps:

1. **The child JobSet does not reflect the admitted shape precisely.** The
   `nodeSelector` and `tolerations` from flavors flow through `podset.Merge`
   into the template, but the controller does not explicitly adjust replica
   counts or surface the admitted flavor in status. In practice this works
   because Phase 2 is all-or-nothing admission, but it is an implicit
   contract rather than an explicit one.

2. **Resume requires exact world-size match.** If an RTJ is preempted and
   re-admitted at a different world size (possible if infrastructure
   availability changes or partial admission is enabled), no checkpoint can
   be selected because `worldSize` is a strict compatibility dimension.

3. **There is no path for Kueue to admit fewer replicas than requested.**
   The PodSets reported by `PodSetsFromRTJTemplate` have no `MinCount`, so
   Kueue treats every RTJ as all-or-nothing.

Phase 3 addresses these gaps while preserving Phase 0 through Phase 2
invariants for workloads that do not opt into the new features.

## Decision

### 1. Admission-Aware Launch Is Always Active

The RTJ controller MUST faithfully materialize Kueue's admission decisions
into the child JobSet. This is not optional or gated. It is a correctness
requirement for the RTJ-as-admission-object model.

Specifically, after `RunWithPodSetsInfo` mutates the RTJ template:

- The controller reads the admitted `nodeSelector`, `tolerations`, and
  replica counts from the mutated template.
- The controller renders the child JobSet with these values.
- The controller computes `status.admittedWorldSize` from the admitted
  replica counts and records `status.admittedFlavors`.

When admitted count equals requested count (the Phase 2 steady state),
this degenerates to current behavior with added status visibility.

### 2. World-Size-Flexible Resume Is Opt-In

A new field `spec.resume.worldSizePolicy` controls whether the compatibility
checker requires exact world-size match:

- `Fixed` (default): exact match required. Phase 2 behavior.
- `Flexible`: world-size check skipped. All other dimensions remain strict.

When `Flexible`, the trainer receives `YIELD_SDK_ORIGINAL_WORLD_SIZE` (the
world size in the checkpoint) and `YIELD_SDK_WORLD_SIZE` (the admitted world
size). The trainer MUST use PyTorch DCP's resharding-capable load path.

The controller does NOT verify that resharding succeeded at the DCP level.
It verifies that the resumed training attempt reaches `Running` and that
subsequent checkpoints record monotonically increasing global step.

### 3. Partial Admission Is Experimental and Gated

The `PartialAdmission` feature gate (default: disabled) enables:

- A new field `spec.resume.minWorldSize` that sets the floor for admission.
- `PodSetsFromRTJTemplate` reports `MinCount` on each PodSet.
- Kueue may admit a count between `MinCount` and `Count`.

Partial admission REQUIRES `worldSizePolicy=Flexible` because a partially
admitted RTJ will have a different world size than requested.

When the feature gate is disabled:
- `spec.resume.minWorldSize` is ignored in validation.
- `MinCount` is never set on PodSets.
- Kueue treats the RTJ as all-or-nothing.

### 4. The RTJ Remains the Only Admission Object

The child JobSet continues to be a plain runtime resource with no Kueue
management metadata. This invariant from Phase 2 is unchanged.

### 5. DCP Resharding Is Trainer Responsibility

The RTJ controller provides the world-size metadata. The Python yield SDK
and user training code are responsible for invoking DCP's resharding path.
The controller treats resharding as a black box.

## Consequences

### Positive

- **Flavor-aware placement is guaranteed.** Child JobSets land on the correct
  node pools. This was implicit in Phase 2 but is now explicit and tested.

- **Resume across world sizes is possible.** Workloads preempted from a large
  allocation can resume on a smaller allocation (or vice versa) without
  losing checkpoint progress.

- **Incremental complexity.** Flavor-aware launch and flexible resume are
  independently useful without partial admission. Partial admission is
  additive and gated.

- **Phase 2 backward compatibility.** Default settings produce identical
  behavior to Phase 2.

### Negative

- **Trainer SDK complexity increases.** The yield SDK must handle the
  resharding path. This is a real code change in the Python SDK.

- **Testing matrix grows.** Phase 3 must test: fixed-size launch,
  flavor-aware launch, flexible resume (same size), flexible resume
  (different size), and optionally partial admission.

- **Partial admission interactions with Kueue preemption are not fully
  characterized.** The experimental gate mitigates this risk.

### Neutral

- **GPU shape compatibility is unchanged.** Cross-GPU-type resume (A100 → H100)
  is not supported. This may need a future ADR.

## End-to-End Phase 3 Contract

### RTJ Spec → Kueue → Admitted Shape → Child JobSet

1. User creates RTJ with `spec.identity.worldSize=8`,
   `spec.resume.worldSizePolicy=Flexible`, and an embedded JobSet template
   with a `workers` replicatedJob (replicas=1, parallelism=8).

2. Webhook defaults `suspend=true`, syncs Kueue labels.

3. Kueue creates Workload with one PodSet:
   `{name: "workers", template: ..., count: 8}`.

4. Kueue admits Workload with PodSetAssignment:
   `{name: "workers", count: 8, flavors: {nvidia.com/gpu: "a100-80gb"}, nodeSelector: {pool: "a100"}, tolerations: [...]}`.

5. Kueue calls `RunWithPodSetsInfo`. `podset.Merge` applies `nodeSelector`,
   `tolerations` to the RTJ template. `suspend` set to `false`.

6. RTJ controller reconciles:
   - Reads mutated template.
   - Computes `admittedWorldSize = 8`.
   - Records `status.admittedWorldSize = 8`,
     `status.admittedFlavors = {"workers": "a100-80gb"}`.
   - Renders child JobSet with `nodeSelector: {pool: "a100"}`,
     `replicas` matching admitted count.
   - Injects `YIELD_SDK_WORLD_SIZE=8`.

7. Child JobSet creates pods. Pods land on `a100` pool nodes.

8. Training runs. Periodic checkpoints record `worldSize=8` in manifests.

### Preempt → Re-Admit at Different Shape → Resume

9. Kueue sets `suspend=true` (higher-priority workload arrived).

10. Controller records stop request, writes control ConfigMap.

11. Trainer checkpoints at step boundary: manifest records `worldSize=8`,
    `globalStep=500`.

12. Controller observes evidence, deletes child JobSet. RTJ enters `Queued`.

13. Time passes. A100 pool is full. H100 pool has 4 slots. (Same `gpuShape`
    string, different node pool.)

14. Kueue re-admits with PodSetAssignment:
    `{name: "workers", count: 4, flavors: {nvidia.com/gpu: "h100-80gb"}, nodeSelector: {pool: "h100"}}`.

15. `RunWithPodSetsInfo` applies new mutations.

16. RTJ controller reconciles:
    - `admittedWorldSize = 4`.
    - `worldSizePolicy = Flexible` → skip world-size check in compatibility.
    - Selects checkpoint at `globalStep=500` (worldSize=8, all other fields
      match).
    - Records `status.admittedWorldSize = 4`,
      `status.admittedFlavors = {"workers": "h100-80gb"}`.
    - Renders child JobSet with `nodeSelector: {pool: "h100"}`, replicas=4.
    - Injects `YIELD_SDK_WORLD_SIZE=4`,
      `YIELD_SDK_ORIGINAL_WORLD_SIZE=8`,
      `YIELD_SDK_RESTORE_MANIFEST_URI=...`.

17. Trainer loads checkpoint with DCP resharding (8 → 4 ranks).

18. Training resumes at `globalStep=501`. RTJ enters `Running`.

### Fixed-Size Mode (Phase 2 Compatible)

19. An RTJ with default `worldSizePolicy=Fixed` and no `minWorldSize`:
    - Kueue admits all-or-nothing.
    - Compatibility check requires exact world-size match.
    - Child JobSet replica count equals requested count.
    - Behavior is identical to Phase 2.

## Must-Ship Demo

The Phase 3 must-ship demo proves:

1. **Flavor-aware launch:** Create an RTJ targeting a ClusterQueue with two
   ResourceFlavors (e.g., `pool-a` and `pool-b` with different nodeSelectors).
   Verify the child JobSet pods land on the assigned pool.

2. **Flavor-aware re-launch:** Preempt the RTJ, then re-admit to the other
   flavor. Verify the new child JobSet pods land on the new pool.

3. **World-size-flexible resume:** Create an RTJ with
   `worldSizePolicy=Flexible` and `worldSize=8`. Run until a checkpoint is
   saved. Preempt. Re-admit at `worldSize=4` (either via a smaller quota or
   by updating the Workload). Verify the trainer loads the checkpoint with
   DCP resharding and training continues with monotonically increasing step.

4. **Fixed-size backward compatibility:** Create an RTJ with default settings.
   Verify behavior is identical to Phase 2: exact world-size match,
   all-or-nothing admission, no resharding.

5. **(Experimental) Partial admission:** With the `PartialAdmission` gate
   enabled, create an RTJ with `worldSize=8` and `minWorldSize=4`. Configure
   the ClusterQueue with only 4 GPU slots. Verify Kueue admits with count=4,
   the child JobSet launches with 4 replicas, and if a checkpoint exists
   from world size 8, it is restored with resharding.

Demo items 1-4 are must-ship. Demo item 5 is should-ship (experimental).
