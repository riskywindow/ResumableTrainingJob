#!/usr/bin/env bash

# Phase 6: Install RTJ CRDs on all clusters.
#
# This script applies the RTJ CRD (and related CRDs like
# CheckpointPriorityPolicy, ResumeReadinessPolicy) on all three clusters
# so that both the manager and worker clusters can handle RTJ resources.
#
# The actual operator deployment (with --mode=manager or --mode=worker)
# is left to the user, since the operator may be run out-of-cluster
# during development. This script only ensures the CRDs are present.
#
# Prerequisites:
#   - All three clusters must exist.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$REPO_ROOT/hack/dev/common.sh"

require_command kubectl

PHASE6_MANAGER="${PHASE6_MANAGER:-phase6-manager}"
PHASE6_WORKER_1="${PHASE6_WORKER_1:-phase6-worker-1}"
PHASE6_WORKER_2="${PHASE6_WORKER_2:-phase6-worker-2}"

install_crds_on_cluster() {
  local cluster_name="$1"
  local ctx="kind-${cluster_name}"

  echo "installing RTJ CRDs on ${cluster_name}..."
  kubectl apply --server-side -f "$REPO_ROOT/config/crd/bases/" --context "$ctx"

  kubectl wait --for=condition=Established \
    crd/resumabletrainingjobs.training.checkpoint.example.io \
    --timeout=60s --context "$ctx"

  # CheckpointPriorityPolicy and ResumeReadinessPolicy CRDs may also exist.
  kubectl wait --for=condition=Established \
    crd/checkpointprioritypolicies.training.checkpoint.example.io \
    --timeout=60s --context "$ctx" 2>/dev/null || true

  kubectl wait --for=condition=Established \
    crd/resumereadinesspolicies.training.checkpoint.example.io \
    --timeout=60s --context "$ctx" 2>/dev/null || true

  echo "RTJ CRDs installed on ${cluster_name}"
}

install_crds_on_cluster "$PHASE6_MANAGER"
install_crds_on_cluster "$PHASE6_WORKER_1"
install_crds_on_cluster "$PHASE6_WORKER_2"

echo
echo "RTJ CRDs installed on all Phase 6 clusters"
echo
echo "Next steps:"
echo "  - Run the operator on the manager cluster with:  --mode=manager --kubeconfig=<manager-kubeconfig>"
echo "  - Run the operator on each worker cluster with:  --mode=worker  --kubeconfig=<worker-kubeconfig>"
