# Phase 8 Dev Environment

## Overview

The Phase 8 dev environment adds DRA (Dynamic Resource Allocation) support
to the local kind-based development stack. It provides:

- **Example DRA driver**: A DaemonSet that publishes ResourceSlice objects
  with simulated GPU devices. No real accelerators required.
- **DeviceClass**: `example-gpu` -- matches all devices from the example driver.
- **Kueue deviceClassMappings**: Maps `example-gpu` DeviceClass to a logical
  `example.dev/gpu` resource name for ClusterQueue quota accounting.
- **DRA-aware queues**: ClusterQueue with `example.dev/gpu` quota, LocalQueue
  for DRA-backed training workloads.
- **Sample RTJs**: DRA-backed launch, pause/resume, and incompatible profile
  rejection scenarios.

## Prerequisites

- Docker
- kind
- kubectl
- **Kubernetes v1.33+** (required for stable DRA support)

The `make phase8-up` target automatically uses `kindest/node:v1.33.0` to
ensure DRA API availability. Override with `PHASE8_KIND_NODE_IMAGE`.

## Quick start

```bash
# Full environment from scratch:
make phase8-up

# Verify infrastructure:
make phase8-smoke

# Check status:
make phase8-status

# Tear down:
make phase8-down
```

## Architecture

```
kind cluster (k8s v1.33+)
├── kueue-system/
│   └── kueue-controller-manager
│       └── config: deviceClassMappings[example-gpu → example.dev/gpu]
├── dra-example-driver/
│   └── DaemonSet: dra-example-driver
│       └── publishes ResourceSlice per node (4 simulated GPUs each)
├── (cluster-scoped)
│   ├── DeviceClass: example-gpu
│   ├── ResourceFlavor: phase8-flavor
│   ├── ClusterQueue: phase8-cq (cpu + memory + example.dev/gpu quota)
│   └── ResourceSlice: dra-example-<node> (per worker node)
└── checkpoint-dev/
    ├── LocalQueue: phase8-training → phase8-cq
    ├── RTJ resources (when submitted)
    ├── ResourceClaimTemplates (operator-created)
    └── MinIO (checkpoint store, from base stack)
```

## DRA driver details

The example DRA driver is a minimal DaemonSet that:

1. Reads the node name via the downward API.
2. Publishes a `ResourceSlice` with 4 simulated devices per node.
3. Renews the ResourceSlice every 30 seconds (keep-alive).
4. Cleans up the ResourceSlice on pod termination (SIGTERM handler).

Each simulated device has attributes:
- `model`: `Example-GPU-v1` (string)
- `memory`: `80Gi` (quantity)
- `index`: 0-3 (int)

The driver name is `dra.example.dev` and the pool name matches the node name.

### Why not the upstream dra-example-driver?

The upstream `kubernetes-sigs/dra-example-driver` requires building a Go
binary, a custom container image, and kubelet plugin socket configuration.
Our self-contained approach uses `bitnami/kubectl` with a shell script that
publishes ResourceSlice objects directly via the API server. This trades
kubelet-level device allocation for simpler setup -- devices appear in
ResourceSlice but allocation is simulated at the API level.

For local dev/test of the RTJ operator's DRA integration (template lifecycle,
Kueue accounting, child JobSet rendering, checkpoint compatibility), this
level of simulation is sufficient. Full device allocation testing requires
the upstream driver or real hardware.

## Kueue configuration

The Phase 8 Kueue config extends Phase 7 with:

```yaml
featureGates:
  DynamicResourceAllocation: true  # Enable DRA support in Kueue

resources:
  deviceClassMappings:
    - deviceClassName: example-gpu     # DRA DeviceClass name
      resourceName: example.dev/gpu    # Logical resource for quota
```

This tells Kueue to resolve DRA pod claims referencing the `example-gpu`
DeviceClass into `example.dev/gpu` resource requests, which are then
accounted against the ClusterQueue's `example.dev/gpu` nominalQuota.

## Queue configuration

```yaml
ClusterQueue: phase8-cq
  resourceGroups:
    - coveredResources: [cpu, memory, example.dev/gpu]
      flavors:
        - name: phase8-flavor
          resources:
            - name: example.dev/gpu
              nominalQuota: 8  # 2 nodes × 4 devices each
```

## Sample RTJs

| Sample | File | Description |
|---|---|---|
| DRA launch | `rtj-dra-launch.yaml` | 2 GPUs per worker, successful admission |
| Pause/resume | `rtj-dra-pause-resume.yaml` | Same device profile, compatible resume |
| Incompatible | `rtj-dra-incompatible-profile.yaml` | Different DeviceClass, fail-closed rejection |

All samples use `__RTJ_NAME__`, `__TRAINER_IMAGE__`, and `__DEV_NAMESPACE__`
placeholders that are substituted at apply time.

## Smoke test

`make phase8-smoke` validates 11 checks:

1. DRA APIs available (ResourceSlice, DeviceClass, ResourceClaim, ResourceClaimTemplate)
2. Example DRA driver DaemonSet running
3. DeviceClass `example-gpu` exists with correct driver selector
4. ResourceSlice objects published with simulated devices
5. Kueue config has `deviceClassMappings`
6. Kueue config has `DynamicResourceAllocation` feature gate
7. ClusterQueue `phase8-cq` covers `example.dev/gpu`
8. LocalQueue `phase8-training` points to `phase8-cq`
9. Kueue config has RTJ external framework registration
10. ResumableTrainingJob CRD installed
11. Sample RTJ manifests pass server-side dry-run

## Files

### Deploy manifests

| Path | Purpose |
|---|---|
| `deploy/dev/phase8/dra-driver/00-namespace.yaml` | DRA driver namespace |
| `deploy/dev/phase8/dra-driver/05-device-class.yaml` | DeviceClass for simulated GPUs |
| `deploy/dev/phase8/dra-driver/10-service-account.yaml` | Driver ServiceAccount |
| `deploy/dev/phase8/dra-driver/15-rbac.yaml` | Driver RBAC (ResourceSlice CRUD) |
| `deploy/dev/phase8/dra-driver/20-daemonset.yaml` | Driver DaemonSet |
| `deploy/dev/phase8/kueue/controller_manager_config.phase8.yaml` | Kueue config with deviceClassMappings |
| `deploy/dev/phase8/queues/00-resource-flavor.yaml` | ResourceFlavor |
| `deploy/dev/phase8/queues/10-cluster-queue.yaml` | ClusterQueue with DRA quota |
| `deploy/dev/phase8/queues/20-local-queue.yaml` | LocalQueue |
| `deploy/dev/phase8/samples/rtj-dra-launch.yaml` | Sample: DRA launch |
| `deploy/dev/phase8/samples/rtj-dra-pause-resume.yaml` | Sample: pause/resume |
| `deploy/dev/phase8/samples/rtj-dra-incompatible-profile.yaml` | Sample: incompatible profile |

### Scripts

| Path | Purpose |
|---|---|
| `hack/dev/install-phase8-profile.sh` | Full Phase 8 profile installer |
| `hack/dev/phase8-profile.sh` | Thin wrapper for re-apply |
| `hack/dev/install-phase8-dra-driver.sh` | DRA driver installer |
| `hack/dev/phase8-smoke.sh` | Infrastructure smoke test |

### Makefile targets

| Target | Description |
|---|---|
| `make phase8-up` | Full environment setup |
| `make phase8-down` | Tear down |
| `make phase8-status` | Show DRA and queue state |
| `make phase8-load-images` | Load images into kind |
| `make phase8-smoke` | Infrastructure validation |
| `make phase8-profile` | Re-apply profile |

## Kubernetes version

Phase 8 requires Kubernetes v1.33+ where DRA is stable. The following
DRA APIs must be available:

- `resource.k8s.io/v1beta1` (or later): ResourceSlice, DeviceClass,
  ResourceClaim, ResourceClaimTemplate

The `make phase8-up` target uses `kindest/node:v1.33.0` by default.
Override with:

```bash
make phase8-up PHASE8_KIND_NODE_IMAGE=kindest/node:v1.34.0
```

The `install-phase8-profile.sh` script warns if the detected Kubernetes
version is below v1.33 but does not block installation (to support
testing with beta DRA APIs on older versions).

## Relationship to other phases

- **Phase 7**: Phase 8 carries forward `waitForPodsReady` and
  `ProvisioningACC` from Phase 7. When `spec.devices` is nil or
  `mode=Disabled`, the operator follows the Phase 7 path unchanged.
- **Base dev stack**: Phase 8 reuses `dev-up.sh` for kind cluster creation,
  Kueue, JobSet, MinIO, and namespace setup. Only the kind node image
  is bumped to v1.33+ for DRA support.
- **Phase 6**: Phase 8 uses the single-cluster dev path. Multi-cluster
  DRA testing is out of scope.
