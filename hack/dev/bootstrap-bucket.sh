#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

ensure_cluster_context
apply_dev_namespace

kubectl -n "$DEV_NAMESPACE" rollout status deployment/minio --timeout=180s
kubectl -n "$DEV_NAMESPACE" delete job minio-bootstrap --ignore-not-found >/dev/null

kubectl apply -f - <<EOF
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
              mc alias set local http://${MINIO_SERVICE_NAME}:9000 "\$MINIO_ROOT_USER" "\$MINIO_ROOT_PASSWORD"
              mc mb --ignore-existing local/${MINIO_BUCKET}
EOF

kubectl -n "$DEV_NAMESPACE" wait --for=condition=Complete job/minio-bootstrap --timeout=180s

echo "bucket ${MINIO_BUCKET} is ready"
