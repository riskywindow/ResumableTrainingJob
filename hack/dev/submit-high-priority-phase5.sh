#!/usr/bin/env bash
#
# Submit a high-priority Phase 5 RTJ with checkpoint-aware priority shaping.
#
# The RTJ uses WorkloadPriorityClass phase5-high (base priority 10000) and
# references the dev-checkpoint-priority CheckpointPriorityPolicy.
#
# Usage:
#   make phase5-submit-high
#   PHASE5_HIGH_RTJ_NAME=my-high ./hack/dev/submit-high-priority-phase5.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

PHASE5_TRAINER_IMAGE="${PHASE5_TRAINER_IMAGE:-${PHASE4_TRAINER_IMAGE:-${PHASE3_TRAINER_IMAGE:-${PHASE2_TRAINER_IMAGE:-}}}}"
PHASE5_HIGH_RTJ_NAME="${PHASE5_HIGH_RTJ_NAME:-phase5-high-demo}"
PHASE5_HIGH_TEMPLATE_PATH="${PHASE5_HIGH_TEMPLATE_PATH:-$REPO_ROOT/deploy/dev/phase5/samples/rtj-high-priority.yaml}"

if [[ -z "${PHASE5_TRAINER_IMAGE}" ]]; then
  echo "set PHASE5_TRAINER_IMAGE to a trainer image already loaded into kind" >&2
  exit 1
fi

sed \
  -e "s|__RTJ_NAME__|${PHASE5_HIGH_RTJ_NAME}|g" \
  -e "s|__TRAINER_IMAGE__|${PHASE5_TRAINER_IMAGE}|g" \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "${PHASE5_HIGH_TEMPLATE_PATH}" | kubectl apply -f -

kubectl -n "$DEV_NAMESPACE" get resumabletrainingjobs.training.checkpoint.example.io "$PHASE5_HIGH_RTJ_NAME" -o wide

echo
echo "submitted high-priority Phase 5 RTJ ${DEV_NAMESPACE}/${PHASE5_HIGH_RTJ_NAME} with image ${PHASE5_TRAINER_IMAGE}"
echo "  base priority: 10000 (phase5-high)"
echo "  policy: dev-checkpoint-priority"
echo "  queue: phase5-training"
