#!/usr/bin/env bash
#
# Inspect ProvisioningRequest objects associated with a Phase 7 RTJ.
# Shows: ProvisioningRequest name, class, conditions, parameters, and
# the corresponding Workload AdmissionCheck state.
#
# Usage:
#   PHASE7_RTJ_NAME=phase7-demo make phase7-inspect-provisioningrequest
#   PHASE7_RTJ_NAME=phase7-demo ./hack/dev/phase7-inspect-provisioningrequest.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE7_RTJ_NAME:-${RTJ_NAME:-phase7-demo}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== RTJ provisioning status ==="
prov_state="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.provisioning.provisioningState}' 2>/dev/null || true)"
if [[ -n "${prov_state}" ]]; then
  echo "provisioningState: ${prov_state}"
  pr_ref="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.provisioning.provisioningRequestRef}' 2>/dev/null || true)"
  echo "provisioningRequestRef: ${pr_ref:-<not set>}"
else
  echo "<provisioning not configured or not yet evaluated>"
fi
echo

echo "=== ProvisioningRequest objects in namespace ==="
pr_list="$(kubectl -n "$DEV_NAMESPACE" get provisioningrequests.autoscaling.x-k8s.io --no-headers 2>/dev/null || true)"
if [[ -n "${pr_list}" ]]; then
  echo "$pr_list"
else
  echo "<none>"
fi
echo

# Find the Workload for this RTJ and inspect its associated PRs.
workload_name="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io \
  -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"${RTJ_NAME}\")]}{.metadata.name}{end}" 2>/dev/null || true)"

if [[ -n "${workload_name}" ]]; then
  echo "=== Workload AdmissionChecks ==="
  echo "workload: ${workload_name}"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
    -o jsonpath='{range .status.admissionChecks[*]}  name={.name}  state={.state}  message={.message}{"\n"}{end}' 2>/dev/null || echo "  <none>"
  echo

  # Attempt to find the ProvisioningRequest matching the workload naming convention.
  echo "=== ProvisioningRequest details ==="
  for pr_name in $(kubectl -n "$DEV_NAMESPACE" get provisioningrequests.autoscaling.x-k8s.io \
    --no-headers -o custom-columns='NAME:.metadata.name' 2>/dev/null || true); do
    if [[ "$pr_name" == "${workload_name}"* ]]; then
      echo "ProvisioningRequest: ${pr_name}"
      echo
      echo "  spec.provisioningClassName:"
      kubectl -n "$DEV_NAMESPACE" get provisioningrequests.autoscaling.x-k8s.io "$pr_name" \
        -o jsonpath='{.spec.provisioningClassName}' 2>/dev/null || true
      echo
      echo
      echo "  spec.parameters:"
      kubectl -n "$DEV_NAMESPACE" get provisioningrequests.autoscaling.x-k8s.io "$pr_name" \
        -o jsonpath='{.spec.parameters}' 2>/dev/null || true
      echo
      echo
      echo "  conditions:"
      kubectl -n "$DEV_NAMESPACE" get provisioningrequests.autoscaling.x-k8s.io "$pr_name" \
        -o jsonpath='{range .status.conditions[*]}    {.type}={.status} ({.reason}): {.message}{"\n"}{end}' 2>/dev/null || echo "    <none>"
      echo
      echo "  creationTimestamp:"
      kubectl -n "$DEV_NAMESPACE" get provisioningrequests.autoscaling.x-k8s.io "$pr_name" \
        -o jsonpath='{.metadata.creationTimestamp}' 2>/dev/null || true
      echo
      echo
    fi
  done
else
  echo "<no Workload found for RTJ ${RTJ_NAME}>"
fi

echo "=== Fake-provisioner pod logs (last 10 lines) ==="
FP_POD="$(kubectl -n "$DEV_NAMESPACE" get pods -l app=fake-provisioner \
  --no-headers -o custom-columns='NAME:.metadata.name' 2>/dev/null | head -1 || true)"
if [[ -n "${FP_POD}" ]]; then
  kubectl -n "$DEV_NAMESPACE" logs "$FP_POD" --tail=10 2>/dev/null || echo "  <could not read logs>"
else
  echo "  <fake-provisioner pod not found>"
fi
