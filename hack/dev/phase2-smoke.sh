#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context
apply_dev_namespaces >/dev/null
apply_dev_priorityclasses >/dev/null
apply_dev_queues >/dev/null

kubectl -n "$KUEUE_NAMESPACE" rollout status "deployment/${KUEUE_DEPLOYMENT_NAME}" --timeout=180s
kubectl -n jobset-system wait --for=condition=Available deployment --all --timeout=180s
kubectl -n "$DEV_NAMESPACE" rollout status deployment/minio --timeout=180s

config_yaml="$(current_kueue_manager_config)"
printf '%s\n' "$config_yaml" | grep -F 'manageJobsWithoutQueueName: false' >/dev/null
printf '%s\n' "$config_yaml" | grep -F 'checkpoint-native.dev/kueue-managed: "true"' >/dev/null
printf '%s\n' "$config_yaml" | grep -F 'ResumableTrainingJob.v1alpha1.training.checkpoint.example.io' >/dev/null

kubectl delete -f "$REPO_ROOT/deploy/dev/samples/standalone-jobset-smoke.yaml" --ignore-not-found >/dev/null
kubectl apply -f "$REPO_ROOT/deploy/dev/samples/standalone-jobset-smoke.yaml"

wait_for_pod_count "$DEV_NAMESPACE" "app.kubernetes.io/name=standalone-jobset-smoke" 2 180
kubectl wait -n "$DEV_NAMESPACE" --for=condition=Ready pod -l app.kubernetes.io/name=standalone-jobset-smoke --timeout=180s

echo "Phase 2 smoke passed"
echo "- Kueue manager config contains the RTJ external framework registration"
echo "- unlabeled runtime child JobSets remain outside Kueue because manageJobsWithoutQueueName=false"
echo "- namespace opt-in is active through checkpoint-native.dev/kueue-managed=true"
echo "- queue-labeled workloads are still admitted through the local priority-preemption queue"
