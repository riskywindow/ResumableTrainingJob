#!/usr/bin/env bash

# Phase 5 profile setup.
#
# This script layers the Phase 5 checkpoint-aware priority shaping dev
# environment on top of the base dev stack. It:
#   1. Applies Phase 5 namespace labels (if not already present).
#   2. Applies RTJ + CheckpointPriorityPolicy CRDs (idempotent).
#   3. Applies the Phase 5 WorkloadPriorityClasses (phase5-low, phase5-high).
#   4. Applies the base ResourceFlavor (default-flavor, shared with Phase 1/2).
#   5. Applies the sample CheckpointPriorityPolicy (dev-checkpoint-priority).
#   6. Applies Phase 5 ClusterQueue and LocalQueue.
#   7. Patches Kueue config with RTJ external framework registration.
#
# The Phase 5 queue profile targets within-ClusterQueue preemption only:
#   - withinClusterQueue: LowerPriority
#   - reclaimWithinCohort: Never
#   - borrowWithinCohort disabled
#
# Cohort borrowing/reclaim and Fair Sharing are intentionally excluded.
# See docs/phase5/dev-environment.md for the rationale.
#
# Prerequisites:
#   - The base dev stack must be running (make dev-up).
#   - Kueue must be installed.
#
# Usage:
#   ./hack/dev/install-phase5-profile.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl

ensure_cluster_context

echo "applying Phase 5 profile"

# 1. Apply namespace (idempotent — includes kueue-managed label).
echo "applying namespace..."
kubectl apply -f "$REPO_ROOT/deploy/dev/namespaces/00-checkpoint-dev.yaml"
echo

# 2. Apply CRDs (idempotent — safe to re-apply).
echo "applying CRDs..."
kubectl apply --server-side -f "$REPO_ROOT/config/crd/bases/"
kubectl wait --for=condition=Established crd/resumabletrainingjobs.training.checkpoint.example.io --timeout=60s
kubectl wait --for=condition=Established crd/checkpointprioritypolicies.training.checkpoint.example.io --timeout=60s
echo

# 3. Apply Phase 5 WorkloadPriorityClasses.
echo "applying Phase 5 priority classes..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase5/priorities/"
echo

# 4. Apply base ResourceFlavor (shared with Phase 1/2 queue profile).
echo "applying ResourceFlavor..."
kubectl apply -f "$REPO_ROOT/deploy/dev/queues/00-resource-flavor.yaml"
echo

# 5. Apply sample CheckpointPriorityPolicy.
echo "applying CheckpointPriorityPolicy..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase5/policies/"
echo

# 6. Apply Phase 5 ClusterQueue and LocalQueue.
echo "applying Phase 5 queue profile..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase5/queues/"
echo

# 7. Patch Kueue config with RTJ external framework.
KUEUE_CONFIG_PATH="$REPO_ROOT/deploy/dev/kueue/controller_manager_config.phase2-rtj-external-framework.yaml"
kubectl -n "$KUEUE_NAMESPACE" create configmap "$KUEUE_CONFIGMAP_NAME" \
  --from-file=controller_manager_config.yaml="$KUEUE_CONFIG_PATH" \
  --dry-run=client \
  -o yaml | kubectl apply -f -

kubectl -n "$KUEUE_NAMESPACE" rollout restart "deployment/${KUEUE_DEPLOYMENT_NAME}"
kubectl -n "$KUEUE_NAMESPACE" rollout status "deployment/${KUEUE_DEPLOYMENT_NAME}" --timeout=180s
echo

# 8. Verify Kueue config.
config_yaml="$(current_kueue_manager_config)"
if ! printf '%s\n' "$config_yaml" | grep -Fq 'ResumableTrainingJob.v1alpha1.training.checkpoint.example.io'; then
  echo "FAIL: patched Kueue config is missing the RTJ external framework registration" >&2
  exit 1
fi
if ! printf '%s\n' "$config_yaml" | grep -Fq 'manageJobsWithoutQueueName: false'; then
  echo "FAIL: patched Kueue config does not disable manageJobsWithoutQueueName" >&2
  exit 1
fi

echo
echo "Phase 5 profile is active"
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
