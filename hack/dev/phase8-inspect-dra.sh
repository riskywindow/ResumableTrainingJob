#!/usr/bin/env bash
#
# Inspect Phase 8 DRA status for an RTJ.
#
# Shows: device mode, device profile fingerprint, ResourceClaimTemplate
# ownership, ResourceClaim allocation state, DeviceClass, ResourceSlices.
#
# Usage:
#   make phase8-inspect-dra
#   PHASE8_RTJ_NAME=my-job make phase8-inspect-dra

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE8_RTJ_NAME:-${RTJ_NAME:-phase8-demo}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== RTJ DRA status: ${DEV_NAMESPACE}/${RTJ_NAME} ==="
echo

# Core RTJ status.
echo "--- RTJ summary ---"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'phase={.status.phase}\ncurrentRunAttempt={.status.currentRunAttempt}\nactiveJobSet={.status.activeJobSetName}\n' 2>/dev/null || echo "<RTJ not found>"
echo

# Device status from RTJ.
echo "--- Device status ---"
device_mode="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.devices.deviceMode}' 2>/dev/null || true)"
echo "deviceMode: ${device_mode:-<not set>}"

fingerprint="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.devices.currentDeviceProfileFingerprint}' 2>/dev/null || true)"
echo "currentDeviceProfileFingerprint: ${fingerprint:-<not set>}"

classes="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.devices.requestedDeviceClasses}' 2>/dev/null || true)"
echo "requestedDeviceClasses: ${classes:-<not set>}"

alloc_state="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.devices.claimAllocationState}' 2>/dev/null || true)"
echo "claimAllocationState: ${alloc_state:-<not set>}"

alloc_count="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.devices.allocatedClaimCount}' 2>/dev/null || true)"
echo "allocatedClaimCount: ${alloc_count:-0}"

failure_reason="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.devices.lastClaimFailureReason}' 2>/dev/null || true)"
if [[ -n "${failure_reason}" ]]; then
  echo "lastClaimFailureReason: ${failure_reason}"
fi

echo
echo "resourceClaimTemplateRefs:"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{range .status.devices.resourceClaimTemplateRefs[*]}  {.name} -> {.claimName}{"\n"}{end}' 2>/dev/null || echo "  <none>"
echo

# Checkpoint device profile fingerprints.
echo "--- Checkpoint device fingerprints ---"
last_ckpt_fp="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.devices.lastCheckpointDeviceProfileFingerprint}' 2>/dev/null || true)"
echo "lastCheckpointDeviceProfileFingerprint: ${last_ckpt_fp:-<not set>}"

last_resume_fp="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.devices.lastResumeDeviceProfileFingerprint}' 2>/dev/null || true)"
echo "lastResumeDeviceProfileFingerprint: ${last_resume_fp:-<not set>}"
echo

# ResourceClaimTemplates owned by this RTJ.
echo "--- ResourceClaimTemplates ---"
kubectl -n "$DEV_NAMESPACE" get resourceclaimtemplates \
  -l "training.checkpoint.example.io/rtj-name=${RTJ_NAME}" \
  -o wide 2>/dev/null || echo "  (none)"
echo

# Show template spec for each.
for tpl_name in $(kubectl -n "$DEV_NAMESPACE" get resourceclaimtemplates \
  -l "training.checkpoint.example.io/rtj-name=${RTJ_NAME}" \
  -o jsonpath='{.items[*].metadata.name}' 2>/dev/null); do
  echo "  template/${tpl_name}:"
  kubectl -n "$DEV_NAMESPACE" get resourceclaimtemplate "$tpl_name" \
    -o jsonpath='    deviceClassName: {.spec.spec.devices.requests[0].deviceClassName}{"\n"}    count: {.spec.spec.devices.requests[0].count}{"\n"}    allocationMode: {.spec.spec.devices.requests[0].allocationMode}{"\n"}' 2>/dev/null || true
  echo
done

# ResourceClaims associated with this RTJ.
echo "--- ResourceClaims ---"
kubectl -n "$DEV_NAMESPACE" get resourceclaims \
  -l "training.checkpoint.example.io/rtj-name=${RTJ_NAME}" \
  -o wide 2>/dev/null || echo "  (none — claims are created by the scheduler when pods are bound)"
echo

# DeviceClass.
echo "--- DeviceClass ---"
kubectl get deviceclasses.resource.k8s.io -o wide 2>/dev/null || echo "  (none)"
echo

# ResourceSlices summary.
echo "--- ResourceSlices ---"
kubectl get resourceslices -l app.kubernetes.io/managed-by=dra-example-driver \
  -o wide 2>/dev/null || echo "  (none)"
echo

# Conditions.
echo "--- Phase 8 conditions ---"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{range .status.conditions[*]}  {.type}={.status} ({.reason}): {.message}{"\n"}{end}' 2>/dev/null || echo "  <none>"
