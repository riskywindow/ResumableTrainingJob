#!/usr/bin/env bash

# Phase 9 smoke test.
#
# Validates the Phase 9 dev environment is correctly configured:
#   1. ResumableTrainingJob CRD is installed with elasticity fields.
#   2. Kueue config has RTJ external framework registration.
#   3. Kueue config has manageJobsWithoutQueueName=false.
#   4. Kueue config has waitForPodsReady enabled.
#   5. Phase 9 ClusterQueue exists with correct quota.
#   6. Phase 9 LocalQueue exists and points to phase9-cq.
#   7. ClusterQueue quota supports the dynamic reclaim scenario.
#   8. Elastic shrink sample RTJ validates (dry-run).
#   9. Elastic grow sample RTJ validates (dry-run).
#  10. Non-elastic sample RTJ validates (dry-run).
#  11. Elastic sample manifests include resize fixture knobs.
#  12. Workload status API supports reclaimablePods field path.
#
# This is an infrastructure validation, not an RTJ-level test.
# The e2e suite will exercise actual resize flows.

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

FAIL=0

echo "=== Phase 9 Smoke Test ==="
echo

# ── 1. ResumableTrainingJob CRD ─────────────────────────────────────

echo "--- RTJ CRD ---"

if kubectl get crd resumabletrainingjobs.training.checkpoint.example.io >/dev/null 2>&1; then
  echo "PASS: ResumableTrainingJob CRD installed"
else
  echo "FAIL: ResumableTrainingJob CRD not installed"
  FAIL=$((FAIL + 1))
fi

# Verify the CRD includes elasticity fields.
CRD_SCHEMA="$(kubectl get crd resumabletrainingjobs.training.checkpoint.example.io -o jsonpath='{.spec.versions[0].schema.openAPIV3Schema}' 2>/dev/null || echo "")"
if printf '%s\n' "$CRD_SCHEMA" | grep -Fq 'elasticity'; then
  echo "PASS: CRD schema includes elasticity section"
else
  echo "FAIL: CRD schema missing elasticity section"
  FAIL=$((FAIL + 1))
fi

if printf '%s\n' "$CRD_SCHEMA" | grep -Fq 'targetWorkerCount'; then
  echo "PASS: CRD schema includes targetWorkerCount field"
else
  echo "FAIL: CRD schema missing targetWorkerCount field"
  FAIL=$((FAIL + 1))
fi

echo

# ── 2-4. Kueue configuration ────────────────────────────────────────

echo "--- Kueue configuration ---"

config_yaml="$(current_kueue_manager_config 2>/dev/null || echo "")"

if [[ -z "$config_yaml" ]]; then
  echo "FAIL: could not read Kueue manager config (is Kueue installed?)"
  FAIL=$((FAIL + 1))
fi

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

if printf '%s\n' "$config_yaml" | grep -Fq 'waitForPodsReady'; then
  echo "PASS: Kueue config has waitForPodsReady"
else
  echo "FAIL: Kueue config missing waitForPodsReady"
  FAIL=$((FAIL + 1))
fi

echo

# ── 5. Phase 9 ClusterQueue ─────────────────────────────────────────

echo "--- Queues ---"

if kubectl get clusterqueues.kueue.x-k8s.io phase9-cq >/dev/null 2>&1; then
  echo "PASS: ClusterQueue phase9-cq exists"
else
  echo "FAIL: ClusterQueue phase9-cq missing"
  FAIL=$((FAIL + 1))
fi

# Verify quota covers cpu and memory.
CQ_RESOURCES="$(kubectl get clusterqueues.kueue.x-k8s.io phase9-cq -o jsonpath='{.spec.resourceGroups[0].coveredResources}' 2>/dev/null || echo "")"
if printf '%s\n' "$CQ_RESOURCES" | grep -Fq 'cpu'; then
  echo "PASS: ClusterQueue phase9-cq covers cpu"
else
  echo "FAIL: ClusterQueue phase9-cq does not cover cpu"
  FAIL=$((FAIL + 1))
fi
if printf '%s\n' "$CQ_RESOURCES" | grep -Fq 'memory'; then
  echo "PASS: ClusterQueue phase9-cq covers memory"
else
  echo "FAIL: ClusterQueue phase9-cq does not cover memory"
  FAIL=$((FAIL + 1))
fi

# ── 6. Phase 9 LocalQueue ───────────────────────────────────────────

if kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase9-training >/dev/null 2>&1; then
  echo "PASS: LocalQueue phase9-training exists"
else
  echo "FAIL: LocalQueue phase9-training missing"
  FAIL=$((FAIL + 1))
fi

LQ_CQ="$(kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase9-training -o jsonpath='{.spec.clusterQueue}' 2>/dev/null || echo "")"
if [[ "$LQ_CQ" == "phase9-cq" ]]; then
  echo "PASS: LocalQueue phase9-training points to phase9-cq"
else
  echo "FAIL: LocalQueue clusterQueue='${LQ_CQ}', expected 'phase9-cq'"
  FAIL=$((FAIL + 1))
fi

echo

# ── 7. Dynamic reclaim quota verification ────────────────────────────

echo "--- Dynamic reclaim quota ---"

CPU_QUOTA="$(kubectl get clusterqueues.kueue.x-k8s.io phase9-cq -o jsonpath='{.spec.resourceGroups[0].flavors[0].resources[?(@.name=="cpu")].nominalQuota}' 2>/dev/null || echo "")"
if [[ "$CPU_QUOTA" == "1250m" ]]; then
  echo "PASS: CPU quota is 1250m (correct for dynamic reclaim demo)"
else
  echo "FAIL: CPU quota='${CPU_QUOTA}', expected '1250m'"
  FAIL=$((FAIL + 1))
fi

MEM_QUOTA="$(kubectl get clusterqueues.kueue.x-k8s.io phase9-cq -o jsonpath='{.spec.resourceGroups[0].flavors[0].resources[?(@.name=="memory")].nominalQuota}' 2>/dev/null || echo "")"
if [[ "$MEM_QUOTA" == "1280Mi" ]]; then
  echo "PASS: Memory quota is 1280Mi (correct for dynamic reclaim demo)"
else
  echo "FAIL: Memory quota='${MEM_QUOTA}', expected '1280Mi'"
  FAIL=$((FAIL + 1))
fi

PREEMPTION_POLICY="$(kubectl get clusterqueues.kueue.x-k8s.io phase9-cq -o jsonpath='{.spec.preemption.withinClusterQueue}' 2>/dev/null || echo "")"
if [[ "$PREEMPTION_POLICY" == "LowerPriority" ]]; then
  echo "PASS: Preemption withinClusterQueue=LowerPriority"
else
  echo "FAIL: Preemption withinClusterQueue='${PREEMPTION_POLICY}', expected 'LowerPriority'"
  FAIL=$((FAIL + 1))
fi

echo

# ── 8-10. Sample RTJ validation (dry-run) ────────────────────────────

echo "--- Sample RTJ validation (dry-run) ---"

SHRINK_MANIFEST="$(sed \
  -e 's|__RTJ_NAME__|phase9-smoke-shrink|g' \
  -e 's|__TRAINER_IMAGE__|busybox:latest|g' \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "$REPO_ROOT/deploy/dev/phase9/samples/rtj-elastic-shrink.yaml")"

if printf '%s\n' "$SHRINK_MANIFEST" | kubectl apply --dry-run=server -f - >/dev/null 2>&1; then
  echo "PASS: Elastic shrink RTJ sample validates (dry-run)"
else
  echo "FAIL: Elastic shrink RTJ sample fails validation"
  printf '%s\n' "$SHRINK_MANIFEST" | kubectl apply --dry-run=server -f - 2>&1 | head -5
  FAIL=$((FAIL + 1))
fi

GROW_MANIFEST="$(sed \
  -e 's|__RTJ_NAME__|phase9-smoke-grow|g' \
  -e 's|__TRAINER_IMAGE__|busybox:latest|g' \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "$REPO_ROOT/deploy/dev/phase9/samples/rtj-elastic-grow.yaml")"

if printf '%s\n' "$GROW_MANIFEST" | kubectl apply --dry-run=server -f - >/dev/null 2>&1; then
  echo "PASS: Elastic grow RTJ sample validates (dry-run)"
else
  echo "FAIL: Elastic grow RTJ sample fails validation"
  printf '%s\n' "$GROW_MANIFEST" | kubectl apply --dry-run=server -f - 2>&1 | head -5
  FAIL=$((FAIL + 1))
fi

NONELASTIC_MANIFEST="$(sed \
  -e 's|__RTJ_NAME__|phase9-smoke-nonelastic|g' \
  -e 's|__TRAINER_IMAGE__|busybox:latest|g' \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "$REPO_ROOT/deploy/dev/phase9/samples/rtj-non-elastic.yaml")"

if printf '%s\n' "$NONELASTIC_MANIFEST" | kubectl apply --dry-run=server -f - >/dev/null 2>&1; then
  echo "PASS: Non-elastic RTJ sample validates (dry-run)"
else
  echo "FAIL: Non-elastic RTJ sample fails validation"
  printf '%s\n' "$NONELASTIC_MANIFEST" | kubectl apply --dry-run=server -f - 2>&1 | head -5
  FAIL=$((FAIL + 1))
fi

echo

# ── 11. Resize fixture knobs in manifests ────────────────────────────

echo "--- Resize fixture knobs ---"

if printf '%s\n' "$SHRINK_MANIFEST" | grep -Fq 'YIELD_SDK_ELASTICITY_MODE'; then
  echo "PASS: Shrink sample includes YIELD_SDK_ELASTICITY_MODE"
else
  echo "FAIL: Shrink sample missing YIELD_SDK_ELASTICITY_MODE"
  FAIL=$((FAIL + 1))
fi

if printf '%s\n' "$SHRINK_MANIFEST" | grep -Fq 'YIELD_SDK_SUPPORTS_IN_PLACE_SHRINK'; then
  echo "PASS: Shrink sample includes YIELD_SDK_SUPPORTS_IN_PLACE_SHRINK"
else
  echo "FAIL: Shrink sample missing YIELD_SDK_SUPPORTS_IN_PLACE_SHRINK"
  FAIL=$((FAIL + 1))
fi

if printf '%s\n' "$SHRINK_MANIFEST" | grep -Fq 'YIELD_SDK_RESIZE_SIGNAL_DIR'; then
  echo "PASS: Shrink sample includes YIELD_SDK_RESIZE_SIGNAL_DIR"
else
  echo "FAIL: Shrink sample missing YIELD_SDK_RESIZE_SIGNAL_DIR"
  FAIL=$((FAIL + 1))
fi

if printf '%s\n' "$SHRINK_MANIFEST" | grep -Fq 'resize-signal'; then
  echo "PASS: Shrink sample includes resize-signal volume"
else
  echo "FAIL: Shrink sample missing resize-signal volume"
  FAIL=$((FAIL + 1))
fi

# Verify the elastic samples set mode to Manual.
ELASTICITY_MODE="$(printf '%s\n' "$SHRINK_MANIFEST" | grep -A1 'YIELD_SDK_ELASTICITY_MODE' | grep 'value:' | awk '{print $2}' | tr -d '"')"
if [[ "$ELASTICITY_MODE" == "Manual" ]]; then
  echo "PASS: Shrink sample YIELD_SDK_ELASTICITY_MODE=Manual"
else
  echo "FAIL: Shrink sample YIELD_SDK_ELASTICITY_MODE='${ELASTICITY_MODE}', expected 'Manual'"
  FAIL=$((FAIL + 1))
fi

# Verify non-elastic sample does NOT have elasticity env vars.
if printf '%s\n' "$NONELASTIC_MANIFEST" | grep -Fq 'YIELD_SDK_ELASTICITY_MODE'; then
  echo "FAIL: Non-elastic sample should NOT include YIELD_SDK_ELASTICITY_MODE"
  FAIL=$((FAIL + 1))
else
  echo "PASS: Non-elastic sample correctly omits YIELD_SDK_ELASTICITY_MODE"
fi

echo

# ── 12. reclaimablePods patching path ────────────────────────────────

echo "--- reclaimablePods patching path ---"

# Verify the Workload CRD supports reclaimablePods in status.
# This is always true for Kueue v0.15.1+ but we check to be explicit.
WL_CRD="$(kubectl get crd workloads.kueue.x-k8s.io -o jsonpath='{.spec.versions[0].schema.openAPIV3Schema}' 2>/dev/null || echo "")"
if printf '%s\n' "$WL_CRD" | grep -Fq 'reclaimablePods'; then
  echo "PASS: Workload CRD includes reclaimablePods in status schema"
else
  echo "FAIL: Workload CRD missing reclaimablePods field"
  echo "  Kueue version may be too old. Phase 9 requires Kueue v0.15.1+."
  FAIL=$((FAIL + 1))
fi

# Verify the Kueue Workload API group is available.
if kubectl api-resources --api-group=kueue.x-k8s.io 2>/dev/null | grep -q 'workloads'; then
  echo "PASS: Workload API available (kueue.x-k8s.io)"
else
  echo "FAIL: Workload API not available"
  FAIL=$((FAIL + 1))
fi

echo

# ── Summary ──────────────────────────────────────────────────────────

echo "phase9 resources:"
echo "  queues:"
kubectl get clusterqueues.kueue.x-k8s.io phase9-cq --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done
kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase9-training --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done
echo "  quota:"
echo "    cpu: ${CPU_QUOTA:-unknown}"
echo "    memory: ${MEM_QUOTA:-unknown}"
echo "  RTJ external framework: configured"
echo "  reclaimablePods SSA path: available"
echo "  field manager: rtj-elastic-reclaim (used by controller at runtime)"

echo
if [[ "$FAIL" -eq 0 ]]; then
  echo "=== Phase 9 smoke PASSED ==="
else
  echo "=== Phase 9 smoke FAILED (${FAIL} checks) ==="
  exit 1
fi
