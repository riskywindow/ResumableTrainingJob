#!/usr/bin/env bash

# Phase 9 profile setup.
#
# This script layers the Phase 9 elastic resize dev environment on top of
# the base dev stack. It:
#   1. Applies namespace labels (idempotent).
#   2. Applies RTJ CRDs (idempotent).
#   3. Applies Phase 9 ResourceFlavor.
#   4. Applies Phase 9 ClusterQueue and LocalQueue with elastic quota.
#   5. Patches Kueue config with Phase 9 settings.
#   6. Verifies Kueue config.
#
# The Phase 9 queue is sized for the dynamic reclaim demonstration:
#   - 1250m CPU / 1280Mi memory total
#   - Enough for one 4-worker RTJ (1000m) but not two
#   - After shrink from 4→2 workers, reclaimablePods releases 500m
#   - Second RTJ (2 workers, 500m) can then be admitted
#
# No special Kueue feature gates are required for reclaimablePods.
# The RTJ controller writes Workload.status.reclaimablePods via SSA
# with field manager "rtj-elastic-reclaim". Kueue reads this field
# unconditionally and adjusts quota accounting.
#
# Prerequisites:
#   - The base dev stack must be running (make dev-up).
#   - Kueue must be installed.
#
# Usage:
#   ./hack/dev/install-phase9-profile.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl

ensure_cluster_context

echo "=== Installing Phase 9 profile ==="
echo

# 1. Namespace.
echo "applying namespace..."
kubectl apply -f "$REPO_ROOT/deploy/dev/namespaces/00-checkpoint-dev.yaml"
echo

# 2. RTJ CRDs.
echo "applying CRDs..."
kubectl apply --server-side -f "$REPO_ROOT/config/crd/bases/"
kubectl wait --for=condition=Established crd/resumabletrainingjobs.training.checkpoint.example.io --timeout=60s
echo

# 3. ResourceFlavor.
echo "applying Phase 9 ResourceFlavor..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase9/queues/00-resource-flavor.yaml"
echo

# 4. Phase 9 ClusterQueue and LocalQueue.
echo "applying Phase 9 queue profile..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase9/queues/10-cluster-queue.yaml"
kubectl apply -f "$REPO_ROOT/deploy/dev/phase9/queues/20-local-queue.yaml"
echo

# 5. Patch Kueue config with Phase 9 settings.
echo "patching Kueue config with Phase 9 settings..."
KUEUE_CONFIG_PATH="$REPO_ROOT/deploy/dev/phase9/kueue/controller_manager_config.phase9.yaml"
kubectl -n "$KUEUE_NAMESPACE" create configmap "$KUEUE_CONFIGMAP_NAME" \
  --from-file=controller_manager_config.yaml="$KUEUE_CONFIG_PATH" \
  --dry-run=client \
  -o yaml | kubectl apply -f -

kubectl -n "$KUEUE_NAMESPACE" rollout restart "deployment/${KUEUE_DEPLOYMENT_NAME}"
kubectl -n "$KUEUE_NAMESPACE" rollout status "deployment/${KUEUE_DEPLOYMENT_NAME}" --timeout=180s
echo

# 6. Verify Kueue config.
echo "verifying Kueue configuration..."
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
echo "=== Phase 9 profile is active ==="
echo
echo "cluster queues:"
kubectl get clusterqueues.kueue.x-k8s.io phase9-cq 2>/dev/null || echo "  (none)"
echo
echo "local queues:"
kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase9-training 2>/dev/null || echo "  (none)"
echo
echo "quota (for dynamic reclaim demo):"
kubectl get clusterqueues.kueue.x-k8s.io phase9-cq -o jsonpath='{.spec.resourceGroups[0].flavors[0].resources}' 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "  (could not read quota)"
echo
