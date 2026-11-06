#!/usr/bin/env bash
# Thin wrapper — portable contract lives in scripts/ci/govuln.sh (Phase 17).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
exec bash "${ROOT}/scripts/ci/govuln.sh" "$@"
