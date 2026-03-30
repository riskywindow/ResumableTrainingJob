#!/usr/bin/env bash
#
# Inspect RTJ base priority, effective priority, preemption state, and
# priority shaping status for a Phase 5 RTJ.
#
# Shows: base vs effective priority, preemption state, protection window,
# checkpoint freshness evidence, yield budget, and Workload.Spec.Priority.
#
# Usage:
#   PHASE5_RTJ_NAME=phase5-low-demo make phase5-inspect-priority
#   RTJ_NAME=phase5-low-demo ./hack/dev/inspect-priority.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE5_RTJ_NAME:-${RTJ_NAME:-phase5-low-demo}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== RTJ priority shaping overview ==="
phase="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.phase}' 2>/dev/null || true)"
echo "rtj: ${DEV_NAMESPACE}/${RTJ_NAME}"
echo "phase: ${phase:-<not found>}"
echo

echo "=== Base vs effective priority ==="
base="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.basePriority}' 2>/dev/null || true)"
effective="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.effectivePriority}' 2>/dev/null || true)"
ep_annotation="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.metadata.annotations.training\.checkpoint\.example\.io/effective-priority}' 2>/dev/null || true)"
echo "basePriority: ${base:-<not set>}"
echo "effectivePriority: ${effective:-<not set>}"
echo "effective-priority annotation: ${ep_annotation:-<not set>}"
if [[ -n "${base}" && -n "${effective}" ]]; then
  delta=$((effective - base))
  echo "delta (effective - base): ${delta}"
fi
echo

echo "=== Preemption state ==="
state="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.preemptionState}' 2>/dev/null || true)"
reason="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.preemptionStateReason}' 2>/dev/null || true)"
state_annotation="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.metadata.annotations.training\.checkpoint\.example\.io/preemption-state}' 2>/dev/null || true)"
echo "preemptionState: ${state:-<not set>}"
echo "preemptionStateReason: ${reason:-<not set>}"
echo "preemption-state annotation: ${state_annotation:-<not set>}"
echo

echo "=== Protection window ==="
protected_until="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.protectedUntil}' 2>/dev/null || true)"
echo "protectedUntil: ${protected_until:-<not active>}"
echo

echo "=== Checkpoint freshness ==="
ckpt_time="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.lastCompletedCheckpointTime}' 2>/dev/null || true)"
ckpt_age="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.checkpointAge}' 2>/dev/null || true)"
echo "lastCompletedCheckpointTime: ${ckpt_time:-<none>}"
echo "checkpointAge: ${ckpt_age:-<none>}"
echo

echo "=== Yield budget ==="
yield_time="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.lastYieldTime}' 2>/dev/null || true)"
resume_time="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.lastResumeTime}' 2>/dev/null || true)"
yield_count="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.recentYieldCount}' 2>/dev/null || true)"
echo "lastYieldTime: ${yield_time:-<none>}"
echo "lastResumeTime: ${resume_time:-<none>}"
echo "recentYieldCount: ${yield_count:-0}"
echo

echo "=== Applied policy ==="
applied_ref="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.priorityShaping.appliedPolicyRef}' 2>/dev/null || true)"
spec_ref="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.spec.priorityPolicyRef.name}' 2>/dev/null || true)"
echo "spec.priorityPolicyRef: ${spec_ref:-<not set>}"
echo "status.appliedPolicyRef: ${applied_ref:-<not set>}"
echo

echo "=== PriorityShaping condition ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{range .status.conditions[?(@.type=="PriorityShaping")]}type={.type}  status={.status}  reason={.reason}  message={.message}{"\n"}{end}' 2>/dev/null || echo "  <no PriorityShaping condition>"
echo

echo "=== Workload.Spec.Priority ==="
workload_name="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io \
  -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"${RTJ_NAME}\")]}{.metadata.name}{end}" 2>/dev/null || true)"
if [[ -n "${workload_name}" ]]; then
  wl_priority="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
    -o jsonpath='{.spec.priority}' 2>/dev/null || true)"
  echo "workload: ${workload_name}"
  echo "spec.priority: ${wl_priority:-<not set>}"
else
  echo "<no Workload found for RTJ ${RTJ_NAME}>"
fi
