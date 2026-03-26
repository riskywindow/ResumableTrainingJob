# Phase 4 Operations Guide

This guide explains how to inspect the Phase 4 admission pipeline
components without reading the source code.

## Inspecting the Custom AdmissionCheck

The ResumeReadiness AdmissionCheck gates workload admission until the
operator confirms checkpoint readiness. It is a cluster-scoped Kueue
resource.

### Check existence and health

```bash
# List all admission checks.
kubectl get admissionchecks.kueue.x-k8s.io

# Inspect a specific check.
kubectl get admissionchecks.kueue.x-k8s.io resume-readiness -o yaml
```

Key fields:
- `spec.controllerName`: Must be `training.checkpoint.example.io/resume-readiness`
- `spec.parameters`: Must reference a valid `ResumeReadinessPolicy`
- `status.conditions[type=Active]`: Must be `True` for the check to be functional

### Check the Active condition

```bash
kubectl get admissionchecks.kueue.x-k8s.io resume-readiness \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} reason={.reason}{"\n"}{end}'
```

- `Active=True, reason=ControllerReady` — healthy
- `Active=False, reason=PolicyNotFound` — policy missing or wrong reference
- `Active=False, reason=ParametersMissing` — no parameters on the AdmissionCheck

### Inspect the referenced policy

```bash
kubectl get resumereadinesspolicies.training.checkpoint.example.io \
  default-resume-readiness -o yaml
```

Key fields:
- `spec.requireCompleteCheckpoint`: Whether checkpoint must be complete (default: true)
- `spec.allowInitialLaunchWithoutCheckpoint`: Allow first launch without checkpoint (default: true)
- `spec.failurePolicy`: `FailClosed` (default) or `FailOpen`
- `spec.maxCheckpointAge`: Optional age limit for checkpoints

### One-command inspection

```bash
make phase4-inspect-admissioncheck
```

---

## Inspecting RTJ and Workload Status

### RTJ status overview

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io \
  <rtj-name> -o yaml
```

Phase 4 status fields (populated only when using the gated path):
- `status.launchReadiness` — gate state
  - `ready`: `true`/`false`
  - `gateState`: `Pending`, `Ready`, or `Rejected`
  - `reason`: machine-readable reason (e.g., `WaitingForReadinessGate`)
  - `message`: human-readable explanation
- `status.topology` — admitted topology assignment
  - `levels`: topology level label keys
  - `domains`: per-domain values and pod counts
- `status.effectiveLaunchShape` — computed launch parameters
  - `workerCount`, `worldSize`, `resumeMode`, `selectedCheckpointID`

### Kueue Workload for an RTJ

```bash
# Find the Workload owned by an RTJ.
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io \
  -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"<rtj-name>\")]}{.metadata.name}{end}"

# Inspect it.
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io <workload-name> -o yaml
```

Key sections:
- `status.admission.clusterQueue` — which queue admitted the workload
- `status.admission.podSetAssignments` — per-PodSet counts, flavors, topology
- `status.admissionChecks` — per-check state and message

### One-command inspection

```bash
make phase4-inspect-workload PHASE4_RTJ_NAME=<rtj-name>
```

---

## Inspecting Topology Assignment and Admitted Flavors

### Topology chain overview

The topology chain flows:
1. **Node labels** (`topology.example.io/block`, `topology.example.io/rack`)
2. **Kueue Topology object** (`dev-topology`) defines the level hierarchy
3. **ResourceFlavor** (`phase4-topology`) references the Topology object
4. **ClusterQueue** uses the flavor with resource groups
5. **Workload** receives a `TopologyAssignment` per PodSet on admission
6. **RTJ** parses the assignment into `status.topology`
7. **Child JobSet** gets `nodeSelector` labels injected from the assignment

### Inspect node topology labels

```bash
kubectl get nodes \
  -L topology.example.io/block \
  -L topology.example.io/rack \
  -L checkpoint-native.dev/pool
```

### Inspect the Topology object

```bash
kubectl get topologies.kueue.x-k8s.io dev-topology -o yaml
```

### Inspect ResourceFlavor topology binding

```bash
kubectl get resourceflavors.kueue.x-k8s.io phase4-topology -o yaml
```

Key field: `spec.topologyName: dev-topology`

### Inspect Workload topology assignment

```bash
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io <workload-name> \
  -o jsonpath='{range .status.admission.podSetAssignments[*]}name={.name} flavors={.flavors} topology={.topologyAssignment}{"\n"}{end}'
```

### Inspect admitted flavors

```bash
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io <workload-name> \
  -o jsonpath='{range .status.admission.podSetAssignments[*]}name={.name} flavors={.flavors}{"\n"}{end}'
```

### One-command inspection

```bash
make phase4-inspect-topology PHASE4_RTJ_NAME=<rtj-name>
```

---

## Inspecting the Rendered Child JobSet

The child JobSet is a plain runtime resource with no Kueue management
metadata. Topology constraints are expressed as `nodeSelector` labels
on the pod template.

### Inspect the child JobSet

```bash
# Get the active child JobSet name from the RTJ.
active=$(kubectl -n checkpoint-dev get \
  resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.status.activeJobSetName}')

# Inspect the full spec.
kubectl -n checkpoint-dev get jobset "$active" -o yaml
```

### Check nodeSelector (topology injection)

```bash
kubectl -n checkpoint-dev get jobset "$active" \
  -o jsonpath='{range .spec.replicatedJobs[*]}{.name}: {.template.spec.template.spec.template.spec.nodeSelector}{"\n"}{end}'
```

### Check replicas (admitted count)

```bash
kubectl -n checkpoint-dev get jobset "$active" \
  -o jsonpath='{range .spec.replicatedJobs[*]}{.name}: replicas={.replicas}{"\n"}{end}'
```

### Verify no Kueue management labels

```bash
kubectl -n checkpoint-dev get jobset "$active" \
  -o jsonpath='{.metadata.labels}' | python3 -c "
import json, sys
labels = json.load(sys.stdin)
kueue = {k: v for k, v in labels.items() if 'kueue' in k.lower()}
if kueue:
    print('WARNING: Kueue labels found on child JobSet:', kueue)
else:
    print('OK: No Kueue management labels on child JobSet')
"
```

### Check pod placement

```bash
kubectl -n checkpoint-dev get pods \
  -l "jobset.sigs.k8s.io/jobset-name=$active" \
  -o custom-columns='NAME:.metadata.name,NODE:.spec.nodeName,STATUS:.status.phase'
```

---

## Inspecting Checkpoint Evidence Used by the Gate

The ResumeReadiness admission check uses checkpoint evidence to decide
whether to approve or reject a launch.

### What the gate evaluates

1. Is this an initial launch (no prior checkpoint)?
   - If `allowInitialLaunchWithoutCheckpoint=true` → Ready
2. Is a compatible, complete checkpoint available?
   - Checks: lineage, code version, format, completeness
3. Is the checkpoint within age limits?
   - If `maxCheckpointAge` is set, compares checkpoint timestamp
4. Error handling:
   - Storage errors + `FailClosed` → Retry
   - Storage errors + `FailOpen` → Ready

### Inspect checkpoint state

```bash
make phase4-inspect-checkpoints PHASE4_RTJ_NAME=<rtj-name>
```

### Check the Workload admission check message

The message on the admission check state contains the decision reason:

```bash
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io <workload-name> \
  -o jsonpath='{range .status.admissionChecks[*]}name={.name} state={.state} message={.message}{"\n"}{end}'
```

Reason strings:
- `InitialLaunchReady` — first launch, no checkpoint needed
- `CheckpointReady` — valid checkpoint found
- `NoCheckpointAvailable` — no compatible checkpoint (rejected)
- `CheckpointTooOld` — checkpoint exceeds age limit (rejected)
- `CheckpointIncomplete` — checkpoint failed completeness check (rejected)
- `CheckpointIncompatible` — no compatible checkpoint (rejected)
- `StorageUnavailable` — catalog error (retry or ready based on policy)
- `InitialLaunchBlocked` — no checkpoint + `allowInitial=false` (rejected)
- `PolicyResolutionFailed` — policy reference broken (retry)

---

## Metrics

The operator exposes Phase 4 metrics on the metrics endpoint (default `:8080/metrics`).

### Phase 4 metrics

| Metric | Type | Description |
|--------|------|-------------|
| `checkpoint_native_operator_launches_blocked_by_readiness_gate_total` | Counter | Launches blocked by readiness gate |
| `checkpoint_native_operator_readiness_gate_outcomes_total` | Counter (by reason) | Gate evaluation outcomes |
| `checkpoint_native_operator_topology_aware_launches_total` | Counter | Topology-aware launches |
| `checkpoint_native_operator_topology_assignment_waits_total` | Counter | Waits for topology assignment |
| `checkpoint_native_operator_phase4_resumes_attempted_total` | Counter | Phase 4 gated resume attempts |
| `checkpoint_native_operator_phase4_resumes_succeeded_total` | Counter | Phase 4 gated resume successes |
| `checkpoint_native_operator_phase4_resumes_failed_total` | Counter | Phase 4 gated resume failures |
| `checkpoint_native_operator_unsupported_topology_shape_failures_total` | Counter | Topology shapes that could not be represented |

### Scraping metrics

```bash
# Port-forward to the operator metrics endpoint.
kubectl -n checkpoint-dev port-forward deploy/checkpoint-operator 8080:8080 &

# Query Phase 4 metrics.
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_.*phase4
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_topology
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_readiness_gate
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_launches_blocked
```
