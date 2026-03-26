#!/usr/bin/env bash

# Phase 4 profile setup.
#
# This script layers the Phase 4 topology-aware dev environment on top of the
# base dev stack. It:
#   1. Labels and taints kind worker nodes for pools AND topology domains.
#   2. Applies Phase 4 namespace labels.
#   3. Applies RTJ CRDs (ResumeReadinessPolicy + ResumableTrainingJob).
#   4. Applies the Kueue Topology object (dev-topology).
#   5. Applies the topology-aware ResourceFlavor (phase4-topology).
#   6. Applies ResumeReadinessPolicy and AdmissionCheck.
#   7. Applies Phase 4 ClusterQueue and LocalQueue.
#   8. Patches Kueue config with TopologyAwareScheduling feature gate.
#
# Prerequisites:
#   - The base dev stack must be running (make dev-up with Phase 3 kind config).
#   - At least 4 worker nodes in the kind cluster.
#
# Usage:
#   ./hack/dev/install-phase4-profile.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl

ensure_cluster_context

echo "applying Phase 4 profile"

# 1. Label/taint nodes for pools AND topology domains.
"$REPO_ROOT/hack/dev/label-kind-nodes.sh"
echo

# 2. Apply Phase 4 namespace labels.
kubectl apply -f "$REPO_ROOT/deploy/dev/namespaces/02-checkpoint-dev-phase4.yaml"
echo

# 3. Apply RTJ CRDs (idempotent — safe to re-apply).
echo "applying CRDs..."
kubectl apply --server-side -f "$REPO_ROOT/config/crd/bases/"
kubectl wait --for=condition=Established crd/resumabletrainingjobs.training.checkpoint.example.io --timeout=60s
kubectl wait --for=condition=Established crd/resumereadinesspolicies.training.checkpoint.example.io --timeout=60s
echo

# 4. Wait for Kueue Topology CRD (installed with Kueue manifest).
echo "waiting for Kueue Topology CRD..."
kubectl wait --for=condition=Established crd/topologies.kueue.x-k8s.io --timeout=60s 2>/dev/null || {
  echo "WARNING: topologies.kueue.x-k8s.io CRD not found."
  echo "  Kueue v0.15.1 may not include the Topology CRD in the base manifest."
  echo "  TAS-specific tests may be skipped until the CRD is available."
  echo "  Continuing with non-TAS Phase 4 resources..."
}
echo

# 5. Apply Topology object (if CRD is available).
if kubectl get crd topologies.kueue.x-k8s.io >/dev/null 2>&1; then
  echo "applying Topology object..."
  kubectl apply -f "$REPO_ROOT/deploy/dev/topology/"
  echo
fi

# 6. Apply Phase 3 ResourceFlavors (on-demand, spot — needed for pool labels).
kubectl apply -f "$REPO_ROOT/deploy/dev/flavors/"
echo

# 7. Apply ResumeReadinessPolicy and AdmissionCheck.
echo "applying admission check resources..."
kubectl apply -f "$REPO_ROOT/deploy/dev/admissionchecks/"
echo

# 8. Apply Phase 4 queue profile.
kubectl apply -f "$REPO_ROOT/deploy/dev/queues/phase4/"
echo

# 9. Patch Kueue config with TAS feature gate.
KUEUE_CONFIG_PATH="$REPO_ROOT/deploy/dev/kueue/controller_manager_config.phase4-topology.yaml"
kubectl -n "$KUEUE_NAMESPACE" create configmap "$KUEUE_CONFIGMAP_NAME" \
  --from-file=controller_manager_config.yaml="$KUEUE_CONFIG_PATH" \
  --dry-run=client \
  -o yaml | kubectl apply -f -

kubectl -n "$KUEUE_NAMESPACE" rollout restart "deployment/${KUEUE_DEPLOYMENT_NAME}"
kubectl -n "$KUEUE_NAMESPACE" rollout status "deployment/${KUEUE_DEPLOYMENT_NAME}" --timeout=180s
echo

# 10. Verify Kueue config.
config_yaml="$(current_kueue_manager_config)"
if ! printf '%s\n' "$config_yaml" | grep -Fq 'ResumableTrainingJob.v1alpha1.training.checkpoint.example.io'; then
  echo "FAIL: patched Kueue config is missing the RTJ external framework registration" >&2
  exit 1
fi
if ! printf '%s\n' "$config_yaml" | grep -Fq 'manageJobsWithoutQueueName: false'; then
  echo "FAIL: patched Kueue config does not disable manageJobsWithoutQueueName" >&2
  exit 1
fi
if ! printf '%s\n' "$config_yaml" | grep -Fq 'TopologyAwareScheduling: true'; then
  echo "WARNING: Kueue config does not show TopologyAwareScheduling: true"
  echo "  TAS may already be default-on in Kueue v0.15.1, or the config format differs."
fi

echo
echo "Phase 4 profile is active"
echo
echo "topology:"
if kubectl get crd topologies.kueue.x-k8s.io >/dev/null 2>&1; then
  kubectl get topologies.kueue.x-k8s.io 2>/dev/null || echo "  (none)"
else
  echo "  (Topology CRD not available)"
fi
echo
echo "resource flavors:"
kubectl get resourceflavors.kueue.x-k8s.io
echo
echo "cluster queues:"
kubectl get clusterqueues.kueue.x-k8s.io
echo
echo "local queues:"
kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE"
echo
echo "admission checks:"
kubectl get admissionchecks.kueue.x-k8s.io 2>/dev/null || echo "  (none)"
echo
echo "resume readiness policies:"
kubectl get resumereadinesspolicies.training.checkpoint.example.io 2>/dev/null || echo "  (none)"
