#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context
apply_dev_namespaces >/dev/null

kubectl -n "$KUEUE_NAMESPACE" wait --for=condition=Available deployment --all --timeout=180s
kubectl -n jobset-system wait --for=condition=Available deployment --all --timeout=180s
kubectl -n "$DEV_NAMESPACE" rollout status deployment/minio --timeout=180s

apply_dev_priorityclasses >/dev/null
apply_dev_queues >/dev/null

kubectl delete -f "$REPO_ROOT/deploy/dev/samples/standalone-jobset-smoke.yaml" --ignore-not-found >/dev/null
kubectl apply -f "$REPO_ROOT/deploy/dev/samples/standalone-jobset-smoke.yaml"

wait_for_pod_count "$DEV_NAMESPACE" "app.kubernetes.io/name=standalone-jobset-smoke" 2 180
kubectl wait -n "$DEV_NAMESPACE" --for=condition=Ready pod -l app.kubernetes.io/name=standalone-jobset-smoke --timeout=180s

echo "jobset smoke workload admitted and pods are ready"
kubectl get jobset -n "$DEV_NAMESPACE"
kubectl get workloads.kueue.x-k8s.io -n "$DEV_NAMESPACE" || true
kubectl get pods -n "$DEV_NAMESPACE" -l app.kubernetes.io/name=standalone-jobset-smoke
