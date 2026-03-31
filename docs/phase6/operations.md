# Phase 6 Operations Guide

Operational procedures for inspecting and managing MultiKueue RTJ
execution across manager and worker clusters.

## Inspect Manager RTJ Status

The manager cluster holds the source-of-truth RTJ with MultiCluster
status fields.

```bash
# Quick overview.
make phase6-inspect-manager

# Or manually:
MANAGER_CTX="kind-phase6-manager"
NS="checkpoint-dev"
RTJ="phase6-dispatch-demo"

# RTJ phase and MultiCluster status.
kubectl -n $NS get resumabletrainingjobs.training.checkpoint.example.io $RTJ \
  -o wide --context $MANAGER_CTX

# Detailed MultiCluster status.
kubectl -n $NS get resumabletrainingjobs.training.checkpoint.example.io $RTJ \
  -o jsonpath='{.status.multiCluster}' --context $MANAGER_CTX | python3 -m json.tool
```

Key fields to check:
- `status.multiCluster.dispatchPhase`: `Pending` | `Dispatched` | `Active`
- `status.multiCluster.executionCluster`: which worker was selected
- `status.multiCluster.remotePhase`: the worker's current phase
- `status.multiCluster.localExecutionSuppressed`: must be `true`

## Inspect MultiKueue Objects and Worker Selection

MultiKueue resources on the manager control dispatch routing.

```bash
MANAGER_CTX="kind-phase6-manager"

# AdmissionCheck — must be Active.
kubectl get admissionchecks.kueue.x-k8s.io multikueue --context $MANAGER_CTX

# MultiKueueConfig — dispatch strategy.
kubectl get multikueueconfigs.kueue.x-k8s.io multikueue-config \
  -o yaml --context $MANAGER_CTX

# MultiKueueClusters — worker cluster connectivity.
kubectl get multikueueclusters.kueue.x-k8s.io --context $MANAGER_CTX
kubectl get multikueueclusters.kueue.x-k8s.io worker-1 \
  -o jsonpath='{.status.conditions}' --context $MANAGER_CTX | python3 -m json.tool

# ClusterQueue — must list 'multikueue' in admissionChecks.
kubectl get clusterqueues.kueue.x-k8s.io phase6-multikueue-cq \
  -o jsonpath='{.spec.admissionChecks}' --context $MANAGER_CTX

# Workload — shows which admission checks have passed.
kubectl -n $NS get workloads.kueue.x-k8s.io -o wide --context $MANAGER_CTX
```

Worker selection is determined by:
1. Which MultiKueueCluster resources are `Active`.
2. Which worker ClusterQueues have available quota.
3. The ClusterQueue `stopPolicy` (used in dev to force deterministic selection).

## Inspect the Mirror RTJ on the Worker Cluster

The Kueue adapter creates a mirror copy of the RTJ on the selected
worker cluster.

```bash
# Automated check across both workers.
make phase6-inspect-worker

# Or manually on a specific worker:
WORKER_CTX="kind-phase6-worker-1"

# Mirror RTJ.
kubectl -n $NS get resumabletrainingjobs.training.checkpoint.example.io $RTJ \
  -o wide --context $WORKER_CTX

# Child JobSet (created by worker operator).
kubectl -n $NS get jobsets.jobset.x-k8s.io \
  -l training.checkpoint.example.io/rtj-name=$RTJ --context $WORKER_CTX

# Trainer pods.
kubectl -n $NS get pods \
  -l training.checkpoint.example.io/rtj-name=$RTJ --context $WORKER_CTX

# Worker Workload.
kubectl -n $NS get workloads.kueue.x-k8s.io --context $WORKER_CTX

# Pod logs.
kubectl -n $NS logs -l training.checkpoint.example.io/rtj-name=$RTJ \
  --context $WORKER_CTX --tail=20
```

The mirror RTJ:
- Has the same name and namespace as the manager-side RTJ.
- Does NOT have `spec.managedBy` set (stripped by the adapter).
- Follows the full Phase 5 reconciliation path on the worker.

## Confirm No Manager-Local JobSet Was Launched

The manager must never create local child JobSets for MultiKueue-
managed RTJs.

```bash
MANAGER_CTX="kind-phase6-manager"

# Must return zero results.
kubectl -n $NS get jobsets.jobset.x-k8s.io \
  -l training.checkpoint.example.io/rtj-name=$RTJ \
  --context $MANAGER_CTX --no-headers

# Also confirm no pods from this RTJ on the manager.
kubectl -n $NS get pods \
  -l training.checkpoint.example.io/rtj-name=$RTJ \
  --context $MANAGER_CTX --no-headers
```

If any local JobSets exist on the manager, this indicates a bug in
the mode split logic. Check:
- The manager operator is running with `--mode=manager`.
- The RTJ has `spec.managedBy: kueue.x-k8s.io/multikueue`.

## Inspect Shared Checkpoint Evidence

Checkpoint evidence is available on both the manager (summary) and the
worker (full detail).

```bash
# Automated cross-cluster checkpoint inspection.
make phase6-inspect-checkpoints

# Manager-side remote checkpoint summary.
kubectl -n $NS get resumabletrainingjobs.training.checkpoint.example.io $RTJ \
  -o jsonpath='{.status.multiCluster.remoteCheckpoint}' \
  --context $MANAGER_CTX | python3 -m json.tool

# Worker-side full checkpoint status.
kubectl -n $NS get resumabletrainingjobs.training.checkpoint.example.io $RTJ \
  -o jsonpath='{.status.lastCompletedCheckpoint}' \
  --context $WORKER_CTX | python3 -m json.tool

# Shared store ConfigMap.
kubectl -n $NS get configmap shared-checkpoint-store \
  -o jsonpath='{.data}' --context $MANAGER_CTX

# Credential Secrets (existence check only).
for ctx in kind-phase6-manager kind-phase6-worker-1 kind-phase6-worker-2; do
  echo -n "$ctx: "
  kubectl -n $NS get secret checkpoint-storage-credentials \
    --context $ctx --no-headers 2>/dev/null && echo "present" || echo "MISSING"
done
```

## Metrics

The operator exposes Phase 6 metrics on the configured metrics port
(default `:8080`).

```bash
# Port-forward to the manager operator metrics.
kubectl -n checkpoint-system port-forward deploy/rtj-operator 8080:8080 \
  --context kind-phase6-manager &

# Query Phase 6 metrics.
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator | grep -E \
  'execution_role|remote_rtjs|manager_local|remote_status|remote_pause|remote_resume|remote_checkpoint|shared_store'
```

Key Phase 6 metrics:
| Metric | Type | Description |
|--------|------|-------------|
| `rtjs_by_execution_role` | gauge | RTJs by operator role (manager/worker) |
| `remote_rtjs_by_cluster` | gauge | Remote RTJs by selected worker cluster |
| `manager_local_suppressions_total` | counter | Manager-mode local launch suppressions |
| `remote_status_sync_successes_total` | counter | Successful remote status syncs |
| `remote_status_sync_failures_total` | counter | Failed remote status syncs |
| `remote_pause_events_total` | counter | Remote pause completions |
| `remote_resume_events_total` | counter | Remote resume initiations |
| `remote_checkpoint_observations_total` | counter | Remote checkpoint summaries observed |
| `shared_store_access_failures_total` | counter | Shared store access failures |
