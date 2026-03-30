#!/usr/bin/env bash
#
# Submit a low-priority Phase 5 RTJ with checkpoint-aware priority shaping.
#
# The RTJ uses WorkloadPriorityClass phase5-low (base priority 100) and
# references the dev-checkpoint-priority CheckpointPriorityPolicy.
#
# Usage:
#   make phase5-submit-low
#   PHASE5_LOW_RTJ_NAME=my-low ./hack/dev/submit-low-priority-phase5.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

PHASE5_TRAINER_IMAGE="${PHASE5_TRAINER_IMAGE:-${PHASE4_TRAINER_IMAGE:-${PHASE3_TRAINER_IMAGE:-${PHASE2_TRAINER_IMAGE:-}}}}"
PHASE5_LOW_RTJ_NAME="${PHASE5_LOW_RTJ_NAME:-phase5-low-demo}"
PHASE5_LOW_TEMPLATE_PATH="${PHASE5_LOW_TEMPLATE_PATH:-$REPO_ROOT/deploy/dev/phase5/samples/rtj-low-priority.yaml}"

if [[ -z "${PHASE5_TRAINER_IMAGE}" ]]; then
  echo "set PHASE5_TRAINER_IMAGE to a trainer image already loaded into kind" >&2
  exit 1
fi

sed \
  -e "s|__RTJ_NAME__|${PHASE5_LOW_RTJ_NAME}|g" \
  -e "s|__TRAINER_IMAGE__|${PHASE5_TRAINER_IMAGE}|g" \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "${PHASE5_LOW_TEMPLATE_PATH}" | kubectl apply -f -

kubectl -n "$DEV_NAMESPACE" get resumabletrainingjobs.training.checkpoint.example.io "$PHASE5_LOW_RTJ_NAME" -o wide

echo
echo "submitted low-priority Phase 5 RTJ ${DEV_NAMESPACE}/${PHASE5_LOW_RTJ_NAME} with image ${PHASE5_TRAINER_IMAGE}"
echo "  base priority: 100 (phase5-low)"
echo "  policy: dev-checkpoint-priority"
echo "  queue: phase5-training"
