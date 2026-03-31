#!/usr/bin/env bash
#
# Inspect the mirror RTJ on Phase 6 worker clusters.
#
# Checks both worker-1 and worker-2 for the mirror RTJ created by the
# MultiKueue adapter. Shows: RTJ status, child JobSet, pods, and
# checkpoint state on the active worker.
#
# Usage:
#   make phase6-inspect-worker
#   PHASE6_RTJ_NAME=my-job ./hack/dev/phase6-inspect-worker.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$REPO_ROOT/hack/dev/common.sh"

require_command kubectl
require_command kind

PHASE6_WORKER_1="${PHASE6_WORKER_1:-phase6-worker-1}"
PHASE6_WORKER_2="${PHASE6_WORKER_2:-phase6-worker-2}"
PHASE6_RTJ_NAME="${PHASE6_RTJ_NAME:-phase6-dispatch-demo}"
DEV_NAMESPACE="${DEV_NAMESPACE:-checkpoint-dev}"

RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

inspect_worker() {
  local cluster="$1"
  local ctx="kind-${cluster}"

  echo "=== Worker: ${cluster} ==="

  # Check if RTJ exists.
  if ! kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
    --context "$ctx" >/dev/null 2>&1; then
    echo "  RTJ not present on ${cluster}"
    echo
    return
  fi

  echo "--- RTJ status ---"
  kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
    -o wide --context "$ctx"
  echo

  phase="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
    -o jsonpath='{.status.phase}' --context "$ctx" 2>/dev/null || true)"
  run_attempt="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
    -o jsonpath='{.status.currentRunAttempt}' --context "$ctx" 2>/dev/null || true)"
  active_js="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
    -o jsonpath='{.status.activeJobSetName}' --context "$ctx" 2>/dev/null || true)"
  echo "phase: ${phase:-<not set>}"
  echo "currentRunAttempt: ${run_attempt:-0}"
  echo "activeJobSetName: ${active_js:-<none>}"
  echo

  echo "--- Child JobSets ---"
  kubectl -n "$DEV_NAMESPACE" get jobsets.jobset.x-k8s.io \
    -l training.checkpoint.example.io/rtj-name="${PHASE6_RTJ_NAME}" \
    --context "$ctx" --no-headers 2>/dev/null || echo "  (none)"
  echo

  echo "--- Pods ---"
  kubectl -n "$DEV_NAMESPACE" get pods \
    -l training.checkpoint.example.io/rtj-name="${PHASE6_RTJ_NAME}" \
    --context "$ctx" --no-headers 2>/dev/null || echo "  (none)"
  echo

  echo "--- Last completed checkpoint ---"
  ckpt_uri="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
    -o jsonpath='{.status.lastCompletedCheckpoint.manifestURI}' --context "$ctx" 2>/dev/null || true)"
  ckpt_step="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
    -o jsonpath='{.status.lastCompletedCheckpoint.globalStep}' --context "$ctx" 2>/dev/null || true)"
  ckpt_time="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
    -o jsonpath='{.status.lastCompletedCheckpoint.completionTime}' --context "$ctx" 2>/dev/null || true)"
  if [[ -n "${ckpt_uri}" ]]; then
    echo "manifestURI: ${ckpt_uri}"
    echo "globalStep: ${ckpt_step:-<not set>}"
    echo "completionTime: ${ckpt_time:-<not set>}"
  else
    echo "<no checkpoint recorded>"
  fi
  echo

  echo "--- Worker Workload ---"
  workload_name="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io \
    -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"${PHASE6_RTJ_NAME}\")]}{.metadata.name}{end}" \
    --context "$ctx" 2>/dev/null || true)"
  if [[ -n "${workload_name}" ]]; then
    echo "workload: ${workload_name}"
    wl_admitted="$(kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io "$workload_name" \
      -o jsonpath='{.status.admission.clusterQueue}' --context "$ctx" 2>/dev/null || true)"
    echo "admitted to: ${wl_admitted:-<pending>}"
  else
    echo "<no Workload found>"
  fi
  echo
}

inspect_worker "$PHASE6_WORKER_1"
inspect_worker "$PHASE6_WORKER_2"
