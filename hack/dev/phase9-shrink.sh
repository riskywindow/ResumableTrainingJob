#!/usr/bin/env bash
#
# Trigger an elastic shrink on a Phase 9 RTJ.
#
# Patches spec.elasticity.targetWorkerCount to 2 (shrink from 4 to 2).
# The controller evaluates the shrink path:
#   - If runtime supports in-place shrink → writes reclaimablePods to Workload.
#   - Otherwise (DDP default)             → checkpoint-and-relaunch at size 2.
#
# After patching, shows the RTJ elasticity status so the operator transition
# (resizeState, resizePath) is immediately visible.
#
# Usage:
#   make phase9-shrink
#   PHASE9_SHRINK_RTJ_NAME=my-job make phase9-shrink

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

PHASE9_SHRINK_RTJ_NAME="${PHASE9_SHRINK_RTJ_NAME:-phase9-elastic-a}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"
TARGET_WORKER_COUNT=2

echo "=== Elastic shrink: ${DEV_NAMESPACE}/${PHASE9_SHRINK_RTJ_NAME} (4 → ${TARGET_WORKER_COUNT}) ==="
echo

# Show current elasticity state before patching.
echo "--- Before patch ---"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_SHRINK_RTJ_NAME" \
  -o jsonpath=$'  spec.elasticity.targetWorkerCount={.spec.elasticity.targetWorkerCount}\n  status.elasticity.resizeState={.status.elasticity.resizeState}\n  status.elasticity.admittedWorkerCount={.status.elasticity.admittedWorkerCount}\n' 2>/dev/null || echo "  <RTJ not found>"
echo

# Patch targetWorkerCount to trigger shrink.
kubectl -n "$DEV_NAMESPACE" patch "$RTJ_RESOURCE" "$PHASE9_SHRINK_RTJ_NAME" \
  --type merge \
  -p "{\"spec\":{\"elasticity\":{\"targetWorkerCount\":${TARGET_WORKER_COUNT}}}}"

echo
echo "shrink requested for ${DEV_NAMESPACE}/${PHASE9_SHRINK_RTJ_NAME}: targetWorkerCount=${TARGET_WORKER_COUNT}"
echo

# Brief wait for the controller to reconcile.
sleep 2

echo "--- RTJ elasticity status (after patch) ---"
resize_state="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_SHRINK_RTJ_NAME" \
  -o jsonpath='{.status.elasticity.resizeState}' 2>/dev/null || true)"
echo "  resizeState:              ${resize_state:-<not yet set>}"

resize_path="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_SHRINK_RTJ_NAME" \
  -o jsonpath='{.status.elasticity.resizePath}' 2>/dev/null || true)"
echo "  resizePath:               ${resize_path:-<not yet set>}"

admitted_count="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_SHRINK_RTJ_NAME" \
  -o jsonpath='{.status.elasticity.admittedWorkerCount}' 2>/dev/null || true)"
echo "  admittedWorkerCount:      ${admitted_count:-<not yet set>}"

target_count="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_SHRINK_RTJ_NAME" \
  -o jsonpath='{.status.elasticity.targetWorkerCount}' 2>/dev/null || true)"
echo "  targetWorkerCount:        ${target_count:-<not yet set>}"

reclaim_published="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_SHRINK_RTJ_NAME" \
  -o jsonpath='{.status.elasticity.reclaimablePodsPublished}' 2>/dev/null || true)"
echo "  reclaimablePodsPublished: ${reclaim_published:-<not yet set>}"

exec_mode="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_SHRINK_RTJ_NAME" \
  -o jsonpath='{.status.elasticity.executionMode}' 2>/dev/null || true)"
echo "  executionMode:            ${exec_mode:-<not yet set>}"

echo
echo "Poll for progress:"
echo "  make phase9-inspect-elastic PHASE9_SHRINK_RTJ_NAME=${PHASE9_SHRINK_RTJ_NAME}"
echo "  make phase9-inspect-workload PHASE9_SHRINK_RTJ_NAME=${PHASE9_SHRINK_RTJ_NAME}"
