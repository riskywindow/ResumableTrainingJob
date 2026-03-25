#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl

ensure_cluster_context

kubectl -n "$KUEUE_NAMESPACE" create configmap "$KUEUE_CONFIGMAP_NAME" \
  --from-file=controller_manager_config.yaml="$KUEUE_CONFIG_PATH" \
  --dry-run=client \
  -o yaml | kubectl apply -f -

kubectl -n "$KUEUE_NAMESPACE" rollout restart "deployment/${KUEUE_DEPLOYMENT_NAME}"
kubectl -n "$KUEUE_NAMESPACE" rollout status "deployment/${KUEUE_DEPLOYMENT_NAME}" --timeout=180s

config_yaml="$(current_kueue_manager_config)"
if ! printf '%s\n' "$config_yaml" | grep -Fq 'ResumableTrainingJob.v1alpha1.training.checkpoint.example.io'; then
  echo "patched Kueue config is missing the RTJ external framework registration" >&2
  exit 1
fi
if ! printf '%s\n' "$config_yaml" | grep -Fq 'manageJobsWithoutQueueName: false'; then
  echo "patched Kueue config does not explicitly disable manageJobsWithoutQueueName" >&2
  exit 1
fi
if ! printf '%s\n' "$config_yaml" | grep -Fq 'checkpoint-native.dev/kueue-managed: "true"'; then
  echo "patched Kueue config is missing the namespace opt-in selector" >&2
  exit 1
fi

echo "patched Kueue manager config for Phase 2 RTJ external integration"
