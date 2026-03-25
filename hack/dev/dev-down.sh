#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "$0")" && pwd)/common.sh"

"$REPO_ROOT/hack/dev/delete-kind-cluster.sh"

echo "Phase 2 development environment is removed"
