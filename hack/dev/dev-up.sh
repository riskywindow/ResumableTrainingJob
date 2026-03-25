#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_command kubectl
require_command kind

"$REPO_ROOT/hack/dev/create-kind-cluster.sh"
"$REPO_ROOT/hack/dev/install-kueue.sh"
"$REPO_ROOT/hack/dev/install-jobset.sh"
apply_dev_namespaces
apply_dev_priorityclasses
apply_dev_queues
"$REPO_ROOT/hack/dev/install-minio.sh"
"$REPO_ROOT/hack/dev/bootstrap-bucket.sh"
"$REPO_ROOT/hack/dev/status.sh"

echo "Phase 2 development environment is ready"
