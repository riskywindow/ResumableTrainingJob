#!/usr/bin/env bash
#
# Submit a Phase 7 RTJ that exercises the provisioning failure path.
# The ProvisioningRequest AC will be permanently rejected by the fake backend,
# preventing child JobSet creation.
#
# Usage:
#   PHASE7_RTJ_NAME=phase7-fail make phase7-submit-fail
#   PHASE7_RTJ_NAME=phase7-fail ./hack/dev/phase7-submit-fail.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

PHASE7_RTJ_NAME="${PHASE7_RTJ_NAME:-phase7-fail}"
PHASE7_TRAINER_IMAGE="${PHASE7_TRAINER_IMAGE:-busybox:latest}"
SAMPLE_PATH="$REPO_ROOT/deploy/dev/phase7/samples/rtj-provision-failure.yaml"

# Ensure the failure queue exists.
FAILURE_QUEUE="$REPO_ROOT/deploy/dev/phase7/samples/failure-queue.yaml"
if ! kubectl get clusterqueues.kueue.x-k8s.io phase7-failure-cq >/dev/null 2>&1; then
  echo "applying failure queue profile..."
  kubectl apply -f "$FAILURE_QUEUE"
  echo
fi

echo "=== Submitting Phase 7 provision-failure RTJ ==="
echo "  name:    ${PHASE7_RTJ_NAME}"
echo "  image:   ${PHASE7_TRAINER_IMAGE}"
echo "  queue:   phase7-failure-training"
echo

MANIFEST="$(sed \
  -e "s|__RTJ_NAME__|${PHASE7_RTJ_NAME}|g" \
  -e "s|__TRAINER_IMAGE__|${PHASE7_TRAINER_IMAGE}|g" \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "$SAMPLE_PATH")"

printf '%s\n' "$MANIFEST" | kubectl apply -f -

echo
echo "RTJ submitted. Watch the provisioning failure:"
echo "  make phase7-inspect-launchgate PHASE7_RTJ_NAME=${PHASE7_RTJ_NAME}"
echo "  make phase7-inspect-provisioningrequest PHASE7_RTJ_NAME=${PHASE7_RTJ_NAME}"
echo
echo "Expected: ProvisioningRequest → Failed=True, no child JobSet created."
