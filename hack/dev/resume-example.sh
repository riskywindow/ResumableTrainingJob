#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

kubectl -n "$DEV_NAMESPACE" patch resumabletrainingjobs.training.checkpoint.example.io "$EXAMPLE_RTJ_NAME" \
  --type=merge \
  -p '{"spec":{"control":{"desiredState":"Running"}}}'

kubectl -n "$DEV_NAMESPACE" get resumabletrainingjobs.training.checkpoint.example.io "$EXAMPLE_RTJ_NAME" -o wide
