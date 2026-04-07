#!/usr/bin/env bash
#
# Inspect Phase 9 checkpoint catalog and resize-related checkpoint metadata
# for an elastic RTJ.
#
# Shows: RTJ checkpoint catalog (status.checkpoints), latest checkpoint
# details (manifestURI, globalStep, worldSize, timestamp), and any
# resize-related checkpoint metadata (world-size changes across resize
# boundaries, checkpoint compatibility across grow/shrink events).
#
# Usage:
#   make phase9-inspect-checkpoints
#   PHASE9_SHRINK_RTJ_NAME=my-job make phase9-inspect-checkpoints
#   RTJ_NAME=my-job make phase9-inspect-checkpoints

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE9_SHRINK_RTJ_NAME:-${PHASE9_GROW_RTJ_NAME:-${RTJ_NAME:-phase9-elastic-a}}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== Phase 9 Checkpoint Catalog: ${DEV_NAMESPACE}/${RTJ_NAME} ==="
echo

# ── RTJ overview ─────────────────────────────────────────────────────────────

echo "--- RTJ summary ---"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'  phase={.status.phase}\n  currentRunAttempt={.status.currentRunAttempt}\n  activeJobSet={.status.activeJobSetName}\n' 2>/dev/null || echo "  <RTJ not found>"
echo

# ── Elasticity resize context ─────────────────────────────────────────────────

echo "--- Resize context ---"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'  resizeState={.status.elasticity.resizeState}\n  resizePath={.status.elasticity.resizePath}\n  admittedWorkerCount={.status.elasticity.admittedWorkerCount}\n  targetWorkerCount={.status.elasticity.targetWorkerCount}\n' 2>/dev/null || echo "  <not set>"
echo

# ── Last completed checkpoint ─────────────────────────────────────────────────

echo "--- Last completed checkpoint ---"
last_uri="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.lastCompletedCheckpoint.manifestURI}' 2>/dev/null || true)"
if [[ -n "${last_uri}" ]]; then
  echo "  manifestURI:  ${last_uri}"
  last_step="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.lastCompletedCheckpoint.globalStep}' 2>/dev/null || true)"
  echo "  globalStep:   ${last_step:-<not set>}"
  last_ws="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.lastCompletedCheckpoint.worldSize}' 2>/dev/null || true)"
  echo "  worldSize:    ${last_ws:-<not set>}"
  last_ts="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.lastCompletedCheckpoint.completedAt}' 2>/dev/null || true)"
  echo "  completedAt:  ${last_ts:-<not set>}"
else
  echo "  <none recorded yet>"
fi
echo

# ── Selected checkpoint (for restore) ────────────────────────────────────────

echo "--- Selected checkpoint (for restore) ---"
selected_uri="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.selectedCheckpoint.manifestURI}' 2>/dev/null || true)"
if [[ -n "${selected_uri}" ]]; then
  echo "  manifestURI:  ${selected_uri}"
  selected_ws="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.selectedCheckpoint.worldSize}' 2>/dev/null || true)"
  echo "  worldSize:    ${selected_ws:-<not set>}"
  selected_step="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.selectedCheckpoint.globalStep}' 2>/dev/null || true)"
  echo "  globalStep:   ${selected_step:-<not set>}"
else
  echo "  <none>"
fi
echo

# ── Checkpoint catalog ────────────────────────────────────────────────────────

echo "--- Checkpoint catalog (status.checkpoints) ---"
ckpt_count="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.checkpoints}' 2>/dev/null | python3 -c 'import sys,json; data=json.load(sys.stdin); print(len(data))' 2>/dev/null || echo "0")"

if [[ "${ckpt_count}" -gt 0 ]] 2>/dev/null; then
  kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{range .status.checkpoints[*]}  step={.globalStep}  worldSize={.worldSize}  uri={.manifestURI}  completedAt={.completedAt}{"\n"}{end}' 2>/dev/null || echo "  <none>"
else
  # Try raw jsonpath without length check.
  catalog="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{range .status.checkpoints[*]}  step={.globalStep}  worldSize={.worldSize}  uri={.manifestURI}  completedAt={.completedAt}{"\n"}{end}' 2>/dev/null || true)"
  if [[ -n "${catalog}" ]]; then
    printf '%s\n' "${catalog}"
  else
    echo "  <no checkpoints recorded yet>"
  fi
fi
echo

# ── Resize-related checkpoint metadata ───────────────────────────────────────

echo "--- Resize-related checkpoint metadata ---"
echo "Note: checkpoints taken during a resize event record the world size"
echo "at the time of checkpoint. A grow relaunch will restore from the latest"
echo "checkpoint even if worldSize differs from the new target (allowWorldSizeChange)."
echo

# Show world sizes across checkpoint history to reveal resize boundaries.
echo "worldSize history (from catalog):"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{range .status.checkpoints[*]}  step={.globalStep}  worldSize={.worldSize}{"\n"}{end}' 2>/dev/null | sort -t= -k2 -n || echo "  <not available>"
echo

# Show the identity worldSize (current configured world size).
identity_ws="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.spec.identity.worldSize}' 2>/dev/null || true)"
echo "spec.identity.worldSize (current): ${identity_ws:-<not set>}"
echo "spec.elasticity.targetWorkerCount: $(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.spec.elasticity.targetWorkerCount}' 2>/dev/null || echo "<not set>")"
echo

# ── Checkpoint store contents (via minio) ────────────────────────────────────

echo "--- Checkpoint store contents ---"
MINIO_POD="$(kubectl -n "$DEV_NAMESPACE" get pod -l app=minio -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
if [[ -n "$MINIO_POD" ]]; then
  echo "listing s3://rtj-checkpoints/${RTJ_NAME}/:"
  kubectl -n "$DEV_NAMESPACE" exec "$MINIO_POD" -- \
    mc ls --recursive "local/rtj-checkpoints/${RTJ_NAME}/" 2>/dev/null || echo "  (empty or not accessible)"
else
  echo "  (minio pod not found — checkpoint store inspection unavailable)"
fi
echo

# ── Checkpoint conditions ────────────────────────────────────────────────────

echo "--- Checkpoint-related conditions ---"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{range .status.conditions[*]}  {.type}={.status} ({.reason}): {.message}{"\n"}{end}' 2>/dev/null \
  | grep -iE 'checkpoint|resize|elastic|drain|relaunch|compatible' \
  || echo "  (no checkpoint/resize-related conditions found)"
