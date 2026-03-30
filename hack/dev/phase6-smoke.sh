#!/usr/bin/env bash

# Phase 6 smoke test.
#
# Validates the Phase 6 multi-cluster dev environment:
#   1. Manager cluster exists and is reachable.
#   2. Worker-1 cluster exists and is reachable.
#   3. Worker-2 cluster exists and is reachable.
#   4. Kueue is installed and running on all clusters.
#   5. MultiKueue AdmissionCheck exists on manager.
#   6. MultiKueueConfig exists on manager.
#   7. MultiKueueCluster resources exist on manager.
#   8. Manager ClusterQueue has MultiKueue admission check.
#   9. Manager LocalQueue exists.
#  10. Worker ClusterQueues exist with real quotas.
#  11. Worker LocalQueues exist with matching queue names.
#  12. RTJ CRD is installed on all clusters.
#  13. Shared checkpoint store is reachable from manager.
#  14. Checkpoint credentials Secret exists on all clusters.
#  15. Sample RTJ manifest validates (dry-run on manager).
#
# This is an infrastructure validation, not an e2e test.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$REPO_ROOT/hack/dev/common.sh"

require_command kubectl
require_command kind
require_command docker

PHASE6_MANAGER="${PHASE6_MANAGER:-phase6-manager}"
PHASE6_WORKER_1="${PHASE6_WORKER_1:-phase6-worker-1}"
PHASE6_WORKER_2="${PHASE6_WORKER_2:-phase6-worker-2}"
DEV_NAMESPACE="${DEV_NAMESPACE:-checkpoint-dev}"

MANAGER_CTX="kind-${PHASE6_MANAGER}"
WORKER1_CTX="kind-${PHASE6_WORKER_1}"
WORKER2_CTX="kind-${PHASE6_WORKER_2}"

FAIL=0

echo "=== Phase 6 Smoke Test ==="
echo

# ── 1-3. Cluster existence ────────────────────────────────────────

check_cluster() {
  local name="$1"
  local ctx="kind-${name}"

  if kind get clusters 2>/dev/null | grep -Fxq "$name"; then
    echo "PASS: cluster ${name} exists"
  else
    echo "FAIL: cluster ${name} does not exist"
    FAIL=$((FAIL + 1))
    return 1
  fi

  if kubectl cluster-info --context "$ctx" >/dev/null 2>&1; then
    echo "PASS: cluster ${name} is reachable"
  else
    echo "FAIL: cluster ${name} is not reachable"
    FAIL=$((FAIL + 1))
    return 1
  fi
}

check_cluster "$PHASE6_MANAGER" || true
check_cluster "$PHASE6_WORKER_1" || true
check_cluster "$PHASE6_WORKER_2" || true
echo

# ── 4. Kueue on all clusters ─────────────────────────────────────

check_kueue() {
  local ctx="$1"
  local name="$2"

  if kubectl -n "$KUEUE_NAMESPACE" get deployment "$KUEUE_DEPLOYMENT_NAME" --context "$ctx" >/dev/null 2>&1; then
    local ready
    ready="$(kubectl -n "$KUEUE_NAMESPACE" get deployment "$KUEUE_DEPLOYMENT_NAME" \
      -o jsonpath='{.status.readyReplicas}' --context "$ctx" 2>/dev/null || echo "0")"
    if [[ "$ready" -ge 1 ]]; then
      echo "PASS: Kueue running on ${name} (ready=${ready})"
    else
      echo "FAIL: Kueue not ready on ${name} (ready=${ready})"
      FAIL=$((FAIL + 1))
    fi
  else
    echo "FAIL: Kueue deployment not found on ${name}"
    FAIL=$((FAIL + 1))
  fi
}

check_kueue "$MANAGER_CTX" "$PHASE6_MANAGER"
check_kueue "$WORKER1_CTX" "$PHASE6_WORKER_1"
check_kueue "$WORKER2_CTX" "$PHASE6_WORKER_2"
echo

# ── 5. MultiKueue AdmissionCheck ─────────────────────────────────

if kubectl get admissionchecks.kueue.x-k8s.io multikueue --context "$MANAGER_CTX" >/dev/null 2>&1; then
  echo "PASS: AdmissionCheck 'multikueue' exists on manager"
else
  echo "FAIL: AdmissionCheck 'multikueue' missing on manager"
  FAIL=$((FAIL + 1))
fi

# ── 6. MultiKueueConfig ──────────────────────────────────────────

if kubectl get multikueueconfigs.kueue.x-k8s.io multikueue-config --context "$MANAGER_CTX" >/dev/null 2>&1; then
  echo "PASS: MultiKueueConfig 'multikueue-config' exists on manager"
else
  echo "FAIL: MultiKueueConfig 'multikueue-config' missing on manager"
  FAIL=$((FAIL + 1))
fi

# ── 7. MultiKueueCluster resources ───────────────────────────────

for cluster in worker-1 worker-2; do
  if kubectl get multikueueclusters.kueue.x-k8s.io "$cluster" --context "$MANAGER_CTX" >/dev/null 2>&1; then
    active_status="$(kubectl get multikueueclusters.kueue.x-k8s.io "$cluster" \
      -o jsonpath='{.status.conditions[?(@.type=="Active")].status}' \
      --context "$MANAGER_CTX" 2>/dev/null || echo "Unknown")"
    echo "PASS: MultiKueueCluster '${cluster}' exists (Active=${active_status})"
  else
    echo "FAIL: MultiKueueCluster '${cluster}' missing on manager"
    FAIL=$((FAIL + 1))
  fi
done

# ── 8. Manager ClusterQueue ──────────────────────────────────────

if kubectl get clusterqueues.kueue.x-k8s.io phase6-multikueue-cq --context "$MANAGER_CTX" >/dev/null 2>&1; then
  cq_checks="$(kubectl get clusterqueues.kueue.x-k8s.io phase6-multikueue-cq \
    -o jsonpath='{.spec.admissionChecks}' --context "$MANAGER_CTX" 2>/dev/null)"
  if echo "$cq_checks" | grep -Fq 'multikueue'; then
    echo "PASS: Manager ClusterQueue has MultiKueue admission check"
  else
    echo "FAIL: Manager ClusterQueue missing MultiKueue admission check"
    FAIL=$((FAIL + 1))
  fi
else
  echo "FAIL: Manager ClusterQueue phase6-multikueue-cq missing"
  FAIL=$((FAIL + 1))
fi

# ── 9. Manager LocalQueue ────────────────────────────────────────

if kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase6-training \
  --context "$MANAGER_CTX" >/dev/null 2>&1; then
  echo "PASS: Manager LocalQueue phase6-training exists"
else
  echo "FAIL: Manager LocalQueue phase6-training missing"
  FAIL=$((FAIL + 1))
fi

# ── 10. Worker ClusterQueues ─────────────────────────────────────

check_worker_cq() {
  local ctx="$1"
  local name="$2"

  if kubectl get clusterqueues.kueue.x-k8s.io phase6-worker-cq --context "$ctx" >/dev/null 2>&1; then
    echo "PASS: Worker ClusterQueue phase6-worker-cq exists on ${name}"
  else
    echo "FAIL: Worker ClusterQueue phase6-worker-cq missing on ${name}"
    FAIL=$((FAIL + 1))
  fi
}

check_worker_cq "$WORKER1_CTX" "$PHASE6_WORKER_1"
check_worker_cq "$WORKER2_CTX" "$PHASE6_WORKER_2"

# ── 11. Worker LocalQueues ───────────────────────────────────────

check_worker_lq() {
  local ctx="$1"
  local name="$2"

  if kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase6-training \
    --context "$ctx" >/dev/null 2>&1; then
    local lq_cq
    lq_cq="$(kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase6-training \
      -o jsonpath='{.spec.clusterQueue}' --context "$ctx" 2>/dev/null)"
    if [[ "$lq_cq" == "phase6-worker-cq" ]]; then
      echo "PASS: Worker LocalQueue phase6-training on ${name} -> phase6-worker-cq"
    else
      echo "FAIL: Worker LocalQueue on ${name} points to '${lq_cq}', expected 'phase6-worker-cq'"
      FAIL=$((FAIL + 1))
    fi
  else
    echo "FAIL: Worker LocalQueue phase6-training missing on ${name}"
    FAIL=$((FAIL + 1))
  fi
}

check_worker_lq "$WORKER1_CTX" "$PHASE6_WORKER_1"
check_worker_lq "$WORKER2_CTX" "$PHASE6_WORKER_2"
echo

# ── 12. RTJ CRD on all clusters ──────────────────────────────────

check_rtj_crd() {
  local ctx="$1"
  local name="$2"

  if kubectl get crd resumabletrainingjobs.training.checkpoint.example.io \
    --context "$ctx" >/dev/null 2>&1; then
    echo "PASS: RTJ CRD installed on ${name}"
  else
    echo "FAIL: RTJ CRD missing on ${name}"
    FAIL=$((FAIL + 1))
  fi
}

check_rtj_crd "$MANAGER_CTX" "$PHASE6_MANAGER"
check_rtj_crd "$WORKER1_CTX" "$PHASE6_WORKER_1"
check_rtj_crd "$WORKER2_CTX" "$PHASE6_WORKER_2"
echo

# ── 13. Shared checkpoint store reachable ─────────────────────────

STORE_ENDPOINT="$(kubectl -n "$DEV_NAMESPACE" get configmap shared-checkpoint-store \
  -o jsonpath='{.data.endpoint}' --context "$MANAGER_CTX" 2>/dev/null || echo "")"

if [[ -n "$STORE_ENDPOINT" ]]; then
  echo "PASS: Shared store ConfigMap exists (endpoint: ${STORE_ENDPOINT})"

  # Test reachability from the manager cluster's control-plane container.
  MANAGER_CONTAINER="${PHASE6_MANAGER}-control-plane"
  if docker exec "$MANAGER_CONTAINER" wget -q -O /dev/null --timeout=5 "${STORE_ENDPOINT}/minio/health/ready" 2>/dev/null; then
    echo "PASS: Shared checkpoint store is reachable from manager"
  else
    echo "FAIL: Shared checkpoint store is NOT reachable from manager at ${STORE_ENDPOINT}"
    FAIL=$((FAIL + 1))
  fi
else
  echo "FAIL: Shared store ConfigMap missing on manager"
  FAIL=$((FAIL + 1))
fi

# ── 14. Checkpoint credentials on all clusters ───────────────────

check_credentials() {
  local ctx="$1"
  local name="$2"

  if kubectl -n "$DEV_NAMESPACE" get secret checkpoint-storage-credentials \
    --context "$ctx" >/dev/null 2>&1; then
    echo "PASS: Checkpoint credentials Secret on ${name}"
  else
    echo "FAIL: Checkpoint credentials Secret missing on ${name}"
    FAIL=$((FAIL + 1))
  fi
}

check_credentials "$MANAGER_CTX" "$PHASE6_MANAGER"
check_credentials "$WORKER1_CTX" "$PHASE6_WORKER_1"
check_credentials "$WORKER2_CTX" "$PHASE6_WORKER_2"
echo

# ── 15. Sample RTJ dry-run ───────────────────────────────────────

echo "validating sample RTJ manifest (dry-run on manager)..."

SAMPLE_MANIFEST="$(sed \
  -e 's|__RTJ_NAME__|phase6-smoke-test|g' \
  -e 's|__TRAINER_IMAGE__|busybox:latest|g' \
  -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
  -e "s|__SHARED_ENDPOINT__|http://placeholder:30900|g" \
  "$REPO_ROOT/deploy/dev/phase6/samples/rtj-multikueue-dispatch.yaml")"

if printf '%s\n' "$SAMPLE_MANIFEST" | kubectl apply --dry-run=server -f - --context "$MANAGER_CTX" >/dev/null 2>&1; then
  echo "PASS: Sample MultiKueue RTJ validates (dry-run)"
else
  echo "FAIL: Sample MultiKueue RTJ fails validation"
  FAIL=$((FAIL + 1))
fi

# ── Summary ───────────────────────────────────────────────────────

echo
echo "phase6 topology:"
echo "  manager:  kind-${PHASE6_MANAGER}"
echo "  worker-1: kind-${PHASE6_WORKER_1}"
echo "  worker-2: kind-${PHASE6_WORKER_2}"
echo "  shared store: ${STORE_ENDPOINT:-unknown}"
echo

if [[ "$FAIL" -eq 0 ]]; then
  echo "=== Phase 6 smoke PASSED ==="
else
  echo "=== Phase 6 smoke FAILED (${FAIL} checks) ==="
  exit 1
fi
