#!/usr/bin/env bash

# Phase 6: Install Kueue on all three clusters.
#
# Manager cluster gets the MultiKueue-enabled Kueue config (with RTJ
# external framework + MultiKueue feature gates).
#
# Worker clusters get the standard Phase 2 Kueue config (RTJ external
# framework only, no MultiKueue).
#
# Both use the pinned Kueue v0.15.1 manifests.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$REPO_ROOT/hack/dev/common.sh"

require_command kubectl

PHASE6_MANAGER="${PHASE6_MANAGER:-phase6-manager}"
PHASE6_WORKER_1="${PHASE6_WORKER_1:-phase6-worker-1}"
PHASE6_WORKER_2="${PHASE6_WORKER_2:-phase6-worker-2}"

MANAGER_KUEUE_CONFIG="$REPO_ROOT/deploy/multikueue/manager-config/kueue-controller-manager-config.yaml"
WORKER_KUEUE_CONFIG="$REPO_ROOT/deploy/dev/kueue/controller_manager_config.phase2-rtj-external-framework.yaml"

install_kueue_on_cluster() {
  local cluster_name="$1"
  local config_path="$2"
  local ctx="kind-${cluster_name}"

  echo "--- installing Kueue ${KUEUE_VERSION} on ${cluster_name} ---"

  kubectl apply --server-side -f "$KUEUE_MANIFEST_URL" --context "$ctx"
  kubectl wait --for=condition=Established crd/clusterqueues.kueue.x-k8s.io --timeout=180s --context "$ctx"
  kubectl wait --for=condition=Established crd/localqueues.kueue.x-k8s.io --timeout=180s --context "$ctx"
  kubectl wait --for=condition=Established crd/workloadpriorityclasses.kueue.x-k8s.io --timeout=180s --context "$ctx"
  kubectl -n "$KUEUE_NAMESPACE" wait --for=condition=Available deployment --all --timeout=180s --context "$ctx"

  # Patch Kueue config.
  kubectl -n "$KUEUE_NAMESPACE" create configmap "$KUEUE_CONFIGMAP_NAME" \
    --from-file=controller_manager_config.yaml="$config_path" \
    --dry-run=client \
    -o yaml | kubectl apply -f - --context "$ctx"

  kubectl -n "$KUEUE_NAMESPACE" rollout restart "deployment/${KUEUE_DEPLOYMENT_NAME}" --context "$ctx"
  kubectl -n "$KUEUE_NAMESPACE" rollout status "deployment/${KUEUE_DEPLOYMENT_NAME}" --timeout=180s --context "$ctx"

  # Verify RTJ external framework is configured.
  local config_yaml
  config_yaml="$(kubectl -n "$KUEUE_NAMESPACE" get configmap "$KUEUE_CONFIGMAP_NAME" \
    -o jsonpath='{.data.controller_manager_config\.yaml}' --context "$ctx")"

  if ! printf '%s\n' "$config_yaml" | grep -Fq 'ResumableTrainingJob.v1alpha1.training.checkpoint.example.io'; then
    echo "FAIL: Kueue config on ${cluster_name} missing RTJ external framework" >&2
    exit 1
  fi

  echo "Kueue ${KUEUE_VERSION} installed on ${cluster_name}"
  echo
}

install_kueue_on_cluster "$PHASE6_MANAGER" "$MANAGER_KUEUE_CONFIG"
install_kueue_on_cluster "$PHASE6_WORKER_1" "$WORKER_KUEUE_CONFIG"
install_kueue_on_cluster "$PHASE6_WORKER_2" "$WORKER_KUEUE_CONFIG"

echo "Kueue installed on all Phase 6 clusters"
