#!/usr/bin/env bash
#
# Resume a Phase 8 DRA-backed RTJ.
#
# Patches spec.control.desiredState back to Running. The operator selects
# the latest compatible checkpoint (matching device profile fingerprint)
# and launches a new child JobSet with DRA claim references.
#
# Usage:
#   make phase8-resume
#   PHASE8_RTJ_NAME=my-job make phase8-resume

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

PHASE8_RTJ_NAME="${PHASE8_RTJ_NAME:-phase8-demo}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== Resuming Phase 8 DRA-backed RTJ ${DEV_NAMESPACE}/${PHASE8_RTJ_NAME} ==="

# Show device profile fingerprint before resume.
fingerprint="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE8_RTJ_NAME" \
  -o jsonpath='{.status.devices.currentDeviceProfileFingerprint}' 2>/dev/null || true)"
echo "current device profile fingerprint: ${fingerprint:-<not set>}"
echo

kubectl -n "$DEV_NAMESPACE" patch "$RTJ_RESOURCE" "$PHASE8_RTJ_NAME" \
  --type merge \
  -p '{"spec":{"control":{"desiredState":"Running"}}}'

echo
echo "resume requested for ${DEV_NAMESPACE}/${PHASE8_RTJ_NAME}"
echo

echo "=== Current RTJ status ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE8_RTJ_NAME" -o wide
echo

phase="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE8_RTJ_NAME" \
  -o jsonpath='{.status.phase}' 2>/dev/null || true)"
echo "phase: ${phase:-<pending>}"
echo

echo "Poll DRA + checkpoint status with:"
echo "  make phase8-inspect-dra PHASE8_RTJ_NAME=${PHASE8_RTJ_NAME}"
echo "  make phase8-inspect-checkpoints PHASE8_RTJ_NAME=${PHASE8_RTJ_NAME}"
echo
echo "Expected flow:"
echo "  1. Operator selects latest checkpoint with matching device profile"
echo "  2. ResourceClaimTemplates are reused (same device spec)"
echo "  3. Kueue re-admits Workload with DRA quota"
echo "  4. Child JobSet created with DRA claim references"
echo "  5. Device profile fingerprint preserved across resume"
