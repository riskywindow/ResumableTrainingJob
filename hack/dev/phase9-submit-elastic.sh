#!/usr/bin/env bash
#
# Submit a Phase 9 elastic RTJ (shrink demo: 4 workers).
#
# Applies deploy/dev/phase9/samples/rtj-elastic-shrink.yaml. The RTJ starts
# with 4 workers and elasticity.mode=Manual. After admission, patch
# spec.elasticity.targetWorkerCount to trigger a resize.
#
# Usage:
#   make phase9-submit-elastic
#   PHASE9_SHRINK_RTJ_NAME=my-job make phase9-submit-elastic
#   PHASE9_TRAINER_IMAGE=busybox:latest make phase9-submit-elastic

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

PHASE9_SHRINK_RTJ_NAME="${PHASE9_SHRINK_RTJ_NAME:-phase9-elastic-a}"
PHASE9_TRAINER_IMAGE="${PHASE9_TRAINER_IMAGE:-busybox:latest}"
SAMPLE_PATH="$REPO_ROOT/deploy/dev/phase9/samples/rtj-elastic-shrink.yaml"

echo "=== Submitting Phase 9 elastic RTJ (shrink demo) ==="
echo "  name:    ${PHASE9_SHRINK_RTJ_NAME}"
echo "  image:   ${PHASE9_TRAINER_IMAGE}"
echo "  queue:   phase9-training"
echo "  workers: 4 (shrink target: 2)"
echo

MANIFEST="$(sed \
  -e "s|__RTJ_NAME__|${PHASE9_SHRINK_RTJ_NAME}|g" \
  -e "s|__TRAINER_IMAGE__|${PHASE9_TRAINER_IMAGE}|g" \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "$SAMPLE_PATH")"

printf '%s\n' "$MANIFEST" | kubectl apply -f -

echo
echo "Waiting briefly for initial status..."
sleep 3

echo
echo "=== RTJ status ==="
RTJ_RESOURCE="resumabletrainingjobs.training.checkpoint.example.io"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_SHRINK_RTJ_NAME" -o wide 2>/dev/null || echo "<not yet visible>"

echo
echo "elasticity spec:"
kubectl -n "$DEV_NAMESPACE" get "$RTJ_RESOURCE" "$PHASE9_SHRINK_RTJ_NAME" \
  -o jsonpath=$'  mode={.spec.elasticity.mode}\n  targetWorkerCount={.spec.elasticity.targetWorkerCount}\n  inPlaceShrinkPolicy={.spec.elasticity.inPlaceShrinkPolicy}\n  reclaimMode={.spec.elasticity.reclaimMode}\n' 2>/dev/null || true

echo
echo "RTJ submitted. Next steps:"
echo "  Inspect elastic status:  make phase9-inspect-elastic PHASE9_SHRINK_RTJ_NAME=${PHASE9_SHRINK_RTJ_NAME}"
echo "  Trigger shrink (4→2):    make phase9-shrink PHASE9_SHRINK_RTJ_NAME=${PHASE9_SHRINK_RTJ_NAME}"
echo "  Inspect workload:        make phase9-inspect-workload PHASE9_SHRINK_RTJ_NAME=${PHASE9_SHRINK_RTJ_NAME}"
