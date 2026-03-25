#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kind

if cluster_exists; then
  kind delete cluster --name "$KIND_CLUSTER_NAME"
  echo "deleted kind cluster ${KIND_CLUSTER_NAME}"
else
  echo "kind cluster ${KIND_CLUSTER_NAME} does not exist"
fi
