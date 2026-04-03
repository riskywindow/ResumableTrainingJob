#!/usr/bin/env bash

# Phase 8 smoke test.
#
# Validates the Phase 8 dev environment is correctly configured:
#   1. DRA APIs are available (ResourceSlice, DeviceClass CRDs).
#   2. Example DRA driver DaemonSet is running.
#   3. DeviceClass "example-gpu" exists.
#   4. ResourceSlice objects exist (simulated devices published).
#   5. Kueue config has deviceClassMappings with example-gpu mapping.
#   6. Kueue config has DynamicResourceAllocation feature gate.
#   7. Phase 8 ClusterQueue exists with example.dev/gpu resource.
#   8. Phase 8 LocalQueue exists and points to phase8-cq.
#   9. Kueue config has RTJ external framework registration.
#  10. ResumableTrainingJob CRD is installed.
#  11. Sample RTJ manifests can be dry-run applied.
#
# This is an infrastructure validation, not an RTJ-level test.
# Sample RTJs can be applied later for e2e.

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

FAIL=0

echo "=== Phase 8 Smoke Test ==="
echo

# ── 1. DRA APIs ─────────────────────────────────────────────────────

echo "--- DRA API availability ---"

if kubectl api-resources --api-group=resource.k8s.io 2>/dev/null | grep -q 'resourceslices'; then
  echo "PASS: ResourceSlice API available (resource.k8s.io)"
else
  echo "FAIL: ResourceSlice API not available"
  echo "  Kubernetes v1.33+ is required for stable DRA."
  echo "  Current kind node image may be too old."
  FAIL=$((FAIL + 1))
fi

if kubectl api-resources --api-group=resource.k8s.io 2>/dev/null | grep -q 'deviceclasses'; then
  echo "PASS: DeviceClass API available (resource.k8s.io)"
else
  echo "FAIL: DeviceClass API not available"
  FAIL=$((FAIL + 1))
fi

if kubectl api-resources --api-group=resource.k8s.io 2>/dev/null | grep -q 'resourceclaims'; then
  echo "PASS: ResourceClaim API available (resource.k8s.io)"
else
  echo "FAIL: ResourceClaim API not available"
  FAIL=$((FAIL + 1))
fi

if kubectl api-resources --api-group=resource.k8s.io 2>/dev/null | grep -q 'resourceclaimtemplates'; then
  echo "PASS: ResourceClaimTemplate API available (resource.k8s.io)"
else
  echo "FAIL: ResourceClaimTemplate API not available"
  FAIL=$((FAIL + 1))
fi

echo

# ── 2. Example DRA driver DaemonSet ─────────────────────────────────

echo "--- DRA driver ---"

DS_READY="$(kubectl -n dra-example-driver get daemonset dra-example-driver -o jsonpath='{.status.numberReady}' 2>/dev/null || echo "0")"
DS_DESIRED="$(kubectl -n dra-example-driver get daemonset dra-example-driver -o jsonpath='{.status.desiredNumberScheduled}' 2>/dev/null || echo "0")"
if [[ "${DS_READY:-0}" -ge 1 ]] && [[ "$DS_READY" == "$DS_DESIRED" ]]; then
  echo "PASS: DRA driver DaemonSet running (${DS_READY}/${DS_DESIRED} ready)"
else
  echo "FAIL: DRA driver DaemonSet not fully ready (${DS_READY:-0}/${DS_DESIRED:-0})"
  FAIL=$((FAIL + 1))
fi

echo

# ── 3. DeviceClass ──────────────────────────────────────────────────

echo "--- DeviceClass ---"

if kubectl get deviceclasses.resource.k8s.io example-gpu >/dev/null 2>&1; then
  echo "PASS: DeviceClass example-gpu exists"
else
  echo "FAIL: DeviceClass example-gpu not found"
  FAIL=$((FAIL + 1))
fi

# Verify selector targets the correct driver.
DC_SELECTOR="$(kubectl get deviceclasses.resource.k8s.io example-gpu -o jsonpath='{.spec.selectors[0].cel.expression}' 2>/dev/null || echo "")"
if printf '%s\n' "$DC_SELECTOR" | grep -Fq 'dra.example.dev'; then
  echo "PASS: DeviceClass selector targets dra.example.dev driver"
else
  echo "FAIL: DeviceClass selector='${DC_SELECTOR}', expected dra.example.dev"
  FAIL=$((FAIL + 1))
fi

echo

# ── 4. ResourceSlices ───────────────────────────────────────────────

echo "--- ResourceSlices ---"

SLICE_COUNT="$(kubectl get resourceslices -l app.kubernetes.io/managed-by=dra-example-driver --no-headers 2>/dev/null | wc -l | tr -d ' ')"
if [[ "$SLICE_COUNT" -ge 1 ]]; then
  echo "PASS: ResourceSlice objects exist (${SLICE_COUNT} slices)"
else
  echo "FAIL: no ResourceSlice objects found"
  FAIL=$((FAIL + 1))
fi

# Verify at least one slice has devices.
FIRST_SLICE="$(kubectl get resourceslices -l app.kubernetes.io/managed-by=dra-example-driver -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")"
if [[ -n "$FIRST_SLICE" ]]; then
  DEVICE_COUNT="$(kubectl get resourceslice "$FIRST_SLICE" -o jsonpath='{.spec.devices}' 2>/dev/null | grep -o '"name"' | wc -l | tr -d ' ')"
  if [[ "$DEVICE_COUNT" -ge 1 ]]; then
    echo "PASS: ResourceSlice ${FIRST_SLICE} has ${DEVICE_COUNT} devices"
  else
    echo "FAIL: ResourceSlice ${FIRST_SLICE} has no devices"
    FAIL=$((FAIL + 1))
  fi

  # Verify driver name in slice.
  SLICE_DRIVER="$(kubectl get resourceslice "$FIRST_SLICE" -o jsonpath='{.spec.driver}' 2>/dev/null || echo "")"
  if [[ "$SLICE_DRIVER" == "dra.example.dev" ]]; then
    echo "PASS: ResourceSlice driver is dra.example.dev"
  else
    echo "FAIL: ResourceSlice driver='${SLICE_DRIVER}', expected dra.example.dev"
    FAIL=$((FAIL + 1))
  fi
fi

echo

# ── 5. Kueue deviceClassMappings ───────────────────────────────────

echo "--- Kueue configuration ---"

config_yaml="$(current_kueue_manager_config)"

if printf '%s\n' "$config_yaml" | grep -Fq 'deviceClassMappings'; then
  echo "PASS: Kueue config has deviceClassMappings"
else
  echo "FAIL: Kueue config missing deviceClassMappings"
  FAIL=$((FAIL + 1))
fi

if printf '%s\n' "$config_yaml" | grep -Fq 'example-gpu'; then
  echo "PASS: Kueue config has example-gpu mapping"
else
  echo "FAIL: Kueue config missing example-gpu mapping"
  FAIL=$((FAIL + 1))
fi

if printf '%s\n' "$config_yaml" | grep -Fq 'example.dev/gpu'; then
  echo "PASS: Kueue config maps example-gpu to example.dev/gpu"
else
  echo "FAIL: Kueue config missing example.dev/gpu resource name"
  FAIL=$((FAIL + 1))
fi

# ── 6. DynamicResourceAllocation feature gate ───────────────────────

if printf '%s\n' "$config_yaml" | grep -Fq 'DynamicResourceAllocation'; then
  echo "PASS: Kueue config has DynamicResourceAllocation feature gate"
else
  echo "INFO: DynamicResourceAllocation feature gate not found (may be default-on)"
fi

# ── 7. Phase 8 ClusterQueue ────────────────────────────────────────

echo

echo "--- Queues ---"

if kubectl get clusterqueues.kueue.x-k8s.io phase8-cq >/dev/null 2>&1; then
  echo "PASS: ClusterQueue phase8-cq exists"
else
  echo "FAIL: ClusterQueue phase8-cq missing"
  FAIL=$((FAIL + 1))
fi

# Verify the queue covers example.dev/gpu.
CQ_RESOURCES="$(kubectl get clusterqueues.kueue.x-k8s.io phase8-cq -o jsonpath='{.spec.resourceGroups[0].coveredResources}' 2>/dev/null || echo "")"
if printf '%s\n' "$CQ_RESOURCES" | grep -Fq 'example.dev/gpu'; then
  echo "PASS: ClusterQueue phase8-cq covers example.dev/gpu"
else
  echo "FAIL: ClusterQueue phase8-cq does not cover example.dev/gpu"
  FAIL=$((FAIL + 1))
fi

# ── 8. Phase 8 LocalQueue ──────────────────────────────────────────

if kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase8-training >/dev/null 2>&1; then
  echo "PASS: LocalQueue phase8-training exists"
else
  echo "FAIL: LocalQueue phase8-training missing"
  FAIL=$((FAIL + 1))
fi

LQ_CQ="$(kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase8-training -o jsonpath='{.spec.clusterQueue}' 2>/dev/null || echo "")"
if [[ "$LQ_CQ" == "phase8-cq" ]]; then
  echo "PASS: LocalQueue phase8-training points to phase8-cq"
else
  echo "FAIL: LocalQueue clusterQueue='${LQ_CQ}', expected 'phase8-cq'"
  FAIL=$((FAIL + 1))
fi

# ── 9. RTJ external framework ──────────────────────────────────────

echo

echo "--- RTJ integration ---"

if printf '%s\n' "$config_yaml" | grep -Fq 'ResumableTrainingJob.v1alpha1.training.checkpoint.example.io'; then
  echo "PASS: Kueue config has RTJ external framework"
else
  echo "FAIL: Kueue config missing RTJ external framework"
  FAIL=$((FAIL + 1))
fi

if printf '%s\n' "$config_yaml" | grep -Fq 'manageJobsWithoutQueueName: false'; then
  echo "PASS: manageJobsWithoutQueueName=false"
else
  echo "FAIL: manageJobsWithoutQueueName not disabled"
  FAIL=$((FAIL + 1))
fi

# ── 10. ResumableTrainingJob CRD ───────────────────────────────────

if kubectl get crd resumabletrainingjobs.training.checkpoint.example.io >/dev/null 2>&1; then
  echo "PASS: ResumableTrainingJob CRD installed"
else
  echo "FAIL: ResumableTrainingJob CRD not installed"
  FAIL=$((FAIL + 1))
fi

# ── 11. Sample RTJ dry-run ─────────────────────────────────────────

echo
echo "--- Sample RTJ validation (dry-run) ---"

LAUNCH_MANIFEST="$(sed \
  -e 's|__RTJ_NAME__|phase8-smoke-launch|g' \
  -e 's|__TRAINER_IMAGE__|busybox:latest|g' \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "$REPO_ROOT/deploy/dev/phase8/samples/rtj-dra-launch.yaml")"

if printf '%s\n' "$LAUNCH_MANIFEST" | kubectl apply --dry-run=server -f - >/dev/null 2>&1; then
  echo "PASS: DRA launch RTJ sample validates (dry-run)"
else
  echo "FAIL: DRA launch RTJ sample fails validation"
  FAIL=$((FAIL + 1))
fi

RESUME_MANIFEST="$(sed \
  -e 's|__RTJ_NAME__|phase8-smoke-resume|g' \
  -e 's|__TRAINER_IMAGE__|busybox:latest|g' \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "$REPO_ROOT/deploy/dev/phase8/samples/rtj-dra-pause-resume.yaml")"

if printf '%s\n' "$RESUME_MANIFEST" | kubectl apply --dry-run=server -f - >/dev/null 2>&1; then
  echo "PASS: DRA pause/resume RTJ sample validates (dry-run)"
else
  echo "FAIL: DRA pause/resume RTJ sample fails validation"
  FAIL=$((FAIL + 1))
fi

# ── Summary ─────────────────────────────────────────────────────────

echo
echo "phase8 resources:"
echo "  DeviceClass:"
kubectl get deviceclasses.resource.k8s.io example-gpu --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done
echo "  ResourceSlices:"
kubectl get resourceslices -l app.kubernetes.io/managed-by=dra-example-driver --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done
echo "  DRA driver DaemonSet:"
kubectl -n dra-example-driver get daemonset dra-example-driver --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done
echo "  queues:"
kubectl get clusterqueues.kueue.x-k8s.io phase8-cq --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done
kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase8-training --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done
echo "  deviceClassMappings:"
printf '%s\n' "$config_yaml" | grep -A3 'deviceClassMappings' | while IFS= read -r line; do
  echo "    $line"
done

echo
if [[ "$FAIL" -eq 0 ]]; then
  echo "=== Phase 8 smoke PASSED ==="
else
  echo "=== Phase 8 smoke FAILED (${FAIL} checks) ==="
  exit 1
fi
