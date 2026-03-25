# Phase 3 Demo Walkthroughs

## Prerequisites

1. Phase 3 kind cluster running:

   ```bash
   make phase3-up
   ```

2. Trainer image built and loaded:

   ```bash
   docker build -t phase1-ddp-counter:dev -f fixtures/pytorch_ddp_counter/Dockerfile .
   make phase3-load-images IMAGES=phase1-ddp-counter:dev
   ```

3. CRD installed:

   ```bash
   kubectl apply -f deploy/crd/
   ```

4. Operator running in a separate terminal:

   ```bash
   go run ./cmd/operator --leader-elect=false
   ```

5. MinIO port-forward for checkpoint inspection (optional, separate terminal):

   ```bash
   kubectl -n checkpoint-dev port-forward svc/minio 9000:9000
   ```

## Demo 1: Flavor-Aware Launch

This demo shows Kueue assigning a ResourceFlavor (on-demand or spot) and the
controller materializing nodeSelector/tolerations into the child JobSet.

### Step 1: Submit the RTJ

```bash
make phase3-submit-flavor PHASE3_TRAINER_IMAGE=phase1-ddp-counter:dev
```

### Step 2: Wait for Running

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io phase3-demo -w
```

Wait until `PHASE` shows `Running`.

### Step 3: Inspect admission

```bash
make phase3-inspect-admission RTJ_NAME=phase3-demo
```

Expected output includes:

- `admitted=2` and `preferred=2` (full admission).
- Kueue Workload shows `podSetAssignments` with a flavor name (`on-demand`
  or `spot`).
- Child JobSet pod template has `nodeSelector` with
  `checkpoint-native.dev/pool` matching the assigned flavor.
- If `spot` was assigned, the pod template includes the
  `checkpoint-native.dev/spot=true:NoSchedule` toleration.
- Pods are placed on nodes in the assigned pool.

### Step 4: Verify node placement

```bash
kubectl -n checkpoint-dev get pods -o wide
make phase3-status
```

Compare pod `NODE` column with the node pool labels.

### Step 5: Clean up

```bash
kubectl -n checkpoint-dev delete resumabletrainingjobs.training.checkpoint.example.io phase3-demo
```

## Demo 2: Mixed-Size Resume (Flexible World Size)

This demo shows the full pause/resume cycle with `allowWorldSizeChange=true`.
In the default `flavors` profile, Kueue admits all-or-nothing (same size). The
controller code path is identical to different-size — only the restore mode
differs (`SameSize` vs `Reshard`).

### Step 1: Submit the flexible RTJ

```bash
make phase3-submit-flex PHASE3_TRAINER_IMAGE=phase1-ddp-counter:dev
```

### Step 2: Wait for Running

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io phase3-demo -w
```

### Step 3: Let training run, then pause

Wait at least 10 seconds for checkpoints to accumulate, then:

```bash
kubectl -n checkpoint-dev patch resumabletrainingjobs.training.checkpoint.example.io phase3-demo \
  --type=merge -p '{"spec":{"control":{"desiredState":"Paused"}}}'
```

### Step 4: Wait for Paused with checkpoint

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io phase3-demo -w
```

Wait until `PHASE` shows `Paused`.

### Step 5: Inspect checkpoint state

```bash
make phase3-inspect-checkpoints RTJ_NAME=phase3-demo
```

Expected output:

- `lastCompletedCheckpoint.manifestURI` is populated.
- `lastCompletedCheckpoint.worldSize` is set.

### Step 6: Resume

```bash
kubectl -n checkpoint-dev patch resumabletrainingjobs.training.checkpoint.example.io phase3-demo \
  --type=merge -p '{"spec":{"control":{"desiredState":"Running"}}}'
```

### Step 7: Verify resume state

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io phase3-demo -w
```

Wait for `Running`, then inspect:

```bash
make phase3-inspect-checkpoints RTJ_NAME=phase3-demo
make phase3-inspect-admission RTJ_NAME=phase3-demo
```

Expected output:

- `status.restore.restoreMode` is `SameSize` (since admitted == checkpoint
  world size in the default profile).
- `status.restore.lastCheckpointWorldSize` and `lastRestoreWorldSize` match.
- `status.selectedCheckpoint.manifestURI` matches the first pause checkpoint.
- `status.admission.admittedWorkerCount > 0`.

### Step 8: Pause again and verify step monotonicity

```bash
kubectl -n checkpoint-dev patch resumabletrainingjobs.training.checkpoint.example.io phase3-demo \
  --type=merge -p '{"spec":{"control":{"desiredState":"Paused"}}}'
```

Wait for `Paused`, then inspect the second checkpoint:

```bash
make phase3-inspect-checkpoints RTJ_NAME=phase3-demo
```

The `globalStep` in the second checkpoint manifest should be greater than the
first.

### Step 9: Clean up

```bash
kubectl -n checkpoint-dev delete resumabletrainingjobs.training.checkpoint.example.io phase3-demo
```

## Demo 3: Different-Size Resume (Experimental)

This requires the `experimental` profile and the partial admission operator
flag. In this mode, Kueue can admit fewer pods than requested, and the trainer
uses DCP resharding to restore from a checkpoint taken at a different world
size.

### Prerequisites (additional)

```bash
# Create cluster with experimental profile:
make phase3-up PHASE3_PROFILE=experimental

# Or switch an existing cluster:
make phase3-profile PHASE3_PROFILE=experimental

# Start operator with partial admission enabled:
go run ./cmd/operator --leader-elect=false --enable-experimental-partial-admission
```

### Steps

```bash
# Submit the partial admission RTJ (preferred=4, min=2):
kubectl apply -f deploy/dev/samples/phase3/rtj-partial-admission.yaml

# Wait for Running, then inspect admission count:
make phase3-inspect-admission RTJ_NAME=phase3-partial

# Pause and checkpoint:
kubectl -n checkpoint-dev patch resumabletrainingjobs.training.checkpoint.example.io phase3-partial \
  --type=merge -p '{"spec":{"control":{"desiredState":"Paused"}}}'

# Wait for Paused, then inspect checkpoint world size:
make phase3-inspect-checkpoints RTJ_NAME=phase3-partial

# Resume — Kueue may re-admit at a different count:
kubectl -n checkpoint-dev patch resumabletrainingjobs.training.checkpoint.example.io phase3-partial \
  --type=merge -p '{"spec":{"control":{"desiredState":"Running"}}}'

# Inspect restore status:
make phase3-inspect-checkpoints RTJ_NAME=phase3-partial
# If admitted count differs from checkpoint world size,
# restoreMode will be "Reshard" and the trainer will use DCP resharding.
```

## Metrics Inspection

While the operator is running, check Phase 3 metrics:

```bash
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator
```

Key Phase 3 metrics:

| Metric | Description |
| --- | --- |
| `admission_comparisons_total{comparison="equal"}` | Full admissions |
| `admission_comparisons_total{comparison="partial"}` | Partial admissions |
| `same_size_resumes_total` | Resumes at same world size |
| `different_size_resumes_total` | Resumes at different world size |
| `reshard_restores_attempted_total` | Reshard restore attempts |
| `flavor_assignments_total{flavor="..."}` | Flavor assignment counts |
| `partial_admission_launches_total` | Launches with partial admission |
