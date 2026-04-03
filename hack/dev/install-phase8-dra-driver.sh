#!/usr/bin/env bash

# Phase 8: Install the example DRA driver.
#
# This script deploys the example DRA driver into the kind cluster:
#   1. Creates the dra-example-driver namespace.
#   2. Applies the DeviceClass.
#   3. Applies RBAC and ServiceAccount.
#   4. Deploys the DaemonSet that publishes ResourceSlice objects
#      with simulated GPU devices.
#   5. Waits for the DaemonSet to be ready.
#   6. Verifies ResourceSlice objects are published.
#
# Prerequisites:
#   - Kind cluster must be running with Kubernetes v1.33+.
#   - kubectl must be configured for the target cluster.
#
# Usage:
#   ./hack/dev/install-phase8-dra-driver.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl

ensure_cluster_context

echo "=== Installing Phase 8 example DRA driver ==="
echo

# 1. Namespace.
echo "creating dra-example-driver namespace..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase8/dra-driver/00-namespace.yaml"
echo

# 2. DeviceClass.
echo "applying DeviceClass..."
kubectl apply --server-side -f "$REPO_ROOT/deploy/dev/phase8/dra-driver/05-device-class.yaml"
echo

# 3. ServiceAccount.
echo "applying ServiceAccount..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase8/dra-driver/10-service-account.yaml"
echo

# 4. RBAC.
echo "applying RBAC..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase8/dra-driver/15-rbac.yaml"
echo

# 5. DaemonSet.
echo "deploying DRA driver DaemonSet..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase8/dra-driver/20-daemonset.yaml"
echo

# 6. Wait for DaemonSet readiness.
echo "waiting for DRA driver DaemonSet to be ready..."
kubectl -n dra-example-driver rollout status daemonset/dra-example-driver --timeout=120s
echo

# 7. Verify ResourceSlice objects.
echo "verifying ResourceSlice objects..."
DEADLINE=$((SECONDS + 60))
SLICE_COUNT=0
while (( SECONDS < DEADLINE )); do
  SLICE_COUNT="$(kubectl get resourceslices -l app.kubernetes.io/managed-by=dra-example-driver --no-headers 2>/dev/null | wc -l | tr -d ' ')"
  if [[ "$SLICE_COUNT" -ge 1 ]]; then
    break
  fi
  sleep 2
done

if [[ "$SLICE_COUNT" -ge 1 ]]; then
  echo "ResourceSlice objects published (${SLICE_COUNT} slices)"
else
  echo "WARNING: no ResourceSlice objects found after 60s"
  echo "  The DRA driver may still be initializing."
fi

echo
echo "=== DRA driver installed ==="
echo
echo "DeviceClass:"
kubectl get deviceclasses.resource.k8s.io example-gpu --no-headers 2>/dev/null || echo "  (not found)"
echo
echo "ResourceSlices:"
kubectl get resourceslices -l app.kubernetes.io/managed-by=dra-example-driver --no-headers 2>/dev/null || echo "  (none)"
echo
echo "DaemonSet:"
kubectl -n dra-example-driver get daemonset dra-example-driver --no-headers 2>/dev/null || echo "  (not found)"
