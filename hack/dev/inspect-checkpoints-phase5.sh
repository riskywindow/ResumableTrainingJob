#!/usr/bin/env bash
#
# Inspect checkpoint freshness evidence for a Phase 5 RTJ.
#
# Shows: last completed checkpoint, checkpoint age, freshness target
# from the attached policy, and whether the checkpoint is stale.
#
# Usage:
#   PHASE5_RTJ_NAME=phase5-low-demo make phase5-inspect-checkpoints
#   RTJ_NAME=phase5-low-demo ./hack/dev/inspect-checkpoints-phase5.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE5_RTJ_NAME:-${RTJ_NAME:-phase5-low-demo}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"
POLICY_RESOURCE="checkpointprioritypolicies.training.checkpoint.example.io"

echo "=== RTJ checkpoint status ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'phase={.status.phase}\ncurrentRunAttempt={.status.currentRunAttempt}\n' 2>/dev/null || echo "<RTJ not found>"
echo

echo "=== Last completed checkpoint (RTJ status) ==="
last_uri="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.lastCompletedCheckpoint.manifestURI}' 2>/dev/null || true)"
if [[ -n "${last_uri}" ]]; then
  echo "manifestURI: ${last_uri}"
  last_step="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.lastCompletedCheckpoint.globalStep}' 2>/dev/null || true)"
  last_time="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.lastCompletedCheckpoint.completionTime}' 2>/dev/null || true)"
  echo "globalStep: ${last_step:-<not set>}"
  echo "completionTime: ${last_time:-<not set>}"
else
  echo "<none recorded yet>"
fi
echo

echo "=== Checkpoint freshness (priority shaping telemetry) ==="
ckpt_time="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.lastCompletedCheckpointTime}' 2>/dev/null || true)"
ckpt_age="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.checkpointAge}' 2>/dev/null || true)"
echo "lastCompletedCheckpointTime: ${ckpt_time:-<none>}"
echo "checkpointAge: ${ckpt_age:-<none>}"
echo

echo "=== Freshness target (from policy) ==="
policy_name="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.spec.priorityPolicyRef.name}' 2>/dev/null || true)"
if [[ -n "${policy_name}" ]]; then
  target="$(kubectl get "$POLICY_RESOURCE" "$policy_name" \
    -o jsonpath='{.spec.checkpointFreshnessTarget}' 2>/dev/null || true)"
  echo "policy: ${policy_name}"
  echo "checkpointFreshnessTarget: ${target:-<not set>}"
else
  echo "<no policy attached>"
fi
echo

echo "=== Preemption state (derived from freshness) ==="
state="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.preemptionState}' 2>/dev/null || true)"
reason="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.preemptionStateReason}' 2>/dev/null || true)"
echo "preemptionState: ${state:-<not set>}"
echo "preemptionStateReason: ${reason:-<not set>}"
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
