#!/usr/bin/env bash
#
# Submit an RTJ that exercises the ResumeReadiness admission gate.
#
# This RTJ targets the phase4-training queue which has the resume-readiness
# AdmissionCheck. On initial launch, the default policy auto-approves. On
# resume after preemption, the controller checks for a valid checkpoint.
#
# Usage:
#   make phase4-submit-gated-resume
#   PHASE4_RTJ_NAME=my-gated make phase4-submit-gated-resume

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

PHASE4_RTJ_NAME="${PHASE4_RTJ_NAME:-phase4-demo}"
PHASE4_TRAINER_IMAGE="${PHASE4_TRAINER_IMAGE:-}"
TEMPLATE_PATH="${REPO_ROOT}/deploy/dev/samples/phase4/rtj-resume-readiness-gated.yaml"

if [[ -z "${PHASE4_TRAINER_IMAGE}" ]]; then
  echo "set PHASE4_TRAINER_IMAGE to a trainer image already loaded into kind" >&2
  exit 1
fi

echo "=== Submitting gated-resume RTJ ==="
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
echo "  make phase4-inspect-admissioncheck"
