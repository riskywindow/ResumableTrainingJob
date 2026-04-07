#!/usr/bin/env bash
#
# Trigger an elastic grow on a Phase 9 RTJ.
#
# Patches spec.elasticity.targetWorkerCount to 4 (grow from 2 to 4).
# Grow always uses checkpoint-and-relaunch:
#   1. Controller marks resizeState=InProgress and triggers drain.
#   2. Current workers checkpoint and exit cleanly.
#   3. RTJ re-queues with targetWorkerCount=4.
#   4. Kueue re-admits with the larger quota allocation.
#   5. A new 4-worker JobSet launches from the latest checkpoint.
#
# IMPORTANT: submit the shrink RTJ (RTJ A) first and wait for it to shrink
# so the released quota is available for the grow RTJ (RTJ B).
#
# Usage:
#   make phase9-grow
#   PHASE9_GROW_RTJ_NAME=my-job make phase9-grow

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

PHASE9_GROW_RTJ_NAME="${PHASE9_GROW_RTJ_NAME:-phase9-elastic-b}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"
TARGET_WORKER_COUNT=4

echo "=== Elastic grow: ${DEV_NAMESPACE}/${PHASE9_GROW_RTJ_NAME} (2 → ${TARGET_WORKER_COUNT}) ==="
echo

# Show current elasticity state before patching.
echo "--- Before patch ---"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_GROW_RTJ_NAME" \
  -o jsonpath=$'  spec.elasticity.targetWorkerCount={.spec.elasticity.targetWorkerCount}\n  status.elasticity.resizeState={.status.elasticity.resizeState}\n  status.elasticity.admittedWorkerCount={.status.elasticity.admittedWorkerCount}\n' 2>/dev/null || echo "  <RTJ not found>"
echo

# Patch targetWorkerCount to trigger grow.
kubectl -n "$DEV_NAMESPACE" patch "$RTJ_RESOURCE" "$PHASE9_GROW_RTJ_NAME" \
  --type merge \
  -p "{\"spec\":{\"elasticity\":{\"targetWorkerCount\":${TARGET_WORKER_COUNT}}}}"

echo
echo "grow requested for ${DEV_NAMESPACE}/${PHASE9_GROW_RTJ_NAME}: targetWorkerCount=${TARGET_WORKER_COUNT}"
echo

# Brief wait for the controller to reconcile.
sleep 2

echo "--- RTJ elasticity status (after patch) ---"
resize_state="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_GROW_RTJ_NAME" \
  -o jsonpath='{.status.elasticity.resizeState}' 2>/dev/null || true)"
echo "  resizeState:              ${resize_state:-<not yet set>}"

resize_path="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_GROW_RTJ_NAME" \
  -o jsonpath='{.status.elasticity.resizePath}' 2>/dev/null || true)"
echo "  resizePath:               ${resize_path:-<not yet set>}"

admitted_count="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_GROW_RTJ_NAME" \
  -o jsonpath='{.status.elasticity.admittedWorkerCount}' 2>/dev/null || true)"
echo "  admittedWorkerCount:      ${admitted_count:-<not yet set>}"

target_count="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_GROW_RTJ_NAME" \
  -o jsonpath='{.status.elasticity.targetWorkerCount}' 2>/dev/null || true)"
echo "  targetWorkerCount:        ${target_count:-<not yet set>}"

reclaim_published="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_GROW_RTJ_NAME" \
  -o jsonpath='{.status.elasticity.reclaimablePodsPublished}' 2>/dev/null || true)"
echo "  reclaimablePodsPublished: ${reclaim_published:-<not yet set>}"

exec_mode="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_GROW_RTJ_NAME" \
  -o jsonpath='{.status.elasticity.executionMode}' 2>/dev/null || true)"
echo "  executionMode:            ${exec_mode:-<not yet set>}"

echo
echo "Grow uses checkpoint-and-relaunch. The RTJ will drain, checkpoint, then"
echo "re-queue at size ${TARGET_WORKER_COUNT}. New quota is needed for the larger allocation."
echo
echo "Poll for progress:"
echo "  make phase9-inspect-elastic PHASE9_GROW_RTJ_NAME=${PHASE9_GROW_RTJ_NAME}"
echo "  make phase9-inspect-workload PHASE9_GROW_RTJ_NAME=${PHASE9_GROW_RTJ_NAME}"
echo "  make phase9-inspect-checkpoints PHASE9_GROW_RTJ_NAME=${PHASE9_GROW_RTJ_NAME}"
