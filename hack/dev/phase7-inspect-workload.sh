#!/usr/bin/env bash
#
# Inspect RTJ and Kueue Workload status for a Phase 7 RTJ.
# Shows: RTJ phase, Workload admission, admission checks with states,
# podSetAssignments (including podSetUpdates from provisioning), and
# child JobSet status.
#
# Usage:
#   PHASE7_RTJ_NAME=phase7-demo make phase7-inspect-workload
#   PHASE7_RTJ_NAME=phase7-demo ./hack/dev/phase7-inspect-workload.sh

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
  echo

  echo "admissionCheck podSetUpdates:"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
    -o jsonpath='{range .status.admissionChecks[*]}  check={.name}:{"\n"}{range .podSetUpdates[*]}    podSet={.name} labels={.labels} nodeSelector={.nodeSelector} tolerations={.tolerations}{"\n"}{end}{end}' 2>/dev/null || echo "  <none>"
  echo

  echo "conditions:"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
    -o jsonpath='{range .status.conditions[*]}  {.type}={.status} ({.reason}){"\n"}{end}' 2>/dev/null || echo "  <none>"
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
  echo "<no active child JobSet — launch gate may be blocking>"
fi
