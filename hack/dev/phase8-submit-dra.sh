#!/usr/bin/env bash
#
# Submit a Phase 8 DRA-backed RTJ.
#
# Uses the rtj-dra-launch.yaml sample by default, or rtj-dra-pause-resume.yaml
# when PHASE8_SAMPLE=pause-resume.
#
# Usage:
#   make phase8-submit
#   PHASE8_RTJ_NAME=my-job make phase8-submit
#   PHASE8_SAMPLE=pause-resume make phase8-submit

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

PHASE8_RTJ_NAME="${PHASE8_RTJ_NAME:-phase8-demo}"
PHASE8_TRAINER_IMAGE="${PHASE8_TRAINER_IMAGE:-busybox:latest}"
PHASE8_SAMPLE="${PHASE8_SAMPLE:-launch}"

case "${PHASE8_SAMPLE}" in
  launch)
    SAMPLE_PATH="$REPO_ROOT/deploy/dev/phase8/samples/rtj-dra-launch.yaml"
    ;;
  pause-resume)
    SAMPLE_PATH="$REPO_ROOT/deploy/dev/phase8/samples/rtj-dra-pause-resume.yaml"
    ;;
  incompatible)
    SAMPLE_PATH="$REPO_ROOT/deploy/dev/phase8/samples/rtj-dra-incompatible-profile.yaml"
    ;;
  *)
    echo "unknown PHASE8_SAMPLE=${PHASE8_SAMPLE}; use launch, pause-resume, or incompatible" >&2
    exit 1
    ;;
esac

echo "=== Submitting Phase 8 DRA-backed RTJ ==="
echo "  name:    ${PHASE8_RTJ_NAME}"
echo "  image:   ${PHASE8_TRAINER_IMAGE}"
echo "  sample:  ${PHASE8_SAMPLE}"
echo "  queue:   phase8-training"
echo

MANIFEST="$(sed \
  -e "s|__RTJ_NAME__|${PHASE8_RTJ_NAME}|g" \
  -e "s|__TRAINER_IMAGE__|${PHASE8_TRAINER_IMAGE}|g" \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "$SAMPLE_PATH")"

printf '%s\n' "$MANIFEST" | kubectl apply -f -

echo
echo "RTJ submitted. Inspect DRA status:"
echo "  make phase8-inspect-dra PHASE8_RTJ_NAME=${PHASE8_RTJ_NAME}"
echo "  make phase8-inspect-kueue"
