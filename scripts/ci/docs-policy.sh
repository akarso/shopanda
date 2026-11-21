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

# Manifest + hook markers (present on Shopanda / product forks that keep the doc).
# Fake trees in docs-policy_test omit docs/PRODUCT_OWNED.md and skip this step.
if [[ -f "${ROOT}/docs/PRODUCT_OWNED.md" ]]; then
  bash "${ROOT}/scripts/ci/check-product-owned.sh"
fi

# Product-owned CI checks (optional). One script per file under product/checks/.
if [[ -d "${ROOT}/product/checks" ]]; then
  shopt -s nullglob
  for check in "${ROOT}/product/checks/"*.sh; do
    echo "==> product check: ${check#"${ROOT}/"}"
    bash "${check}"
  done
  shopt -u nullglob
fi

if [[ -f "${ROOT}/scripts/promote-dev-to-main_test.sh" ]]; then
  bash "${ROOT}/scripts/promote-dev-to-main_test.sh"
elif [[ "${SHOPANDA_PLATFORM}" == "1" ]]; then
  echo "ERROR: scripts/promote-dev-to-main_test.sh missing (required on Shopanda platform CI)" >&2
  exit 1
else
  echo "NOTE: scripts/promote-dev-to-main_test.sh absent — skipping promote fixture (product tree)."
fi

echo "docs-policy OK"
