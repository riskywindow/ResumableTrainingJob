#!/usr/bin/env bash

# Phase 8 profile setup.
#
# This script layers the Phase 8 DRA dev environment on top of the base
# dev stack. It:
#   1. Validates the Kubernetes version supports DRA (v1.33+).
#   2. Applies namespace labels (idempotent).
#   3. Applies RTJ CRDs (idempotent).
#   4. Installs the example DRA driver (DeviceClass, RBAC, DaemonSet).
#   5. Applies Phase 8 ResourceFlavor.
#   6. Applies Phase 8 ClusterQueue and LocalQueue with DRA device quota.
#   7. Patches Kueue config with deviceClassMappings + DRA feature gate.
#   8. Verifies Kueue config.
#
# Prerequisites:
#   - The base dev stack must be running (make dev-up).
#   - Kueue must be installed.
#   - Kind cluster must use Kubernetes v1.33+ for stable DRA.
#
# Usage:
#   ./hack/dev/install-phase8-profile.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl

ensure_cluster_context

echo "=== Installing Phase 8 profile ==="
echo

# 1. Validate Kubernetes version.
echo "checking Kubernetes version for DRA support..."
K8S_VERSION="$(kubectl version -o json 2>/dev/null | grep -o '"gitVersion": *"[^"]*"' | head -1 | grep -o 'v[0-9]*\.[0-9]*' || echo "unknown")"
K8S_MINOR="$(echo "$K8S_VERSION" | grep -o '\.[0-9]*' | tr -d '.' || echo "0")"

if [[ "$K8S_MINOR" -lt 33 ]] && [[ "$K8S_VERSION" != "unknown" ]]; then
  echo "WARNING: Kubernetes ${K8S_VERSION} detected. Phase 8 requires v1.33+ for stable DRA."
  echo "  DRA APIs may not be available or may be in alpha/beta."
  echo "  Set KIND_NODE_IMAGE=kindest/node:v1.33.0 to use a compatible version."
  echo
fi
echo "Kubernetes version: ${K8S_VERSION}"
echo

# 2. Namespace.
echo "applying namespace..."
kubectl apply -f "$REPO_ROOT/deploy/dev/namespaces/00-checkpoint-dev.yaml"
echo

# 3. RTJ CRDs.
echo "applying CRDs..."
kubectl apply --server-side -f "$REPO_ROOT/config/crd/bases/"
kubectl wait --for=condition=Established crd/resumabletrainingjobs.training.checkpoint.example.io --timeout=60s
echo

# 4. Install example DRA driver.
"$REPO_ROOT/hack/dev/install-phase8-dra-driver.sh"
echo

# 5. ResourceFlavor.
echo "applying Phase 8 ResourceFlavor..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase8/queues/00-resource-flavor.yaml"
echo

# 6. Phase 8 ClusterQueue and LocalQueue.
echo "applying Phase 8 queue profile..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase8/queues/10-cluster-queue.yaml"
kubectl apply -f "$REPO_ROOT/deploy/dev/phase8/queues/20-local-queue.yaml"
echo

# 7. Patch Kueue config with Phase 8 settings.
echo "patching Kueue config with Phase 8 settings (deviceClassMappings + DRA)..."
KUEUE_CONFIG_PATH="$REPO_ROOT/deploy/dev/phase8/kueue/controller_manager_config.phase8.yaml"
kubectl -n "$KUEUE_NAMESPACE" create configmap "$KUEUE_CONFIGMAP_NAME" \
  --from-file=controller_manager_config.yaml="$KUEUE_CONFIG_PATH" \
  --dry-run=client \
  -o yaml | kubectl apply -f -

kubectl -n "$KUEUE_NAMESPACE" rollout restart "deployment/${KUEUE_DEPLOYMENT_NAME}"
kubectl -n "$KUEUE_NAMESPACE" rollout status "deployment/${KUEUE_DEPLOYMENT_NAME}" --timeout=180s
echo

# 8. Verify Kueue config.
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
if ! printf '%s\n' "$config_yaml" | grep -Fq 'deviceClassMappings'; then
  echo "FAIL: patched Kueue config is missing deviceClassMappings" >&2
  exit 1
fi
if ! printf '%s\n' "$config_yaml" | grep -Fq 'example-gpu'; then
  echo "FAIL: patched Kueue config is missing example-gpu deviceClassMapping" >&2
  exit 1
fi
if ! printf '%s\n' "$config_yaml" | grep -Fq 'DynamicResourceAllocation'; then
  echo "FAIL: patched Kueue config is missing DynamicResourceAllocation feature gate" >&2
  exit 1
fi

echo
echo "=== Phase 8 profile is active ==="
echo
echo "DeviceClass:"
kubectl get deviceclasses.resource.k8s.io example-gpu 2>/dev/null || echo "  (none)"
echo
echo "ResourceSlices:"
kubectl get resourceslices -l app.kubernetes.io/managed-by=dra-example-driver --no-headers 2>/dev/null || echo "  (none)"
echo
echo "cluster queues:"
kubectl get clusterqueues.kueue.x-k8s.io phase8-cq 2>/dev/null || echo "  (none)"
echo
echo "local queues:"
kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase8-training 2>/dev/null || echo "  (none)"
echo
echo "Kueue deviceClassMappings:"
printf '%s\n' "$config_yaml" | grep -A2 'deviceClassMappings' || echo "  (not found)"
