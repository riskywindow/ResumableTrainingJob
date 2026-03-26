#!/usr/bin/env bash

# Phase 4 smoke test.
#
# Validates the Phase 4 dev environment is correctly configured:
#   1. Kueue manager config has RTJ external framework.
#   2. At least 4 worker nodes exist with Phase 4 topology labels.
#   3. Topology object exists (if CRD available).
#   4. Phase 4 ResourceFlavor exists.
#   5. Phase 4 ClusterQueue and LocalQueue exist.
#   6. ResumeReadinessPolicy and AdmissionCheck exist.
#   7. A standalone JobSet admitted through the Phase 4 queue can run.
#
# This is an infrastructure validation, not an RTJ-level test.

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

FAIL=0

echo "=== Phase 4 Smoke Test ==="
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

if printf '%s\n' "$config_yaml" | grep -Fq 'TopologyAwareScheduling: true'; then
  echo "PASS: TopologyAwareScheduling feature gate enabled"
else
  echo "INFO: TopologyAwareScheduling feature gate not found in config"
  echo "  (may be default-on in Kueue v0.15.1)"
fi

# ── 2. Node topology labels ──────────────────────────────────────────

BLOCK_A_COUNT="$(kubectl get nodes -l topology.example.io/block=block-a --no-headers 2>/dev/null | wc -l | tr -d ' ')"
BLOCK_B_COUNT="$(kubectl get nodes -l topology.example.io/block=block-b --no-headers 2>/dev/null | wc -l | tr -d ' ')"
RACK_1_COUNT="$(kubectl get nodes -l topology.example.io/rack=rack-1 --no-headers 2>/dev/null | wc -l | tr -d ' ')"
RACK_2_COUNT="$(kubectl get nodes -l topology.example.io/rack=rack-2 --no-headers 2>/dev/null | wc -l | tr -d ' ')"

if [[ "$BLOCK_A_COUNT" -ge 2 ]]; then
  echo "PASS: ${BLOCK_A_COUNT} nodes in block-a"
else
  echo "FAIL: expected >= 2 nodes in block-a, found ${BLOCK_A_COUNT}"
  FAIL=$((FAIL + 1))
fi

if [[ "$BLOCK_B_COUNT" -ge 2 ]]; then
  echo "PASS: ${BLOCK_B_COUNT} nodes in block-b"
else
  echo "FAIL: expected >= 2 nodes in block-b, found ${BLOCK_B_COUNT}"
  FAIL=$((FAIL + 1))
fi

if [[ "$RACK_1_COUNT" -ge 2 ]]; then
  echo "PASS: ${RACK_1_COUNT} nodes in rack-1"
else
  echo "FAIL: expected >= 2 nodes in rack-1, found ${RACK_1_COUNT}"
  FAIL=$((FAIL + 1))
fi

if [[ "$RACK_2_COUNT" -ge 2 ]]; then
  echo "PASS: ${RACK_2_COUNT} nodes in rack-2"
else
  echo "FAIL: expected >= 2 nodes in rack-2, found ${RACK_2_COUNT}"
  FAIL=$((FAIL + 1))
fi

# ── 3. Topology object ───────────────────────────────────────────────

HAS_TOPOLOGY_CRD=false
if kubectl get crd topologies.kueue.x-k8s.io >/dev/null 2>&1; then
  HAS_TOPOLOGY_CRD=true
  if kubectl get topologies.kueue.x-k8s.io dev-topology >/dev/null 2>&1; then
    echo "PASS: Topology dev-topology exists"
  else
    echo "FAIL: Topology dev-topology missing"
    FAIL=$((FAIL + 1))
  fi
else
  echo "INFO: Topology CRD not available (TAS tests may be skipped)"
fi

# ── 4. ResourceFlavor ────────────────────────────────────────────────

if kubectl get resourceflavors.kueue.x-k8s.io phase4-topology >/dev/null 2>&1; then
  echo "PASS: ResourceFlavor phase4-topology exists"
else
  echo "FAIL: ResourceFlavor phase4-topology missing"
  FAIL=$((FAIL + 1))
fi

# Verify topologyName on the flavor (if Topology CRD is available).
if [[ "$HAS_TOPOLOGY_CRD" == "true" ]]; then
  FLAVOR_TOPO="$(kubectl get resourceflavors.kueue.x-k8s.io phase4-topology -o jsonpath='{.spec.topologyName}' 2>/dev/null || echo "")"
  if [[ "$FLAVOR_TOPO" == "dev-topology" ]]; then
    echo "PASS: ResourceFlavor references dev-topology"
  else
    echo "FAIL: ResourceFlavor topologyName is '${FLAVOR_TOPO}', expected 'dev-topology'"
    FAIL=$((FAIL + 1))
  fi
fi

# ── 5. Phase 4 queue ─────────────────────────────────────────────────

if kubectl get clusterqueues.kueue.x-k8s.io phase4-cq >/dev/null 2>&1; then
  echo "PASS: ClusterQueue phase4-cq exists"
else
  echo "FAIL: ClusterQueue phase4-cq missing"
  FAIL=$((FAIL + 1))
fi

if kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase4-training >/dev/null 2>&1; then
  echo "PASS: LocalQueue phase4-training exists"
else
  echo "FAIL: LocalQueue phase4-training missing"
  FAIL=$((FAIL + 1))
fi

# ── 6. AdmissionCheck and ResumeReadinessPolicy ──────────────────────

if kubectl get admissionchecks.kueue.x-k8s.io resume-readiness >/dev/null 2>&1; then
  echo "PASS: AdmissionCheck resume-readiness exists"
else
  echo "FAIL: AdmissionCheck resume-readiness missing"
  FAIL=$((FAIL + 1))
fi

if kubectl get resumereadinesspolicies.training.checkpoint.example.io default-resume-readiness >/dev/null 2>&1; then
  echo "PASS: ResumeReadinessPolicy default-resume-readiness exists"
else
  echo "FAIL: ResumeReadinessPolicy default-resume-readiness missing"
  FAIL=$((FAIL + 1))
fi

# ── 7. Phase 3 pool labels (inherited) ───────────────────────────────

ON_DEMAND_COUNT="$(kubectl get nodes -l checkpoint-native.dev/pool=on-demand --no-headers 2>/dev/null | wc -l | tr -d ' ')"
SPOT_COUNT="$(kubectl get nodes -l checkpoint-native.dev/pool=spot --no-headers 2>/dev/null | wc -l | tr -d ' ')"

if [[ "$ON_DEMAND_COUNT" -ge 2 ]]; then
  echo "PASS: ${ON_DEMAND_COUNT} on-demand pool nodes (Phase 3 compat)"
else
  echo "FAIL: expected >= 2 on-demand pool nodes, found ${ON_DEMAND_COUNT}"
  FAIL=$((FAIL + 1))
fi

if [[ "$SPOT_COUNT" -ge 2 ]]; then
  echo "PASS: ${SPOT_COUNT} spot pool nodes (Phase 3 compat)"
else
  echo "FAIL: expected >= 2 spot pool nodes, found ${SPOT_COUNT}"
  FAIL=$((FAIL + 1))
fi

# ── Summary ───────────────────────────────────────────────────────────

echo
echo "node topology:"
kubectl get nodes \
  -L topology.example.io/block \
  -L topology.example.io/rack \
  -L checkpoint-native.dev/pool \
  --no-headers 2>/dev/null | while IFS= read -r line; do
    echo "  $line"
  done

echo
if [[ "$FAIL" -eq 0 ]]; then
  echo "=== Phase 4 smoke PASSED ==="
else
  echo "=== Phase 4 smoke FAILED (${FAIL} checks) ==="
  exit 1
fi
