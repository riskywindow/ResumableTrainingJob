#!/usr/bin/env bash
#
# Resume a Phase 6 MultiKueue-managed RTJ from the manager cluster.
#
# Patches spec.control.desiredState back to Running. The Kueue adapter
# detects the spec drift and tears down the Paused remote copy, then
# creates a new Running remote. The worker resumes from the shared
# checkpoint store.
#
# Usage:
#   make phase6-resume
#   PHASE6_RTJ_NAME=my-job ./hack/dev/phase6-resume-manager-rtj.sh

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

echo "=== Resuming remote RTJ ${DEV_NAMESPACE}/${PHASE6_RTJ_NAME} ==="

# Patch desiredState back to Running.
kubectl -n "$DEV_NAMESPACE" patch "$RTJ_RESOURCE" "$PHASE6_RTJ_NAME" \
  --type merge \
  -p '{"spec":{"control":{"desiredState":"Running"}}}' \
  --context "$MANAGER_CTX"

echo
echo "resume requested for ${DEV_NAMESPACE}/${PHASE6_RTJ_NAME}"
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

echo "The adapter will recreate the remote with Running spec. Poll with:"
echo "  make phase6-inspect-manager"
echo "  make phase6-inspect-worker"
echo
echo "Expected flow:"
echo "  1. Manager patches spec.control.desiredState=Running"
echo "  2. Kueue adapter detects spec drift, deletes Paused remote"
echo "  3. Adapter creates new Running remote on a worker cluster"
echo "  4. Worker resumes from shared checkpoint store"
echo "  5. Manager observes new remote status signal -> DispatchPhase=Active"
