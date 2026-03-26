#!/usr/bin/env bash
#
# Submit a topology-aware RTJ on the Phase 4 queue.
#
# Defaults to topology Required at rack level. Override with:
#   PHASE4_TOPOLOGY_MODE=preferred  — uses rtj-topology-preferred.yaml
#   PHASE4_TOPOLOGY_MODE=required   — uses rtj-topology-required.yaml (default)
#
# Usage:
#   make phase4-submit-topology
#   PHASE4_TOPOLOGY_MODE=preferred make phase4-submit-topology

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

PHASE4_RTJ_NAME="${PHASE4_RTJ_NAME:-phase4-demo}"
PHASE4_TRAINER_IMAGE="${PHASE4_TRAINER_IMAGE:-}"
PHASE4_TOPOLOGY_MODE="${PHASE4_TOPOLOGY_MODE:-required}"

if [[ -z "${PHASE4_TRAINER_IMAGE}" ]]; then
  echo "set PHASE4_TRAINER_IMAGE to a trainer image already loaded into kind" >&2
  exit 1
fi

case "${PHASE4_TOPOLOGY_MODE}" in
  required)
    TEMPLATE_PATH="${REPO_ROOT}/deploy/dev/samples/phase4/rtj-topology-required.yaml"
    ;;
  preferred)
    TEMPLATE_PATH="${REPO_ROOT}/deploy/dev/samples/phase4/rtj-topology-preferred.yaml"
    ;;
  *)
    echo "unsupported PHASE4_TOPOLOGY_MODE: ${PHASE4_TOPOLOGY_MODE} (use required or preferred)" >&2
    exit 1
    ;;
esac

echo "=== Submitting topology-aware RTJ (mode=${PHASE4_TOPOLOGY_MODE}) ==="
echo "  name:     ${PHASE4_RTJ_NAME}"
echo "  image:    ${PHASE4_TRAINER_IMAGE}"
echo "  template: ${TEMPLATE_PATH}"
echo

rendered=$(sed \
  -e "s|__RTJ_NAME__|${PHASE4_RTJ_NAME}|g" \
  -e "s|__TRAINER_IMAGE__|${PHASE4_TRAINER_IMAGE}|g" \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "${TEMPLATE_PATH}")

echo "${rendered}" | kubectl apply -f -
echo
echo "RTJ ${PHASE4_RTJ_NAME} submitted. Inspect with:"
echo "  make phase4-inspect-workload PHASE4_RTJ_NAME=${PHASE4_RTJ_NAME}"
echo "  make phase4-inspect-topology PHASE4_RTJ_NAME=${PHASE4_RTJ_NAME}"
