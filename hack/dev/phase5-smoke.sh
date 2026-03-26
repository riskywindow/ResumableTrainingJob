#!/usr/bin/env bash

# Phase 5 smoke test.
#
# Validates the Phase 5 dev environment is correctly configured:
#   1. Kueue manager config has RTJ external framework.
#   2. CheckpointPriorityPolicy CRD is installed.
#   3. Sample CheckpointPriorityPolicy exists (dev-checkpoint-priority).
#   4. Phase 5 WorkloadPriorityClasses exist (phase5-low, phase5-high).
#   5. Phase 5 ClusterQueue exists with LowerPriority preemption.
#   6. Phase 5 LocalQueue exists and points to phase5-cq.
#   7. ClusterQueue has correct preemption policy (no cohort borrowing/reclaim).
#   8. Sample RTJ manifests can be dry-run applied.
#
# This is an infrastructure validation, not an RTJ-level test.

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

FAIL=0

echo "=== Phase 5 Smoke Test ==="
echo

# ── 1. Kueue config ──────────────────────────────────────────────────

config_yaml="$(current_kueue_manager_config)"
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

# ── 2. CRDs ──────────────────────────────────────────────────────────

if kubectl get crd resumabletrainingjobs.training.checkpoint.example.io >/dev/null 2>&1; then
  echo "PASS: ResumableTrainingJob CRD installed"
else
  echo "FAIL: ResumableTrainingJob CRD not installed"
  FAIL=$((FAIL + 1))
fi

if kubectl get crd checkpointprioritypolicies.training.checkpoint.example.io >/dev/null 2>&1; then
  echo "PASS: CheckpointPriorityPolicy CRD installed"
else
  echo "FAIL: CheckpointPriorityPolicy CRD not installed"
  FAIL=$((FAIL + 1))
fi

# ── 3. Sample CheckpointPriorityPolicy ───────────────────────────────

if kubectl get checkpointprioritypolicies.training.checkpoint.example.io dev-checkpoint-priority >/dev/null 2>&1; then
  echo "PASS: CheckpointPriorityPolicy dev-checkpoint-priority exists"
else
  echo "FAIL: CheckpointPriorityPolicy dev-checkpoint-priority missing"
  FAIL=$((FAIL + 1))
fi

# Verify key policy fields.
CPP_FRESHNESS="$(kubectl get checkpointprioritypolicies.training.checkpoint.example.io dev-checkpoint-priority -o jsonpath='{.spec.checkpointFreshnessTarget}' 2>/dev/null || echo "")"
if [[ -n "$CPP_FRESHNESS" ]]; then
  echo "PASS: Policy checkpointFreshnessTarget=${CPP_FRESHNESS}"
else
  echo "FAIL: Policy checkpointFreshnessTarget not set"
  FAIL=$((FAIL + 1))
fi

CPP_PROTECTION="$(kubectl get checkpointprioritypolicies.training.checkpoint.example.io dev-checkpoint-priority -o jsonpath='{.spec.startupProtectionWindow}' 2>/dev/null || echo "")"
if [[ -n "$CPP_PROTECTION" ]]; then
  echo "PASS: Policy startupProtectionWindow=${CPP_PROTECTION}"
else
  echo "FAIL: Policy startupProtectionWindow not set"
  FAIL=$((FAIL + 1))
fi

# ── 4. WorkloadPriorityClasses ────────────────────────────────────────

if kubectl get workloadpriorityclasses.kueue.x-k8s.io phase5-low >/dev/null 2>&1; then
  LOW_VALUE="$(kubectl get workloadpriorityclasses.kueue.x-k8s.io phase5-low -o jsonpath='{.value}')"
  echo "PASS: WorkloadPriorityClass phase5-low exists (value=${LOW_VALUE})"
else
  echo "FAIL: WorkloadPriorityClass phase5-low missing"
  FAIL=$((FAIL + 1))
fi

if kubectl get workloadpriorityclasses.kueue.x-k8s.io phase5-high >/dev/null 2>&1; then
  HIGH_VALUE="$(kubectl get workloadpriorityclasses.kueue.x-k8s.io phase5-high -o jsonpath='{.value}')"
  echo "PASS: WorkloadPriorityClass phase5-high exists (value=${HIGH_VALUE})"
else
  echo "FAIL: WorkloadPriorityClass phase5-high missing"
  FAIL=$((FAIL + 1))
fi

# ── 5. Phase 5 ClusterQueue ──────────────────────────────────────────

if kubectl get clusterqueues.kueue.x-k8s.io phase5-cq >/dev/null 2>&1; then
  echo "PASS: ClusterQueue phase5-cq exists"
else
  echo "FAIL: ClusterQueue phase5-cq missing"
  FAIL=$((FAIL + 1))
fi

# Verify preemption policy.
CQ_PREEMPTION_WITHIN="$(kubectl get clusterqueues.kueue.x-k8s.io phase5-cq -o jsonpath='{.spec.preemption.withinClusterQueue}' 2>/dev/null || echo "")"
if [[ "$CQ_PREEMPTION_WITHIN" == "LowerPriority" ]]; then
  echo "PASS: ClusterQueue preemption withinClusterQueue=LowerPriority"
else
  echo "FAIL: ClusterQueue preemption withinClusterQueue='${CQ_PREEMPTION_WITHIN}', expected 'LowerPriority'"
  FAIL=$((FAIL + 1))
fi

CQ_RECLAIM="$(kubectl get clusterqueues.kueue.x-k8s.io phase5-cq -o jsonpath='{.spec.preemption.reclaimWithinCohort}' 2>/dev/null || echo "")"
if [[ "$CQ_RECLAIM" == "Never" ]]; then
  echo "PASS: ClusterQueue preemption reclaimWithinCohort=Never"
else
  echo "FAIL: ClusterQueue preemption reclaimWithinCohort='${CQ_RECLAIM}', expected 'Never'"
  FAIL=$((FAIL + 1))
fi

# ── 6. Phase 5 LocalQueue ────────────────────────────────────────────

if kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase5-training >/dev/null 2>&1; then
  echo "PASS: LocalQueue phase5-training exists"
else
  echo "FAIL: LocalQueue phase5-training missing"
  FAIL=$((FAIL + 1))
fi

LQ_CQ="$(kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase5-training -o jsonpath='{.spec.clusterQueue}' 2>/dev/null || echo "")"
if [[ "$LQ_CQ" == "phase5-cq" ]]; then
  echo "PASS: LocalQueue points to phase5-cq"
else
  echo "FAIL: LocalQueue clusterQueue='${LQ_CQ}', expected 'phase5-cq'"
  FAIL=$((FAIL + 1))
fi

# ── 7. Sample RTJ dry-run ────────────────────────────────────────────

echo
echo "validating sample RTJ manifests (dry-run)..."

# Create a temporary rendered low-priority RTJ for dry-run validation.
LOW_MANIFEST="$(sed \
  -e 's|__RTJ_NAME__|phase5-smoke-low|g' \
  -e "s|__TRAINER_IMAGE__|busybox:latest|g" \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "$REPO_ROOT/deploy/dev/phase5/samples/rtj-low-priority.yaml")"

if printf '%s\n' "$LOW_MANIFEST" | kubectl apply --dry-run=server -f - >/dev/null 2>&1; then
  echo "PASS: Low-priority RTJ sample validates (dry-run)"
else
  echo "FAIL: Low-priority RTJ sample fails validation"
  FAIL=$((FAIL + 1))
fi

HIGH_MANIFEST="$(sed \
  -e 's|__RTJ_NAME__|phase5-smoke-high|g' \
  -e "s|__TRAINER_IMAGE__|busybox:latest|g" \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "$REPO_ROOT/deploy/dev/phase5/samples/rtj-high-priority.yaml")"

if printf '%s\n' "$HIGH_MANIFEST" | kubectl apply --dry-run=server -f - >/dev/null 2>&1; then
  echo "PASS: High-priority RTJ sample validates (dry-run)"
else
  echo "FAIL: High-priority RTJ sample fails validation"
  FAIL=$((FAIL + 1))
fi

# ── Summary ───────────────────────────────────────────────────────────

echo
echo "phase5 resources:"
echo "  priority classes:"
kubectl get workloadpriorityclasses.kueue.x-k8s.io -l checkpoint-native.dev/profile=phase5-priority-shaping --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done
echo "  policies:"
kubectl get checkpointprioritypolicies.training.checkpoint.example.io --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done
echo "  queues:"
kubectl get clusterqueues.kueue.x-k8s.io phase5-cq --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done
kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase5-training --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done

echo
if [[ "$FAIL" -eq 0 ]]; then
  echo "=== Phase 5 smoke PASSED ==="
else
  echo "=== Phase 5 smoke FAILED (${FAIL} checks) ==="
  exit 1
fi
