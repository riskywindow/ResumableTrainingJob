#!/usr/bin/env bash
#
# Inspect the ResumeReadiness AdmissionCheck and its associated policy.
# Shows: AdmissionCheck status (Active condition), ResumeReadinessPolicy spec,
# and all Workloads referencing this check with their check states.
#
# Usage:
#   make phase4-inspect-admissioncheck
#   ./hack/dev/inspect-admissioncheck.sh

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

ADMISSION_CHECK_NAME="${ADMISSION_CHECK_NAME:-resume-readiness}"

echo "=== AdmissionCheck: ${ADMISSION_CHECK_NAME} ==="
kubectl get admissionchecks.kueue.x-k8s.io "$ADMISSION_CHECK_NAME" \
  -o jsonpath=$'controllerName: {.spec.controllerName}\nparameters: {.spec.parameters}\n' 2>/dev/null || echo "<AdmissionCheck not found>"
echo

echo "=== AdmissionCheck conditions ==="
kubectl get admissionchecks.kueue.x-k8s.io "$ADMISSION_CHECK_NAME" \
  -o jsonpath='{range .status.conditions[*]}  type={.type}  status={.status}  reason={.reason}  message={.message}{"\n"}{end}' 2>/dev/null || echo "  <no conditions>"
echo

echo "=== ResumeReadinessPolicy ==="
policy_name="$(kubectl get admissionchecks.kueue.x-k8s.io "$ADMISSION_CHECK_NAME" \
  -o jsonpath='{.spec.parameters.name}' 2>/dev/null || true)"
if [[ -n "${policy_name}" ]]; then
  echo "policy: ${policy_name}"
  kubectl get resumereadinesspolicies.training.checkpoint.example.io "$policy_name" \
    -o jsonpath=$'requireCompleteCheckpoint: {.spec.requireCompleteCheckpoint}\nallowInitialLaunchWithoutCheckpoint: {.spec.allowInitialLaunchWithoutCheckpoint}\nfailurePolicy: {.spec.failurePolicy}\nmaxCheckpointAge: {.spec.maxCheckpointAge}\n' 2>/dev/null || echo "  <policy not found>"
else
  echo "<no parameters reference on AdmissionCheck>"
fi
echo

echo "=== ClusterQueues using this check ==="
kubectl get clusterqueues.kueue.x-k8s.io -o json 2>/dev/null | \
  python3 -c "
import json, sys
data = json.load(sys.stdin)
for cq in data.get('items', []):
    strategy = cq.get('spec', {}).get('admissionChecksStrategy', {})
    checks = strategy.get('admissionChecks', [])
    names = [c.get('name', '') for c in checks]
    if '${ADMISSION_CHECK_NAME}' in names:
        print(f\"  {cq['metadata']['name']}\")
" 2>/dev/null || echo "  <unable to parse>"
echo

echo "=== Workloads with check state ==="
kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io -o json 2>/dev/null | \
  python3 -c "
import json, sys
data = json.load(sys.stdin)
for wl in data.get('items', []):
    name = wl['metadata']['name']
    checks = wl.get('status', {}).get('admissionChecks', [])
    for c in checks:
        if c.get('name', '') == '${ADMISSION_CHECK_NAME}':
            state = c.get('state', 'Unknown')
            msg = c.get('message', '')
            print(f'  {name}: state={state}  message={msg}')
" 2>/dev/null || echo "  <unable to parse>"
