#!/usr/bin/env bash
#
# Pause a Phase 6 MultiKueue-managed RTJ from the manager cluster.
#
# Patches spec.control.desiredState to Paused. The Kueue adapter detects
# the spec drift and tears down the remote worker copy (delete-recreate).
# The manager controller marks the RTJ as Paused once the remote is
# no longer active.
#
# Usage:
#   make phase6-pause
#   PHASE6_RTJ_NAME=my-job ./hack/dev/phase6-pause-manager-rtj.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$REPO_ROOT/hack/dev/common.sh"

require_command kubectl
require_command kind

PHASE6_MANAGER="${PHASE6_MANAGER:-phase6-manager}"
PHASE6_RTJ_NAME="${PHASE6_RTJ_NAME:-phase6-dispatch-demo}"
DEV_NAMESPACE="${DEV_NAMESPACE:-checkpoint-dev}"

MANAGER_CTX="kind-${PHASE6_MANAGER}"
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"

echo "=== Pausing remote RTJ ${DEV_NAMESPACE}/${PHASE6_RTJ_NAME} ==="

# Patch desiredState to Paused.
kubectl -n "$DEV_NAMESPACE" patch "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  --type merge \
  -p '{"spec":{"control":{"desiredState":"Paused"}}}' \
  --context "$MANAGER_CTX"

echo
echo "pause requested for ${DEV_NAMESPACE}/${PHASE6_RTJ_NAME}"
echo

# Show current status.
echo "=== Current RTJ status ==="
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o wide --context "$MANAGER_CTX"
echo

phase="$(kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  -o jsonpath='{.status.phase}' --context "$MANAGER_CTX" 2>/dev/null || true)"
echo "phase: ${phase:-<pending>}"
echo

echo "The adapter will tear down the remote copy. Poll with:"
echo "  make phase6-inspect-manager"
echo
echo "Expected flow:"
echo "  1. Manager patches spec.control.desiredState=Paused"
echo "  2. Kueue adapter detects spec drift, deletes remote RTJ"
echo "  3. Adapter recreates remote with Paused spec"
echo "  4. Manager detects no active remote signal -> marks Paused"
echo "  5. Checkpoint evidence preserved in status.multiCluster.remoteCheckpoint"
