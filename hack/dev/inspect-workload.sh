#!/usr/bin/env bash
#
# Inspect RTJ and Kueue Workload status for a Phase 4 RTJ.
# Shows: RTJ phase, launch readiness, effective launch shape, admission status,
# Kueue Workload admission payload with admission checks, and child JobSet.
#
# Usage:
#   PHASE4_RTJ_NAME=phase4-demo make phase4-inspect-workload
#   PHASE4_RTJ_NAME=phase4-demo ./hack/dev/inspect-workload.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE4_RTJ_NAME:-${RTJ_NAME:-phase4-demo}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== RTJ status ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'phase={.status.phase}\ncurrentRunAttempt={.status.currentRunAttempt}\nactiveJobSet={.status.activeJobSetName}\n' 2>/dev/null || echo "<RTJ not found>"
echo

echo "=== Launch readiness ==="
ready="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.launchReadiness.ready}' 2>/dev/null || true)"
if [[ -n "${ready}" ]]; then
  gate_state="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.launchReadiness.gateState}' 2>/dev/null || true)"
  reason="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.launchReadiness.reason}' 2>/dev/null || true)"
  message="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.launchReadiness.message}' 2>/dev/null || true)"
  echo "ready: ${ready}"
  echo "gateState: ${gate_state:-<not set>}"
  echo "reason: ${reason:-<not set>}"
  echo "message: ${message:-<not set>}"
else
  echo "<not populated — Phase 3 path or not yet evaluated>"
fi
echo

echo "=== Effective launch shape ==="
worker_count="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.effectiveLaunchShape.workerCount}' 2>/dev/null || true)"
if [[ -n "${worker_count}" ]]; then
  world_size="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.effectiveLaunchShape.worldSize}' 2>/dev/null || true)"
  resume_mode="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.effectiveLaunchShape.resumeMode}' 2>/dev/null || true)"
  ckpt_id="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.status.effectiveLaunchShape.selectedCheckpointID}' 2>/dev/null || true)"
  echo "workerCount: ${worker_count}"
  echo "worldSize: ${world_size:-<not set>}"
  echo "resumeMode: ${resume_mode:-<not set>}"
  echo "selectedCheckpointID: ${ckpt_id:-<not set>}"
else
  echo "<not populated>"
fi
echo

echo "=== Admission status ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'admittedWorkerCount={.status.admission.admittedWorkerCount}\npreferredWorkerCount={.status.admission.preferredWorkerCount}\nadmittedFlavors={.status.admission.admittedFlavors}\n' 2>/dev/null || echo "<not set>"
echo

echo "=== Kueue Workload ==="
workload_name="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io \
  -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"${RTJ_NAME}\")]}{.metadata.name}{end}" 2>/dev/null || true)"
if [[ -n "${workload_name}" ]]; then
  echo "workload: ${workload_name}"
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
  echo "admissionChecks:"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
    -o jsonpath='{range .status.admissionChecks[*]}  name={.name}  state={.state}  message={.message}{"\n"}{end}' 2>/dev/null || echo "  <none>"
else
  echo "<no Workload found for RTJ ${RTJ_NAME}>"
fi
echo

echo "=== Child JobSet ==="
active_jobset="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.activeJobSetName}' 2>/dev/null || true)"
if [[ -n "${active_jobset}" ]]; then
  echo "child JobSet: ${active_jobset}"
  echo
  echo "replicas:"
  kubectl -n "$DEV_NAMESPACE" get jobset "$active_jobset" \
    -o jsonpath='{range .spec.replicatedJobs[*]}  {.name}: {.replicas}{"\n"}{end}' 2>/dev/null || true
  echo
  echo "pods:"
  kubectl -n "$DEV_NAMESPACE" get pods -l "jobset.sigs.k8s.io/jobset-name=${active_jobset}" \
    -o custom-columns='NAME:.metadata.name,NODE:.spec.nodeName,STATUS:.status.phase' 2>/dev/null || echo "  <no pods>"
else
  echo "<no active child JobSet>"
fi
