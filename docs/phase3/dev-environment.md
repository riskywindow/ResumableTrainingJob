# Phase 3 Dev Environment

## Goal

Phase 3 extends the Phase 2 local `kind` stack to support heterogeneous
resource pools and flavor-aware admission. The dev environment simulates
two node pools (on-demand and spot) using kind worker node labels and taints,
and configures Kueue with two ResourceFlavors and a multi-flavor ClusterQueue.

Two profiles are available:

| Profile | Default | What It Exercises |
| --- | --- | --- |
| `flavors` | **Yes** | G1 (admission-aware launch), G2 (flavor-aware rendering), G3 (world-size-flexible resume) |
| `experimental` | No | All of `flavors` plus G4 (partial admission for RTJ) |

## Base Stack (Unchanged)

The Phase 3 dev environment reuses the Phase 2 base stack:

- `kindest/node:v1.31.2`
- Kueue `v0.15.1`
- JobSet `v0.10.1`
- `checkpoint-dev` namespace
- MinIO development object store
- `phase1-dev` and `phase1-high` WorkloadPriorityClasses

The only structural change is the kind cluster config: Phase 3 uses 4 worker
nodes instead of 1 to simulate heterogeneous pools.

## Cluster Topology

```
┌───────────────────┐
│  control-plane    │
└───────────────────┘

┌───────────────────┐  ┌───────────────────┐
│  worker           │  │  worker2          │
│  pool: on-demand  │  │  pool: on-demand  │
│  (no taint)       │  │  (no taint)       │
└───────────────────┘  └───────────────────┘

┌───────────────────┐  ┌───────────────────┐
│  worker3          │  │  worker4          │
│  pool: spot       │  │  pool: spot       │
│  taint: NoSchedule│  │  taint: NoSchedule│
└───────────────────┘  └───────────────────┘
```

### Node Labels

| Label | Values | Purpose |
| --- | --- | --- |
| `checkpoint-native.dev/pool` | `on-demand`, `spot` | Identifies the resource pool |
| `checkpoint-native.dev/phase3` | `true` | Marks Phase 3-labeled nodes |

### Node Taints

| Node Pool | Taint | Effect |
| --- | --- | --- |
| on-demand | (none) | Pods schedule freely |
| spot | `checkpoint-native.dev/spot=true` | `NoSchedule` — only pods with matching toleration can schedule |

## ResourceFlavors

Two ResourceFlavors select nodes by pool label:

### `on-demand`

```yaml
spec:
  nodeLabels:
    checkpoint-native.dev/pool: "on-demand"
```

Pods admitted with this flavor get `nodeSelector: {checkpoint-native.dev/pool: on-demand}`
injected by Kueue. They land on the untainted on-demand nodes.

### `spot`

```yaml
spec:
  nodeLabels:
    checkpoint-native.dev/pool: "spot"
  tolerations:
    - key: checkpoint-native.dev/spot
      value: "true"
      effect: NoSchedule
      operator: Equal
```

Pods admitted with this flavor get both the `nodeSelector` and the toleration
injected by Kueue, allowing them to schedule on the tainted spot nodes.

## Queue Profile

The Phase 3 ClusterQueue (`phase3-cq`) has both flavors:

```yaml
spec:
  resourceGroups:
    - coveredResources: [cpu, memory]
      flavors:
        - name: on-demand
          resources:
            - name: cpu
              nominalQuota: 2
            - name: memory
              nominalQuota: 2Gi
        - name: spot
          resources:
            - name: cpu
              nominalQuota: 2
            - name: memory
              nominalQuota: 2Gi
```

Kueue tries flavors in order: `on-demand` first, then `spot`. When on-demand
quota is exhausted, workloads spill to spot. This ordering enables testing of
flavor-aware launch and re-launch after preemption to a different flavor.

The Phase 3 LocalQueue is `phase3-training` in namespace `checkpoint-dev`.

The Phase 2 queue (`checkpoint-dev-cq` / `training`) remains available for
backward-compatible testing.

## Profiles

### `flavors` (Default)

The `flavors` profile sets up:

- 4 worker nodes with pool labels/taints
- `on-demand` and `spot` ResourceFlavors
- `phase3-cq` ClusterQueue with both flavors
- `phase3-training` LocalQueue
- Kueue config identical to Phase 2 (no special feature gates)

This profile exercises:

- **G1:** Admission-aware launch — nodeSelector/tolerations from the admitted
  ResourceFlavor are materialized into the child JobSet.
- **G2:** Flavor-aware rendering — replica counts match the admitted counts.
- **G3:** World-size-flexible resume — RTJs with `allowWorldSizeChange: true`
  can resume from checkpoints at a different world size.

### `experimental`

The `experimental` profile adds partial-admission support:

- Everything in `flavors`, plus:
- Kueue config with documentation for the PartialAdmission feature gate
  (Beta, default-on in v0.15.1 — no actual config change needed).

To fully activate partial admission (G4), the operator must also be started
with `--enable-experimental-partial-admission`, and each RTJ must set
`spec.parallelism.enablePartialAdmission: true`.

## Quick Start

### Full Setup (From Scratch)

```bash
# Default flavors profile:
make phase3-up

# Or experimental profile:
make phase3-up PHASE3_PROFILE=experimental
```

This creates the kind cluster, installs Kueue/JobSet/MinIO, labels nodes,
applies flavors and queues, and patches the Kueue config.

### Verify

```bash
make phase3-status
make phase3-smoke
```

### Switch Profile

```bash
# Switch an existing cluster to the experimental profile:
make phase3-profile PHASE3_PROFILE=experimental
```

### Load Images

```bash
make phase3-load-images IMAGES="phase1-ddp-counter:dev controller:latest"
```

### Tear Down

```bash
make phase3-down
```

## Makefile Targets

| Target | Description |
| --- | --- |
| `make phase3-up` | Create cluster + install stack + apply Phase 3 profile |
| `make phase3-down` | Delete the kind cluster |
| `make phase3-status` | Show cluster, node pools, flavors, queues |
| `make phase3-load-images IMAGES=...` | Load images into the kind cluster |
| `make phase3-smoke` | Run Phase 3 infrastructure smoke test |
| `make phase3-profile PHASE3_PROFILE=...` | Apply/switch profile on existing cluster |

## Sample Manifests

| File | Description |
| --- | --- |
| `deploy/dev/samples/phase3/rtj-fixed-size.yaml` | Fixed-size RTJ on multi-flavor queue (G1, G2) |
| `deploy/dev/samples/phase3/rtj-flexible-size.yaml` | Flexible-size RTJ with DCP resharding (G3) |
| `deploy/dev/samples/phase3/rtj-partial-admission.yaml` | Partial-admission RTJ (G4, experimental) |
| `deploy/dev/samples/phase3/jobset-flavor-smoke.yaml` | Standalone JobSet for infrastructure validation |

Sample RTJ manifests use placeholders (`__RTJ_NAME__`, `__TRAINER_IMAGE__`,
`__DEV_NAMESPACE__`) that can be rendered with `sed` or the `render_phase3_manifest`
helper.

## File Layout

```
hack/dev/
  kind-config-phase3.yaml          # 4-worker kind config
  label-kind-nodes.sh              # Labels/taints nodes for pools
  phase3-profile.sh                # Applies Phase 3 profile
  phase3-smoke.sh                  # Infrastructure smoke test

deploy/dev/
  flavors/
    00-on-demand.yaml              # on-demand ResourceFlavor
    01-spot.yaml                   # spot ResourceFlavor
  queues/phase3/
    10-cluster-queue.yaml          # Multi-flavor ClusterQueue
    20-local-queue.yaml            # Phase 3 LocalQueue
  kueue/
    controller_manager_config.phase3-flavors.yaml
    controller_manager_config.phase3-experimental-partial-admission.yaml
  namespaces/
    01-checkpoint-dev-phase3.yaml  # Phase 3 namespace labels
  samples/phase3/
    rtj-fixed-size.yaml
    rtj-flexible-size.yaml
    rtj-partial-admission.yaml
    jobset-flavor-smoke.yaml
```

## Relationship to Phase 2

The Phase 3 dev environment is additive. Phase 2 resources remain available:

- `checkpoint-dev-cq` and `training` LocalQueue still work.
- `default-flavor` ResourceFlavor still exists.
- Phase 2 Makefile targets (`dev-up`, `phase2-smoke`, etc.) still work.
- The Phase 3 profile adds new resources without modifying Phase 2 ones.

The only exception is the kind cluster config: `phase3-up` uses a 4-worker
config. If you previously ran `dev-up` (1 worker), you need to `dev-down`
first and then `phase3-up`.

## What the Experimental Profile Enables

| Feature | `flavors` Profile | `experimental` Profile |
| --- | --- | --- |
| Flavor-aware launch (G1, G2) | Yes | Yes |
| World-size-flexible resume (G3) | Yes | Yes |
| Partial admission (G4) | No | Yes (requires operator flag + per-job opt-in) |
| `PodSet.MinCount` synthesis | No | Yes |
| Kueue PartialAdmission gate | N/A (default-on in v0.15.1) | N/A (default-on in v0.15.1) |

## Troubleshooting

### "Phase 3 requires at least 4 worker nodes"

You created the cluster with the Phase 2 kind config (1 worker). Run
`make phase3-down && make phase3-up` to recreate with the 4-worker config.

### Pods stuck Pending on spot nodes

Check that the spot toleration is correctly applied:

```bash
kubectl get nodes -o json | jq '.items[].spec.taints'
kubectl describe pod <pod-name> -n checkpoint-dev
```

### Kueue not admitting through Phase 3 queue

Verify the ClusterQueue and LocalQueue exist and are active:

```bash
kubectl get clusterqueues.kueue.x-k8s.io phase3-cq -o yaml
kubectl get localqueues.kueue.x-k8s.io -n checkpoint-dev phase3-training -o yaml
```

Check Kueue logs for admission decisions:

```bash
kubectl logs -n kueue-system deployment/kueue-controller-manager --tail=50
```
