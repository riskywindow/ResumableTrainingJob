# Phase 9 Demo: Elastic Resize for ResumableTrainingJobs

This document is the exact command sequence for the Phase 9 demo.
Phase 9 adds elastic worker-count resize to ResumableTrainingJob (RTJ):
manual target-based shrink with dynamic quota reclaim via
`Workload.status.reclaimablePods`, and grow via checkpoint-and-relaunch.

The demo covers:

1. Prerequisites and environment setup
2. Launching an elastic RTJ at 4 workers (`rtj-elastic-shrink`)
3. Shrinking 4 to 2 workers, observing `reclaimablePods` publication and quota release
4. A second RTJ (`rtj-elastic-grow`) being admitted into the freed quota
5. Growing the first RTJ back to 4 workers via checkpoint-and-relaunch
6. Cleanup

## Prerequisites

- Docker, kind, kubectl, and Go installed locally.
- The trainer image built and available (the demo uses the DDP counter fixture).

```bash
docker build -t phase9-ddp-counter:dev -f fixtures/pytorch_ddp_counter/Dockerfile .
```

## 1. Environment Setup

### Terminal A: Bring up the Phase 9 cluster and operator

Create the kind cluster (name: `checkpoint-phase1`), install the base stack,
and apply the Phase 9 elastic resize profile. This installs:

- RTJ CRD with `spec.elasticity` and `status.elasticity` fields
- ClusterQueue `phase9-cq` (1250m CPU / 1280Mi memory)
- LocalQueue `phase9-training` in namespace `checkpoint-dev`
- Kueue configured with RTJ external framework and `waitForPodsReady`

```bash
make phase9-up
```

Load the trainer image into the kind cluster:

```bash
make phase9-load-images IMAGES=phase9-ddp-counter:dev
```

Verify the infrastructure is correctly configured (17+ checks):

```bash
make phase9-smoke
```

Expected: All checks print `PASS`. The summary shows `phase9-cq` at 1250m CPU
and confirms the `reclaimablePods` SSA path is available.

Start the operator in Terminal A:

```bash
go run ./cmd/operator --leader-elect=false
```

Keep this process running for all remaining steps.

### Terminal B: Verify cluster state

```bash
make phase9-submit
```

Note: The `phase9-submit`, `phase9-shrink`, `phase9-grow`,
`phase9-inspect-elastic`, `phase9-inspect-workload`, and
`phase9-inspect-checkpoints` targets follow the same pattern as earlier phases.
Where those targets delegate to scripts, the manual `kubectl` commands below
are the canonical reference.

Check quota before submitting any RTJ:

```bash
kubectl -n checkpoint-dev get clusterqueues.kueue.x-k8s.io phase9-cq \
  -o jsonpath='{.status.flavorsUsage}' | jq .
```

Expected: No RTJs admitted yet; used CPU is 0.

## 2. Launch the Elastic RTJ at 4 Workers

### Submit `rtj-elastic-shrink`

This RTJ starts with 4 workers (`preferredCount: 4`) and `spec.elasticity.mode:
Manual`. Each worker requests 250m CPU, so it consumes 1000m of the 1250m quota.

```bash
make phase9-submit
```

Or apply the sample directly:

```bash
sed \
  -e 's|__RTJ_NAME__|rtj-elastic-shrink|g' \
  -e 's|__TRAINER_IMAGE__|phase9-ddp-counter:dev|g' \
  -e 's|__DEV_NAMESPACE__|checkpoint-dev|g' \
  deploy/dev/phase9/samples/rtj-elastic-shrink.yaml | kubectl apply -f -
```

### Wait for Running

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io \
  rtj-elastic-shrink -w
```

Wait until `status.phase` shows `Running`.

### Inspect the elastic status

```bash
make phase9-inspect-elastic
```

Or directly:

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io \
  rtj-elastic-shrink \
  -o jsonpath='{.status.elasticity}' | jq .
```

Expected observations:

- `status.elasticity.resizeState` is `Idle` (no resize in progress).
- `status.elasticity.admittedWorkerCount` is `4`.
- `status.elasticity.currentExecutionMode` is `Elastic`.
- `status.elasticity.reclaimablePodsPublished` is `false`.

### Inspect the Kueue Workload

```bash
make phase9-inspect-workload
```

Or directly:

```bash
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io \
  -l training.checkpoint.example.io/rtj-name=rtj-elastic-shrink -o yaml | \
  grep -A5 'reclaimablePods\|admission\|podSets'
```

Expected: The Workload is admitted with 4 worker pods. The `reclaimablePods`
field is absent or empty — no quota has been released yet.

### Note what this step demonstrates

The Phase 9 queue is deliberately undersized (1250m) so that two full-size
4-worker RTJs cannot coexist (they would need 2000m). With one 4-worker RTJ
running, only 250m CPU remains — not enough to admit a second 2-worker RTJ
(which needs 500m). The dynamic reclaim flow resolves this without preemption.

## 3. Submit the Second RTJ and Observe Queuing

### Submit `rtj-elastic-grow`

This RTJ starts with 2 workers and requests 500m CPU. With `rtj-elastic-shrink`
holding 1000m, the queue has only 250m free — insufficient for admission.

```bash
sed \
  -e 's|__RTJ_NAME__|rtj-elastic-grow|g' \
  -e 's|__TRAINER_IMAGE__|phase9-ddp-counter:dev|g' \
  -e 's|__DEV_NAMESPACE__|checkpoint-dev|g' \
  deploy/dev/phase9/samples/rtj-elastic-grow.yaml | kubectl apply -f -
```

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io \
  rtj-elastic-grow -w
```

Expected: `rtj-elastic-grow` remains in `Queued` phase. Kueue cannot admit it
because the combined CPU demand (1000m + 500m = 1500m) exceeds the 1250m quota.

```bash
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io \
  -l training.checkpoint.example.io/rtj-name=rtj-elastic-grow
```

Expected: The Workload exists but `status.admission` is nil (not yet admitted).

## 4. Shrink `rtj-elastic-shrink` from 4 to 2 Workers

### Patch the target worker count

This is the single user-facing action for a manual elastic resize. The
controller evaluates the delta and chooses the resize path.

```bash
make phase9-shrink
```

Or directly:

```bash
kubectl -n checkpoint-dev patch \
  resumabletrainingjobs.training.checkpoint.example.io rtj-elastic-shrink \
  --type=merge \
  -p '{"spec":{"elasticity":{"targetWorkerCount":2}}}'
```

### Watch the resize lifecycle

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io \
  rtj-elastic-shrink -w
```

Because `YIELD_SDK_SUPPORTS_IN_PLACE_SHRINK=false` in the DDP fixture, the
controller falls back to checkpoint-and-relaunch. The RTJ will transition:

`Running` → (drain + checkpoint) → `Paused` → `Queued` → `Running` (at 2 workers)

### Inspect the resize state during transition

```bash
make phase9-inspect-elastic
```

Expected observations at various points during the resize:

- `resizeState` progresses: `Idle` → `Pending` → `InProgress` → `Completed`.
- `resizePath` is `CheckpointAndRelaunch` (DDP does not support in-place shrink).
- Conditions `ResizeCheckpointing` and then `RelaunchingForResize` are set
  during the transition.
- Once `Running` again: `admittedWorkerCount` is `2`, `resizeState` is
  `Completed` or `Idle`.

### Observe `reclaimablePods` and quota release

During a checkpoint-and-relaunch shrink, quota is released via the full
suspend/re-admit cycle rather than the `reclaimablePods` SSA patch. After
the RTJ re-admits at 2 workers, the Workload's admitted CPU drops from 1000m
to 500m. The remaining 750m is now available to the queue.

```bash
make phase9-inspect-workload
```

Expected: Workload admission shows 2 worker pods. The `reclaimablePods` field
is absent (this field is used for the in-place shrink path; C&R shrink releases
quota implicitly through re-admission at the smaller count).

To observe the in-place shrink path (where `reclaimablePods` IS written), the
runtime must report `inPlaceShrinkSupported=true`. This is exercised by the e2e
test `TestElasticShrinkDynamicReclaim`, which patches the status fixture. In
that path:

- `status.elasticity.reclaimablePodsPublished` becomes `true`.
- `Workload.status.reclaimablePods` shows `{name: workers, count: 2}`.
- The RTJ remains `Running` throughout — it is NOT evicted.
- Kueue reads `reclaimablePods` and releases 500m CPU to the queue.

### Inspect checkpoints after shrink

```bash
make phase9-inspect-checkpoints
```

Or directly:

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io \
  rtj-elastic-shrink \
  -o jsonpath='{.status.lastCompletedCheckpoint}' | jq .
```

Expected:

- `manifestURI` is populated (the checkpoint written during the resize drain).
- The checkpoint manifest contains `resizeDirection: Shrink`,
  `resizeTargetWorkerCount: 2`, and `resizeActiveWorkerCount: 4`.
- `currentRunAttempt` has incremented (the re-launch increments the attempt counter).

## 5. Observe the Second RTJ Being Admitted

With `rtj-elastic-shrink` now using only 500m CPU, the queue has 750m free.
The 2-worker `rtj-elastic-grow` (500m CPU) can now be admitted.

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io \
  rtj-elastic-grow -w
```

Expected: `rtj-elastic-grow` transitions from `Queued` to `Running`. Kueue
admits it once the freed quota becomes available.

```bash
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io \
  -l training.checkpoint.example.io/rtj-name=rtj-elastic-grow
```

Expected: `status.admission` is now populated with 2 worker pods.

Verify the combined quota usage:

```bash
kubectl get clusterqueues.kueue.x-k8s.io phase9-cq \
  -o jsonpath='{.status.flavorsUsage}' | jq .
```

Expected: Total CPU used is approximately 1000m (500m + 500m), within the 1250m
quota ceiling.

### Note what this step demonstrates

Dynamic quota reclaim — a running RTJ voluntarily reduces its resource footprint,
enabling another workload to be admitted without any operator intervention or
preemption. The key Kueue API used is `Workload.status.reclaimablePods`
(for the in-place path) or implicit re-admission (for the C&R path).

## 6. Grow `rtj-elastic-shrink` Back to 4 Workers

With both RTJs now running at 2 workers (1000m total), there is no room to
grow `rtj-elastic-shrink` back to 4 workers without first freeing quota.
For this step, delete `rtj-elastic-grow` to reclaim its 500m, leaving 750m
free — not quite enough for 2 additional workers (500m needed). To fully
demonstrate the grow path, first free more quota:

```bash
kubectl -n checkpoint-dev delete \
  resumabletrainingjobs.training.checkpoint.example.io rtj-elastic-grow
```

Wait for the deletion to complete and the quota to be released:

```bash
kubectl get clusterqueues.kueue.x-k8s.io phase9-cq \
  -o jsonpath='{.status.flavorsUsage}' | jq .
```

Expected: CPU used drops to approximately 500m (only `rtj-elastic-shrink`
remaining at 2 workers), leaving 750m free.

### Patch the target worker count to grow

```bash
make phase9-grow
```

Or directly:

```bash
kubectl -n checkpoint-dev patch \
  resumabletrainingjobs.training.checkpoint.example.io rtj-elastic-shrink \
  --type=merge \
  -p '{"spec":{"elasticity":{"targetWorkerCount":4}}}'
```

### Watch the grow lifecycle

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io \
  rtj-elastic-shrink -w
```

Grow always uses checkpoint-and-relaunch (Invariant I-10: scale-up always
goes through C&R because it requires new Kueue admission for the additional
workers). The RTJ transitions:

`Running` (2 workers) → (drain + checkpoint) → `Paused` → `Queued` → `Running` (4 workers)

### Inspect the grow state

```bash
make phase9-inspect-elastic
```

Expected observations:

- `resizePath` is `CheckpointAndRelaunch`.
- `resizeState` progresses through `InProgress` → `Completed`.
- Conditions: `ResizeCheckpointing` → `RelaunchingForResize` → (cleared on success).
- After completion: `admittedWorkerCount` is `4`.
- `currentRunAttempt` has incremented again.

### Inspect checkpoints after grow

```bash
make phase9-inspect-checkpoints
```

Expected:

- A new checkpoint with `resizeDirection: Grow` and `resizeTargetWorkerCount: 4`.
- The new child JobSet has 4 worker replicas.
- `globalStep` in the grow checkpoint is greater than the shrink checkpoint —
  training progress is monotonic across resize cycles.

## 7. Inspect Metrics

While the operator is running, check Phase 9 elasticity metrics:

```bash
curl -s http://localhost:8080/metrics | grep -E \
  'checkpoint_native_operator_(elastic|resize|reclaim)'
```

Key Phase 9 metrics to look for:

| Metric | What it shows |
|--------|---------------|
| `elastic_resize_total{direction="shrink",path="checkpoint_and_relaunch"}` | C&R shrink count |
| `elastic_resize_total{direction="shrink",path="in_place"}` | In-place shrink count |
| `elastic_resize_total{direction="grow",path="checkpoint_and_relaunch"}` | Grow count |
| `elastic_reclaim_pods_published_total` | Times `reclaimablePods` was written |
| `elastic_resize_state{state="completed"}` | Successful resize completions |

## 8. Run the Automated e2e Tests

The three Phase 9 e2e tests cover the resize paths exercised manually above:

```bash
make e2e-phase9 PHASE9_TRAINER_IMAGE=phase9-ddp-counter:dev
```

Or run individual tests:

```bash
# Dynamic quota reclaim via reclaimablePods (in-place shrink path):
RUN_KIND_E2E=1 PHASE9_TRAINER_IMAGE=phase9-ddp-counter:dev \
  go test ./test/e2e -run TestElasticShrinkDynamicReclaim -v -timeout 20m

# Grow via checkpoint-and-relaunch:
RUN_KIND_E2E=1 PHASE9_TRAINER_IMAGE=phase9-ddp-counter:dev \
  go test ./test/e2e -run TestElasticGrowViaRelaunch -v -timeout 20m

# C&R shrink fallback (DDP default, no in-place support):
RUN_KIND_E2E=1 PHASE9_TRAINER_IMAGE=phase9-ddp-counter:dev \
  go test ./test/e2e -run TestElasticFallbackShrinkViaRelaunch -v -timeout 20m
```

`TestElasticShrinkDynamicReclaim` is the canonical test for the in-place shrink
path. It patches `status.elasticity.inPlaceShrinkSupported=true` as a fixture
knob, which causes the controller to write `reclaimablePods` to the Workload
instead of triggering a drain. This is the path where Kueue releases quota
while the RTJ remains continuously `Running`.

## 9. Cleanup

Delete individual RTJs:

```bash
kubectl -n checkpoint-dev delete \
  resumabletrainingjobs.training.checkpoint.example.io \
  rtj-elastic-shrink rtj-elastic-grow --ignore-not-found
```

Or tear down the entire environment:

```bash
make phase9-down
```

## Demo Command Summary

| Step | Command |
|------|---------|
| Create environment | `make phase9-up` |
| Load trainer image | `make phase9-load-images IMAGES=phase9-ddp-counter:dev` |
| Verify infrastructure | `make phase9-smoke` |
| Start operator | `go run ./cmd/operator --leader-elect=false` |
| Submit elastic RTJ (4 workers) | `make phase9-submit` |
| Inspect elastic status | `make phase9-inspect-elastic` |
| Inspect Kueue Workload | `make phase9-inspect-workload` |
| Trigger 4→2 shrink | `make phase9-shrink` |
| Inspect checkpoints | `make phase9-inspect-checkpoints` |
| Trigger 2→4 grow | `make phase9-grow` |
| Run all e2e tests | `make e2e-phase9 PHASE9_TRAINER_IMAGE=phase9-ddp-counter:dev` |
| Tear down | `make phase9-down` |

## Quota Accounting Reference

| Scenario | Workers | CPU | Fits in 1250m? |
|----------|---------|-----|----------------|
| RTJ A at 4 workers | 4 | 1000m | Yes (250m spare) |
| RTJ A (4) + RTJ B (2) | 6 | 1500m | No (over quota) |
| RTJ A shrinks to 2 | 2 | 500m | Yes (750m spare) |
| RTJ A (2) + RTJ B (2) | 4 | 1000m | Yes (250m spare) |
| RTJ A grows back to 4 (after B deleted) | 4 | 1000m | Yes (250m spare) |

Each worker pod requests 250m CPU and 256Mi memory.
The `phase9-cq` ClusterQueue nominal quota is 1250m CPU / 1280Mi memory.
