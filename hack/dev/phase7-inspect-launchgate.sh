#!/usr/bin/env bash
#
# Inspect Phase 7 launch gate status for an RTJ.
# Shows: launch gate state, per-AC summary, provisioning state,
# startup/recovery state, and capacity guarantee indicator.
#
# Usage:
#   PHASE7_RTJ_NAME=phase7-demo make phase7-inspect-launchgate
#   PHASE7_RTJ_NAME=phase7-demo ./hack/dev/phase7-inspect-launchgate.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE7_RTJ_NAME:-${RTJ_NAME:-phase7-demo}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== RTJ status ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'phase={.status.phase}\ncurrentRunAttempt={.status.currentRunAttempt}\nactiveJobSet={.status.activeJobSetName}\n' 2>/dev/null || echo "<RTJ not found>"
echo

echo "=== Launch gate ==="
gate_state="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.launchGate.launchGateState}' 2>/dev/null || true)"
if [[ -n "${gate_state}" ]]; then
  echo "launchGateState: ${gate_state}"
  message="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.launchGate.message}' 2>/dev/null || true)"
  echo "message: ${message:-<not set>}"
  echo
  echo "admissionCheckSummary:"
  kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{range .status.launchGate.admissionCheckSummary[*]}  {.name}: {.state}{"\n"}{end}' 2>/dev/null || echo "  <none>"
else
  echo "<not populated — provisioning may not be configured>"
fi
echo

echo "=== Provisioning status ==="
prov_state="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.provisioning.provisioningState}' 2>/dev/null || true)"
if [[ -n "${prov_state}" ]]; then
  echo "provisioningState: ${prov_state}"
  pr_ref="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.provisioning.provisioningRequestRef}' 2>/dev/null || true)"
  echo "provisioningRequestRef: ${pr_ref:-<not set>}"
  attempt="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.provisioning.provisioningAttempt}' 2>/dev/null || true)"
  echo "provisioningAttempt: ${attempt:-<not set>}"
else
  echo "<not populated>"
fi
echo

echo "=== Startup/recovery status ==="
startup_state="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.startupRecovery.startupState}' 2>/dev/null || true)"
if [[ -n "${startup_state}" ]]; then
  echo "startupState: ${startup_state}"
  pods_ready="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.startupRecovery.podsReadyState}' 2>/dev/null || true)"
  echo "podsReadyState: ${pods_ready:-<not set>}"
  eviction_reason="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.startupRecovery.evictionReason}' 2>/dev/null || true)"
  echo "evictionReason: ${eviction_reason:-<none>}"
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

echo "=== Phase 7 conditions ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{range .status.conditions[*]}  {.type}={.status} ({.reason}): {.message}{"\n"}{end}' 2>/dev/null || echo "  <none>"
