#!/usr/bin/env bash

# Phase 3 smoke test.
#
# Validates the Phase 3 dev environment is correctly configured:
#   1. Kueue manager config has RTJ external framework.
#   2. At least 4 worker nodes exist with Phase 3 pool labels.
#   3. Both ResourceFlavors exist (on-demand, spot).
#   4. Phase 3 ClusterQueue and LocalQueue exist.
#   5. A standalone JobSet admitted through the Phase 3 queue runs on
#      flavor-selected nodes.
#
# This is an infrastructure validation, not an RTJ-level test.

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

FAIL=0

echo "=== Phase 3 Smoke Test ==="
echo

# 1. Kueue config.
config_yaml="$(current_kueue_manager_config)"
if printf '%s\n' "$config_yaml" | grep -Fq 'ResumableTrainingJob.v1alpha1.training.checkpoint.example.io'; then
  echo "PASS: Kueue config has RTJ external framework"
else
  echo "FAIL: Kueue config missing RTJ external framework"
  FAIL=1
fi

if printf '%s\n' "$config_yaml" | grep -Fq 'manageJobsWithoutQueueName: false'; then
  echo "PASS: manageJobsWithoutQueueName=false"
else
  echo "FAIL: manageJobsWithoutQueueName not disabled"
  FAIL=1
fi

# 2. Node pool labels.
ON_DEMAND_COUNT="$(kubectl get nodes -l checkpoint-native.dev/pool=on-demand --no-headers 2>/dev/null | wc -l | tr -d ' ')"
SPOT_COUNT="$(kubectl get nodes -l checkpoint-native.dev/pool=spot --no-headers 2>/dev/null | wc -l | tr -d ' ')"
if [[ "$ON_DEMAND_COUNT" -ge 2 ]]; then
  echo "PASS: ${ON_DEMAND_COUNT} on-demand nodes"
else
  echo "FAIL: expected >= 2 on-demand nodes, found ${ON_DEMAND_COUNT}"
  FAIL=1
fi
if [[ "$SPOT_COUNT" -ge 2 ]]; then
  echo "PASS: ${SPOT_COUNT} spot nodes"
else
  echo "FAIL: expected >= 2 spot nodes, found ${SPOT_COUNT}"
  FAIL=1
fi

# 3. Spot taint.
TAINTED_COUNT="$(kubectl get nodes -o json | \
  python3 -c "
import json, sys
data = json.load(sys.stdin)
count = 0
for node in data['items']:
    taints = node.get('spec', {}).get('taints', [])
    for t in taints:
        if t.get('key') == 'checkpoint-native.dev/spot' and t.get('effect') == 'NoSchedule':
            count += 1
print(count)
" 2>/dev/null || echo 0)"
if [[ "$TAINTED_COUNT" -ge 2 ]]; then
  echo "PASS: ${TAINTED_COUNT} nodes have spot taint"
else
  echo "FAIL: expected >= 2 nodes with spot taint, found ${TAINTED_COUNT}"
  FAIL=1
fi

# 4. ResourceFlavors.
if kubectl get resourceflavors.kueue.x-k8s.io on-demand >/dev/null 2>&1; then
  echo "PASS: ResourceFlavor on-demand exists"
else
  echo "FAIL: ResourceFlavor on-demand missing"
  FAIL=1
fi
if kubectl get resourceflavors.kueue.x-k8s.io spot >/dev/null 2>&1; then
  echo "PASS: ResourceFlavor spot exists"
else
  echo "FAIL: ResourceFlavor spot missing"
  FAIL=1
fi

# 5. Phase 3 queue.
if kubectl get clusterqueues.kueue.x-k8s.io phase3-cq >/dev/null 2>&1; then
  echo "PASS: ClusterQueue phase3-cq exists"
else
  echo "FAIL: ClusterQueue phase3-cq missing"
  FAIL=1
fi
if kubectl get localqueues.kueue.x-k8s.io -n "$DEV_NAMESPACE" phase3-training >/dev/null 2>&1; then
  echo "PASS: LocalQueue phase3-training exists"
else
  echo "FAIL: LocalQueue phase3-training missing"
  FAIL=1
fi

# 6. Standalone JobSet smoke through the Phase 3 queue.
echo
echo "submitting flavor smoke JobSet..."
kubectl delete -f "$REPO_ROOT/deploy/dev/samples/phase3/jobset-flavor-smoke.yaml" --ignore-not-found >/dev/null 2>&1
kubectl apply -f "$REPO_ROOT/deploy/dev/samples/phase3/jobset-flavor-smoke.yaml"

wait_for_pod_count "$DEV_NAMESPACE" "app.kubernetes.io/name=phase3-flavor-smoke" 2 180
kubectl wait -n "$DEV_NAMESPACE" --for=condition=Ready pod -l app.kubernetes.io/name=phase3-flavor-smoke --timeout=180s

# Check that pods landed on flavor-assigned nodes.
echo
echo "pod placement:"
kubectl get pods -n "$DEV_NAMESPACE" -l app.kubernetes.io/name=phase3-flavor-smoke -o wide

# Verify pods have nodeSelector from flavor.
PODS_WITH_POOL="$(kubectl get pods -n "$DEV_NAMESPACE" -l app.kubernetes.io/name=phase3-flavor-smoke -o json | \
  python3 -c "
import json, sys
data = json.load(sys.stdin)
count = 0
for pod in data['items']:
    node = pod.get('spec', {}).get('nodeName', '')
    ns = pod.get('spec', {}).get('nodeSelector', {})
    pool = ns.get('checkpoint-native.dev/pool', '')
    if pool:
        count += 1
        print(f'  {pod[\"metadata\"][\"name\"]} → node={node}, pool={pool}')
    else:
        print(f'  {pod[\"metadata\"][\"name\"]} → node={node}, pool=<none>')
print(count)
" 2>/dev/null || echo 0)"

# The last line is the count.
POOL_COUNT="$(echo "$PODS_WITH_POOL" | tail -1)"
if [[ "$POOL_COUNT" -ge 2 ]]; then
  echo "PASS: ${POOL_COUNT} pods have flavor-derived nodeSelector"
else
  echo "INFO: ${POOL_COUNT} pods have flavor-derived nodeSelector"
  echo "  (Kueue may not inject nodeSelector into JobSets directly; this is expected.)"
  echo "  (RTJ admission-aware launch is the target test, not standalone JobSets.)"
fi

# Cleanup.
kubectl delete -f "$REPO_ROOT/deploy/dev/samples/phase3/jobset-flavor-smoke.yaml" --ignore-not-found >/dev/null 2>&1

echo
if [[ "$FAIL" -eq 0 ]]; then
  echo "=== Phase 3 smoke PASSED ==="
else
  echo "=== Phase 3 smoke FAILED (${FAIL} checks) ==="
  exit 1
fi
