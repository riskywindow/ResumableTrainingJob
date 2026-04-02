#!/usr/bin/env bash

# Phase 7 smoke test.
#
# Validates the Phase 7 dev environment is correctly configured:
#   1. Kueue manager config has RTJ external framework.
#   2. Kueue manager config has waitForPodsReady enabled.
#   3. Kueue manager config has ProvisioningACC feature gate.
#   4. ProvisioningRequest CRD is installed.
#   5. ProvisioningRequestConfig objects exist.
#   6. AdmissionChecks exist with correct controllerName.
#   7. Phase 7 ClusterQueue exists with admission check.
#   8. Phase 7 LocalQueue exists and points to phase7-cq.
#   9. Fake-provisioner Deployment is running.
#  10. ResumableTrainingJob CRD is installed.
#  11. Sample RTJ manifests can be dry-run applied.
#
# This is an infrastructure validation, not an RTJ-level test.

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

FAIL=0

echo "=== Phase 7 Smoke Test ==="
echo

# ── 1. Kueue RTJ framework ─────────────────────────────────────────

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

# ── 2. waitForPodsReady ────────────────────────────────────────────

if printf '%s\n' "$config_yaml" | grep -Fq 'waitForPodsReady'; then
  echo "PASS: Kueue config has waitForPodsReady section"
else
  echo "FAIL: Kueue config missing waitForPodsReady"
  FAIL=$((FAIL + 1))
fi

WFPR_ENABLE="$(printf '%s\n' "$config_yaml" | grep -A1 'waitForPodsReady' | grep 'enable:' | tr -d ' ' || echo "")"
if printf '%s\n' "$WFPR_ENABLE" | grep -Fq 'true'; then
  echo "PASS: waitForPodsReady enabled"
else
  echo "FAIL: waitForPodsReady not enabled"
  FAIL=$((FAIL + 1))
fi

if printf '%s\n' "$config_yaml" | grep -Fq 'requeuingStrategy'; then
  echo "PASS: Kueue config has requeuingStrategy"
else
  echo "FAIL: Kueue config missing requeuingStrategy"
  FAIL=$((FAIL + 1))
fi

# ── 3. ProvisioningACC feature gate ────────────────────────────────

if printf '%s\n' "$config_yaml" | grep -Fq 'ProvisioningACC'; then
  echo "PASS: Kueue config has ProvisioningACC feature gate"
else
  echo "INFO: ProvisioningACC feature gate not found (may be default-on)"
fi

# ── 4. ProvisioningRequest CRD ─────────────────────────────────────

if kubectl get crd provisioningrequests.autoscaling.x-k8s.io >/dev/null 2>&1; then
  echo "PASS: ProvisioningRequest CRD installed"
else
  echo "FAIL: ProvisioningRequest CRD not installed"
  FAIL=$((FAIL + 1))
fi

# ── 5. ProvisioningRequestConfig ───────────────────────────────────

if kubectl get provisioningrequestconfigs.kueue.x-k8s.io dev-provisioning-config >/dev/null 2>&1; then
  echo "PASS: ProvisioningRequestConfig dev-provisioning-config exists"
else
  echo "FAIL: ProvisioningRequestConfig dev-provisioning-config missing"
  FAIL=$((FAIL + 1))
fi

if kubectl get provisioningrequestconfigs.kueue.x-k8s.io dev-provisioning-failure-config >/dev/null 2>&1; then
  echo "PASS: ProvisioningRequestConfig dev-provisioning-failure-config exists"
else
  echo "FAIL: ProvisioningRequestConfig dev-provisioning-failure-config missing"
  FAIL=$((FAIL + 1))
fi

if kubectl get provisioningrequestconfigs.kueue.x-k8s.io dev-provisioning-expiry-config >/dev/null 2>&1; then
  echo "PASS: ProvisioningRequestConfig dev-provisioning-expiry-config exists"
else
  echo "FAIL: ProvisioningRequestConfig dev-provisioning-expiry-config missing"
  FAIL=$((FAIL + 1))
fi

# ── 6. AdmissionChecks ─────────────────────────────────────────────

check_admission_check() {
  local name="$1"
  if kubectl get admissionchecks.kueue.x-k8s.io "$name" >/dev/null 2>&1; then
    CONTROLLER="$(kubectl get admissionchecks.kueue.x-k8s.io "$name" -o jsonpath='{.spec.controllerName}' 2>/dev/null || echo "")"
    if [[ "$CONTROLLER" == "kueue.x-k8s.io/provisioning" ]]; then
      echo "PASS: AdmissionCheck $name exists (controllerName=kueue.x-k8s.io/provisioning)"
    else
      echo "FAIL: AdmissionCheck $name has wrong controllerName='${CONTROLLER}'"
      FAIL=$((FAIL + 1))
    fi
  else
    echo "FAIL: AdmissionCheck $name missing"
    FAIL=$((FAIL + 1))
  fi
}

check_admission_check "dev-provisioning"
check_admission_check "dev-provisioning-failure"
check_admission_check "dev-provisioning-expiry"

# ── 7. Phase 7 ClusterQueue ───────────────────────────────────────

if kubectl get clusterqueues.kueue.x-k8s.io phase7-cq >/dev/null 2>&1; then
  echo "PASS: ClusterQueue phase7-cq exists"
else
  echo "FAIL: ClusterQueue phase7-cq missing"
  FAIL=$((FAIL + 1))
fi

# Verify admission check is wired.
CQ_AC="$(kubectl get clusterqueues.kueue.x-k8s.io phase7-cq -o jsonpath='{.spec.admissionChecksStrategy.admissionChecks[0].name}' 2>/dev/null || echo "")"
if [[ "$CQ_AC" == "dev-provisioning" ]]; then
  echo "PASS: ClusterQueue phase7-cq has dev-provisioning admission check"
else
  echo "FAIL: ClusterQueue phase7-cq admission check='${CQ_AC}', expected 'dev-provisioning'"
  FAIL=$((FAIL + 1))
fi

# ── 8. Phase 7 LocalQueue ─────────────────────────────────────────

if kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase7-training >/dev/null 2>&1; then
  echo "PASS: LocalQueue phase7-training exists"
else
  echo "FAIL: LocalQueue phase7-training missing"
  FAIL=$((FAIL + 1))
fi

LQ_CQ="$(kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase7-training -o jsonpath='{.spec.clusterQueue}' 2>/dev/null || echo "")"
if [[ "$LQ_CQ" == "phase7-cq" ]]; then
  echo "PASS: LocalQueue phase7-training points to phase7-cq"
else
  echo "FAIL: LocalQueue clusterQueue='${LQ_CQ}', expected 'phase7-cq'"
  FAIL=$((FAIL + 1))
fi

# ── 9. Fake-provisioner Deployment ─────────────────────────────────

FP_READY="$(kubectl -n "$DEV_NAMESPACE" get deployment fake-provisioner -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")"
if [[ "${FP_READY:-0}" -ge 1 ]]; then
  echo "PASS: fake-provisioner is running (${FP_READY} ready)"
else
  echo "FAIL: fake-provisioner not running (ready=${FP_READY:-0})"
  FAIL=$((FAIL + 1))
fi

# ── 10. ResumableTrainingJob CRD ───────────────────────────────────

if kubectl get crd resumabletrainingjobs.training.checkpoint.example.io >/dev/null 2>&1; then
  echo "PASS: ResumableTrainingJob CRD installed"
else
  echo "FAIL: ResumableTrainingJob CRD not installed"
  FAIL=$((FAIL + 1))
fi

# ── 11. Sample RTJ dry-run ────────────────────────────────────────

echo
echo "validating sample RTJ manifests (dry-run)..."

DELAYED_MANIFEST="$(sed \
  -e 's|__RTJ_NAME__|phase7-smoke-delayed|g' \
  -e 's|__TRAINER_IMAGE__|busybox:latest|g' \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "$REPO_ROOT/deploy/dev/phase7/samples/rtj-delayed-success.yaml")"

if printf '%s\n' "$DELAYED_MANIFEST" | kubectl apply --dry-run=server -f - >/dev/null 2>&1; then
  echo "PASS: Delayed-success RTJ sample validates (dry-run)"
else
  echo "FAIL: Delayed-success RTJ sample fails validation"
  FAIL=$((FAIL + 1))
fi

TIMEOUT_MANIFEST="$(sed \
  -e 's|__RTJ_NAME__|phase7-smoke-timeout|g' \
  -e 's|__TRAINER_IMAGE__|nonexistent:v999|g' \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  "$REPO_ROOT/deploy/dev/phase7/samples/rtj-startup-timeout.yaml")"

if printf '%s\n' "$TIMEOUT_MANIFEST" | kubectl apply --dry-run=server -f - >/dev/null 2>&1; then
  echo "PASS: Startup-timeout RTJ sample validates (dry-run)"
else
  echo "FAIL: Startup-timeout RTJ sample fails validation"
  FAIL=$((FAIL + 1))
fi

# ── Summary ───────────────────────────────────────────────────────

echo
echo "phase7 resources:"
echo "  admission checks:"
kubectl get admissionchecks.kueue.x-k8s.io -l checkpoint-native.dev/profile=phase7-provisioning --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done
echo "  provisioning request configs:"
kubectl get provisioningrequestconfigs.kueue.x-k8s.io -l checkpoint-native.dev/profile=phase7-provisioning --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done
echo "  queues:"
kubectl get clusterqueues.kueue.x-k8s.io phase7-cq --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done
kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase7-training --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done
echo "  fake-provisioner:"
kubectl -n "$DEV_NAMESPACE" get deployment fake-provisioner --no-headers 2>/dev/null | while IFS= read -r line; do
  echo "    $line"
done

echo
if [[ "$FAIL" -eq 0 ]]; then
  echo "=== Phase 7 smoke PASSED ==="
else
  echo "=== Phase 7 smoke FAILED (${FAIL} checks) ==="
  exit 1
fi
