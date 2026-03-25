#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

echo "Kueue manager config:"
current_kueue_manager_config | grep -E 'manageJobsWithoutQueueName|checkpoint-native.dev/kueue-managed|externalFrameworks|ResumableTrainingJob' || true
echo

echo "ClusterQueues:"
kubectl get clusterqueues.kueue.x-k8s.io
echo

echo "LocalQueues:"
kubectl -n "$DEV_NAMESPACE" get localqueues.kueue.x-k8s.io
echo

echo "WorkloadPriorityClasses:"
kubectl get workloadpriorityclasses.kueue.x-k8s.io
echo

echo "RTJ-owned Workloads:"
kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io \
  -o custom-columns='NAME:.metadata.name,OWNER_KIND:.metadata.ownerReferences[0].kind,OWNER:.metadata.ownerReferences[0].name,QUEUE:.spec.queueName,CLUSTER_QUEUE:.status.admission.clusterQueue'
echo

echo "Runtime child JobSets:"
kubectl -n "$DEV_NAMESPACE" get jobset \
  -o custom-columns='NAME:.metadata.name,RTJ:.metadata.labels.training\.checkpoint\.example\.io/rtj-name,QUEUE_LABEL:.metadata.labels.kueue\.x-k8s\.io/queue-name,PRIORITY_LABEL:.metadata.labels.kueue\.x-k8s\.io/priority-class'
