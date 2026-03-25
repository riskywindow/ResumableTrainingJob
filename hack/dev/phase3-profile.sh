#!/usr/bin/env bash

# Phase 3 profile setup.
#
# This script layers the Phase 3 flavor-aware dev environment on top of the
# base dev stack. It:
#   1. Labels and taints kind worker nodes for heterogeneous pools.
#   2. Applies Phase 3 ResourceFlavors (on-demand, spot).
#   3. Applies Phase 3 ClusterQueue and LocalQueue with both flavors.
#   4. Patches Kueue config to the Phase 3 version.
#   5. Updates the namespace labels for Phase 3.
#
# Prerequisites:
#   - The base dev stack must be running (make dev-up with Phase 3 kind config).
#   - At least 4 worker nodes in the kind cluster.
#
# Usage:
#   PHASE3_PROFILE=flavors ./hack/dev/phase3-profile.sh
#   PHASE3_PROFILE=experimental ./hack/dev/phase3-profile.sh
#
# Profiles:
#   flavors      - Flavor-aware launch + fixed-size resume (default).
#                  Exercises G1, G2, and G3 without partial admission.
#   experimental - All of "flavors" plus partial-admission support.
#                  Exercises G1 through G4.
#                  NOTE: The operator must also be started with
#                  --enable-experimental-partial-admission for G4.

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl

ensure_cluster_context

PHASE3_PROFILE="${PHASE3_PROFILE:-flavors}"

echo "applying Phase 3 profile: ${PHASE3_PROFILE}"

# 1. Label/taint nodes for heterogeneous pools.
"$REPO_ROOT/hack/dev/label-kind-nodes.sh"
echo

# 2. Apply Phase 3 namespace labels.
kubectl apply -f "$REPO_ROOT/deploy/dev/namespaces/01-checkpoint-dev-phase3.yaml"
echo

# 3. Apply Phase 3 ResourceFlavors.
kubectl apply -f "$REPO_ROOT/deploy/dev/flavors/"
echo

# 4. Apply Phase 3 queue profile.
kubectl apply -f "$REPO_ROOT/deploy/dev/queues/phase3/"
echo

# 5. Patch Kueue config.
case "$PHASE3_PROFILE" in
  flavors)
    KUEUE_CONFIG_PATH="$REPO_ROOT/deploy/dev/kueue/controller_manager_config.phase3-flavors.yaml"
    ;;
  experimental)
    KUEUE_CONFIG_PATH="$REPO_ROOT/deploy/dev/kueue/controller_manager_config.phase3-experimental-partial-admission.yaml"
    ;;
  *)
    echo "unknown Phase 3 profile: ${PHASE3_PROFILE}" >&2
    echo "valid profiles: flavors, experimental" >&2
    exit 1
    ;;
esac

kubectl -n "$KUEUE_NAMESPACE" create configmap "$KUEUE_CONFIGMAP_NAME" \
  --from-file=controller_manager_config.yaml="$KUEUE_CONFIG_PATH" \
  --dry-run=client \
  -o yaml | kubectl apply -f -

kubectl -n "$KUEUE_NAMESPACE" rollout restart "deployment/${KUEUE_DEPLOYMENT_NAME}"
kubectl -n "$KUEUE_NAMESPACE" rollout status "deployment/${KUEUE_DEPLOYMENT_NAME}" --timeout=180s
echo

# 6. Verify Kueue config.
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
echo "Phase 3 profile '${PHASE3_PROFILE}' is active"
echo
echo "resources:"
kubectl get resourceflavors.kueue.x-k8s.io
echo
kubectl get clusterqueues.kueue.x-k8s.io
echo
kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE"
echo

if [[ "$PHASE3_PROFILE" == "experimental" ]]; then
  echo "EXPERIMENTAL: partial admission is enabled in Kueue config."
  echo "To activate per-RTJ, also start the operator with:"
  echo "  --enable-experimental-partial-admission"
  echo "and set spec.parallelism.enablePartialAdmission=true on RTJs."
fi
