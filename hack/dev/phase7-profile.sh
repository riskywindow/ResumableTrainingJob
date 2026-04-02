#!/usr/bin/env bash

# Phase 7 profile apply / re-apply.
#
# Thin wrapper that delegates to install-phase7-profile.sh.
# Use this to switch an existing cluster to the Phase 7 profile.
#
# Usage:
#   ./hack/dev/phase7-profile.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
exec "$SCRIPT_DIR/install-phase7-profile.sh"
