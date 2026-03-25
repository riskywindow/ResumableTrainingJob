#!/usr/bin/env bash
#
# Inspect the Phase 3 admission state for an RTJ.
# Shows: Kueue Workload admission payload, admitted flavors, effective worker
# counts, child JobSet nodeSelector/tolerations, and bridge annotation.
#
# Usage:
#   RTJ_NAME=phase3-demo make phase3-inspect-admission
#   RTJ_NAME=phase3-demo ./hack/dev/inspect-admission.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${RTJ_NAME:-$PHASE3_RTJ_NAME}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== RTJ admission status ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath=$'{.status.phase}\tadmitted={.status.admission.admittedWorkerCount}\tpreferred={.status.admission.preferredWorkerCount}\tflavors={.status.admission.admittedFlavors}\n' 2>/dev/null || echo "<RTJ not found>"
echo

echo "=== Bridge annotation (admitted-pod-sets) ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.metadata.annotations.training\.checkpoint\.example\.io/admitted-pod-sets}' 2>/dev/null || echo "<none>"
echo
echo

echo "=== Kueue Workload admission ==="
# Find the Workload owned by this RTJ.
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
else
  echo "<no Workload found for RTJ ${RTJ_NAME}>"
fi
echo

echo "=== ResourceFlavors ==="
kubectl get resourceflavors.kueue.x-k8s.io -o custom-columns='NAME:.metadata.name,NODE_LABELS:.spec.nodeLabels' 2>/dev/null || echo "<none>"
echo

echo "=== Child JobSet pod template ==="
active_jobset="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.activeJobSetName}' 2>/dev/null || true)"
if [[ -n "${active_jobset}" ]]; then
  echo "child JobSet: ${active_jobset}"
  echo
  echo "nodeSelector:"
  kubectl -n "$DEV_NAMESPACE" get jobset "$active_jobset" \
    -o jsonpath='{range .spec.replicatedJobs[*]}  {.name}: {.template.spec.template.spec.template.spec.nodeSelector}{"\n"}{end}' 2>/dev/null || true
  echo
  echo "tolerations:"
  kubectl -n "$DEV_NAMESPACE" get jobset "$active_jobset" \
    -o jsonpath='{range .spec.replicatedJobs[*]}  {.name}: {.template.spec.template.spec.template.spec.tolerations}{"\n"}{end}' 2>/dev/null || true
  echo
  echo "replicas:"
  kubectl -n "$DEV_NAMESPACE" get jobset "$active_jobset" \
    -o jsonpath='{range .spec.replicatedJobs[*]}  {.name}: {.replicas}{"\n"}{end}' 2>/dev/null || true
else
  echo "<no active child JobSet>"
fi
echo

echo "=== Pods and node placement ==="
if [[ -n "${active_jobset}" ]]; then
  kubectl -n "$DEV_NAMESPACE" get pods -l "jobset.sigs.k8s.io/jobset-name=${active_jobset}" \
    -o custom-columns='NAME:.metadata.name,NODE:.spec.nodeName,STATUS:.status.phase' 2>/dev/null || echo "<no pods>"
else
  echo "<no active child JobSet>"
fi
