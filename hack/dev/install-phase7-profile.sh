#!/usr/bin/env bash

# Phase 7 profile setup.
#
# This script layers the Phase 7 capacity-guaranteed launch dev environment
# on top of the base dev stack. It:
#   1. Applies namespace labels (idempotent).
#   2. Applies RTJ CRDs (idempotent).
#   3. Installs the ProvisioningRequest CRD (dev-only minimal version).
#   4. Applies ProvisioningRequestConfig objects.
#   5. Applies AdmissionCheck objects for provisioning.
#   6. Applies base ResourceFlavor.
#   7. Applies Phase 7 ClusterQueue and LocalQueue (with AdmissionCheck).
#   8. Builds and loads the fake-provisioner image into kind.
#   9. Deploys the fake-provisioner (SA, RBAC, Deployment).
#  10. Patches Kueue config with waitForPodsReady + ProvisioningACC.
#  11. Verifies Kueue config.
#
# Prerequisites:
#   - The base dev stack must be running (make dev-up).
#   - Kueue must be installed.
#   - Docker must be available (for building fake-provisioner image).
#
# Usage:
#   ./hack/dev/install-phase7-profile.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command docker

ensure_cluster_context

FAKE_PROVISIONER_IMAGE="${FAKE_PROVISIONER_IMAGE:-fake-provisioner:dev}"

echo "=== Installing Phase 7 profile ==="
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

# 3. ProvisioningRequest CRD.
echo "applying ProvisioningRequest CRD (dev-only)..."
kubectl apply --server-side -f "$REPO_ROOT/deploy/dev/phase7/provisioning/00-provisioning-request-crd.yaml"
kubectl wait --for=condition=Established crd/provisioningrequests.autoscaling.x-k8s.io --timeout=60s
echo

# 4. ProvisioningRequestConfig objects.
echo "applying ProvisioningRequestConfig..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase7/provisioning/10-provisioning-request-config.yaml"
echo

# 5. AdmissionCheck objects.
echo "applying AdmissionChecks..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase7/provisioning/20-admission-check.yaml"
echo

# 6. ResourceFlavor (shared with Phase 1/2).
echo "applying ResourceFlavor..."
kubectl apply -f "$REPO_ROOT/deploy/dev/queues/00-resource-flavor.yaml"
echo

# 7. Phase 7 ClusterQueue and LocalQueue.
echo "applying Phase 7 queue profile..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase7/queues/"
echo

# 8. Build and load fake-provisioner image.
echo "building fake-provisioner image..."
docker build -t "$FAKE_PROVISIONER_IMAGE" -f "$REPO_ROOT/cmd/fake-provisioner/Dockerfile" "$REPO_ROOT"
echo

echo "loading fake-provisioner image into kind..."
kind load docker-image "$FAKE_PROVISIONER_IMAGE" --name "$KIND_CLUSTER_NAME"
echo

# 9. Deploy fake-provisioner.
echo "deploying fake-provisioner..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase7/fake-provisioner/"
echo

echo "waiting for fake-provisioner to be ready..."
kubectl -n "$DEV_NAMESPACE" rollout status "deployment/fake-provisioner" --timeout=120s
echo

# 10. Patch Kueue config with Phase 7 settings.
echo "patching Kueue config with Phase 7 settings (waitForPodsReady + ProvisioningACC)..."
KUEUE_CONFIG_PATH="$REPO_ROOT/deploy/dev/phase7/kueue/controller_manager_config.phase7.yaml"
kubectl -n "$KUEUE_NAMESPACE" create configmap "$KUEUE_CONFIGMAP_NAME" \
  --from-file=controller_manager_config.yaml="$KUEUE_CONFIG_PATH" \
  --dry-run=client \
  -o yaml | kubectl apply -f -

kubectl -n "$KUEUE_NAMESPACE" rollout restart "deployment/${KUEUE_DEPLOYMENT_NAME}"
kubectl -n "$KUEUE_NAMESPACE" rollout status "deployment/${KUEUE_DEPLOYMENT_NAME}" --timeout=180s
echo

# 11. Verify Kueue config.
config_yaml="$(current_kueue_manager_config)"
if ! printf '%s\n' "$config_yaml" | grep -Fq 'ResumableTrainingJob.v1alpha1.training.checkpoint.example.io'; then
  echo "FAIL: patched Kueue config is missing the RTJ external framework registration" >&2
  exit 1
fi
if ! printf '%s\n' "$config_yaml" | grep -Fq 'manageJobsWithoutQueueName: false'; then
  echo "FAIL: patched Kueue config does not disable manageJobsWithoutQueueName" >&2
  exit 1
fi
if ! printf '%s\n' "$config_yaml" | grep -Fq 'waitForPodsReady'; then
  echo "FAIL: patched Kueue config is missing waitForPodsReady" >&2
  exit 1
fi

echo
echo "=== Phase 7 profile is active ==="
echo
echo "admission checks:"
kubectl get admissionchecks.kueue.x-k8s.io -l checkpoint-native.dev/profile=phase7-provisioning 2>/dev/null || echo "  (none)"
echo
echo "provisioning request configs:"
kubectl get provisioningrequestconfigs.kueue.x-k8s.io -l checkpoint-native.dev/profile=phase7-provisioning 2>/dev/null || echo "  (none)"
echo
echo "cluster queues:"
kubectl get clusterqueues.kueue.x-k8s.io phase7-cq 2>/dev/null || echo "  (none)"
echo
echo "local queues:"
kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase7-training 2>/dev/null || echo "  (none)"
echo
echo "fake-provisioner:"
kubectl -n "$DEV_NAMESPACE" get deployment fake-provisioner 2>/dev/null || echo "  (not deployed)"
