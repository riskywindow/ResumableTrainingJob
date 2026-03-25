#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

kubectl apply --server-side -f "$KUEUE_MANIFEST_URL"
kubectl wait --for=condition=Established crd/clusterqueues.kueue.x-k8s.io --timeout=180s
kubectl wait --for=condition=Established crd/localqueues.kueue.x-k8s.io --timeout=180s
kubectl wait --for=condition=Established crd/workloadpriorityclasses.kueue.x-k8s.io --timeout=180s
kubectl -n "$KUEUE_NAMESPACE" wait --for=condition=Available deployment --all --timeout=180s
"$REPO_ROOT/hack/dev/patch-kueue-config.sh"

echo "kueue ${KUEUE_VERSION} is installed and patched for Phase 2"
