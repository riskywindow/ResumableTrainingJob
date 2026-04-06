#!/usr/bin/env bash

# Phase 9 profile wrapper.
#
# Applies or re-applies the Phase 9 elastic resize profile on an existing
# kind cluster. This is the idempotent entry point for profile setup.
#
# Usage:
#   ./hack/dev/phase9-profile.sh

set -euo pipefail

exec "$(cd "$(dirname "$0")" && pwd)/install-phase9-profile.sh"
