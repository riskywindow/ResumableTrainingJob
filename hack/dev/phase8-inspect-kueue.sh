#!/usr/bin/env bash
#
# Inspect Phase 8 Kueue accounting for DRA-backed RTJs.
#
# Shows: ClusterQueue usage including deviceClassMappings-resolved resources,
# Workload admission status, LocalQueue pending/admitted counts, and
# Kueue config for deviceClassMappings.
#
# Usage:
#   make phase8-inspect-kueue
#   PHASE8_RTJ_NAME=my-job make phase8-inspect-kueue

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE8_RTJ_NAME:-${RTJ_NAME:-phase8-demo}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== Phase 8 Kueue Accounting ==="
echo

# ClusterQueue status.
echo "--- ClusterQueue: phase8-cq ---"
kubectl get clusterqueues.kueue.x-k8s.io phase8-cq -o wide 2>/dev/null || echo "  (not found)"
echo

echo "resource usage:"
kubectl get clusterqueues.kueue.x-k8s.io phase8-cq \
  -o jsonpath='{range .status.flavorsReservation[*].resources[*]}  {.name}: used={.total}{"\n"}{end}' 2>/dev/null || echo "  <none>"
echo

echo "admission counts:"
pending="$(kubectl get clusterqueues.kueue.x-k8s.io phase8-cq \
  -o jsonpath='{.status.pendingWorkloads}' 2>/dev/null || echo "0")"
admitted="$(kubectl get clusterqueues.kueue.x-k8s.io phase8-cq \
  -o jsonpath='{.status.admittedWorkloads}' 2>/dev/null || echo "0")"
reserving="$(kubectl get clusterqueues.kueue.x-k8s.io phase8-cq \
  -o jsonpath='{.status.reservingWorkloads}' 2>/dev/null || echo "0")"
echo "  pending:   ${pending}"
echo "  admitted:  ${admitted}"
echo "  reserving: ${reserving}"
echo

# LocalQueue status.
echo "--- LocalQueue: phase8-training ---"
kubectl -n "$DEV_NAMESPACE" get localqueues.kueue.x-k8s.io phase8-training -o wide 2>/dev/null || echo "  (not found)"
echo

# Workload for the specified RTJ.
echo "--- Workload for ${RTJ_NAME} ---"
wl_name="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io \
  -l "training.checkpoint.example.io/rtj-name=${RTJ_NAME}" \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
if [[ -n "$wl_name" ]]; then
  echo "workload: ${wl_name}"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$wl_name" -o wide 2>/dev/null || true
  echo
  echo "admission:"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$wl_name" \
    -o jsonpath='  clusterQueue: {.status.admission.clusterQueue}{"\n"}  podSetAssignments:{"\n"}{range .status.admission.podSetAssignments[*]}    {.name}: {.flavors}{"\n"}{end}' 2>/dev/null || echo "  <not admitted>"
  echo
  echo "conditions:"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$wl_name" \
    -o jsonpath='{range .status.conditions[*]}  {.type}={.status} ({.reason}){"\n"}{end}' 2>/dev/null || echo "  <none>"
else
  echo "  (no Workload found for ${RTJ_NAME})"
fi
echo

# deviceClassMappings from Kueue config.
echo "--- deviceClassMappings ---"
config_yaml="$(current_kueue_manager_config 2>/dev/null || true)"
if [[ -n "$config_yaml" ]]; then
  printf '%s\n' "$config_yaml" | grep -A5 'deviceClassMappings' || echo "  (not configured)"
else
  echo "  (Kueue config not accessible)"
fi
echo

# All workloads in the namespace.
echo "--- All Workloads in ${DEV_NAMESPACE} ---"
kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io -o wide 2>/dev/null || echo "  (none)"
