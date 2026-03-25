#!/usr/bin/env bash
#
# Submit a Phase 3 flexible-size RTJ with allowWorldSizeChange=true.
# This exercises G3 (world-size-flexible resume via DCP resharding).
#
# Usage:
#   PHASE3_TRAINER_IMAGE=phase1-ddp-counter:dev make phase3-submit-flex
#   PHASE3_RTJ_NAME=custom-name PHASE3_TRAINER_IMAGE=img:tag ./hack/dev/submit-flex-example.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context
apply_dev_namespace
require_phase3_trainer_image

render_phase3_flex_rtj_manifest | kubectl apply -f -
kubectl -n "$DEV_NAMESPACE" get resumabletrainingjobs.training.checkpoint.example.io "$PHASE3_RTJ_NAME" -o wide

echo
echo "submitted Phase 3 flexible-size RTJ ${DEV_NAMESPACE}/${PHASE3_RTJ_NAME} with image ${PHASE3_TRAINER_IMAGE}"
echo "queue: phase3-training (multi-flavor: on-demand, spot)"
echo "allowWorldSizeChange: true"
echo
echo "next steps:"
echo "  make phase3-inspect-admission RTJ_NAME=${PHASE3_RTJ_NAME}"
echo "  # after running, pause to checkpoint:"
echo "  kubectl -n ${DEV_NAMESPACE} patch resumabletrainingjobs.training.checkpoint.example.io ${PHASE3_RTJ_NAME} --type=merge -p '{\"spec\":{\"control\":{\"desiredState\":\"Paused\"}}}'"
echo "  # then resume for DCP resharding path:"
echo "  kubectl -n ${DEV_NAMESPACE} patch resumabletrainingjobs.training.checkpoint.example.io ${PHASE3_RTJ_NAME} --type=merge -p '{\"spec\":{\"control\":{\"desiredState\":\"Running\"}}}'"
