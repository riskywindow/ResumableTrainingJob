#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

if ! cluster_exists; then
  echo "kind cluster ${KIND_CLUSTER_NAME} does not exist"
  exit 0
fi

ensure_cluster_context

echo "context:"
kubectl config current-context
echo

echo "nodes:"
kubectl get nodes -o wide
echo

echo "system deployments:"
kubectl get deployments -n "$KUEUE_NAMESPACE"
kubectl get deployments -n jobset-system
echo

echo "kueue manager config:"
kubectl -n "$KUEUE_NAMESPACE" get configmap "$KUEUE_CONFIGMAP_NAME" -o jsonpath='{.data.controller_manager_config\.yaml}' | \
  grep -E 'manageJobsWithoutQueueName|checkpoint-native.dev/kueue-managed|externalFrameworks|ResumableTrainingJob' || true
echo

echo "dev namespace:"
kubectl get ns "$DEV_NAMESPACE"
kubectl get all -n "$DEV_NAMESPACE"
echo

echo "queues and priorities:"
kubectl get resourceflavors.kueue.x-k8s.io
kubectl get clusterqueues.kueue.x-k8s.io
kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE"
kubectl get workloadpriorityclasses.kueue.x-k8s.io
