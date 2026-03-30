#!/usr/bin/env bash
#
# Inspect the CheckpointPriorityPolicy attached to a Phase 5 RTJ.
#
# Shows: policy spec including timing windows, priority adjustments,
# fail-open controls, yield budget, and priority clamping bounds.
#
# Usage:
#   make phase5-inspect-policy
#   POLICY_NAME=dev-checkpoint-priority ./hack/dev/inspect-policy.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE5_RTJ_NAME:-${RTJ_NAME:-phase5-low-demo}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"
POLICY_RESOURCE="checkpointprioritypolicies.training.checkpoint.example.io"

# Resolve the policy name from the RTJ if POLICY_NAME is not explicitly set.
POLICY_NAME="${POLICY_NAME:-}"
if [[ -z "${POLICY_NAME}" ]]; then
  POLICY_NAME="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.spec.priorityPolicyRef.name}' 2>/dev/null || true)"
fi

if [[ -z "${POLICY_NAME}" ]]; then
  echo "no CheckpointPriorityPolicy found for RTJ ${RTJ_NAME}" >&2
  echo "set POLICY_NAME or ensure the RTJ has spec.priorityPolicyRef" >&2
  exit 1
fi

echo "=== CheckpointPriorityPolicy: ${POLICY_NAME} ==="
kubectl get "$POLICY_RESOURCE" "$POLICY_NAME" -o wide 2>/dev/null || {
  echo "<policy ${POLICY_NAME} not found>"
  exit 1
}
echo

echo "=== Timing windows ==="
kubectl get "$POLICY_RESOURCE" "$POLICY_NAME" \
  -o jsonpath=$'startupProtectionWindow: {.spec.startupProtectionWindow}\ncheckpointFreshnessTarget: {.spec.checkpointFreshnessTarget}\nminRuntimeBetweenYields: {.spec.minRuntimeBetweenYields}\n' 2>/dev/null
echo

echo "=== Priority adjustments ==="
kubectl get "$POLICY_RESOURCE" "$POLICY_NAME" \
  -o jsonpath=$'protectedBoost: {.spec.protectedBoost}\ncooldownBoost: {.spec.cooldownBoost}\nstaleCheckpointBoost: {.spec.staleCheckpointBoost}\npreemptibleOffset: {.spec.preemptibleOffset}\n' 2>/dev/null
echo

echo "=== Fail-open controls ==="
kubectl get "$POLICY_RESOURCE" "$POLICY_NAME" \
  -o jsonpath=$'failOpenOnTelemetryLoss: {.spec.failOpenOnTelemetryLoss}\nfailOpenOnCheckpointStoreErrors: {.spec.failOpenOnCheckpointStoreErrors}\n' 2>/dev/null
echo

echo "=== Yield budget ==="
kubectl get "$POLICY_RESOURCE" "$POLICY_NAME" \
  -o jsonpath=$'maxYieldsPerWindow: {.spec.maxYieldsPerWindow}\nyieldWindow: {.spec.yieldWindow}\n' 2>/dev/null
echo

echo "=== Priority clamping ==="
kubectl get "$POLICY_RESOURCE" "$POLICY_NAME" \
  -o jsonpath=$'minEffectivePriority: {.spec.minEffectivePriority}\nmaxEffectivePriority: {.spec.maxEffectivePriority}\n' 2>/dev/null
echo

echo
echo "=== RTJs referencing this policy ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" \
  -o jsonpath="{range .items[?(@.spec.priorityPolicyRef.name==\"${POLICY_NAME}\")]}{.metadata.name}  phase={.status.phase}  state={.status.priorityShaping.preemptionState}{\"\\n\"}{end}" 2>/dev/null || echo "  <none>"
