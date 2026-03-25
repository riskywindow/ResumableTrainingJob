#!/usr/bin/env bash
#
# Submit a Phase 3 fixed-size RTJ on the multi-flavor queue.
# This exercises G1 (admission-aware launch) and G2 (flavor-aware rendering).
#
# Usage:
#   PHASE3_TRAINER_IMAGE=phase1-ddp-counter:dev make phase3-submit-flavor
#   PHASE3_RTJ_NAME=custom-name PHASE3_TRAINER_IMAGE=img:tag ./hack/dev/submit-flavor-example.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context
apply_dev_namespace
require_phase3_trainer_image

render_phase3_flavor_rtj_manifest | kubectl apply -f -
kubectl -n "$DEV_NAMESPACE" get resumabletrainingjobs.training.checkpoint.example.io "$PHASE3_RTJ_NAME" -o wide

echo
echo "submitted Phase 3 flavor-aware RTJ ${DEV_NAMESPACE}/${PHASE3_RTJ_NAME} with image ${PHASE3_TRAINER_IMAGE}"
echo "queue: phase3-training (multi-flavor: on-demand, spot)"
echo
echo "next steps:"
echo "  make phase3-inspect-admission RTJ_NAME=${PHASE3_RTJ_NAME}"
