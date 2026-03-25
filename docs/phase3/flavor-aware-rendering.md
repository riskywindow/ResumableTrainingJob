# Flavor-Aware Rendering

## Overview

Phase 3 Session 5 wires the admitted shape from Kueue into the child JobSet
rendering path. When Kueue admits an RTJ, the controller now:

1. Reads the admitted pod counts from a bridge annotation.
2. Passes admitted counts and world-size metadata to the renderer.
3. The renderer adjusts replica counts, strips Kueue pod template labels,
   and injects Phase 3 env vars for the training code.
4. The controller uses the admitted world size for checkpoint selection.
5. Admission and restore status are synced on launch and resume.

## Bridge Annotation

### Problem

`RunWithPodSetsInfo` (called by Kueue during admission) receives per-pod-set
admitted counts, nodeSelector, and tolerations. The controller's reconcile
loop renders the child JobSet from the RTJ spec. The counts are not in the spec.

### Solution

`RunWithPodSetsInfo` stores admitted pod counts in an annotation on the RTJ:

```
training.checkpoint.example.io/admitted-pod-sets: {"trainer": 4}
```

The controller reads this annotation and passes the counts to `RenderInput`.
NodeSelector and tolerations are already merged into the RTJ's template by
`podset.Merge` in `RunWithPodSetsInfo`, so they flow through automatically.

### Why Not Read the Workload

Reading the Workload object would require an additional API call. The
annotation avoids this and keeps the controller's critical path simple.
Flavor names (needed for `status.admission.admittedFlavors`) can be
populated from the Workload in a follow-up session.

## Renderer Changes

### RenderInput Phase 3 Fields

| Field | Type | Purpose |
| --- | --- | --- |
| `AdmittedCounts` | `map[string]int32` | Pod set name to admitted pod count |
| `OriginalWorldSize` | `int32` | `spec.identity.worldSize` (gates Phase 3 env vars) |
| `AllowWorldSizeChange` | `bool` | `spec.resume.allowWorldSizeChange` |
| `AdmittedFlavor` | `string` | Flavor name for observability env var |

### Rendering Flow

```
Template from RTJ spec (may include Kueue-merged nodeSelector/tolerations)
  |
  v
Parse template -> ReplicatedJobs[]
  |
  v (for each replicatedJob)
  +-- Apply admitted replica count (admittedPodCount / podsPerReplica)
  +-- Strip Kueue management labels from pod template
  +-- Inject volumes and env vars
  +-- Inject Phase 3 env vars (when OriginalWorldSize > 0)
  |
  v
Rendered child JobSet (plain runtime resource, no Kueue metadata)
```

### Replica Count Math

```
replicas = admittedPodCount / podsPerReplica(replicatedJob)
podsPerReplica = min(parallelism, completions)
```

Example: template has parallelism=1, completions=1, Kueue admits 4 pods
-> replicas = 4/1 = 4.

### Phase 3 Env Vars

| Env Var | Value | Purpose |
| --- | --- | --- |
| `YIELD_SDK_WORLD_SIZE` | Total admitted pod count | Effective world size |
| `YIELD_SDK_ORIGINAL_WORLD_SIZE` | `spec.identity.worldSize` | Original world size for resharding |
| `YIELD_SDK_ALLOW_WORLD_SIZE_CHANGE` | `"true"` or `"false"` | Whether DCP resharding is allowed |
| `YIELD_SDK_ADMITTED_FLAVOR` | Flavor name | Observability (optional) |

These are only injected when `OriginalWorldSize > 0` (i.e., when the
controller has Phase 3 admission data). Phase 2 RTJs get no new env vars.

## Controller Changes

### Checkpoint Selection

`resumeCheckpointForAttempt` now uses the admitted world size (from the
annotation) instead of `spec.identity.worldSize` for `ResumeRequest.WorldSize`.
It also sets `AllowWorldSizeChange` from `spec.resume.allowWorldSizeChange`.

### Status Sync

- `syncAdmissionStatus` is called during launch and resume to populate
  `status.admission` with the admitted worker count and preferred count.
- `syncRestoreStatus` is called during resume to record the checkpoint
  world size, restore world size, and whether resharding is needed.

### Data Flow

```
Kueue admission
  |
  v
RunWithPodSetsInfo
  |-- podset.Merge (nodeSelector, tolerations -> template)
  |-- Store admitted counts annotation
  |
  v
Controller reconcile
  |-- Parse admitted counts annotation
  |-- Build RenderInput with admitted shape
  |-- Render child JobSet (adjusted replicas, Phase 3 env vars)
  |-- Select checkpoint (admitted world size, AllowWorldSizeChange)
  |-- Sync admission status
  |-- Sync restore status (on resume)
```

## Flavor Injection Helpers

### Location

`internal/jobset/flavor_injection.go`

### Functions

| Function | Purpose |
| --- | --- |
| `applyAdmittedReplicaCount` | Sets replica count from admitted pod count |
| `podsPerReplica` | Computes pods per replica (mirrors kueue/rtj_podsets.go) |
| `stripKueuePodTemplateLabels` | Removes Kueue labels/annotations from pod templates |

## Test Coverage

### Flavor Injection Tests (10 tests)

| Test | What It Verifies |
| --- | --- |
| `TestApplyAdmittedReplicaCountSinglePodPerReplica` | Basic 1:1 pod-to-replica mapping |
| `TestApplyAdmittedReplicaCountMultiPodsPerReplica` | Multi-pod-per-replica division |
| `TestApplyAdmittedReplicaCountZeroCountIsNoOp` | Zero count preserves original |
| `TestApplyAdmittedReplicaCountNegativeCountIsNoOp` | Negative count preserves original |
| `TestApplyAdmittedReplicaCountClampsToAtLeastOne` | Floor clamped to 1 |
| `TestPodsPerReplicaDefaultsToOne` | Default parallelism/completions |
| `TestPodsPerReplicaUsesParallelism` | Explicit parallelism |
| `TestPodsPerReplicaCompletionsLessThanParallelism` | Completions caps result |
| `TestStripKueuePodTemplateLabelsRemovesKueueLabels` | Kueue labels stripped, others preserved |
| `TestStripKueuePodTemplateLabelsNilMapsNoOp` | Nil maps don't panic |

### Render Tests (10 tests)

| Test | What It Verifies |
| --- | --- |
| `TestRenderChildJobSetInjectsOperatorLabelsEnvAndVolumes` | Phase 1/2 baseline |
| `TestRenderChildJobSetStripsKueueManagementMetadata` | Kueue metadata stripped from JobSet |
| `TestRenderChildJobSetUnstructuredRoundTrip` | Unstructured serialization |
| `TestRenderChildJobSetAppliesAdmittedWorkerCount` | Admitted count sets replicas |
| `TestRenderChildJobSetPreservesLeaderCountWithAdmission` | Leader fixed, worker scaled |
| `TestRenderChildJobSetInjectsPhase3EnvVars` | World size, flavor env vars |
| `TestRenderChildJobSetOmitsPhase3EnvVarsWhenNotSet` | Phase 2 backward compat |
| `TestRenderChildJobSetStripsPodTemplateKueueLabels` | Kueue labels stripped from pods |
| `TestRenderChildJobSetPreservesFlavorNodeSelectorFromTemplate` | NodeSelector propagates |
| `TestRenderChildJobSetUsesOriginalReplicaCountWhenNoAdmission` | No admission = original count |

## What This Session Does NOT Change

- Flavor names are not yet populated in `status.admission.admittedFlavors`
  (requires reading the Workload object; deferred to next session).
- Partial admission synthesis is not implemented (per explicit instruction).
- The `workload_observer.go` is not updated to extract flavors.
- The PodSet synthesis (`rtj_podsets.go`) does not yet use
  `EffectivePreferredCount()` or `EffectiveMinCount()`.

## Phase 2 Backward Compatibility

When the admitted-pod-sets annotation is absent:

- `parseAdmittedCounts` returns nil.
- `RenderInput.AdmittedCounts` is nil -> original template replica counts used.
- `RenderInput.OriginalWorldSize` is 0 -> no Phase 3 env vars injected.
- `admittedWorldSize` falls back to `spec.identity.worldSize`.
- `AllowWorldSizeChange` defaults to false.
- All existing tests pass unchanged.
