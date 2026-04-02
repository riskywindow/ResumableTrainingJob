#!/usr/bin/env bash
#
# Inspect checkpoint evidence for a Phase 7 RTJ.
# Shows: RTJ checkpoint status, selected checkpoint, startup/recovery state
# (to correlate with timeout-triggered requeue), and child JobSet state.
#
# Usage:
#   PHASE7_RTJ_NAME=phase7-demo make phase7-inspect-checkpoints
#   PHASE7_RTJ_NAME=phase7-demo ./hack/dev/phase7-inspect-checkpoints.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE7_RTJ_NAME:-${RTJ_NAME:-phase7-demo}}"
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

echo "=== Startup/recovery state ==="
startup_state="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.startupRecovery.startupState}' 2>/dev/null || true)"
if [[ -n "${startup_state}" ]]; then
  echo "startupState: ${startup_state}"
  eviction_reason="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.startupRecovery.evictionReason}' 2>/dev/null || true)"
  echo "evictionReason: ${eviction_reason:-<none>}"
  echo
  echo "Note: StartupTimedOut = eviction before first Running; checkpoint=nil"
  echo "      RecoveryTimedOut = eviction after Running; checkpoint preserved"
else
  echo "<not populated>"
fi
echo

echo "=== Capacity guarantee ==="
cap_guaranteed="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.capacity.capacityGuaranteed}' 2>/dev/null || true)"
if [[ -n "${cap_guaranteed}" ]]; then
  echo "capacityGuaranteed: ${cap_guaranteed}"
else
  echo "<not populated>"
fi
echo

echo "=== Child JobSet ==="
active_jobset="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.activeJobSetName}' 2>/dev/null || true)"
if [[ -n "${active_jobset}" ]]; then
  echo "child JobSet: ${active_jobset}"
  echo
  echo "pods:"
  kubectl -n "$DEV_NAMESPACE" get pods -l "jobset.sigs.k8s.io/jobset-name=${active_jobset}" \
    -o custom-columns='NAME:.metadata.name,NODE:.spec.nodeName,STATUS:.status.phase,READY:.status.containerStatuses[0].ready' 2>/dev/null || echo "  <no pods>"
else
  echo "<no active child JobSet>"
fi
