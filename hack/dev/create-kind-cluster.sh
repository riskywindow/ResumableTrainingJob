#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command docker
require_command kind
require_command kubectl

if cluster_exists; then
  echo "kind cluster ${KIND_CLUSTER_NAME} already exists"
else
  kind create cluster \
    --name "$KIND_CLUSTER_NAME" \
    --image "$KIND_NODE_IMAGE" \
    --config "$KIND_CONFIG_PATH" \
    --wait 120s
fi

ensure_cluster_context
kubectl wait --for=condition=Ready nodes --all --timeout=180s

echo "kind cluster ${KIND_CLUSTER_NAME} is ready"
