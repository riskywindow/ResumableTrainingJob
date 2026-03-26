#!/usr/bin/env bash

# Labels and taints kind worker nodes to simulate heterogeneous resource pools
# and topology domains for Phase 3/4 testing.
#
# Pool layout (Phase 3):
#   worker, worker2  → on-demand pool (no taint)
#   worker3, worker4 → spot pool (tainted with NoSchedule)
#
# Topology layout (Phase 4):
#   worker, worker2  → block-a / rack-1
#   worker3, worker4 → block-b / rack-2
#
# The ResourceFlavors in deploy/dev/flavors/ reference these labels.
# The Topology in deploy/dev/topology/ references the topology labels.

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

# ── Phase 4: Topology labels ──────────────────────────────────────────
#
# Two-level topology hierarchy for Kueue TAS:
#   Level 0: block (topology.example.io/block)
#   Level 1: rack  (topology.example.io/rack)
#
# These labels are additive and harmless for Phase 3 (which ignores them).

echo
echo "applying Phase 4 topology labels"

# Block A / Rack 1 → first two workers
for i in 0 1; do
  node="${WORKERS[$i]}"
  echo "  ${node} → block-a / rack-1"
  kubectl label node "$node" \
    topology.example.io/block=block-a \
    topology.example.io/rack=rack-1 \
    checkpoint-native.dev/phase4=true \
    --overwrite
done

# Block B / Rack 2 → last two workers
for i in 2 3; do
  node="${WORKERS[$i]}"
  echo "  ${node} → block-b / rack-2"
  kubectl label node "$node" \
    topology.example.io/block=block-b \
    topology.example.io/rack=rack-2 \
    checkpoint-native.dev/phase4=true \
    --overwrite
done

echo
echo "node labels and taints applied"
kubectl get nodes \
  -L checkpoint-native.dev/pool \
  -L checkpoint-native.dev/phase3 \
  -L topology.example.io/block \
  -L topology.example.io/rack
