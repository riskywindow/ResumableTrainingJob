#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context

RTJ_NAME="${RTJ_NAME:-$PHASE2_LOW_RTJ_NAME}"

echo "RTJ:"
kubectl -n "$DEV_NAMESPACE" get resumabletrainingjobs.training.checkpoint.example.io "$RTJ_NAME" -o wide
echo

echo "RTJ key status:"
kubectl -n "$DEV_NAMESPACE" get resumabletrainingjobs.training.checkpoint.example.io "$RTJ_NAME" \
  -o jsonpath=$'{range [0]}phase={.status.phase}{"\n"}suspend={.spec.suspend}{"\n"}pauseRequestID={.status.pauseRequestID}{"\n"}activeJobSet={.status.activeJobSetName}{"\n"}currentRunAttempt={.status.currentRunAttempt}{"\n"}selectedCheckpoint={.status.selectedCheckpoint.manifestURI}{"\n"}lastCompletedCheckpoint={.status.lastCompletedCheckpoint.manifestURI}{"\n"}currentSuspensionSource={.status.currentSuspension.source}{"\n"}{end}'
echo

echo "RTJ YAML:"
kubectl -n "$DEV_NAMESPACE" get resumabletrainingjobs.training.checkpoint.example.io "$RTJ_NAME" -o yaml
echo

echo "Child JobSets:"
kubectl -n "$DEV_NAMESPACE" get jobset -l training.checkpoint.example.io/rtj-name="$RTJ_NAME" -o wide || true
echo

echo "Pods:"
kubectl -n "$DEV_NAMESPACE" get pods -o wide
