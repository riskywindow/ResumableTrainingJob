#!/usr/bin/env bash

# Phase 6: Create three kind clusters for MultiKueue dev environment.
#
# Clusters:
#   - phase6-manager: control-plane only (no workload execution)
#   - phase6-worker-1: worker cluster (runs RTJ workloads)
#   - phase6-worker-2: worker cluster (runs RTJ workloads)
#
# All three clusters are connected via the default kind Docker network,
# which allows inter-cluster API server access via container IPs.
#
# This script does NOT touch the default single-cluster dev path
# (checkpoint-phase1). It uses separate cluster names.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$REPO_ROOT/hack/dev/common.sh"

require_command docker
require_command kind
require_command kubectl

PHASE6_MANAGER="${PHASE6_MANAGER:-phase6-manager}"
PHASE6_WORKER_1="${PHASE6_WORKER_1:-phase6-worker-1}"
PHASE6_WORKER_2="${PHASE6_WORKER_2:-phase6-worker-2}"
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:-kindest/node:v1.31.2}"

# Manager cluster: 1 control-plane node only.
# No worker nodes -- the manager does NOT execute workloads.
MANAGER_CONFIG="$(cat <<'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
EOF
)"

# Worker clusters: 1 control-plane + 1 worker node each.
WORKER_CONFIG="$(cat <<'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
  - role: worker
EOF
)"

create_cluster() {
  local name="$1"
  local config="$2"

  if kind get clusters 2>/dev/null | grep -Fxq "$name"; then
    echo "kind cluster $name already exists"
  else
    echo "creating kind cluster $name..."
    echo "$config" | kind create cluster \
      --name "$name" \
      --image "$KIND_NODE_IMAGE" \
      --config /dev/stdin \
      --wait 120s
  fi

  kubectl cluster-info --context "kind-${name}" >/dev/null
  kubectl wait --for=condition=Ready nodes --all --timeout=180s --context "kind-${name}"
  echo "kind cluster $name is ready"
  echo
}

create_cluster "$PHASE6_MANAGER" "$MANAGER_CONFIG"
create_cluster "$PHASE6_WORKER_1" "$WORKER_CONFIG"
create_cluster "$PHASE6_WORKER_2" "$WORKER_CONFIG"

echo "=== Phase 6 clusters ==="
echo "  manager:  kind-${PHASE6_MANAGER}"
echo "  worker-1: kind-${PHASE6_WORKER_1}"
echo "  worker-2: kind-${PHASE6_WORKER_2}"
echo
echo "all three Phase 6 kind clusters are ready"
