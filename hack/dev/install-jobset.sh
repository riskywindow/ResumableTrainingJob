#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

kubectl apply --server-side -f "$JOBSET_MANIFEST_URL"
kubectl wait --for=condition=Established crd/jobsets.jobset.x-k8s.io --timeout=180s
kubectl -n jobset-system wait --for=condition=Available deployment --all --timeout=180s

echo "jobset ${JOBSET_VERSION} is installed"
