# Topology-Aware Launch (G2/G4)

## Overview

This document describes the admission-gated, topology-aware launch pipeline
implemented in Phase 4. The operator gates child JobSet creation behind
pre-launch prerequisites (admission, readiness checks, topology assignment)
and injects topology constraints into the rendered child JobSet.

## Pre-Launch Gate Pipeline

Before creating a child JobSet, the operator evaluates the following gates
in order:

1. **Kueue Admission Gate** — RTJ must not be suspended (`spec.suspend=false`).
   This is unchanged from Phase 2 and checked before the gate evaluation.

2. **ResumeReadiness AdmissionCheck** — When a ResumeReadiness admission check
   is configured on the ClusterQueue, it must report `Ready` on the Workload.
   - `Pending` / `Retry` → operator waits and requeues.
   - `Rejected` → operator waits (Kueue manages the workload lifecycle).
   - Not configured → gate passes (Phase 3 behavior).

3. **Topology Assignment** — When `spec.topology.mode` is not `Disabled`,
   the Workload's PodSetAssignment must contain a TopologyAssignment.
   - Missing → operator waits and requeues.
   - Present but not representable → operator fails with status condition.
   - Present and representable → gate passes, topology injected into child.

When all gates pass, the operator computes a **LaunchPlan** that combines
admission counts, flavors, and topology into a single render input.

## Topology Injection Strategy

### Supported Assignments

The child JobSet renderer supports topology assignments that can be expressed
as a single `nodeSelector` on the pod template:

| Assignment Shape | Supported | Injection |
|---|---|---|
| Single domain (1 zone, 1 node) | Yes | All level labels as nodeSelector |
| Multi-domain, uniform higher levels | Yes | Common level labels as nodeSelector |
| Multi-domain, heterogeneous levels | No | Fails with status condition |
| Single-level multi-domain | No | Fails with status condition |

### Why These Limitations

The JobSet API does not support per-pod scheduling constraints (like
scheduling gates or pod-specific affinity). All pods in a replicatedJob share
the same pod template, which means we can only inject a single `nodeSelector`
that must apply to all pods.

For assignments where pods need to land in different topology domains (e.g.,
2 pods in zone-a and 2 pods in zone-b), per-pod scheduling gates would be
required. This is deferred to a future phase.

### How It Works

1. **Parse** — `internal/topology/assignment.go` decodes the compressed Kueue
   `TopologyAssignment` format into flat `DomainAssignment` entries.

2. **Validate** — `CanRepresentInJobSet()` checks whether the assignment can
   be expressed in the child JobSet.

3. **Extract** — `CommonNodeSelector()` computes the labels that are uniform
   across all domains and can safely be set as nodeSelector.

4. **Inject** — `internal/jobset/topology_injection.go` merges the common
   nodeSelector into each replicatedJob's pod template. Existing nodeSelector
   labels (e.g., GPU accelerator labels) are preserved.

## Status Fields

### `status.launchReadiness`

Populated when the launch gate pipeline is active. Shows the current gate
state:

```yaml
status:
  launchReadiness:
    ready: true
    gateState: Ready
```

Or when waiting:

```yaml
status:
  launchReadiness:
    ready: false
    gateState: Pending
    reason: WaitingForTopologyAssignment
    message: "Topology is enabled but topology assignment is not yet present on the Workload."
```

### `status.topology`

Populated after a topology assignment is parsed from the Workload:

```yaml
status:
  topology:
    levels: ["topology.kubernetes.io/zone"]
    domains:
      - values: ["us-east-1a"]
        count: 4
```

### `status.effectiveLaunchShape`

Captures the computed launch parameters:

```yaml
status:
  effectiveLaunchShape:
    workerCount: 4
    worldSize: 4
    resumeMode: SameSize
    selectedCheckpointID: "ckpt-run1-step20"
```

## Backward Compatibility

When `spec.topology` is nil and no WorkloadReference is set, the controller
follows the Phase 3 code path exactly. No gate evaluation occurs, no new
status fields are populated. Phase 3 manifests work unchanged.

## File Index

| File | Purpose |
|---|---|
| `internal/topology/assignment.go` | Parse Kueue TopologyAssignment compressed format |
| `internal/topology/assignment_test.go` | Parser tests (15 tests) |
| `internal/jobset/topology_injection.go` | Inject nodeSelector into child JobSet |
| `internal/jobset/topology_injection_test.go` | Injection tests (7 tests) |
| `internal/controller/launch_gate.go` | Pre-launch gate evaluation |
| `internal/controller/launch_plan.go` | Launch plan computation and status sync |
| `internal/controller/resumabletrainingjob_controller.go` | Updated reconcile loop |
| `internal/controller/resume_flow.go` | Gated launch/resume methods |
| `internal/jobset/render.go` | Updated renderer with topology injection |
