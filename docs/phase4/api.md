# Phase 4 API Reference

This document describes the API surface added in Phase 4 (topology-aware
admission pipeline). All additions are backward-compatible: Phase 3 manifests
continue to work unchanged when the new fields are absent.

## Spec Additions

### `spec.topology` (optional)

Declares topology placement requirements. When absent or mode is `Disabled`,
topology-aware scheduling is off and Phase 3 behavior is preserved exactly.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `mode` | `TopologyMode` enum | Yes (when topology is set) | `Disabled` (via defaulting) | Topology placement mode for the worker pod set. |
| `topologyLevel` | `string` | When mode is `Required` or `Preferred` | — | Node label key used as the topology domain (e.g., `topology.kubernetes.io/zone`, `kubernetes.io/hostname`). |
| `leaderWorkerColocation` | `bool` | No | `false` | Request leader pod co-location in the same topology domain as workers. Only meaningful when mode is `Required`, `Preferred`, or `Unconstrained`. |

#### TopologyMode Enum

| Value | Behavior |
|-------|----------|
| `Disabled` | No topology request. Identical to Phase 3. |
| `Required` | Topology placement at the specified level is mandatory. Admission fails if placement cannot be satisfied. Maps to `TopologyRequest.Required` on Kueue PodSets. |
| `Preferred` | Best-effort topology placement. Kueue tries the specified level but may spread across domains. Maps to `TopologyRequest.Preferred` on Kueue PodSets. |
| `Unconstrained` | Topology-aware scheduling is active but Kueue may place pods freely across all available domains. No specific level constraint. |

### Example: Topology-Aware RTJ

```yaml
apiVersion: training.checkpoint.example.io/v1alpha1
kind: ResumableTrainingJob
metadata:
  name: llm-training
spec:
  queueName: research-gpu
  workloadPriorityClassName: batch-high
  identity:
    image: registry.example.com/training/llm:v2
    codeVersion: "git:abc123"
    worldSize: 8
    gpuShape: a100-80g
  runtime:
    mode: FSDP
    optimizerMode: adamw
    shardingMode: full
    template:
      spec:
        replicatedJobs:
          - name: trainer
            replicas: 8
            template:
              spec:
                containers:
                  - name: trainer
                    image: registry.example.com/training/llm:v2
                    resources:
                      limits:
                        nvidia.com/gpu: "1"
  topology:
    mode: Required
    topologyLevel: "topology.kubernetes.io/zone"
  checkpoint:
    storageURI: "s3://checkpoints/llm/"
    interval: "5m"
    freshnessBudget: "10m"
    maxDrainTime: "15m"
  resume:
    maxResumeRetries: 3
```

## Status Additions

### `status.launchReadiness` (optional)

Populated by the ResumeReadiness AdmissionCheck controller when it is
configured on the ClusterQueue. Nil when the readiness gate is not active.

| Field | Type | Description |
|-------|------|-------------|
| `ready` | `bool` | Whether all pre-launch gates have passed. |
| `gateState` | `ReadinessGateState` enum | Current gate state: `Pending`, `Ready`, or `Rejected`. |
| `reason` | `string` | Machine-readable reason for the current state. |
| `message` | `string` | Human-readable explanation. |
| `lastTransitionTime` | `metav1.Time` | When the readiness state last changed. |

### `status.topology` (optional)

Records the admitted topology assignment from Kueue TAS. Nil when topology
is not enabled or the workload is not yet admitted with a topology assignment.

| Field | Type | Description |
|-------|------|-------------|
| `levels` | `[]string` | Topology level keys from the assignment (e.g., `["topology.kubernetes.io/zone"]`). |
| `domains` | `[]TopologyDomainStatus` | Assigned topology domains with pod counts. |

#### TopologyDomainStatus

| Field | Type | Description |
|-------|------|-------------|
| `values` | `[]string` | Domain values for each level in the levels list. |
| `count` | `int32` | Number of pods assigned to this domain. |

### `status.effectiveLaunchShape` (optional)

Captures the computed launch shape derived from admission decisions.
Nil before first admission.

| Field | Type | Description |
|-------|------|-------------|
| `workerCount` | `int32` | Effective number of worker pods for this launch. |
| `worldSize` | `int32` | Effective world size (may differ from `spec.identity.worldSize` under partial admission). |
| `resumeMode` | `RestoreMode` enum | `SameSize` or `Reshard`. Empty on first launch. |
| `selectedCheckpointID` | `string` | ID of the checkpoint selected for restore. Empty on first launch. |

## Defaulting

| Field | Default | Condition |
|-------|---------|-----------|
| `spec.topology.mode` | `Disabled` | When `spec.topology` is set but `mode` is empty. |

When `spec.topology` is nil, no defaulting occurs. The field stays nil,
and all Phase 3 behavior is preserved unchanged.

## Validation Rules

| Rule | Error |
|------|-------|
| `topologyLevel` required when mode is `Required` or `Preferred` | `spec.topology.topologyLevel: Required` |
| `leaderWorkerColocation` forbidden when mode is `Disabled` | `spec.topology.leaderWorkerColocation: Forbidden` |
| Invalid `mode` value | `spec.topology.mode: Unsupported` |

## Phase 3 Backward Compatibility

A Phase 3 manifest (with `spec.topology` absent) continues to behave
identically:

1. **Defaulting:** `spec.topology` remains nil. No topology-related
   defaulting occurs.
2. **Validation:** All Phase 3 validation rules apply unchanged. The
   topology validation block is skipped entirely when `spec.topology` is nil.
3. **PodSet synthesis:** No `TopologyRequest` is set on PodSets.
4. **Admission:** No ResumeReadiness gate unless configured on the ClusterQueue.
5. **Status:** `status.topology`, `status.launchReadiness`, and
   `status.effectiveLaunchShape` remain nil.
6. **Child JobSet:** Rendered exactly as in Phase 3 (flavor nodeSelector,
   tolerations, admitted counts, no topology constraints).

## Existing Fields (Unchanged)

The following existing status fields continue to serve their Phase 3 purpose
and are listed here for completeness:

| Field | Phase | Description |
|-------|-------|-------------|
| `status.admission.admittedFlavors` | 3 | Admitted flavor summary (pod set name to ResourceFlavor). |
| `status.selectedCheckpoint` | 1 | Current selected checkpoint for restore. |
| `status.restore.restoreMode` | 3 | Current resume mode (SameSize or Reshard). |
