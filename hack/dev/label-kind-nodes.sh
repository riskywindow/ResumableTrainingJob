#!/usr/bin/env bash

# Labels and taints kind worker nodes to simulate heterogeneous resource pools
# for Phase 3 flavor-aware testing.
#
# Pool layout:
#   worker, worker2  → on-demand pool (no taint)
#   worker3, worker4 → spot pool (tainted with NoSchedule)
#
# The ResourceFlavors in deploy/dev/flavors/ reference these labels.

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl

ensure_cluster_context

CLUSTER_PREFIX="${KIND_CLUSTER_NAME}"

# Discover worker nodes. kind names them <cluster>-worker, <cluster>-worker2, etc.
WORKERS=()
while IFS= read -r node; do
  WORKERS+=("$node")
done < <(kubectl get nodes -o name | grep -v control-plane | sed 's|node/||')

if [[ "${#WORKERS[@]}" -lt 4 ]]; then
  echo "Phase 3 requires at least 4 worker nodes, found ${#WORKERS[@]}" >&2
  echo "Use hack/dev/kind-config-phase3.yaml to create the cluster" >&2
  exit 1
fi

# Sort workers so assignment is deterministic.
IFS=$'\n' WORKERS=($(sort <<<"${WORKERS[*]}")); unset IFS

echo "labeling ${#WORKERS[@]} workers for Phase 3 flavor pools"

# First half → on-demand pool
for i in 0 1; do
  node="${WORKERS[$i]}"
  echo "  ${node} → on-demand"
  kubectl label node "$node" \
    checkpoint-native.dev/pool=on-demand \
    checkpoint-native.dev/phase3=true \
    --overwrite
  # Remove spot taint if previously applied.
  kubectl taint node "$node" checkpoint-native.dev/spot=true:NoSchedule- 2>/dev/null || true
done

# Second half → spot pool (tainted)
for i in 2 3; do
  node="${WORKERS[$i]}"
  echo "  ${node} → spot (tainted)"
  kubectl label node "$node" \
    checkpoint-native.dev/pool=spot \
    checkpoint-native.dev/phase3=true \
    --overwrite
  kubectl taint node "$node" \
    checkpoint-native.dev/spot=true:NoSchedule \
    --overwrite 2>/dev/null || \
  kubectl taint node "$node" \
    checkpoint-native.dev/spot=true:NoSchedule 2>/dev/null || true
done

echo "node labels and taints applied"
kubectl get nodes -L checkpoint-native.dev/pool -L checkpoint-native.dev/phase3
