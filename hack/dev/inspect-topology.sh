#!/usr/bin/env bash
#
# Inspect topology assignment and admitted flavors for a Phase 4 RTJ.
# Shows: RTJ topology status, Workload TopologyAssignment, node topology
# labels, child JobSet nodeSelector, and pod placement.
#
# Usage:
#   PHASE4_RTJ_NAME=phase4-demo make phase4-inspect-topology
#   PHASE4_RTJ_NAME=phase4-demo ./hack/dev/inspect-topology.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${PHASE4_RTJ_NAME:-${RTJ_NAME:-phase4-demo}}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== RTJ topology spec ==="
topo_mode="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.spec.topology.mode}' 2>/dev/null || true)"
if [[ -n "${topo_mode}" ]]; then
  topo_level="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.spec.topology.topologyLevel}' 2>/dev/null || true)"
  colocation="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{.spec.topology.leaderWorkerColocation}' 2>/dev/null || true)"
  echo "mode: ${topo_mode}"
  echo "topologyLevel: ${topo_level:-<not set>}"
  echo "leaderWorkerColocation: ${colocation:-false}"
else
  echo "<no topology configured — Phase 3 path>"
fi
echo

echo "=== RTJ topology status ==="
levels="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.topology.levels}' 2>/dev/null || true)"
if [[ -n "${levels}" ]]; then
  echo "levels: ${levels}"
  echo "domains:"
  kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
    -o jsonpath='{range .status.topology.domains[*]}  values={.values}  count={.count}{"\n"}{end}' 2>/dev/null || true
else
  echo "<not populated>"
fi
echo

echo "=== Workload TopologyAssignment ==="
workload_name="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io \
  -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"${RTJ_NAME}\")]}{.metadata.name}{end}" 2>/dev/null || true)"
if [[ -n "${workload_name}" ]]; then
  echo "workload: ${workload_name}"
  echo
  echo "podSetAssignment topology:"
  kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
    -o jsonpath='{range .status.admission.podSetAssignments[*]}  name={.name}  flavors={.flavors}  topologyAssignment.levels={.topologyAssignment.levels}{"\n"}{end}' 2>/dev/null || true
else
  echo "<no Workload found for RTJ ${RTJ_NAME}>"
fi
echo

echo "=== Kueue Topology object ==="
kubectl get topologies.kueue.x-k8s.io -o custom-columns='NAME:.metadata.name,LEVELS:.spec.levels[*].nodeLabel' 2>/dev/null || echo "<Topology CRD not available>"
echo

echo "=== ResourceFlavors with topology ==="
kubectl get resourceflavors.kueue.x-k8s.io -o custom-columns='NAME:.metadata.name,TOPOLOGY:.spec.topologyName,NODE_LABELS:.spec.nodeLabels' 2>/dev/null || echo "<none>"
echo

echo "=== Node topology labels ==="
kubectl get nodes \
  -L topology.example.io/block \
  -L topology.example.io/rack \
  -L checkpoint-native.dev/pool \
  --no-headers 2>/dev/null | while read -r line; do
  echo "  ${line}"
done
echo

echo "=== Child JobSet nodeSelector ==="
active_jobset="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$RTJ_NAME" \
  -o jsonpath='{.status.activeJobSetName}' 2>/dev/null || true)"
if [[ -n "${active_jobset}" ]]; then
  echo "child JobSet: ${active_jobset}"
  echo
  echo "nodeSelector per replicatedJob:"
  kubectl -n "$DEV_NAMESPACE" get jobset "$active_jobset" \
    -o jsonpath='{range .spec.replicatedJobs[*]}  {.name}: {.template.spec.template.spec.template.spec.nodeSelector}{"\n"}{end}' 2>/dev/null || true
  echo
  echo "pod placement:"
  kubectl -n "$DEV_NAMESPACE" get pods -l "jobset.sigs.k8s.io/jobset-name=${active_jobset}" \
    -o custom-columns='NAME:.metadata.name,NODE:.spec.nodeName,STATUS:.status.phase' 2>/dev/null || echo "  <no pods>"
else
  echo "<no active child JobSet>"
fi
