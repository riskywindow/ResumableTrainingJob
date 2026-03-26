# Topology-Aware Workload Synthesis

Phase 4 G1 implementation reference. Describes how the RTJ controller maps
`spec.topology` fields to Kueue `PodSet.TopologyRequest` during Workload
synthesis.

## Overview

When an RTJ has `spec.topology` set with a mode other than `Disabled`, the
`PodSetsFromRTJTemplate` function populates `TopologyRequest` on the
synthesized Kueue PodSets. This enables Kueue's TopologyAwareScheduling (TAS)
to place pods according to the requested topology constraints.

## Field Mapping

### RTJ spec.topology → Kueue PodSet.TopologyRequest

| RTJ Field | Kueue PodSetTopologyRequest Field | Mapping |
|-----------|-----------------------------------|---------|
| `spec.topology.mode = Required` | `Required` | `*string` set to `spec.topology.topologyLevel` |
| `spec.topology.mode = Preferred` | `Preferred` | `*string` set to `spec.topology.topologyLevel` |
| `spec.topology.mode = Unconstrained` | `Unconstrained` | `*bool` set to `true` |
| `spec.topology.mode = Disabled` | _(none)_ | `TopologyRequest` is nil |
| `spec.topology = nil` | _(none)_ | `TopologyRequest` is nil — Phase 3 behavior |

### Sub-Group Metadata (always set when topology is active)

| Kueue Field | Value | Source |
|-------------|-------|--------|
| `PodIndexLabel` | `"kubernetes.io/job-completion-index"` | Standard Kubernetes indexed-job label |
| `SubGroupIndexLabel` | `"jobset.sigs.k8s.io/job-index"` | Standard JobSet replicated-job index label |
| `SubGroupCount` | `replicatedJob.replicas` (default 1) | From the RTJ embedded JobSet template |

These fields tell Kueue about the JobSet sub-group structure so it can make
topology-aware placement decisions that respect replicated-job boundaries.

### Leader/Worker Colocation

| RTJ Field | Kueue Field | Behavior |
|-----------|-------------|----------|
| `leaderWorkerColocation = false` (default) | _(none)_ | Only the worker PodSet gets `TopologyRequest` |
| `leaderWorkerColocation = true` | `PodSetGroupName` | All PodSets get `TopologyRequest` and share `PodSetGroupName = "rtj-topology-group"` |

When colocation is active, the shared `PodSetGroupName` tells Kueue to assign
all grouped PodSets to the same ResourceFlavor and topology domain.

## Which PodSet is the "Worker"?

The worker PodSet is resolved using the same logic as Phase 3 parallelism:

1. If `spec.parallelism.podSetName` is set, that replicatedJob is the worker.
2. Otherwise, the **first** replicatedJob in the embedded template is the worker.

This means users with a driver/worker template (e.g., `["driver", "worker"]`)
should set `spec.parallelism.podSetName = "worker"` when enabling topology.
Without this, topology is applied to the first replicatedJob ("driver").

## Backward Compatibility

- When `spec.topology` is nil: behavior is identical to Phase 3. No
  `TopologyRequest` is emitted on any PodSet.
- When `spec.topology.mode = Disabled`: same as nil — no topology request.
- Phase 3 PodSet fields (`Count`, `MinCount`, `Template`) are never modified
  by the topology logic. Topology and parallelism are orthogonal features.

## Example

### RTJ spec (topology enabled)

```yaml
spec:
  topology:
    mode: Required
    topologyLevel: "topology.kubernetes.io/zone"
    leaderWorkerColocation: false
  parallelism:
    preferredCount: 8
    minCount: 4
    podSetName: "worker"
    enablePartialAdmission: true
  runtime:
    template:
      spec:
        replicatedJobs:
          - name: driver
            replicas: 1
            template: ...
          - name: worker
            replicas: 2
            template: ...
```

### Synthesized Kueue PodSets

```yaml
podSets:
  - name: driver
    count: 1
    template: ...
    # No TopologyRequest (colocation is false, driver is not the worker)
  - name: worker
    count: 8          # From parallelism.preferredCount
    minCount: 4       # From parallelism.minCount (partial admission)
    template: ...
    topologyRequest:
      required: "topology.kubernetes.io/zone"
      podIndexLabel: "kubernetes.io/job-completion-index"
      subGroupIndexLabel: "jobset.sigs.k8s.io/job-index"
      subGroupCount: 2   # From worker replicatedJob.replicas
```

### With colocation enabled

If `leaderWorkerColocation: true` were set, both PodSets would have:

```yaml
    topologyRequest:
      required: "topology.kubernetes.io/zone"
      podIndexLabel: "kubernetes.io/job-completion-index"
      subGroupIndexLabel: "jobset.sigs.k8s.io/job-index"
      subGroupCount: <replicas from respective replicatedJob>
      podSetGroupName: "rtj-topology-group"
```

## Implementation Files

| File | Role |
|------|------|
| `internal/kueue/rtj_topology.go` | `applyTopologyRequests`, `buildTopologyRequest`, `findReplicatedJob` |
| `internal/kueue/rtj_podsets.go` | Phase 4 integration point in `PodSetsFromRTJTemplate` |
| `internal/kueue/rtj_topology_test.go` | 12 tests covering all modes, colocation, and Phase 3 preservation |

## Kueue API Version

Verified against **Kueue v0.15.1** (`sigs.k8s.io/kueue@v0.15.1`):

- `kueuev1beta2.PodSet.TopologyRequest *PodSetTopologyRequest` — confirmed present
- `kueuev1beta2.PodSetTopologyRequest` fields used: `Required`, `Preferred`,
  `Unconstrained`, `PodIndexLabel`, `SubGroupIndexLabel`, `SubGroupCount`,
  `PodSetGroupName`
- `kueuev1beta2.PodSetAssignment.TopologyAssignment *TopologyAssignment` —
  confirmed present (consumed in Phase 4 G2, not yet implemented)
