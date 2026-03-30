# Phase 6 Dev Environment

## Overview

Phase 6 uses a three-cluster local development environment to exercise
MultiKueue dispatch of ResumableTrainingJob (RTJ) across clusters.

| Cluster | Kind Name | Role | Workloads? |
|---------|-----------|------|------------|
| Manager | `phase6-manager` | Control-plane only. Routes RTJs to workers via MultiKueue. | No |
| Worker 1 | `phase6-worker-1` | Runs RTJ workloads. Hosts the shared MinIO checkpoint store. | Yes |
| Worker 2 | `phase6-worker-2` | Runs RTJ workloads. | Yes |

All three clusters run on the same Docker network (kind's default),
enabling cross-cluster API server access via container-internal IPs.

## Quick Start

```bash
# Create the full environment (clusters, Kueue, MultiKueue, CRDs, shared store).
make phase6-up

# Verify everything is working.
make phase6-smoke

# Show cluster status.
make phase6-status

# Tear down everything.
make phase6-down
```

## Architecture

```
                    ┌──────────────────────┐
                    │   phase6-manager     │
                    │  (control-plane)     │
                    │                      │
                    │  Kueue + MultiKueue  │
                    │  RTJ CRD             │
                    │  Operator --mode=mgr │
                    │                      │
                    │  ClusterQueue        │
                    │   └─ multikueue AC   │
                    │  LocalQueue          │
                    │   └─ phase6-training │
                    └────────┬─────────────┘
                             │
              ┌──────────────┴──────────────┐
              │                             │
    ┌─────────▼──────────┐       ┌──────────▼─────────┐
    │  phase6-worker-1   │       │  phase6-worker-2   │
    │  (CP + 1 worker)   │       │  (CP + 1 worker)   │
    │                    │       │                    │
    │  Kueue (standard)  │       │  Kueue (standard)  │
    │  RTJ CRD           │       │  RTJ CRD           │
    │  Operator --mode=w │       │  Operator --mode=w │
    │  MinIO (shared)    │       │                    │
    │                    │       │                    │
    │  ClusterQueue      │       │  ClusterQueue      │
    │   └─ real quotas   │       │   └─ real quotas   │
    │  LocalQueue        │       │  LocalQueue        │
    │   └─ phase6-train. │       │   └─ phase6-train. │
    └────────────────────┘       └────────────────────┘
```

## Shared Checkpoint Store

MinIO runs on worker-1 and is exposed via a NodePort service (port 30900).
All clusters access MinIO via the Docker-internal IP of worker-1's
control-plane container:

```
http://<worker-1-internal-ip>:30900
```

The install script (`install-phase6-shared-store.sh`) automatically:
1. Deploys MinIO on worker-1
2. Creates the checkpoint bucket
3. Resolves the internal IP
4. Distributes credentials Secrets to all three clusters
5. Creates a ConfigMap on the manager recording the endpoint

This ensures the shared store meets the Phase 6 requirement: all
clusters can read and write checkpoints from the same S3-compatible
endpoint with the same credentials.

## MultiKueue Configuration

The manager cluster is configured with:

1. **Kueue Configuration** with RTJ external framework:
   - `integrations.externalFrameworks: [ResumableTrainingJob.v1alpha1.training.checkpoint.example.io]`
   - Feature gates `MultiKueue` and `MultiKueueAdaptersForCustomJobs` are
     Beta and default-on in Kueue v0.15.1.

2. **AdmissionCheck** `multikueue` with controller `kueue.x-k8s.io/multikueue`.

3. **MultiKueueConfig** listing `worker-1` and `worker-2`.

4. **MultiKueueCluster** resources (one per worker) referencing kubeconfig
   Secrets generated from kind's internal Docker network addresses.

5. **ClusterQueue** `phase6-multikueue-cq` with the `multikueue`
   AdmissionCheck (routing layer, generous quotas).

6. **RBAC** for Kueue to manage RTJ on the manager cluster.

Worker clusters have standard Kueue installations with:
- Real resource quotas (500m CPU, 512Mi memory)
- LowerPriority preemption
- Matching namespace and LocalQueue names

## Kubeconfig Management

Kind clusters expose their API servers on localhost with random ports.
For cross-cluster access, the install script:

1. Extracts each worker's kubeconfig from kind
2. Resolves the worker container's Docker-internal IP
3. Rewrites the server URL to `https://<internal-ip>:6443`
4. Creates a Secret in the manager's `kueue-system` namespace

This avoids needing host-level port mappings or external DNS.

## Relationship to Single-Cluster Dev Path

Phase 6 uses **completely separate kind cluster names**:
- `phase6-manager` (not `checkpoint-phase1`)
- `phase6-worker-1`
- `phase6-worker-2`

The existing single-cluster dev path (`make dev-up`, `make phase5-up`,
etc.) is unaffected. Both environments can coexist simultaneously.

## Scripts

| Script | Purpose |
|--------|---------|
| `hack/dev/create-phase6-kind-clusters.sh` | Create 3 kind clusters |
| `hack/dev/delete-phase6-kind-clusters.sh` | Delete 3 kind clusters |
| `hack/dev/install-phase6-kueue.sh` | Install Kueue on all clusters |
| `hack/dev/install-phase6-multikueue.sh` | Configure MultiKueue + JobSet + queues |
| `hack/dev/install-phase6-operator.sh` | Install RTJ CRDs on all clusters |
| `hack/dev/install-phase6-shared-store.sh` | Deploy shared MinIO + distribute creds |
| `hack/dev/phase6-smoke.sh` | Infrastructure smoke test (15 checks) |

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make phase6-up` | Full environment setup |
| `make phase6-down` | Tear down all clusters |
| `make phase6-status` | Show multi-cluster state |
| `make phase6-load-images` | Load images into all 3 clusters |
| `make phase6-smoke` | Run infrastructure smoke test |

## Deploy Manifests

| Path | Description |
|------|-------------|
| `deploy/dev/phase6/manager/00-namespace.yaml` | Manager namespace |
| `deploy/dev/phase6/manager/10-cluster-queue.yaml` | Manager ClusterQueue (MultiKueue) |
| `deploy/dev/phase6/manager/20-local-queue.yaml` | Manager LocalQueue |
| `deploy/dev/phase6/workers/00-namespace.yaml` | Worker namespace |
| `deploy/dev/phase6/workers/10-cluster-queue.yaml` | Worker ClusterQueue (real quotas) |
| `deploy/dev/phase6/workers/20-local-queue.yaml` | Worker LocalQueue |
| `deploy/dev/phase6/shared-store/minio-deployment.yaml` | MinIO Deployment |
| `deploy/dev/phase6/shared-store/minio-nodeport-service.yaml` | MinIO NodePort Service |
| `deploy/dev/phase6/shared-store/checkpoint-credentials-template.yaml` | Credentials template |

## Sample Manifests

| Path | Description |
|------|-------------|
| `deploy/dev/phase6/samples/rtj-multikueue-dispatch.yaml` | RTJ for MultiKueue dispatch |
| `deploy/dev/phase6/samples/worker-queue-setup.yaml` | Self-contained worker setup |
| `deploy/dev/phase6/samples/shared-checkpoint-store-config.yaml` | Shared store config example |

## Smoke Test Checks

The `phase6-smoke` test validates:

1. Manager cluster exists and is reachable
2. Worker-1 cluster exists and is reachable
3. Worker-2 cluster exists and is reachable
4. Kueue running on all clusters
5. MultiKueue AdmissionCheck on manager
6. MultiKueueConfig on manager
7. MultiKueueCluster resources on manager
8. Manager ClusterQueue has MultiKueue admission check
9. Manager LocalQueue exists
10. Worker ClusterQueues exist with real quotas
11. Worker LocalQueues exist with matching names
12. RTJ CRD installed on all clusters
13. Shared checkpoint store reachable from manager
14. Checkpoint credentials on all clusters
15. Sample RTJ validates (dry-run)

## Pinned Versions

| Component | Version |
|-----------|---------|
| Kueue | v0.15.1 |
| JobSet | v0.10.1 |
| Kind node image | kindest/node:v1.31.2 |
| MinIO | RELEASE.2025-06-13T11-33-47Z |
| MinIO MC | RELEASE.2025-07-21T05-28-08Z |
