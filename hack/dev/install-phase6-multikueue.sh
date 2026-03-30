#!/usr/bin/env bash

# Phase 6: Configure MultiKueue on the manager cluster.
#
# This script:
#   1. Installs JobSet on all clusters (required for RTJ child resources).
#   2. Extracts worker cluster kubeconfigs from kind and creates Secrets
#      in the manager's kueue-system namespace.
#   3. Applies manager-side MultiKueue resources:
#      - AdmissionCheck (multikueue)
#      - MultiKueueConfig (lists worker-1, worker-2)
#      - MultiKueueCluster resources (one per worker, referencing kubeconfig Secrets)
#      - RBAC for Kueue to manage RTJ on the manager cluster
#   4. Applies the manager ClusterQueue and LocalQueue with MultiKueue
#      admission check.
#   5. Creates mirrored namespaces and LocalQueues on worker clusters.
#
# Prerequisites:
#   - All three clusters must exist (run create-phase6-kind-clusters.sh).
#   - Kueue must be installed on all clusters (run install-phase6-kueue.sh).

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

# ── 1. Install JobSet on all clusters ──────────────────────────────

install_jobset_on_cluster() {
  local ctx="$1"
  local name="$2"
  echo "installing JobSet ${JOBSET_VERSION} on ${name}..."
  kubectl apply --server-side -f "$JOBSET_MANIFEST_URL" --context "$ctx"
  kubectl wait --for=condition=Established crd/jobsets.jobset.x-k8s.io --timeout=180s --context "$ctx"
  kubectl -n jobset-system wait --for=condition=Available deployment --all --timeout=180s --context "$ctx"
  echo "JobSet installed on ${name}"
}

install_jobset_on_cluster "$MANAGER_CTX" "$PHASE6_MANAGER"
install_jobset_on_cluster "$WORKER1_CTX" "$PHASE6_WORKER_1"
install_jobset_on_cluster "$WORKER2_CTX" "$PHASE6_WORKER_2"
echo

# ── 2. Generate worker kubeconfigs and create Secrets ──────────────
#
# Kind clusters run in Docker containers. The API server is exposed
# on the container's IP within the kind Docker network. We extract
# the internal API server address and the admin kubeconfig, then
# rewrite the server URL to use the container IP so that the manager
# cluster (also on the same Docker network) can reach it.

generate_worker_kubeconfig_secret() {
  local worker_name="$1"
  local secret_name="$2"

  echo "generating kubeconfig for ${worker_name}..."

  # Get the worker control-plane container name (kind names it "<cluster>-control-plane").
  local container="${worker_name}-control-plane"

  # Get the internal IP of the worker control-plane container.
  local internal_ip
  internal_ip="$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$container")"

  if [[ -z "$internal_ip" ]]; then
    echo "FAIL: could not resolve internal IP for ${container}" >&2
    exit 1
  fi

  # Extract the kind-generated kubeconfig and rewrite the server URL.
  local kubeconfig
  kubeconfig="$(kind get kubeconfig --name "$worker_name")"

  # Replace the external-facing server URL with the Docker-internal one.
  # Kind's default server URL is https://127.0.0.1:<port> or https://localhost:<port>.
  # We replace it with https://<internal_ip>:6443 (the default internal port).
  kubeconfig="$(echo "$kubeconfig" | sed "s|server: https://.*|server: https://${internal_ip}:6443|")"

  # Create the Secret in the manager cluster's kueue-system namespace.
  kubectl create secret generic "$secret_name" \
    --from-literal=kubeconfig="$kubeconfig" \
    -n "$KUEUE_NAMESPACE" \
    --context "$MANAGER_CTX" \
    --dry-run=client \
    -o yaml | kubectl apply -f - --context "$MANAGER_CTX"

  echo "created kubeconfig Secret ${secret_name} in ${KUEUE_NAMESPACE} (server: https://${internal_ip}:6443)"
}

generate_worker_kubeconfig_secret "$PHASE6_WORKER_1" "worker-1-kubeconfig"
generate_worker_kubeconfig_secret "$PHASE6_WORKER_2" "worker-2-kubeconfig"
echo

# ── 3. Apply manager-side MultiKueue resources ────────────────────

echo "applying manager-side RBAC..."
kubectl apply -f "$REPO_ROOT/deploy/multikueue/manager-rbac/" --context "$MANAGER_CTX"
echo

echo "applying ResourceFlavor on manager..."
kubectl apply -f "$REPO_ROOT/deploy/dev/queues/00-resource-flavor.yaml" --context "$MANAGER_CTX"
echo

echo "applying manager-side MultiKueue admission check..."
kubectl apply -f "$REPO_ROOT/deploy/multikueue/manager-config/admissioncheck.yaml" --context "$MANAGER_CTX"
echo

echo "applying MultiKueueConfig..."
kubectl apply -f "$REPO_ROOT/deploy/multikueue/manager-config/multikueueconfig.yaml" --context "$MANAGER_CTX"
echo

echo "applying MultiKueueCluster resources..."
kubectl apply -f "$REPO_ROOT/deploy/multikueue/manager-config/multikueuecluster-template.yaml" --context "$MANAGER_CTX"
echo

echo "applying manager ClusterQueue with MultiKueue admission check..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase6/manager/10-cluster-queue.yaml" --context "$MANAGER_CTX"
echo

echo "applying manager namespace..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase6/manager/00-namespace.yaml" --context "$MANAGER_CTX"
echo

echo "applying manager LocalQueue..."
kubectl apply -f "$REPO_ROOT/deploy/dev/phase6/manager/20-local-queue.yaml" --context "$MANAGER_CTX"
echo

# ── 4. Apply worker-side resources ────────────────────────────────
#
# Each worker cluster needs:
#   - The checkpoint-dev namespace (with kueue-managed label)
#   - A ResourceFlavor, ClusterQueue, and LocalQueue with real quotas
#   - RTJ CRDs (applied in install-phase6-operator.sh)

setup_worker_cluster() {
  local ctx="$1"
  local name="$2"

  echo "setting up worker resources on ${name}..."

  kubectl apply -f "$REPO_ROOT/deploy/dev/phase6/workers/00-namespace.yaml" --context "$ctx"
  kubectl apply -f "$REPO_ROOT/deploy/dev/queues/00-resource-flavor.yaml" --context "$ctx"
  kubectl apply -f "$REPO_ROOT/deploy/dev/phase6/workers/10-cluster-queue.yaml" --context "$ctx"
  kubectl apply -f "$REPO_ROOT/deploy/dev/phase6/workers/20-local-queue.yaml" --context "$ctx"

  echo "worker resources applied on ${name}"
}

setup_worker_cluster "$WORKER1_CTX" "$PHASE6_WORKER_1"
setup_worker_cluster "$WORKER2_CTX" "$PHASE6_WORKER_2"
echo

# ── 5. Verify MultiKueueCluster connectivity ─────────────────────

echo "waiting for MultiKueueCluster resources to become Active..."
sleep 5

for cluster in worker-1 worker-2; do
  local_state="$(kubectl get multikueueclusters.kueue.x-k8s.io "$cluster" \
    -o jsonpath='{.status.conditions[?(@.type=="Active")].status}' \
    --context "$MANAGER_CTX" 2>/dev/null || echo "Unknown")"
  echo "  MultiKueueCluster ${cluster}: Active=${local_state}"
done
echo

echo "MultiKueue configured on the Phase 6 manager cluster"
