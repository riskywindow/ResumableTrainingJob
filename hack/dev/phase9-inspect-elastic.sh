#!/usr/bin/env bash
#
# Inspect Phase 9 elastic resize status for an RTJ.
#
# Shows: RTJ elasticity spec and status (resizeState, resizePath,
# admittedWorkerCount, targetWorkerCount, reclaimablePodsPublished,
# executionMode), Workload reclaimablePods, ClusterQueue usage, and
# active vs target worker counts.
#
# Usage:
#   make phase9-inspect-elastic
#   PHASE9_SHRINK_RTJ_NAME=my-job make phase9-inspect-elastic
#   RTJ_NAME=my-job make phase9-inspect-elastic

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE9_SHRINK_RTJ_NAME:-${PHASE9_GROW_RTJ_NAME:-${RTJ_NAME:-phase9-elastic-a}}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== Phase 9 Elastic Resize Status: ${DEV_NAMESPACE}/${RTJ_NAME} ==="
echo

# ── RTJ overview ────────────────────────────────────────────────────────────

echo "--- RTJ summary ---"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'  phase={.status.phase}\n  currentRunAttempt={.status.currentRunAttempt}\n  activeJobSet={.status.activeJobSetName}\n' 2>/dev/null || echo "  <RTJ not found>"
echo

# ── Elasticity spec ──────────────────────────────────────────────────────────

echo "--- Elasticity spec ---"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'  mode={.spec.elasticity.mode}\n  targetWorkerCount={.spec.elasticity.targetWorkerCount}\n  inPlaceShrinkPolicy={.spec.elasticity.inPlaceShrinkPolicy}\n  reclaimMode={.spec.elasticity.reclaimMode}\n' 2>/dev/null || echo "  <not set>"
echo

# ── Elasticity status ────────────────────────────────────────────────────────

echo "--- Elasticity status ---"
resize_state="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.elasticity.resizeState}' 2>/dev/null || true)"
echo "  resizeState:              ${resize_state:-<not set>}"

resize_path="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.elasticity.resizePath}' 2>/dev/null || true)"
echo "  resizePath:               ${resize_path:-<not set>}"

admitted_count="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.elasticity.admittedWorkerCount}' 2>/dev/null || true)"
echo "  admittedWorkerCount:      ${admitted_count:-<not set>}"

target_count="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.elasticity.targetWorkerCount}' 2>/dev/null || true)"
echo "  targetWorkerCount:        ${target_count:-<not set>}"

reclaim_published="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.elasticity.reclaimablePodsPublished}' 2>/dev/null || true)"
echo "  reclaimablePodsPublished: ${reclaim_published:-<not set>}"

exec_mode="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.elasticity.executionMode}' 2>/dev/null || true)"
echo "  executionMode:            ${exec_mode:-<not set>}"

echo

# ── Active vs target worker count ────────────────────────────────────────────

echo "--- Active vs target workers ---"
active_jobset="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.activeJobSetName}' 2>/dev/null || true)"
if [[ -n "${active_jobset}" ]]; then
  active_replicas="$(kubectl -n "$DEV_NAMESPACE" get jobset "$active_jobset" \
    -o jsonpath='{.spec.replicatedJobs[0].replicas}' 2>/dev/null || true)"
  echo "  activeJobSet:    ${active_jobset}"
  echo "  activeReplicas:  ${active_replicas:-<unknown>}"
  echo "  targetWorkers:   ${target_count:-<not set>}"
  echo
  echo "  worker pods:"
  kubectl -n "$DEV_NAMESPACE" get pods \
    -l "jobset.sigs.k8s.io/jobset-name=${active_jobset}" \
    -o custom-columns='NAME:.metadata.name,NODE:.spec.nodeName,STATUS:.status.phase,READY:.status.containerStatuses[0].ready' 2>/dev/null || echo "  <no pods>"
else
  echo "  <no active child JobSet>"
fi
echo

# ── Workload reclaimablePods ─────────────────────────────────────────────────

echo "--- Workload reclaimablePods ---"
wl_name="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io \
  -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"${RTJ_NAME}\")]}{.metadata.name}{end}" 2>/dev/null || true)"
if [[ -n "${wl_name}" ]]; then
  echo "  workload: ${wl_name}"
  reclaimable="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$wl_name" \
    -o jsonpath='{.status.reclaimablePods}' 2>/dev/null || true)"
  if [[ -n "${reclaimable}" ]]; then
    kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$wl_name" \
      -o jsonpath='{range .status.reclaimablePods[*]}  {.name}: count={.count}{"\n"}{end}' 2>/dev/null || true
  else
    echo "  reclaimablePods: <not published>"
  fi
else
  echo "  <no Workload found for ${RTJ_NAME}>"
fi
echo

# ── ClusterQueue usage ───────────────────────────────────────────────────────

echo "--- ClusterQueue: phase9-cq ---"
kubectl get clusterqueues.kueue.x-k8s.io phase9-cq -o wide 2>/dev/null || echo "  (not found)"
echo

echo "resource usage:"
kubectl get clusterqueues.kueue.x-k8s.io phase9-cq \
  -o jsonpath='{range .status.flavorsReservation[*].resources[*]}  {.name}: used={.total}{"\n"}{end}' 2>/dev/null || echo "  <none>"
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

# ── RTJ conditions ───────────────────────────────────────────────────────────

echo "--- RTJ conditions ---"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{range .status.conditions[*]}  {.type}={.status} ({.reason}): {.message}{"\n"}{end}' 2>/dev/null || echo "  <none>"
