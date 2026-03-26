#!/usr/bin/env bash
#
# Inspect checkpoint evidence used by the Phase 4 readiness gate.
# Shows: RTJ checkpoint status, selected checkpoint, the ResumeReadiness
# AdmissionCheck state on the Workload, and the checkpoint manifest if
# MinIO is port-forwarded.
#
# Usage:
#   PHASE4_RTJ_NAME=phase4-demo make phase4-inspect-checkpoints
#   PHASE4_RTJ_NAME=phase4-demo ./hack/dev/inspect-checkpoints-phase4.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE4_RTJ_NAME:-${RTJ_NAME:-phase4-demo}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== RTJ checkpoint status ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'phase={.status.phase}\ncurrentRunAttempt={.status.currentRunAttempt}\n' 2>/dev/null || echo "<RTJ not found>"
echo

echo "=== Last completed checkpoint ==="
last_uri="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.lastCompletedCheckpoint.manifestURI}' 2>/dev/null || true)"
if [[ -n "${last_uri}" ]]; then
  echo "manifestURI: ${last_uri}"
  last_step="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.lastCompletedCheckpoint.globalStep}' 2>/dev/null || true)"
  echo "globalStep: ${last_step:-<not set>}"
  last_ws="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.lastCompletedCheckpoint.worldSize}' 2>/dev/null || true)"
  echo "worldSize: ${last_ws:-<not set>}"
else
  echo "<none recorded yet>"
fi
echo

echo "=== Selected checkpoint (for restore) ==="
selected_uri="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.selectedCheckpoint.manifestURI}' 2>/dev/null || true)"
if [[ -n "${selected_uri}" ]]; then
  echo "manifestURI: ${selected_uri}"
  selected_ws="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.selectedCheckpoint.worldSize}' 2>/dev/null || true)"
  echo "worldSize: ${selected_ws:-<not set>}"
else
  echo "<none>"
fi
echo

echo "=== ResumeReadiness check state on Workload ==="
workload_name="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io \
  -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"${RTJ_NAME}\")]}{.metadata.name}{end}" 2>/dev/null || true)"
if [[ -n "${workload_name}" ]]; then
  echo "workload: ${workload_name}"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
    -o jsonpath='{range .status.admissionChecks[*]}  name={.name}  state={.state}  message={.message}{"\n"}{end}' 2>/dev/null || echo "  <no admission checks>"
else
  echo "<no Workload found for RTJ ${RTJ_NAME}>"
fi
echo

echo "=== Effective launch shape ==="
worker_count="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.effectiveLaunchShape.workerCount}' 2>/dev/null || true)"
if [[ -n "${worker_count}" ]]; then
  world_size="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.effectiveLaunchShape.worldSize}' 2>/dev/null || true)"
  resume_mode="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.effectiveLaunchShape.resumeMode}' 2>/dev/null || true)"
  ckpt_id="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.effectiveLaunchShape.selectedCheckpointID}' 2>/dev/null || true)"
  echo "workerCount: ${worker_count}"
  echo "worldSize: ${world_size:-<not set>}"
  echo "resumeMode: ${resume_mode:-<not set>}"
  echo "selectedCheckpointID: ${ckpt_id:-<not set>}"
else
  echo "<not populated>"
fi
echo

echo "=== Checkpoint manifest (via MinIO) ==="
if [[ -n "${last_uri}" ]] && command -v curl >/dev/null 2>&1; then
  manifest_url="${last_uri/s3:\/\//http://localhost:9000/}"
  manifest_json="$(curl -s --connect-timeout 2 "${manifest_url}" 2>/dev/null || true)"
  if [[ -n "${manifest_json}" && "${manifest_json}" == "{"* ]]; then
    echo "${manifest_json}" | python3 -m json.tool 2>/dev/null || echo "${manifest_json}"
  else
    echo "<MinIO not reachable on localhost:9000 or manifest not found>"
    echo "To access: kubectl -n ${DEV_NAMESPACE} port-forward svc/minio 9000:9000 &"
  fi
else
  echo "<no checkpoint URI or curl not available>"
fi
