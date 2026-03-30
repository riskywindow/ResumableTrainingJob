#!/usr/bin/env bash

# Phase 6: Delete all three kind clusters.
#
# This only deletes the Phase 6 clusters. It does NOT touch
# the single-cluster dev path (checkpoint-phase1 etc.).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$REPO_ROOT/hack/dev/common.sh"

require_command kind

PHASE6_MANAGER="${PHASE6_MANAGER:-phase6-manager}"
PHASE6_WORKER_1="${PHASE6_WORKER_1:-phase6-worker-1}"
PHASE6_WORKER_2="${PHASE6_WORKER_2:-phase6-worker-2}"

delete_cluster() {
  local name="$1"
  if kind get clusters 2>/dev/null | grep -Fxq "$name"; then
    kind delete cluster --name "$name"
    echo "deleted kind cluster $name"
  else
    echo "kind cluster $name does not exist"
  fi
}

delete_cluster "$PHASE6_MANAGER"
delete_cluster "$PHASE6_WORKER_1"
delete_cluster "$PHASE6_WORKER_2"

echo
echo "all Phase 6 kind clusters removed"
