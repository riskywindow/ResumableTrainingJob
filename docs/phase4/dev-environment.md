# Phase 4 Local Dev Environment

This document describes the local kind-based development environment for
Phase 4 (topology-aware admission pipeline).

## Quick Start

```bash
# Stand up the full Phase 4 dev environment
make phase4-up

# Verify everything is configured
make phase4-smoke

# Inspect cluster state
make phase4-status

# Tear down
make phase4-down
```

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| kind | >= 0.20 | Local Kubernetes cluster |
| kubectl | >= 1.28 | Cluster interaction |
| docker | any | Container runtime |
| go | >= 1.22 | Build and test |

## Architecture

Phase 4 layers on top of the existing Phase 2/3 base dev stack:

```
┌─────────────────────────────────────────────────────┐
│                  Phase 4 Profile                     │
│  Topology labels · Topology object · TAS flavor      │
│  AdmissionCheck · ResumeReadinessPolicy              │
│  Phase 4 ClusterQueue + LocalQueue                   │
├─────────────────────────────────────────────────────┤
│                  Phase 3 Profile                     │
│  Pool labels · on-demand/spot flavors                │
│  Phase 3 ClusterQueue + LocalQueue                   │
├─────────────────────────────────────────────────────┤
│                  Base Dev Stack                       │
│  kind cluster · Kueue v0.15.1 · JobSet v0.10.1       │
│  MinIO · Priority classes · Namespace                 │
└─────────────────────────────────────────────────────┘
```

## Kind Cluster

The Phase 4 environment reuses the Phase 3 kind cluster configuration with
**4 worker nodes** (1 control-plane + 4 workers):

```
hack/dev/kind-config-phase3.yaml
```

### Node Layout

Workers are labeled with both pool labels (Phase 3) and topology labels
(Phase 4):

| Node | Pool | Block | Rack |
|------|------|-------|------|
| worker  | on-demand | block-a | rack-1 |
| worker2 | on-demand | block-a | rack-1 |
| worker3 | spot      | block-b | rack-2 |
| worker4 | spot      | block-b | rack-2 |

### Topology Model

Two-level deterministic topology:

```
Level 0: topology.example.io/block
  ├── block-a
  │   └── Level 1: topology.example.io/rack
  │       └── rack-1 (worker, worker2)
  └── block-b
      └── Level 1: topology.example.io/rack
          └── rack-2 (worker3, worker4)
```

This simulates a datacenter with two network blocks, each containing one
rack of two hosts. Kueue TAS uses these labels to make topology-aware
placement decisions.

### Label Application

Labels are applied deterministically by `hack/dev/label-kind-nodes.sh`:
- Workers are sorted lexicographically.
- First two workers → block-a / rack-1 (on-demand pool).
- Last two workers → block-b / rack-2 (spot pool, tainted).

## Kueue Configuration

### Topology Object

```yaml
# deploy/dev/topology/00-dev-topology.yaml
apiVersion: kueue.x-k8s.io/v1beta2
kind: Topology
metadata:
  name: dev-topology
spec:
  levels:
    - nodeLabel: topology.example.io/block
    - nodeLabel: topology.example.io/rack
```

### ResourceFlavor

```yaml
# deploy/dev/flavors/02-phase4-topology.yaml
apiVersion: kueue.x-k8s.io/v1beta2
kind: ResourceFlavor
metadata:
  name: phase4-topology
spec:
  topologyName: dev-topology
```

The `topologyName` tells Kueue to use TAS for workloads admitted through
this flavor.

### Feature Gate

The Kueue controller manager config explicitly enables TAS:

```yaml
# deploy/dev/kueue/controller_manager_config.phase4-topology.yaml
featureGates:
  TopologyAwareScheduling: true
```

### ClusterQueue

```yaml
# deploy/dev/queues/phase4/10-cluster-queue.yaml
apiVersion: kueue.x-k8s.io/v1beta2
kind: ClusterQueue
metadata:
  name: phase4-cq
spec:
  resourceGroups:
    - coveredResources: [cpu, memory]
      flavors:
        - name: phase4-topology
          resources:
            - name: cpu
              nominalQuota: 4
            - name: memory
              nominalQuota: 4Gi
  admissionChecksStrategy:
    admissionChecks:
      - name: resume-readiness
```

Quota: 4 CPU / 4 Gi total — enough for all 4 kind workers.

### LocalQueue

```yaml
# deploy/dev/queues/phase4/20-local-queue.yaml
name: phase4-training → phase4-cq
```

## AdmissionCheck Configuration

### ResumeReadinessPolicy

```yaml
# deploy/dev/admissionchecks/resume-readiness-policy.yaml
spec:
  requireCompleteCheckpoint: true
  allowInitialLaunchWithoutCheckpoint: true
  failurePolicy: FailClosed
```

Default policy: initial launches proceed without a checkpoint; resumes
require a complete, valid checkpoint. FailClosed means catalog errors
block admission (safe for production).

### AdmissionCheck

```yaml
# deploy/dev/admissionchecks/admission-check.yaml
spec:
  controllerName: training.checkpoint.example.io/resume-readiness
  parameters:
    apiGroup: training.checkpoint.example.io
    kind: ResumeReadinessPolicy
    name: default-resume-readiness
```

## Sample RTJs

All samples use placeholders (`__RTJ_NAME__`, `__TRAINER_IMAGE__`,
`__DEV_NAMESPACE__`). Located in `deploy/dev/samples/phase4/`:

| Sample | Topology Mode | Purpose |
|--------|--------------|---------|
| `rtj-topology-disabled.yaml` | Disabled (nil) | Phase 3 behavior on Phase 4 queue |
| `rtj-topology-preferred.yaml` | Preferred | Best-effort rack-level placement |
| `rtj-topology-required.yaml` | Required | Strict rack-level placement |
| `rtj-resume-readiness-gated.yaml` | Disabled | Admission-check-gated launch |

All samples target `phase4-training` queue which has the resume-readiness
admission check.

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make phase4-up` | Create cluster + install base stack + apply Phase 4 profile |
| `make phase4-down` | Delete the kind cluster |
| `make phase4-status` | Show topology, flavors, queues, admission checks |
| `make phase4-load-images` | Load container images into kind |
| `make phase4-smoke` | Run Phase 4 infrastructure smoke test |

## Smoke Test

`make phase4-smoke` validates:

1. Kueue config has RTJ external framework and TAS feature gate.
2. All 4 workers have correct block/rack topology labels.
3. Topology object `dev-topology` exists.
4. ResourceFlavor `phase4-topology` exists with correct topologyName.
5. ClusterQueue `phase4-cq` exists with admission check.
6. LocalQueue `phase4-training` exists.
7. AdmissionCheck `resume-readiness` exists.
8. ResumeReadinessPolicy `default-resume-readiness` exists.
9. Phase 3 pool labels are still present (backward compatibility).

## Local Assumptions

- **No real cluster-autoscaler.** All nodes are statically provisioned by kind.
- **No ProvisioningRequest.** The optional cloud/PR path is not installed.
  G5 is deferred and separate from this local profile.
- **Simulated topology.** Block/rack labels are applied artificially to kind
  workers. In production, these would come from actual hardware topology
  or cloud provider metadata.
- **CPU-only.** No GPU resources. GPU scheduling is simulated via the
  `gpuShape: cpu-only` identity field.
- **Single-cluster.** No multi-Kueue or federation.

## Troubleshooting

### Topology CRD not found

If `make phase4-smoke` reports the Topology CRD is missing:

1. Verify Kueue v0.15.1 is installed: `kubectl get crd | grep kueue`
2. The Topology CRD may require a separate install in some Kueue versions.
   Check the Kueue release notes for v0.15.1.
3. TAS-specific tests will be skipped when the CRD is unavailable; other
   Phase 4 features (admission check, gated launch) still work.

### AdmissionCheck not Active

If the resume-readiness AdmissionCheck shows `Active=False`:

1. Verify the operator is running with the ResumeReadiness controller.
2. Check that the ResumeReadinessPolicy exists:
   `kubectl get resumereadinesspolicies`
3. The AdmissionCheck reconciler sets Active=True only when the referenced
   policy exists and is valid.

### Nodes missing topology labels

If topology labels are missing after `make phase4-up`:

1. Verify the cluster has 4 workers: `kubectl get nodes`
2. Re-run labeling: `./hack/dev/label-kind-nodes.sh`
3. Check for errors in the label script output.
