#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

echo "RTJ:"
kubectl -n "$DEV_NAMESPACE" get resumabletrainingjobs.training.checkpoint.example.io "$EXAMPLE_RTJ_NAME" -o wide
echo

echo "RTJ status:"
kubectl -n "$DEV_NAMESPACE" get resumabletrainingjobs.training.checkpoint.example.io "$EXAMPLE_RTJ_NAME" -o yaml
echo

echo "Child JobSets:"
kubectl -n "$DEV_NAMESPACE" get jobset -l training.checkpoint.example.io/rtj-name="$EXAMPLE_RTJ_NAME" -o wide || true
echo

echo "Kueue workloads:"
kubectl -n "$DEV_NAMESPACE" get workloads.kueue.x-k8s.io || true
echo

echo "Pods:"
kubectl -n "$DEV_NAMESPACE" get pods -o wide
echo

manifest_uri="$(kubectl -n "$DEV_NAMESPACE" get resumabletrainingjobs.training.checkpoint.example.io "$EXAMPLE_RTJ_NAME" -o jsonpath='{.status.lastCompletedCheckpoint.manifestURI}' 2>/dev/null || true)"
if [[ -n "${manifest_uri}" ]]; then
  echo "last completed checkpoint manifest:"
  echo "${manifest_uri}"
else
  echo "last completed checkpoint manifest: <none recorded yet>"
fi
