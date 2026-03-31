#!/usr/bin/env bash
#
# Submit a Phase 6 MultiKueue-managed RTJ on the manager cluster.
#
# The RTJ sets spec.managedBy to enable MultiKueue dispatch to a worker
# cluster. The manager operator suppresses local child JobSet creation
# and delegates runtime execution to the remote worker.
#
# Usage:
#   make phase6-submit
#   PHASE6_RTJ_NAME=my-job ./hack/dev/phase6-submit-manager-rtj.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$REPO_ROOT/hack/dev/common.sh"

require_command kubectl
require_command kind

PHASE6_MANAGER="${PHASE6_MANAGER:-phase6-manager}"
PHASE6_WORKER_1="${PHASE6_WORKER_1:-phase6-worker-1}"
PHASE6_RTJ_NAME="${PHASE6_RTJ_NAME:-phase6-dispatch-demo}"
PHASE6_TRAINER_IMAGE="${PHASE6_TRAINER_IMAGE:-${PHASE5_TRAINER_IMAGE:-${PHASE4_TRAINER_IMAGE:-${PHASE3_TRAINER_IMAGE:-${PHASE2_TRAINER_IMAGE:-}}}}}"
DEV_NAMESPACE="${DEV_NAMESPACE:-checkpoint-dev}"

MANAGER_CTX="kind-${PHASE6_MANAGER}"
TEMPLATE_PATH="${REPO_ROOT}/deploy/dev/phase6/samples/rtj-multikueue-dispatch.yaml"

if [[ -z "${PHASE6_TRAINER_IMAGE}" ]]; then
  echo "set PHASE6_TRAINER_IMAGE to a trainer image already loaded into kind" >&2
  exit 1
fi

# Resolve shared store endpoint from the manager cluster.
SHARED_ENDPOINT="$(kubectl -n "$DEV_NAMESPACE" get configmap shared-checkpoint-store \
  -o jsonpath='{.data.endpoint}' --context "$MANAGER_CTX" 2>/dev/null || echo "http://minio.checkpoint-dev.svc:9000")"

sed \
  -e "s|__RTJ_NAME__|${PHASE6_RTJ_NAME}|g" \
  -e "s|__TRAINER_IMAGE__|${PHASE6_TRAINER_IMAGE}|g" \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  -e "s|__SHARED_ENDPOINT__|${SHARED_ENDPOINT}|g" \
  "${TEMPLATE_PATH}" | kubectl apply --context "$MANAGER_CTX" -f -

kubectl -n "$DEV_NAMESPACE" get resumabletrainingjobs.training.checkpoint.example.io \
  "$PHASE6_RTJ_NAME" -o wide --context "$MANAGER_CTX"

echo
echo "submitted MultiKueue RTJ ${DEV_NAMESPACE}/${PHASE6_RTJ_NAME} on manager (${PHASE6_MANAGER})"
echo "  spec.managedBy: kueue.x-k8s.io/multikueue"
echo "  queue: phase6-training"
echo "  trainer image: ${PHASE6_TRAINER_IMAGE}"
echo
echo "next steps:"
echo "  make phase6-inspect-manager   # check dispatch status on manager"
echo "  make phase6-inspect-worker    # check execution on worker cluster"
