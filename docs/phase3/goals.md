# Phase 3 Goals

## Mission

Make ResumableTrainingJob admission-aware so that Kueue's admission decisions
(resource flavors, node selectors, tolerations, and admitted replica counts)
are faithfully materialized into the child JobSet that carries the training
runtime, and so that checkpoints saved at one world size can be resumed at a
different admitted world size via PyTorch DCP resharding.

## Goals

### G1: Admission-Aware Runtime Launch

When Kueue admits an RTJ, the `Workload.Status.Admission.PodSetAssignments`
carry per-pod-set flavor assignments including `nodeSelector`, `tolerations`,
and `count`. The RTJ controller MUST read these assignments (as delivered
through `RunWithPodSetsInfo` / `podset.PodSetInfo`) and materialize them
faithfully into the child JobSet pod templates.

**Acceptance:** A child JobSet launched after admission carries the correct
nodeSelector, tolerations, and resource requests derived from the admitted
ResourceFlavor, and pods schedule to the intended node pool.

### G2: Flavor-Aware Child JobSet Rendering

The child JobSet renderer MUST apply per-pod-set admission mutations:

- `nodeSelector` from the admitted ResourceFlavor
- `tolerations` from the admitted ResourceFlavor
- `labels` and `annotations` from Kueue pod-set info
- Admitted `count` (replica count) when it differs from the requested count

The renderer MUST NOT carry Kueue management metadata onto the child JobSet
(this invariant is already enforced by Phase 2's `stripKueueManagementMetadata`
and remains unchanged).

**Acceptance:** An RTJ whose ClusterQueue has two ResourceFlavors (e.g.,
`a100-80gb` and `h100-80gb`) launches its child JobSet with the correct
nodeSelector and tolerations for the flavor that Kueue actually assigned.

### G3: World-Size-Flexible Resume via DCP Resharding

Phase 0 through Phase 2 require exact world-size match for resume
compatibility. Phase 3 introduces a new `spec.resume.worldSizePolicy` field:

- `Fixed` (default): exact world-size match required, preserving Phase 2
  behavior.
- `Flexible`: world-size mismatch allowed; the trainer MUST use PyTorch DCP
  resharding to load a checkpoint saved at world size N into world size M.

When `worldSizePolicy=Flexible`:

- The compatibility checker skips the world-size equality check.
- The manifest still records the original world size for auditability.
- The trainer receives the original and current world sizes as environment
  variables and is responsible for invoking DCP's resharding path.
- All other compatibility dimensions (cluster identity, RTJ lineage, runtime
  mode, GPU shape, image, code version, optimizer mode, sharding mode, format
  version) remain strict.

**Acceptance:** An RTJ checkpointed at world size 8 can resume at world size 4
(or vice versa) when `worldSizePolicy=Flexible`, and the resumed training
shows monotonically increasing global step.

### G4: Experimental Partial Admission for RTJ

Kueue supports partial admission via `PodSet.MinCount`. Phase 3 adds an
experimental path where the RTJ declares a minimum acceptable replica count,
and Kueue may admit fewer replicas than requested.

This feature is:

- Behind a feature gate: `PartialAdmission` (default: **disabled**).
- Enabled only by the Phase 3 dev profile or explicit opt-in.
- Requires `worldSizePolicy=Flexible` (partial admission changes world size).

When enabled:

- `spec.resume.minWorldSize` sets the floor for partial admission.
- `PodSetsFromRTJTemplate` reports `MinCount` on each PodSet.
- The admitted count flows through `RunWithPodSetsInfo` and is materialized
  into the child JobSet replica counts.
- The controller computes the effective admitted world size from the admitted
  counts and passes it to the trainer.

**Acceptance:** An RTJ requesting world size 8 with `minWorldSize=4` is
admitted by Kueue at world size 4 when only 4 slots are available, launches
with 4 replicas, and resumes from a world-size-8 checkpoint using DCP
resharding.

## Non-Goals (Explicitly Out of Scope)

| Non-Goal | Reason |
| --- | --- |
| True in-place elastic scaling | Deferred to Elastic Workloads phase. Phase 3 handles resume-time shape changes only, not live scaling. |
| MultiKueue or multi-cluster resume | Phase 0 contract: single cluster only. |
| Topology-aware runtime enforcement | Kueue may consider topology for admission, but the RTJ controller does not enforce topology constraints beyond nodeSelector/tolerations from flavors. |
| ProvisioningRequest integration | Out of scope; no node auto-provisioning. |
| Custom scheduling algorithms | Kueue owns scheduling; the operator does not implement custom preemption or priority logic. |
| Transparent CUDA or container snapshots | Phase 0 contract: DCP checkpointing only. |
| Async checkpoint pipelines | Performance optimization deferred. |
| Partial admission as a default | Experimental only; off by default. |

## Success Criteria

### Must-Ship

1. Child JobSet pods land on the correct node pool based on the admitted
   ResourceFlavor's nodeSelector.
2. An RTJ preempted and re-admitted to a different ResourceFlavor launches
   on the new flavor's nodes.
3. An RTJ with `worldSizePolicy=Flexible` resumes from a checkpoint saved at
   a different world size.
4. Phase 2 behavior is preserved when `worldSizePolicy=Fixed` (default) and
   `PartialAdmission` is disabled (default).

### Should-Ship

5. The experimental partial-admission path works end-to-end behind the feature
   gate: RTJ admitted at a smaller world size, launches, and resumes from a
   larger checkpoint.
6. Status surfaces the admitted flavor and effective world size.

### Exit Criteria

- All must-ship acceptance criteria pass in the local `kind` dev environment
  with at least two ResourceFlavors.
- Unit tests cover: admission-aware rendering, flexible compatibility, admitted
  count propagation, MinCount synthesis.
- E2E test covers: flavor-aware launch and re-launch after preemption.
- Documentation updated: architecture, migration, ADR, session handoff.
