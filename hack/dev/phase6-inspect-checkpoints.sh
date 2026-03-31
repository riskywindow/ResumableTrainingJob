#!/usr/bin/env bash
#
# Inspect shared checkpoint evidence across the Phase 6 multi-cluster
# environment.
#
# Shows: manager-side remote checkpoint summary, worker-side checkpoint
# status, shared store ConfigMap, and credential Secrets on all clusters.
#
# Usage:
#   make phase6-inspect-checkpoints
#   PHASE6_RTJ_NAME=my-job ./hack/dev/phase6-inspect-checkpoints.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$REPO_ROOT/hack/dev/common.sh"

require_command kubectl
require_command kind

PHASE6_MANAGER="${PHASE6_MANAGER:-phase6-manager}"
PHASE6_WORKER_1="${PHASE6_WORKER_1:-phase6-worker-1}"
PHASE6_WORKER_2="${PHASE6_WORKER_2:-phase6-worker-2}"
PHASE6_RTJ_NAME="${PHASE6_RTJ_NAME:-phase6-dispatch-demo}"
DEV_NAMESPACE="${DEV_NAMESPACE:-checkpoint-dev}"

MANAGER_CTX="kind-${PHASE6_MANAGER}"
WORKER1_CTX="kind-${PHASE6_WORKER_1}"
WORKER2_CTX="kind-${PHASE6_WORKER_2}"

RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== Manager remote checkpoint summary ==="
ckpt_id="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.multiCluster.remoteCheckpoint.lastCompletedCheckpointID}' \
  --context "$MANAGER_CTX" 2>/dev/null || true)"
ckpt_time="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.multiCluster.remoteCheckpoint.lastCompletedCheckpointTime}' \
  --context "$MANAGER_CTX" 2>/dev/null || true)"
ckpt_uri="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.multiCluster.remoteCheckpoint.storageURI}' \
  --context "$MANAGER_CTX" 2>/dev/null || true)"
if [[ -n "${ckpt_id}" ]]; then
  echo "checkpointID: ${ckpt_id}"
  echo "completionTime: ${ckpt_time:-<not set>}"
  echo "storageURI: ${ckpt_uri:-<not set>}"
else
  echo "<no remote checkpoint observed on manager>"
fi
echo

echo "=== Manager last completed checkpoint (mirrored from worker) ==="
m_uri="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.lastCompletedCheckpoint.manifestURI}' \
  --context "$MANAGER_CTX" 2>/dev/null || true)"
m_step="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.lastCompletedCheckpoint.globalStep}' \
  --context "$MANAGER_CTX" 2>/dev/null || true)"
if [[ -n "${m_uri}" ]]; then
  echo "manifestURI: ${m_uri}"
  echo "globalStep: ${m_step:-<not set>}"
else
  echo "<none>"
fi
echo

# Check checkpoint status on each worker.
check_worker_checkpoint() {
  local cluster="$1"
  local ctx="kind-${cluster}"

  echo "=== Worker ${cluster} checkpoint status ==="
  if ! kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
    --context "$ctx" >/dev/null 2>&1; then
    echo "  RTJ not present on ${cluster}"
    echo
    return
  fi

  w_uri="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
    -o jsonpath='{.status.lastCompletedCheckpoint.manifestURI}' --context "$ctx" 2>/dev/null || true)"
  w_step="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
    -o jsonpath='{.status.lastCompletedCheckpoint.globalStep}' --context "$ctx" 2>/dev/null || true)"
  w_time="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
    -o jsonpath='{.status.lastCompletedCheckpoint.completionTime}' --context "$ctx" 2>/dev/null || true)"
  if [[ -n "${w_uri}" ]]; then
    echo "manifestURI: ${w_uri}"
    echo "globalStep: ${w_step:-<not set>}"
    echo "completionTime: ${w_time:-<not set>}"
  else
    echo "<no checkpoint recorded>"
  fi
  echo

  echo "--- Selected checkpoint (for restore) ---"
  sel_uri="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
    -o jsonpath='{.status.selectedCheckpoint.manifestURI}' --context "$ctx" 2>/dev/null || true)"
  sel_ws="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
    -o jsonpath='{.status.selectedCheckpoint.worldSize}' --context "$ctx" 2>/dev/null || true)"
  if [[ -n "${sel_uri}" ]]; then
    echo "manifestURI: ${sel_uri}"
    echo "worldSize: ${sel_ws:-<not set>}"
  else
    echo "<none>"
  fi
  echo
}

check_worker_checkpoint "$PHASE6_WORKER_1"
check_worker_checkpoint "$PHASE6_WORKER_2"

echo "=== Shared checkpoint store config ==="
echo "--- Manager ---"
kubectl -n "$DEV_NAMESPACE" get configmap shared-checkpoint-store \
  -o jsonpath='{.data}' --context "$MANAGER_CTX" 2>/dev/null || echo "  (not configured)"
echo
echo

echo "=== Checkpoint credentials ==="
for cluster in "$PHASE6_MANAGER" "$PHASE6_WORKER_1" "$PHASE6_WORKER_2"; do
  ctx="kind-${cluster}"
  if kubectl -n "$DEV_NAMESPACE" get secret checkpoint-storage-credentials \
    --context "$ctx" >/dev/null 2>&1; then
    echo "  ${cluster}: present"
  else
    echo "  ${cluster}: MISSING"
  fi
done
echo

echo "=== Shared store reachability ==="
STORE_ENDPOINT="$(kubectl -n "$DEV_NAMESPACE" get configmap shared-checkpoint-store \
  -o jsonpath='{.data.endpoint}' --context "$MANAGER_CTX" 2>/dev/null || echo "")"
if [[ -n "$STORE_ENDPOINT" ]]; then
  echo "endpoint: ${STORE_ENDPOINT}"
  MANAGER_CONTAINER="${PHASE6_MANAGER}-control-plane"
  if docker exec "$MANAGER_CONTAINER" wget -q -O /dev/null --timeout=5 \
    "${STORE_ENDPOINT}/minio/health/ready" 2>/dev/null; then
    echo "reachable: yes"
  else
    echo "reachable: NO (check network / MinIO health)"
  fi
else
  echo "endpoint: <not configured>"
fi
