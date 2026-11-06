#!/usr/bin/env bash
# Product-doc link policy + promote fixture (portable CI contract, Phase 17).
#
# Usage:
#   bash scripts/ci/docs-policy.sh
#
# SHOPANDA_PLATFORM=1 (set by upstream .github/workflows/ci.yml):
#   fail closed if the promote fixture is missing or no product-doc roots exist.
# Product trees (unset): skip missing optional roots / missing promote fixture.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"

export SHOPANDA_PLATFORM="${SHOPANDA_PLATFORM:-0}"

bash "${ROOT}/scripts/ci/check-product-docs-links.sh"

if [[ -f "${ROOT}/scripts/promote-dev-to-main_test.sh" ]]; then
  bash "${ROOT}/scripts/promote-dev-to-main_test.sh"
elif [[ "${SHOPANDA_PLATFORM}" == "1" ]]; then
  echo "ERROR: scripts/promote-dev-to-main_test.sh missing (required on Shopanda platform CI)" >&2
  exit 1
else
  echo "NOTE: scripts/promote-dev-to-main_test.sh absent — skipping promote fixture (product tree)."
fi

echo "docs-policy OK"
