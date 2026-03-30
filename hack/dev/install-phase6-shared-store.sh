#!/usr/bin/env bash

# Phase 6: Install shared checkpoint store (MinIO) accessible by all clusters.
#
# Strategy: Install MinIO on worker-1 and expose it via a NodePort service.
# All three clusters can reach it via the worker-1 container's Docker-internal
# IP on the NodePort. This avoids needing a separate Docker container or
# host-level port mapping.
#
# The shared store endpoint is:
#   http://<worker-1-internal-ip>:30900
#
# Credentials and endpoint are distributed as Secrets to all clusters
# that need them (both worker clusters for checkpoint read/write,
# manager for optional pre-flight validation).
#
# Prerequisites:
#   - All three clusters must exist.
#   - The checkpoint-dev namespace must exist on worker clusters
#     (created by install-phase6-multikueue.sh).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$REPO_ROOT/hack/dev/common.sh"

require_command kubectl
require_command docker

PHASE6_MANAGER="${PHASE6_MANAGER:-phase6-manager}"
PHASE6_WORKER_1="${PHASE6_WORKER_1:-phase6-worker-1}"
PHASE6_WORKER_2="${PHASE6_WORKER_2:-phase6-worker-2}"
DEV_NAMESPACE="${DEV_NAMESPACE:-checkpoint-dev}"
MINIO_NODEPORT="${MINIO_NODEPORT:-30900}"

MANAGER_CTX="kind-${PHASE6_MANAGER}"
WORKER1_CTX="kind-${PHASE6_WORKER_1}"
WORKER2_CTX="kind-${PHASE6_WORKER_2}"

# ── 1. Install MinIO on worker-1 ──────────────────────────────────

echo "installing MinIO on ${PHASE6_WORKER_1}..."

# Ensure namespace exists on worker-1.
kubectl apply -f "$REPO_ROOT/deploy/dev/phase6/workers/00-namespace.yaml" --context "$WORKER1_CTX"

# Create MinIO root credentials Secret.
kubectl -n "$DEV_NAMESPACE" create secret generic minio-root-credentials \
  --from-literal=MINIO_ROOT_USER="$MINIO_ROOT_USER" \
  --from-literal=MINIO_ROOT_PASSWORD="$MINIO_ROOT_PASSWORD" \
  --dry-run=client \
  -o yaml | kubectl apply -f - --context "$WORKER1_CTX"

# Apply the MinIO deployment.
kubectl apply -f "$REPO_ROOT/deploy/dev/phase6/shared-store/minio-deployment.yaml" --context "$WORKER1_CTX"
kubectl -n "$DEV_NAMESPACE" set image deployment/minio minio="$MINIO_IMAGE" --context "$WORKER1_CTX" >/dev/null

# Apply the NodePort service.
kubectl apply -f "$REPO_ROOT/deploy/dev/phase6/shared-store/minio-nodeport-service.yaml" --context "$WORKER1_CTX"

kubectl -n "$DEV_NAMESPACE" rollout status deployment/minio --timeout=180s --context "$WORKER1_CTX"
echo "MinIO installed on ${PHASE6_WORKER_1}"
echo

# ── 2. Bootstrap the bucket ───────────────────────────────────────

echo "bootstrapping MinIO bucket..."
kubectl -n "$DEV_NAMESPACE" delete job minio-bootstrap --ignore-not-found --context "$WORKER1_CTX" >/dev/null

kubectl apply --context "$WORKER1_CTX" -f - <<EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: minio-bootstrap
  namespace: ${DEV_NAMESPACE}
spec:
  backoffLimit: 3
  ttlSecondsAfterFinished: 60
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: mc
          image: ${MINIO_MC_IMAGE}
          imagePullPolicy: IfNotPresent
          envFrom:
            - secretRef:
                name: minio-root-credentials
          command:
            - /bin/sh
            - -c
            - |
              set -euo pipefail
              mc alias set local http://minio:9000 "\$MINIO_ROOT_USER" "\$MINIO_ROOT_PASSWORD"
              mc mb --ignore-existing local/${MINIO_BUCKET}
EOF

kubectl -n "$DEV_NAMESPACE" wait --for=condition=Complete job/minio-bootstrap --timeout=180s --context "$WORKER1_CTX"
echo "bucket ${MINIO_BUCKET} is ready"
echo

# ── 3. Resolve the shared endpoint ────────────────────────────────

WORKER1_CONTAINER="${PHASE6_WORKER_1}-control-plane"
WORKER1_IP="$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$WORKER1_CONTAINER")"

if [[ -z "$WORKER1_IP" ]]; then
  echo "FAIL: could not resolve internal IP for ${WORKER1_CONTAINER}" >&2
  exit 1
fi

SHARED_ENDPOINT="http://${WORKER1_IP}:${MINIO_NODEPORT}"
echo "shared checkpoint store endpoint: ${SHARED_ENDPOINT}"
echo

# ── 4. Distribute credentials to all clusters ─────────────────────

distribute_checkpoint_secret() {
  local ctx="$1"
  local cluster_name="$2"
  local ns="$3"

  echo "distributing checkpoint credentials to ${cluster_name} (ns: ${ns})..."

  # Ensure namespace exists.
  kubectl create namespace "$ns" --context "$ctx" --dry-run=client -o yaml | kubectl apply -f - --context "$ctx"

  kubectl -n "$ns" create secret generic checkpoint-storage-credentials \
    --from-literal=AWS_ACCESS_KEY_ID="$MINIO_ROOT_USER" \
    --from-literal=AWS_SECRET_ACCESS_KEY="$MINIO_ROOT_PASSWORD" \
    --from-literal=AWS_ENDPOINT_URL="$SHARED_ENDPOINT" \
    --from-literal=AWS_REGION="$MINIO_REGION" \
    --from-literal=AWS_S3_FORCE_PATH_STYLE="true" \
    --from-literal=CHECKPOINT_BUCKET="$MINIO_BUCKET" \
    --dry-run=client \
    -o yaml | kubectl apply -f - --context "$ctx"

  echo "credentials distributed to ${cluster_name}"
}

distribute_checkpoint_secret "$WORKER1_CTX" "$PHASE6_WORKER_1" "$DEV_NAMESPACE"
distribute_checkpoint_secret "$WORKER2_CTX" "$PHASE6_WORKER_2" "$DEV_NAMESPACE"
distribute_checkpoint_secret "$MANAGER_CTX" "$PHASE6_MANAGER" "$DEV_NAMESPACE"
echo

# ── 5. Store shared endpoint in a ConfigMap on manager ─────────────

kubectl -n "$DEV_NAMESPACE" create configmap shared-checkpoint-store \
  --from-literal=endpoint="$SHARED_ENDPOINT" \
  --from-literal=bucket="$MINIO_BUCKET" \
  --from-literal=region="$MINIO_REGION" \
  --context "$MANAGER_CTX" \
  --dry-run=client \
  -o yaml | kubectl apply -f - --context "$MANAGER_CTX"

echo "shared checkpoint store ConfigMap created on manager"
echo
echo "=== Shared checkpoint store is ready ==="
echo "  endpoint: ${SHARED_ENDPOINT}"
echo "  bucket:   ${MINIO_BUCKET}"
echo "  credentials distributed to all clusters"
