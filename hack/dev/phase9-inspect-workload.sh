#!/usr/bin/env bash
#
# Inspect RTJ and Kueue Workload status for a Phase 9 elastic RTJ.
#
# Shows: RTJ status overview (phase, conditions), Kueue Workload admission
# state and reclaimablePods, ClusterQueue quota usage, and pending workloads
# (to observe the dynamic reclaim scenario where RTJ A shrinks and RTJ B
# is subsequently admitted).
#
# Usage:
#   make phase9-inspect-workload
#   PHASE9_SHRINK_RTJ_NAME=my-job make phase9-inspect-workload
#   RTJ_NAME=my-job make phase9-inspect-workload

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE9_SHRINK_RTJ_NAME:-${PHASE9_GROW_RTJ_NAME:-${RTJ_NAME:-phase9-elastic-a}}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== Phase 9 RTJ + Workload Status: ${DEV_NAMESPACE}/${RTJ_NAME} ==="
echo

# ── RTJ status overview ──────────────────────────────────────────────────────

echo "--- RTJ status overview ---"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'  phase={.status.phase}\n  currentRunAttempt={.status.currentRunAttempt}\n  activeJobSet={.status.activeJobSetName}\n' 2>/dev/null || echo "  <RTJ not found>"
echo

echo "elasticity status:"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'  resizeState={.status.elasticity.resizeState}\n  resizePath={.status.elasticity.resizePath}\n  admittedWorkerCount={.status.elasticity.admittedWorkerCount}\n  targetWorkerCount={.status.elasticity.targetWorkerCount}\n  reclaimablePodsPublished={.status.elasticity.reclaimablePodsPublished}\n' 2>/dev/null || echo "  <not set>"
echo

echo "RTJ conditions:"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{range .status.conditions[*]}  {.type}={.status} ({.reason}): {.message}{"\n"}{end}' 2>/dev/null || echo "  <none>"
echo

# ── Kueue Workload ───────────────────────────────────────────────────────────

echo "--- Kueue Workload ---"
wl_name="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io \
  -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"${RTJ_NAME}\")]}{.metadata.name}{end}" 2>/dev/null || true)"
if [[ -n "${wl_name}" ]]; then
  echo "  workload: ${wl_name}"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$wl_name" -o wide 2>/dev/null || true
  echo

  echo "admission:"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$wl_name" \
    -o jsonpath='  clusterQueue: {.status.admission.clusterQueue}{"\n"}{range .status.admission.podSetAssignments[*]}  podSet={.name}: count={.count}  flavors={.flavors}{"\n"}{end}' 2>/dev/null || echo "  <not admitted>"
  echo

  echo "reclaimablePods:"
  reclaimable="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$wl_name" \
    -o jsonpath='{.status.reclaimablePods}' 2>/dev/null || true)"
  if [[ -n "${reclaimable}" ]]; then
    kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$wl_name" \
      -o jsonpath='{range .status.reclaimablePods[*]}  {.name}: count={.count}{"\n"}{end}' 2>/dev/null || true
  else
    echo "  <not published — shrink not triggered or in-place shrink not used>"
  fi
  echo

  echo "Workload conditions:"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$wl_name" \
    -o jsonpath='{range .status.conditions[*]}  {.type}={.status} ({.reason}){"\n"}{end}' 2>/dev/null || echo "  <none>"
else
  echo "  <no Workload found for ${RTJ_NAME}>"
fi
echo

# ── ClusterQueue quota usage ─────────────────────────────────────────────────

echo "--- ClusterQueue: phase9-cq (quota usage) ---"
kubectl get clusterqueues.kueue.x-k8s.io phase9-cq -o wide 2>/dev/null || echo "  (not found)"
echo

echo "resource usage vs quota:"
kubectl get clusterqueues.kueue.x-k8s.io phase9-cq \
  -o jsonpath='{range .status.flavorsUsage[*]}flavor={.name}:{"\n"}{range .resources[*]}  {.name}: used={.total} / nominal={.borrowingLimit}{"\n"}{end}{end}' 2>/dev/null || true

# Fallback: show flavorsReservation if flavorsUsage is empty.
kubectl get clusterqueues.kueue.x-k8s.io phase9-cq \
  -o jsonpath='{range .status.flavorsReservation[*]}reservation/{.name}:{"\n"}{range .resources[*]}  {.name}: total={.total}{"\n"}{end}{end}' 2>/dev/null || echo "  <none>"
echo

echo "admission counts:"
pending="$(kubectl get clusterqueues.kueue.x-k8s.io phase9-cq \
  -o jsonpath='{.status.pendingWorkloads}' 2>/dev/null || echo "0")"
admitted="$(kubectl get clusterqueues.kueue.x-k8s.io phase9-cq \
  -o jsonpath='{.status.admittedWorkloads}' 2>/dev/null || echo "0")"
reserving="$(kubectl get clusterqueues.kueue.x-k8s.io phase9-cq \
  -o jsonpath='{.status.reservingWorkloads}' 2>/dev/null || echo "0")"
echo "  pending:   ${pending}"
echo "  admitted:  ${admitted}"
echo "  reserving: ${reserving}"
echo

# ── All workloads in namespace ───────────────────────────────────────────────

echo "--- All Workloads in ${DEV_NAMESPACE} ---"
kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io -o wide 2>/dev/null || echo "  (none)"
