#!/usr/bin/env bash
#
# Inspect a Phase 6 MultiKueue-managed RTJ on the manager cluster.
#
# Shows: RTJ status, MultiCluster status (dispatch phase, execution cluster,
# remote phase, remote checkpoint), Workload state, MultiKueue admission
# check, and confirmation of local runtime suppression.
#
# Usage:
#   make phase6-inspect-manager
#   PHASE6_RTJ_NAME=my-job ./hack/dev/phase6-inspect-manager.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$REPO_ROOT/hack/dev/common.sh"

require_command kubectl
require_command kind

PHASE6_MANAGER="${PHASE6_MANAGER:-phase6-manager}"
PHASE6_RTJ_NAME="${PHASE6_RTJ_NAME:-phase6-dispatch-demo}"
DEV_NAMESPACE="${DEV_NAMESPACE:-checkpoint-dev}"

MANAGER_CTX="kind-${PHASE6_MANAGER}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== Manager RTJ overview ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o wide --context "$MANAGER_CTX" 2>/dev/null || echo "<RTJ not found>"
echo

echo "=== RTJ phase and spec ==="
phase="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.phase}' --context "$MANAGER_CTX" 2>/dev/null || true)"
desired="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.spec.control.desiredState}' --context "$MANAGER_CTX" 2>/dev/null || true)"
managed="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.spec.managedBy}' --context "$MANAGER_CTX" 2>/dev/null || true)"
echo "phase: ${phase:-<not set>}"
echo "desiredState: ${desired:-<not set>}"
echo "managedBy: ${managed:-<not set>}"
echo

echo "=== MultiCluster status ==="
dispatch="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.multiCluster.dispatchPhase}' --context "$MANAGER_CTX" 2>/dev/null || true)"
exec_cluster="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.multiCluster.executionCluster}' --context "$MANAGER_CTX" 2>/dev/null || true)"
remote_phase="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.multiCluster.remotePhase}' --context "$MANAGER_CTX" 2>/dev/null || true)"
suppressed="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.multiCluster.localExecutionSuppressed}' --context "$MANAGER_CTX" 2>/dev/null || true)"
echo "dispatchPhase: ${dispatch:-<not set>}"
echo "executionCluster: ${exec_cluster:-<not set>}"
echo "remotePhase: ${remote_phase:-<not set>}"
echo "localExecutionSuppressed: ${suppressed:-<not set>}"
echo

echo "=== Remote object reference ==="
remote_cluster="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.multiCluster.remoteObjectRef.cluster}' --context "$MANAGER_CTX" 2>/dev/null || true)"
remote_ns="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.multiCluster.remoteObjectRef.namespace}' --context "$MANAGER_CTX" 2>/dev/null || true)"
remote_name="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.multiCluster.remoteObjectRef.name}' --context "$MANAGER_CTX" 2>/dev/null || true)"
if [[ -n "${remote_cluster}" ]]; then
  echo "cluster: ${remote_cluster}"
  echo "namespace: ${remote_ns}"
  echo "name: ${remote_name}"
else
  echo "<not dispatched yet>"
fi
echo

echo "=== Remote checkpoint summary ==="
ckpt_id="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.multiCluster.remoteCheckpoint.lastCompletedCheckpointID}' --context "$MANAGER_CTX" 2>/dev/null || true)"
ckpt_time="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.multiCluster.remoteCheckpoint.lastCompletedCheckpointTime}' --context "$MANAGER_CTX" 2>/dev/null || true)"
ckpt_uri="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.multiCluster.remoteCheckpoint.storageURI}' --context "$MANAGER_CTX" 2>/dev/null || true)"
if [[ -n "${ckpt_id}" ]]; then
  echo "checkpointID: ${ckpt_id}"
  echo "completionTime: ${ckpt_time:-<not set>}"
  echo "storageURI: ${ckpt_uri:-<not set>}"
else
  echo "<no remote checkpoint observed>"
fi
echo

echo "=== Manager Workload ==="
workload_name="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io \
  -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"${PHASE6_RTJ_NAME}\")]}{.metadata.name}{end}" \
  --context "$MANAGER_CTX" 2>/dev/null || true)"
if [[ -n "${workload_name}" ]]; then
  echo "workload: ${workload_name}"
  wl_admitted="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
    -o jsonpath='{.status.admission.clusterQueue}' --context "$MANAGER_CTX" 2>/dev/null || true)"
  echo "admitted to: ${wl_admitted:-<pending>}"
  # Show admission check status.
  echo "admission checks:"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
    -o jsonpath='{range .status.admissionChecks[*]}  {.name}: {.state}{"\n"}{end}' \
    --context "$MANAGER_CTX" 2>/dev/null || echo "  <none>"
else
  echo "<no Workload found for RTJ ${PHASE6_RTJ_NAME}>"
fi
echo

echo "=== MultiKueue objects ==="
echo "admission checks:"
kubectl get admissionchecks.kueue.x-k8s.io --context "$MANAGER_CTX" --no-headers 2>/dev/null || echo "  (none)"
echo "multikueue clusters:"
kubectl get multikueueclusters.kueue.x-k8s.io --context "$MANAGER_CTX" --no-headers 2>/dev/null || echo "  (none)"
echo

echo "=== Local runtime suppression check ==="
jobsets="$(kubectl -n "$DEV_NAMESPACE" get jobsets.jobset.x-k8s.io \
  -l training.checkpoint.example.io/rtj-name="${PHASE6_RTJ_NAME}" \
  --no-headers --context "$MANAGER_CTX" 2>/dev/null | wc -l | tr -d ' ')"
if [[ "$jobsets" -eq 0 ]]; then
  echo "PASS: no local child JobSets found (manager suppression working)"
else
  echo "FAIL: ${jobsets} local child JobSet(s) found — manager should NOT create local runtime"
fi
