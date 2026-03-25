#!/usr/bin/env bash
#
# Inspect checkpoint and restore state for a Phase 3 RTJ.
# Shows: last completed checkpoint, selected checkpoint, restore status,
# and (if MinIO is port-forwarded) the checkpoint manifest with world-size
# metadata.
#
# Usage:
#   RTJ_NAME=phase3-demo make phase3-inspect-checkpoints
#   RTJ_NAME=phase3-demo ./hack/dev/inspect-checkpoints.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${RTJ_NAME:-$PHASE3_RTJ_NAME}"
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

echo "=== Restore status ==="
restore_mode="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.restore.restoreMode}' 2>/dev/null || true)"
if [[ -n "${restore_mode}" ]]; then
  ckpt_ws="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.restore.lastCheckpointWorldSize}' 2>/dev/null || true)"
  restore_ws="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.restore.lastRestoreWorldSize}' 2>/dev/null || true)"
  echo "restoreMode: ${restore_mode}"
  echo "lastCheckpointWorldSize: ${ckpt_ws:-<not set>}"
  echo "lastRestoreWorldSize: ${restore_ws:-<not set>}"
else
  echo "<no restore recorded>"
fi
echo

echo "=== RTJ resume spec ==="
allow_ws="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.spec.resume.allowWorldSizeChange}' 2>/dev/null || true)"
echo "allowWorldSizeChange: ${allow_ws:-false}"
source_policy="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.spec.resume.sourcePolicy}' 2>/dev/null || true)"
echo "sourcePolicy: ${source_policy:-<not set>}"
echo

echo "=== Checkpoint manifest (via MinIO) ==="
# Try to fetch the manifest from MinIO if a port-forward is active on 9000.
if [[ -n "${last_uri}" ]] && command -v curl >/dev/null 2>&1; then
  # Convert s3://bucket/path to http://localhost:9000/bucket/path
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
