#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context
apply_dev_namespace >/dev/null
apply_dev_priorityclasses >/dev/null
apply_dev_queues >/dev/null
require_phase2_trainer_image

render_phase2_low_rtj_manifest | kubectl apply -f -
kubectl -n "$DEV_NAMESPACE" get resumabletrainingjobs.training.checkpoint.example.io "$PHASE2_LOW_RTJ_NAME" -o wide

echo
echo "submitted low-priority Phase 2 RTJ ${DEV_NAMESPACE}/${PHASE2_LOW_RTJ_NAME} with image ${PHASE2_TRAINER_IMAGE}"
