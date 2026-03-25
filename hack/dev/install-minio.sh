#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context
apply_dev_namespace

kubectl -n "$DEV_NAMESPACE" create secret generic minio-root-credentials \
  --from-literal=MINIO_ROOT_USER="$MINIO_ROOT_USER" \
  --from-literal=MINIO_ROOT_PASSWORD="$MINIO_ROOT_PASSWORD" \
  --dry-run=client \
  -o yaml | kubectl apply -f -

kubectl -n "$DEV_NAMESPACE" create secret generic checkpoint-storage-credentials \
  --from-literal=AWS_ACCESS_KEY_ID="$MINIO_ROOT_USER" \
  --from-literal=AWS_SECRET_ACCESS_KEY="$MINIO_ROOT_PASSWORD" \
  --from-literal=AWS_ENDPOINT_URL="http://${MINIO_SERVICE_NAME}.${DEV_NAMESPACE}.svc.cluster.local:9000" \
  --from-literal=AWS_REGION="$MINIO_REGION" \
  --from-literal=AWS_S3_FORCE_PATH_STYLE="true" \
  --from-literal=CHECKPOINT_BUCKET="$MINIO_BUCKET" \
  --dry-run=client \
  -o yaml | kubectl apply -f -

kubectl apply -f "$REPO_ROOT/deploy/dev/minio/minio.yaml"
kubectl -n "$DEV_NAMESPACE" set image deployment/minio minio="$MINIO_IMAGE" >/dev/null
kubectl -n "$DEV_NAMESPACE" rollout status deployment/minio --timeout=180s

echo "minio ${MINIO_RELEASE} is installed in namespace ${DEV_NAMESPACE}"
