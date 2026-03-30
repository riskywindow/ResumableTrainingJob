#!/usr/bin/env bash
#
# Inspect RTJ and Kueue Workload status for a Phase 5 RTJ.
# Extends the Phase 4 inspect-workload.sh with priority shaping details.
#
# Shows: RTJ phase, priority shaping state, Workload priority,
# admission status, preemption conditions, and child JobSet.
#
# Usage:
#   PHASE5_RTJ_NAME=phase5-low-demo make phase5-inspect-workload
#   RTJ_NAME=phase5-low-demo ./hack/dev/inspect-workload-phase5.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE5_RTJ_NAME:-${RTJ_NAME:-phase5-low-demo}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== RTJ status ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'phase={.status.phase}\ncurrentRunAttempt={.status.currentRunAttempt}\nactiveJobSet={.status.activeJobSetName}\n' 2>/dev/null || echo "<RTJ not found>"
echo

echo "=== Priority shaping ==="
base="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.basePriority}' 2>/dev/null || true)"
effective="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.effectivePriority}' 2>/dev/null || true)"
state="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.preemptionState}' 2>/dev/null || true)"
reason="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.preemptionStateReason}' 2>/dev/null || true)"
if [[ -n "${state}" ]]; then
  echo "basePriority: ${base}"
  echo "effectivePriority: ${effective}"
  echo "preemptionState: ${state}"
  echo "reason: ${reason}"
else
  echo "<no priority shaping active — Phase 4 behavior>"
fi
echo

echo "=== Kueue Workload ==="
workload_name="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io \
  -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"${RTJ_NAME}\")]}{.metadata.name}{end}" 2>/dev/null || true)"
if [[ -n "${workload_name}" ]]; then
  echo "workload: ${workload_name}"
  wl_priority="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
    -o jsonpath='{.spec.priority}' 2>/dev/null || true)"
  wl_priority_class="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
    -o jsonpath='{.spec.priorityClassName}' 2>/dev/null || true)"
  echo "spec.priority: ${wl_priority:-<not set>}"
  echo "spec.priorityClassName: ${wl_priority_class:-<not set>}"
  echo
  echo "admission.clusterQueue:"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
    -o jsonpath='{.status.admission.clusterQueue}' 2>/dev/null || true
  echo
  echo
  echo "podSetAssignments:"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
    -o jsonpath='{range .status.admission.podSetAssignments[*]}  name={.name}  count={.count}  flavors={.flavors}{"\n"}{end}' 2>/dev/null || true
  echo
  echo "conditions:"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
    -o jsonpath='{range .status.conditions[*]}  type={.type}  status={.status}  reason={.reason}{"\n"}{end}' 2>/dev/null || echo "  <none>"
else
  echo "<no Workload found for RTJ ${RTJ_NAME}>"
fi
echo

echo "=== RTJ conditions ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{range .status.conditions[*]}  type={.type}  status={.status}  reason={.reason}  message={.message}{"\n"}{end}' 2>/dev/null || echo "  <none>"
echo

echo "=== Child JobSet ==="
active_jobset="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.activeJobSetName}' 2>/dev/null || true)"
if [[ -n "${active_jobset}" ]]; then
  echo "child JobSet: ${active_jobset}"
  echo
  echo "pods:"
  kubectl -n "$DEV_NAMESPACE" get pods -l "jobset.sigs.k8s.io/jobset-name=${active_jobset}" \
    -o custom-columns='NAME:.metadata.name,NODE:.spec.nodeName,STATUS:.status.phase' 2>/dev/null || echo "  <no pods>"
else
  echo "<no active child JobSet>"
fi
