# Phase 9 — Dev Environment

## Quick start

```bash
# Full environment from scratch
make phase9-up

# Or layer on existing cluster
make phase9-profile

# Verify
make phase9-smoke

# Tear down
make phase9-down
```

## What the profile installs

| Component | Purpose |
|---|---|
| RTJ CRDs | ResumableTrainingJob with `spec.elasticity` and `status.elasticity` |
| ResourceFlavor `phase9-flavor` | Default (no node affinity) |
| ClusterQueue `phase9-cq` | 1250m CPU / 1280Mi memory with LowerPriority preemption |
| LocalQueue `phase9-training` | Routes to `phase9-cq` in `checkpoint-dev` namespace |
| Kueue config | RTJ external framework, waitForPodsReady, no DRA/provisioning |

No special Kueue feature gates are required.  The elastic resize path uses
standard Kubernetes APIs (server-side apply for Workload status patching).

## Queue / quota design

The Phase 9 queue is sized for the **dynamic quota reclaim** demonstration:

```
ClusterQueue phase9-cq
  nominalQuota:
    cpu:    1250m
    memory: 1280Mi
  preemption:
    withinClusterQueue: LowerPriority
    reclaimWithinCohort: Never
```

### Why 1250m CPU?

Each worker pod requests 250m CPU.  The quota is deliberately undersized so
that two full-size RTJs cannot coexist:

| Scenario | Workers | CPU used | Fits? |
|---|---|---|---|
| RTJ A at 4 workers | 4 | 1000m | Yes (250m spare) |
| RTJ A (4) + RTJ B (2) | 6 | 1500m | **No** (exceeds 1250m) |
| RTJ A shrinks to 2 | 2 | 500m | Yes (750m spare) |
| RTJ A (2) + RTJ B (2) | 4 | 1000m | **Yes** (250m spare) |

This forces the dynamic reclaim flow:

1. RTJ A runs at 4 workers, consuming 1000m.
2. RTJ B (2 workers, 500m) is queued — insufficient quota.
3. User patches `spec.elasticity.targetWorkerCount=2` on RTJ A.
4. Controller publishes `reclaimablePods` for 2 surplus workers.
5. Kueue reads `reclaimablePods` → releases 500m CPU.
6. RTJ B is admitted with 500m.

### No cohort required

The Phase 9 profile uses a single ClusterQueue with no cohort.
`reclaimablePods` is a per-Workload field — Kueue adjusts the Workload's
resource usage within the same queue, making room for other Workloads.

## reclaimablePods SSA strategy

The RTJ controller writes `Workload.status.reclaimablePods` using
**server-side apply** (SSA) with a dedicated field manager:

```
Field manager: rtj-elastic-reclaim
Patched field: status.reclaimablePods[name=<podSetName>]
Strategy:      Apply (SSA), not merge-patch
```

### Why SSA?

SSA eliminates read-modify-write races with Kueue's concurrent status
writes.  `reclaimablePods` has `+listType=map` with `+listMapKey=name`,
so SSA treats each PodSet entry as independently owned.  The RTJ controller
owns only the `reclaimablePods` entries; Kueue owns `admission`, `conditions`,
`admissionChecks`, and `requeueState`.

### Why no Kueue configuration is needed

The `reclaimablePods` field is part of the Kueue Workload Status API
(kueue.x-k8s.io/v1beta1).  Kueue reads it unconditionally — there is no
feature gate to enable.  SSA field ownership is a standard Kubernetes API
server feature (GA since v1.22).  The only requirement is Kueue v0.15.1+
(already the baseline for this project).

## Sample manifests

Three sample RTJs demonstrate the elastic resize capabilities:

### rtj-elastic-shrink.yaml

- **Workers**: 4 (initial), shrink target configurable
- **Elasticity**: `mode: Manual`, `inPlaceShrinkPolicy: IfSupported`
- **minCount**: 1, **preferredCount**: 4
- **DDP fixture**: `SUPPORTS_IN_PLACE_SHRINK=false` (DDP requires relaunch)
- **Scenario**: Patch `targetWorkerCount=2` → checkpoint-and-relaunch at 2

```bash
# Apply the sample
sed -e 's|__RTJ_NAME__|elastic-a|g' \
    -e 's|__TRAINER_IMAGE__|your-image:tag|g' \
    -e 's|__DEV_NAMESPACE__|checkpoint-dev|g' \
    deploy/dev/phase9/samples/rtj-elastic-shrink.yaml | kubectl apply -f -

# Trigger shrink
kubectl patch rtj elastic-a -n checkpoint-dev --type=merge \
  -p '{"spec":{"elasticity":{"targetWorkerCount":2}}}'
```

### rtj-elastic-grow.yaml

- **Workers**: 2 (initial), grow target configurable
- **Elasticity**: `mode: Manual`
- **Scenario**: Patch `targetWorkerCount=4` → checkpoint-and-relaunch at 4

Grow always uses checkpoint-and-relaunch (in-place grow requires upstream
Kueue Workload resize support, which is not yet available).

### rtj-non-elastic.yaml

- **Workers**: 2 (fixed)
- **No elasticity section** — Phase 8 behavior preserved
- **Scenario**: Standard pause/resume/preemption, no resize

## Runtime fixture knobs

The elastic samples inject these environment variables for the DDP fixture:

| Variable | Value | Purpose |
|---|---|---|
| `YIELD_SDK_ELASTICITY_MODE` | `Manual` | Enables resize detection |
| `YIELD_SDK_SUPPORTS_IN_PLACE_SHRINK` | `false` | DDP requires relaunch |
| `YIELD_SDK_RESIZE_SIGNAL_DIR` | `/var/run/yield-sdk/resize` | Signal file directory |
| `SLEEP_PER_STEP` | `2` | Fast step iteration for demos |
| `CHECKPOINT_EVERY` | `3` | Frequent checkpoints |
| `TOTAL_STEPS` | `200` | Long enough for manual resize |

The controller also injects `YIELD_SDK_TARGET_WORKER_COUNT` at render time
when the elastic target differs from the current worker count.

A resize-signal emptyDir volume is mounted at `/var/run/yield-sdk/resize`
for the fixture to write resize signal files that the controller can read.

## Smoke test coverage

`make phase9-smoke` validates:

| Check | What it verifies |
|---|---|
| CRD with elasticity fields | RTJ CRD includes `spec.elasticity`, `targetWorkerCount` |
| Kueue RTJ framework | External framework registration in Kueue config |
| manageJobsWithoutQueueName | Set to `false` (required for RTJ) |
| waitForPodsReady | Enabled (required for resize-triggered relaunch failure detection) |
| ClusterQueue phase9-cq | Exists with cpu + memory resources |
| LocalQueue phase9-training | Exists, points to phase9-cq |
| CPU quota | 1250m (correct for dynamic reclaim) |
| Memory quota | 1280Mi |
| Preemption policy | LowerPriority within queue |
| Elastic shrink dry-run | Sample manifest validates against API server |
| Elastic grow dry-run | Sample manifest validates against API server |
| Non-elastic dry-run | Sample manifest validates against API server |
| Fixture knobs | ELASTICITY_MODE, SUPPORTS_IN_PLACE_SHRINK, RESIZE_SIGNAL_DIR |
| resize-signal volume | Present in elastic samples |
| Non-elastic omission | Non-elastic sample correctly omits elastic env vars |
| reclaimablePods schema | Workload CRD includes reclaimablePods in status |
| Workload API | kueue.x-k8s.io Workload API is available |

## Backward compatibility

The Phase 9 profile preserves all Phase 8 and earlier behaviors:

- RTJs without `spec.elasticity` run identically to Phase 8.
- The Phase 9 Kueue config is a superset of the Phase 2 baseline.
- Earlier phase profiles (Phase 3-8) are not modified.
- The Phase 9 queue (`phase9-cq`) is separate from earlier phase queues.

## Makefile targets

| Target | Description |
|---|---|
| `make phase9-up` | Full environment: kind cluster + base stack + Phase 9 profile |
| `make phase9-down` | Delete the kind cluster |
| `make phase9-status` | Show queues, quota, workloads, RTJs |
| `make phase9-load-images` | Load Docker images into kind |
| `make phase9-smoke` | Infrastructure validation (17+ checks) |
| `make phase9-profile` | Apply/re-apply Phase 9 profile on existing cluster |
