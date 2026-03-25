#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-checkpoint-phase1}"
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:-kindest/node:v1.31.2}"
KIND_CONFIG_PATH="${KIND_CONFIG_PATH:-$REPO_ROOT/hack/dev/kind-config.yaml}"

DEV_NAMESPACE="${DEV_NAMESPACE:-checkpoint-dev}"
DEV_NAMESPACES_DIR="${DEV_NAMESPACES_DIR:-$REPO_ROOT/deploy/dev/namespaces}"
DEV_PRIORITYCLASSES_DIR="${DEV_PRIORITYCLASSES_DIR:-$REPO_ROOT/deploy/dev/priorityclasses}"
DEV_QUEUES_DIR="${DEV_QUEUES_DIR:-$REPO_ROOT/deploy/dev/queues}"

KUEUE_VERSION="${KUEUE_VERSION:-v0.15.1}"
KUEUE_MANIFEST_URL="${KUEUE_MANIFEST_URL:-https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml}"
KUEUE_CONFIG_PATH="${KUEUE_CONFIG_PATH:-$REPO_ROOT/deploy/dev/kueue/controller_manager_config.phase2-rtj-external-framework.yaml}"
KUEUE_CONFIGMAP_NAME="${KUEUE_CONFIGMAP_NAME:-kueue-manager-config}"
KUEUE_DEPLOYMENT_NAME="${KUEUE_DEPLOYMENT_NAME:-kueue-controller-manager}"
KUEUE_NAMESPACE="${KUEUE_NAMESPACE:-kueue-system}"

JOBSET_VERSION="${JOBSET_VERSION:-v0.10.1}"
JOBSET_MANIFEST_URL="${JOBSET_MANIFEST_URL:-https://github.com/kubernetes-sigs/jobset/releases/download/${JOBSET_VERSION}/manifests.yaml}"

MINIO_RELEASE="${MINIO_RELEASE:-RELEASE.2025-06-13T11-33-47Z}"
MINIO_IMAGE="${MINIO_IMAGE:-quay.io/minio/minio:${MINIO_RELEASE}}"
MINIO_MC_RELEASE="${MINIO_MC_RELEASE:-RELEASE.2025-07-21T05-28-08Z}"
MINIO_MC_IMAGE="${MINIO_MC_IMAGE:-minio/mc:${MINIO_MC_RELEASE}}"
MINIO_SERVICE_NAME="${MINIO_SERVICE_NAME:-minio}"
MINIO_BUCKET="${MINIO_BUCKET:-rtj-checkpoints}"
MINIO_ROOT_USER="${MINIO_ROOT_USER:-minioadmin}"
MINIO_ROOT_PASSWORD="${MINIO_ROOT_PASSWORD:-minioadmin123}"
MINIO_REGION="${MINIO_REGION:-us-east-1}"

EXAMPLE_RTJ_NAME="${EXAMPLE_RTJ_NAME:-phase1-demo}"
EXAMPLE_TRAINER_IMAGE="${EXAMPLE_TRAINER_IMAGE:-${PAUSE_FLOW_TRAINER_IMAGE:-}}"
EXAMPLE_TEMPLATE_PATH="${EXAMPLE_TEMPLATE_PATH:-$REPO_ROOT/test/e2e/testdata/rtj-pause-flow.yaml}"

PHASE2_TRAINER_IMAGE="${PHASE2_TRAINER_IMAGE:-${PAUSE_FLOW_TRAINER_IMAGE:-${EXAMPLE_TRAINER_IMAGE:-}}}"
PHASE2_LOW_RTJ_NAME="${PHASE2_LOW_RTJ_NAME:-phase2-low}"
PHASE2_HIGH_RTJ_NAME="${PHASE2_HIGH_RTJ_NAME:-phase2-high}"
PHASE2_LOW_TEMPLATE_PATH="${PHASE2_LOW_TEMPLATE_PATH:-$REPO_ROOT/test/e2e/testdata/phase2/rtj-low-priority.yaml}"
PHASE2_HIGH_TEMPLATE_PATH="${PHASE2_HIGH_TEMPLATE_PATH:-$REPO_ROOT/test/e2e/testdata/phase2/rtj-high-priority.yaml}"
LOCAL_QUEUE_NAME="${LOCAL_QUEUE_NAME:-training}"

function require_command() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "missing required command: $cmd" >&2
    exit 1
  fi
}

function cluster_exists() {
  kind get clusters 2>/dev/null | grep -Fxq "$KIND_CLUSTER_NAME"
}

function ensure_cluster_context() {
  kubectl cluster-info --context "kind-${KIND_CLUSTER_NAME}" >/dev/null
}

function apply_dev_namespace() {
  apply_dev_namespaces >/dev/null
}

function apply_dev_namespaces() {
  kubectl apply -f "$DEV_NAMESPACES_DIR"
}

function apply_dev_priorityclasses() {
  kubectl apply -f "$DEV_PRIORITYCLASSES_DIR"
}

function apply_dev_queues() {
  kubectl apply -f "$DEV_QUEUES_DIR"
}

function current_kueue_manager_config() {
  kubectl -n "$KUEUE_NAMESPACE" get configmap "$KUEUE_CONFIGMAP_NAME" -o jsonpath='{.data.controller_manager_config\.yaml}'
}

function wait_for_pod_count() {
  local namespace="$1"
  local selector="$2"
  local expected_count="$3"
  local timeout_seconds="${4:-180}"
  local deadline=$((SECONDS + timeout_seconds))

  while (( SECONDS < deadline )); do
    local count
    count="$(kubectl get pods -n "$namespace" -l "$selector" --no-headers 2>/dev/null | wc -l | tr -d ' ')"
    if [[ "$count" -ge "$expected_count" ]]; then
      return 0
    fi
    sleep 2
  done

  echo "timed out waiting for ${expected_count} pods matching ${selector} in namespace ${namespace}" >&2
  return 1
}

function require_example_trainer_image() {
  if [[ -z "${EXAMPLE_TRAINER_IMAGE}" ]]; then
    echo "set EXAMPLE_TRAINER_IMAGE to a trainer image already loaded into kind" >&2
    exit 1
  fi
}

function render_example_rtj_manifest() {
  require_example_trainer_image
  sed \
    -e "s|__RTJ_NAME__|${EXAMPLE_RTJ_NAME}|g" \
    -e "s|__TRAINER_IMAGE__|${EXAMPLE_TRAINER_IMAGE}|g" \
    -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
    "${EXAMPLE_TEMPLATE_PATH}"
}

function require_phase2_trainer_image() {
  if [[ -z "${PHASE2_TRAINER_IMAGE}" ]]; then
    echo "set PHASE2_TRAINER_IMAGE to a trainer image already loaded into kind" >&2
    exit 1
  fi
}

function render_phase2_manifest() {
  local template_path="$1"
  local rtj_name="$2"

  require_phase2_trainer_image
  sed \
    -e "s|__RTJ_NAME__|${rtj_name}|g" \
    -e "s|__TRAINER_IMAGE__|${PHASE2_TRAINER_IMAGE}|g" \
    -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
    "${template_path}"
}

function render_phase2_low_rtj_manifest() {
  render_phase2_manifest "${PHASE2_LOW_TEMPLATE_PATH}" "${PHASE2_LOW_RTJ_NAME}"
}

function render_phase2_high_rtj_manifest() {
  render_phase2_manifest "${PHASE2_HIGH_TEMPLATE_PATH}" "${PHASE2_HIGH_RTJ_NAME}"
}

# Phase 3 helpers.
PHASE3_TRAINER_IMAGE="${PHASE3_TRAINER_IMAGE:-${PHASE2_TRAINER_IMAGE:-}}"
PHASE3_RTJ_NAME="${PHASE3_RTJ_NAME:-phase3-demo}"
PHASE3_FLAVOR_TEMPLATE_PATH="${PHASE3_FLAVOR_TEMPLATE_PATH:-$REPO_ROOT/deploy/dev/samples/phase3/rtj-fixed-size.yaml}"
PHASE3_FLEX_TEMPLATE_PATH="${PHASE3_FLEX_TEMPLATE_PATH:-$REPO_ROOT/deploy/dev/samples/phase3/rtj-flexible-size.yaml}"

function require_phase3_trainer_image() {
  if [[ -z "${PHASE3_TRAINER_IMAGE}" ]]; then
    echo "set PHASE3_TRAINER_IMAGE to a trainer image already loaded into kind" >&2
    exit 1
  fi
}

function render_phase3_manifest() {
  local template_path="$1"
  local rtj_name="$2"

  require_phase3_trainer_image
  sed \
    -e "s|__RTJ_NAME__|${rtj_name}|g" \
    -e "s|__TRAINER_IMAGE__|${PHASE3_TRAINER_IMAGE}|g" \
    -e "s|__DEV_NAMESPACE__|${DEV_NAMESPACE}|g" \
    "${template_path}"
}

function render_phase3_flavor_rtj_manifest() {
  render_phase3_manifest "${PHASE3_FLAVOR_TEMPLATE_PATH}" "${PHASE3_RTJ_NAME}"
}

function render_phase3_flex_rtj_manifest() {
  render_phase3_manifest "${PHASE3_FLEX_TEMPLATE_PATH}" "${PHASE3_RTJ_NAME}"
}
