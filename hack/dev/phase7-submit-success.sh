#!/usr/bin/env bash
#
# Submit a Phase 7 RTJ that exercises the delayed-success provisioning path.
# The ProvisioningRequest AC will pend for ~10s (fake backend delay), then
# succeed, allowing the launch gate to open and child JobSet to be created.
#
# Usage:
#   PHASE7_RTJ_NAME=phase7-demo make phase7-submit-success
#   PHASE7_RTJ_NAME=phase7-demo ./hack/dev/phase7-submit-success.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

PHASE7_RTJ_NAME="${PHASE7_RTJ_NAME:-phase7-demo}"
PHASE7_TRAINER_IMAGE="${PHASE7_TRAINER_IMAGE:-busybox:latest}"
SAMPLE_PATH="$REPO_ROOT/deploy/dev/phase7/samples/rtj-delayed-success.yaml"

echo "=== Submitting Phase 7 delayed-success RTJ ==="
echo "  name:    ${PHASE7_RTJ_NAME}"
echo "  image:   ${PHASE7_TRAINER_IMAGE}"
echo "  queue:   phase7-training"
echo

MANIFEST="$(sed \
  -e "s|__RTJ_NAME__|${PHASE7_RTJ_NAME}|g" \
  -e "s|__TRAINER_IMAGE__|${PHASE7_TRAINER_IMAGE}|g" \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "$SAMPLE_PATH")"

printf '%s\n' "$MANIFEST" | kubectl apply -f -

echo
echo "RTJ submitted. Watch the provisioning lifecycle:"
echo "  make phase7-inspect-launchgate PHASE7_RTJ_NAME=${PHASE7_RTJ_NAME}"
echo "  make phase7-inspect-provisioningrequest PHASE7_RTJ_NAME=${PHASE7_RTJ_NAME}"
