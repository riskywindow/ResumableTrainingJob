#!/usr/bin/env bash

# Phase 8 profile apply / re-apply.
#
# Thin wrapper that delegates to install-phase8-profile.sh.
# Use this to switch an existing cluster to the Phase 8 profile.
#
# Usage:
#   ./hack/dev/phase8-profile.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
exec "$SCRIPT_DIR/install-phase8-profile.sh"
