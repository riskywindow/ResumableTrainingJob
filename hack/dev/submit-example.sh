#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context
apply_dev_namespace
require_example_trainer_image

render_example_rtj_manifest | kubectl apply -f -
kubectl -n "$DEV_NAMESPACE" get resumabletrainingjobs.training.checkpoint.example.io "$EXAMPLE_RTJ_NAME" -o wide

echo
echo "submitted example RTJ ${DEV_NAMESPACE}/${EXAMPLE_RTJ_NAME} with image ${EXAMPLE_TRAINER_IMAGE}"
