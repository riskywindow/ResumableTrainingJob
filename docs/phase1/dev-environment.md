# Phase 1 Dev Environment

## Goal

Phase 1 uses a reproducible, CPU-friendly local environment built on `kind`.
The environment is designed to validate the control-plane slice before the RTJ operator creates child `JobSet` objects.

The stack includes:

- `kind` for the local Kubernetes cluster
- Kueue for queueing and admission
- JobSet for distributed workload orchestration
- MinIO as the local S3-compatible object store
- a standalone `JobSet` smoke workload to validate Kueue plus JobSet wiring

## Pinned Versions

The dev scripts pin these versions by default:

| Component | Pinned value | Notes |
| --- | --- | --- |
| `kind` node image | `kindest/node:v1.31.2` | CPU-friendly local cluster image used by `hack/dev/create-kind-cluster.sh`. |
| Kueue | `v0.15.1` | Installed from the upstream release manifest URL. |
| JobSet | `v0.10.1` | Installed from the upstream release manifest URL. |
| MinIO server image | `quay.io/minio/minio:RELEASE.2025-06-13T11-33-47Z` | Used for a single-node dev-only object store deployment. |
| MinIO client image | `minio/mc:RELEASE.2025-07-21T05-28-08Z` | Used only to create the development bucket. |

These values can be overridden with environment variables, but the defaults are pinned for reproducibility.

## Source References

- Kueue installation and release manifest version: <https://github.com/kubernetes-sigs/kueue>
- Kueue queue and workload priority concepts: <https://kueue.sigs.k8s.io/docs/tasks/manage/administer_cluster_quotas/> and <https://kueue.sigs.k8s.io/docs/concepts/workload_priority_class/>
- Kueue JobSet usage example: <https://kueue.sigs.k8s.io/docs/tasks/run/jobsets/>
- JobSet installation and release manifest version: <https://github.com/kubernetes-sigs/jobset>
- MinIO server release tags: <https://github.com/minio/minio/releases>
- MinIO client release tags: <https://github.com/minio/mc/releases>
- MinIO container deployment guidance: <https://min.io/docs/minio/container/operations/install-deploy-manage/deploy-minio-single-node-single-drive.html>

## Namespaces And Core Objects

The local environment uses the `checkpoint-dev` namespace for Phase 1 development resources.

The queueing setup is:

- one `ResourceFlavor`: `default-flavor`
- one `ClusterQueue`: `checkpoint-dev-cq`
- one `LocalQueue`: `training`
- two `WorkloadPriorityClass` objects:
  - `phase1-dev`
  - `phase1-high`

The MinIO deployment also runs in `checkpoint-dev`.

## Secrets

The scripts create these concrete secrets in `checkpoint-dev`:

- `minio-root-credentials`
- `checkpoint-storage-credentials`

The repository includes example shapes under [deploy/dev/secrets](/Users/rishivinodkumar/Daedelus/deploy/dev/secrets).

By default, the scripts use simple local credentials suitable for disposable development only.
Override them by exporting:

- `MINIO_ROOT_USER`
- `MINIO_ROOT_PASSWORD`
- `MINIO_BUCKET`
- `MINIO_REGION`

## Scripts

| Script | Purpose |
| --- | --- |
| `hack/dev/create-kind-cluster.sh` | Create the local `kind` cluster if it does not already exist. |
| `hack/dev/delete-kind-cluster.sh` | Delete the local `kind` cluster. |
| `hack/dev/install-kueue.sh` | Install Kueue from the pinned upstream release manifest. |
| `hack/dev/install-jobset.sh` | Install JobSet from the pinned upstream release manifest. |
| `hack/dev/install-minio.sh` | Create MinIO secrets, deploy MinIO, and wait for readiness. |
| `hack/dev/bootstrap-bucket.sh` | Create the dev checkpoint bucket using the pinned `mc` image. |
| `hack/dev/status.sh` | Print a compact environment status report. |
| `hack/dev/smoke.sh` | Submit the standalone `JobSet` smoke manifest and wait for pods to become ready. |

All scripts are intended to be idempotent.
Re-running them should reconcile to the same state rather than requiring manual cleanup.

## Make Targets

The `Makefile` exposes the main workflow:

- `make dev-up`
- `make dev-down`
- `make dev-status`
- `make dev-smoke`
- `make load-images`

The default sequence for a new environment is:

```bash
make dev-up
make dev-status
make dev-smoke
```

## Smoke Workflow

The smoke test uses [standalone-jobset-smoke.yaml](/Users/rishivinodkumar/Daedelus/deploy/dev/samples/standalone-jobset-smoke.yaml).

That manifest is intentionally independent of the RTJ operator.
It proves the environment can:

1. submit a `JobSet`
2. have Kueue observe and admit it through the queue label
3. let JobSet create the child `Job` resources and Pods
4. reach running Pods on a CPU-only local cluster

This gives a reliable pre-RTJ checkpoint for validating Kueue plus JobSet wiring.

## Local Development Caveats

- The MinIO deployment uses a single-node `emptyDir` configuration for simplicity, not durability.
- The default setup is intentionally CPU-only and does not require GPUs or `NCCL`.
- The environment is meant for correctness and wiring validation, not performance measurement.
