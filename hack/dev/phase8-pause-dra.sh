#!/usr/bin/env bash
#
# Pause a Phase 8 DRA-backed RTJ.
#
# Patches spec.control.desiredState to Paused. The operator checkpoints
# and tears down the child JobSet. ResourceClaimTemplates are preserved
# (owned by the RTJ, not the JobSet).
#
# Usage:
#   make phase8-pause
#   PHASE8_RTJ_NAME=my-job make phase8-pause

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

PHASE8_RTJ_NAME="${PHASE8_RTJ_NAME:-phase8-demo}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== Pausing Phase 8 DRA-backed RTJ ${DEV_NAMESPACE}/${PHASE8_RTJ_NAME} ==="

kubectl -n "$DEV_NAMESPACE" patch "$RTJ_RESOURCE" "$PHASE8_RTJ_NAME" \
  --type merge \
  -p '{"spec":{"control":{"desiredState":"Paused"}}}'

echo
echo "pause requested for ${DEV_NAMESPACE}/${PHASE8_RTJ_NAME}"
echo

echo "=== Current RTJ status ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE8_RTJ_NAME" -o wide
echo

phase="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE8_RTJ_NAME" \
  -o jsonpath='{.status.phase}' 2>/dev/null || true)"
echo "phase: ${phase:-<pending>}"
echo

echo "ResourceClaimTemplates remain (owned by RTJ):"
kubectl -n "$DEV_NAMESPACE" get resourceclaimtemplates \
  -l "training.checkpoint.example.io/rtj-name=${PHASE8_RTJ_NAME}" \
  --no-headers 2>/dev/null || echo "  (none)"
echo

echo "Poll DRA status with:"
echo "  make phase8-inspect-dra PHASE8_RTJ_NAME=${PHASE8_RTJ_NAME}"
echo "  make phase8-inspect-checkpoints PHASE8_RTJ_NAME=${PHASE8_RTJ_NAME}"
