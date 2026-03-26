# Phase 4 Demo Walkthroughs

This document provides exact command sequences for three Phase 4 scenarios.
Each walkthrough assumes a running Phase 4 cluster (`make phase4-up`) and a
trainer image loaded into kind (`make phase4-load-images`).

## Prerequisites

```bash
# 1. Create the Phase 4 cluster with topology labels, Kueue TAS, and
#    the ResumeReadiness AdmissionCheck.
make phase4-up

# 2. Build and load the operator + trainer images.
make phase4-load-images IMAGES="controller:latest my-trainer:latest"

# 3. Verify infrastructure.
make phase4-smoke
```

---

## Demo 1: Blocked Launch That Later Becomes Allowed

This demo shows an RTJ blocked by the ResumeReadiness admission gate,
then released when the gate clears.

### Step 1: Hold the queue

Create a LocalQueue in held state so the RTJ cannot be admitted
immediately. This lets you observe the blocked state.

```bash
# Patch the LocalQueue to held state (stops new admissions).
kubectl annotate localqueue phase4-training \
  kueue.x-k8s.io/stop-policy=Hold \
  -n checkpoint-dev --overwrite
```

### Step 2: Submit the RTJ

```bash
make phase4-submit-gated-resume \
  PHASE4_RTJ_NAME=gated-demo \
  PHASE4_TRAINER_IMAGE=my-trainer:latest
```

### Step 3: Observe the blocked state

```bash
# RTJ should be in Queued phase — no child JobSet created.
make phase4-inspect-workload PHASE4_RTJ_NAME=gated-demo
```

Expected output:
- `phase=Queued`
- `activeJobSet=<no active child JobSet>`
- Workload exists but has no admission

### Step 4: Release the queue

```bash
# Remove the hold — Kueue will admit the Workload.
kubectl annotate localqueue phase4-training \
  kueue.x-k8s.io/stop-policy- \
  -n checkpoint-dev
```

### Step 5: Observe the launch

```bash
# Wait a few seconds, then inspect again.
sleep 10
make phase4-inspect-workload PHASE4_RTJ_NAME=gated-demo
```

Expected output:
- `phase=Starting` or `phase=Running`
- `launchReadiness.ready=true`
- `launchReadiness.gateState=Ready`
- Active child JobSet created

### Step 6: Inspect the admission check

```bash
make phase4-inspect-admissioncheck
```

Expected output:
- AdmissionCheck Active condition: `status=True`, `reason=ControllerReady`
- Workload check state: `state=Ready`, message contains `InitialLaunchReady`

### Cleanup

```bash
kubectl -n checkpoint-dev delete resumabletrainingjobs.training.checkpoint.example.io gated-demo
```

---

## Demo 2: Topology-Aware Launch

This demo shows an RTJ with `topology.mode=Required` that gets placed
into a specific rack by Kueue TAS.

### Step 1: Submit the topology-aware RTJ

```bash
make phase4-submit-topology \
  PHASE4_RTJ_NAME=topo-demo \
  PHASE4_TRAINER_IMAGE=my-trainer:latest \
  PHASE4_TOPOLOGY_MODE=required
```

### Step 2: Inspect the Workload topology assignment

```bash
# Wait for admission.
sleep 10
make phase4-inspect-topology PHASE4_RTJ_NAME=topo-demo
```

Expected output:
- `spec.topology.mode: Required`
- `spec.topology.topologyLevel: topology.example.io/rack`
- Workload `podSetAssignment` shows `topologyAssignment.levels`
- RTJ `status.topology` shows levels and domains

### Step 3: Verify node placement

```bash
make phase4-inspect-workload PHASE4_RTJ_NAME=topo-demo
```

Expected output:
- Child JobSet has `nodeSelector` with rack label
- Pods land on nodes within the same rack
- `effectiveLaunchShape.workerCount=2`, `worldSize=2`

### Step 4: Inspect the full topology chain

```bash
# Node labels → Topology object → ResourceFlavor → Workload → child JobSet
make phase4-inspect-topology PHASE4_RTJ_NAME=topo-demo
```

### Cleanup

```bash
kubectl -n checkpoint-dev delete resumabletrainingjobs.training.checkpoint.example.io topo-demo
```

---

## Demo 3: Topology-Aware Resume

This demo shows pause → resume with topology preservation: the resumed
child JobSet gets the same topology constraints.

### Step 1: Submit and run

```bash
make phase4-submit-topology \
  PHASE4_RTJ_NAME=topo-resume \
  PHASE4_TRAINER_IMAGE=my-trainer:latest \
  PHASE4_TOPOLOGY_MODE=required
```

### Step 2: Wait for Running

```bash
# Wait for the RTJ to reach Running and produce a checkpoint.
# (Timing depends on trainer — adjust as needed.)
sleep 30
make phase4-inspect-workload PHASE4_RTJ_NAME=topo-resume
```

Expected: `phase=Running`, child JobSet active, pods on same rack.

### Step 3: Pause the RTJ

```bash
kubectl -n checkpoint-dev patch \
  resumabletrainingjobs.training.checkpoint.example.io topo-resume \
  --type merge -p '{"spec":{"control":{"desiredState":"Paused"}}}'
```

### Step 4: Wait for Paused

```bash
sleep 20
make phase4-inspect-workload PHASE4_RTJ_NAME=topo-resume
```

Expected: `phase=Paused`, child JobSet deleted.

### Step 5: Resume

```bash
kubectl -n checkpoint-dev patch \
  resumabletrainingjobs.training.checkpoint.example.io topo-resume \
  --type merge -p '{"spec":{"control":{"desiredState":"Running"}}}'
```

### Step 6: Verify resumed topology

```bash
sleep 15
make phase4-inspect-topology PHASE4_RTJ_NAME=topo-resume
make phase4-inspect-checkpoints PHASE4_RTJ_NAME=topo-resume
```

Expected:
- `phase=Running` (or `Restoring` briefly)
- New child JobSet with topology nodeSelector
- `status.topology` re-populated with rack placement
- `effectiveLaunchShape.selectedCheckpointID` populated
- `effectiveLaunchShape.resumeMode` = `SameSize` or `Reshard`
- Pods land on nodes within the same rack

### Cleanup

```bash
kubectl -n checkpoint-dev delete resumabletrainingjobs.training.checkpoint.example.io topo-resume
```

---

## Quick Reference

| Command | What it does |
|---------|-------------|
| `make phase4-submit-topology` | Submit topology-aware RTJ |
| `make phase4-submit-gated-resume` | Submit readiness-gated RTJ |
| `make phase4-inspect-workload` | RTJ + Workload + child JobSet |
| `make phase4-inspect-admissioncheck` | AdmissionCheck + policy + states |
| `make phase4-inspect-topology` | Topology chain end-to-end |
| `make phase4-inspect-checkpoints` | Checkpoint evidence for gate |
| `make phase4-status` | Cluster infrastructure overview |
| `make phase4-smoke` | Infrastructure health check |
| `make e2e-phase4` | Run Phase 4 e2e tests |
