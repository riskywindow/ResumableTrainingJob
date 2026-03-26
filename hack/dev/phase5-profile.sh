#!/usr/bin/env bash

# Phase 5 profile applicator.
#
# This script applies or re-applies the Phase 5 checkpoint-aware priority
# shaping profile on an existing cluster. Unlike install-phase5-profile.sh,
# this script assumes the base dev stack is already running and focuses on
# the Phase 5-specific resources only.
#
# Use this when you want to reset or update the Phase 5 profile without
# tearing down the entire cluster.
#
# Usage:
#   ./hack/dev/phase5-profile.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl

ensure_cluster_context

echo "applying Phase 5 priority shaping profile"

# 1. Apply CRDs (idempotent).
echo "applying CRDs..."
kubectl apply --server-side -f "$REPO_ROOT/config/crd/bases/"
kubectl wait --for=condition=Established crd/resumabletrainingjobs.training.checkpoint.example.io --timeout=60s
kubectl wait --for=condition=Established crd/checkpointprioritypolicies.training.checkpoint.example.io --timeout=60s
echo

# 2. Apply Phase 5 WorkloadPriorityClasses.
echo "applying Phase 5 priority classes..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase5/priorities/"
echo

# 3. Apply base ResourceFlavor.
echo "applying ResourceFlavor..."
kubectl apply -f "$REPO_ROOT/deploy/dev/queues/00-resource-flavor.yaml"
echo

# 4. Apply CheckpointPriorityPolicy.
echo "applying CheckpointPriorityPolicy..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase5/policies/"
echo

# 5. Apply Phase 5 queue profile.
echo "applying Phase 5 queue profile..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase5/queues/"
echo

echo "Phase 5 profile applied"
echo
echo "priority classes:"
kubectl get workloadpriorityclasses.kueue.x-k8s.io -l checkpoint-native.dev/profile=phase5-priority-shaping 2>/dev/null || echo "  (none)"
echo
echo "checkpoint priority policies:"
kubectl get checkpointprioritypolicies.training.checkpoint.example.io 2>/dev/null || echo "  (none)"
echo
echo "cluster queues:"
kubectl get clusterqueues.kueue.x-k8s.io phase5-cq 2>/dev/null || echo "  (none)"
echo
echo "local queues:"
kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase5-training 2>/dev/null || echo "  (none)"
