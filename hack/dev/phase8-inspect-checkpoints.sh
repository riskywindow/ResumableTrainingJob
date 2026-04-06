#!/usr/bin/env bash
#
# Inspect Phase 8 checkpoint device-profile metadata for an RTJ.
#
# Shows: checkpoint manifest including device profile fingerprint,
# checkpoint history, resume compatibility assessment.
#
# Usage:
#   make phase8-inspect-checkpoints
#   PHASE8_RTJ_NAME=my-job make phase8-inspect-checkpoints

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE8_RTJ_NAME:-${RTJ_NAME:-phase8-demo}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== Phase 8 Checkpoint Device-Profile Metadata ==="
echo "  RTJ: ${DEV_NAMESPACE}/${RTJ_NAME}"
echo

# Current device profile from RTJ status.
echo "--- Current device profile ---"
fingerprint="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.devices.currentDeviceProfileFingerprint}' 2>/dev/null || true)"
echo "currentDeviceProfileFingerprint: ${fingerprint:-<not set>}"

device_mode="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.devices.deviceMode}' 2>/dev/null || true)"
echo "deviceMode: ${device_mode:-<not set>}"

classes="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.devices.requestedDeviceClasses}' 2>/dev/null || true)"
echo "requestedDeviceClasses: ${classes:-<not set>}"
echo

# Last checkpoint device profile.
echo "--- Last checkpoint fingerprint ---"
last_ckpt_fp="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.devices.lastCheckpointDeviceProfileFingerprint}' 2>/dev/null || true)"
echo "lastCheckpointDeviceProfileFingerprint: ${last_ckpt_fp:-<not set>}"

# Last resume device profile.
last_resume_fp="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.devices.lastResumeDeviceProfileFingerprint}' 2>/dev/null || true)"
echo "lastResumeDeviceProfileFingerprint: ${last_resume_fp:-<not set>}"
echo

# Compatibility assessment.
echo "--- Resume compatibility ---"
if [[ -z "$fingerprint" ]]; then
  echo "status: no current device profile (Phase 7 or earlier RTJ)"
  echo "action: any checkpoint is compatible (device profile check skipped)"
elif [[ -z "$last_ckpt_fp" ]]; then
  echo "status: no checkpoint device profile recorded yet"
  echo "action: first checkpoint will record the current fingerprint"
elif [[ "$fingerprint" == "$last_ckpt_fp" ]]; then
  echo "status: COMPATIBLE"
  echo "  current fingerprint:    ${fingerprint}"
  echo "  checkpoint fingerprint: ${last_ckpt_fp}"
  echo "action: resume will proceed with matching device profile"
else
  echo "status: INCOMPATIBLE"
  echo "  current fingerprint:    ${fingerprint}"
  echo "  checkpoint fingerprint: ${last_ckpt_fp}"
  echo "action: resume will be REJECTED (fail-closed)"
  echo "  The checkpoint was saved with a different device profile."
  echo "  To resume, either:"
  echo "    1. Change spec.devices to match the checkpoint's device profile"
  echo "    2. Start fresh without resuming from this checkpoint"
fi
echo

# Checkpoint summary from RTJ status.
echo "--- Checkpoint summary ---"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'latestCheckpoint: {.status.checkpoint.latestCheckpointRef}\ncheckpointsDiscovered: {.status.checkpoint.checkpointsDiscovered}\nlastCheckpointTime: {.status.checkpoint.lastCheckpointTime}\n' 2>/dev/null || echo "<not available>"
echo

# Checkpoint store contents (via minio).
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

# Conditions related to checkpoints.
echo "--- Checkpoint conditions ---"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{range .status.conditions[*]}  {.type}={.status} ({.reason}): {.message}{"\n"}{end}' 2>/dev/null | grep -iE 'checkpoint|degraded|compatible|device' || echo "  (no checkpoint-related conditions)"
