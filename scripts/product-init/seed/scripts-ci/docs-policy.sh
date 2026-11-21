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

# SHOPANDA_DOCS_POLICY_FAKE_TREE=1 is test-only (docs-policy_test). Always refuse
# under CI / GITHUB_ACTIONS — the harness must unset those vars first.
if [[ "${SHOPANDA_DOCS_POLICY_FAKE_TREE:-0}" == "1" ]]; then
  if [[ -n "${CI:-}" || -n "${GITHUB_ACTIONS:-}" ]]; then
    echo "ERROR: SHOPANDA_DOCS_POLICY_FAKE_TREE is test-only; refuse when CI or GITHUB_ACTIONS is set" >&2
    exit 1
  fi
fi

bash "${ROOT}/scripts/ci/check-product-docs-links.sh"

# Manifest + hook expectations. Platform CI fail-closed if the doc is missing.
if [[ -f "${ROOT}/docs/PRODUCT_OWNED.md" ]]; then
  bash "${ROOT}/scripts/ci/check-product-owned.sh"
elif [[ "${SHOPANDA_PLATFORM}" == "1" && "${SHOPANDA_DOCS_POLICY_FAKE_TREE:-0}" != "1" ]]; then
  echo "ERROR: docs/PRODUCT_OWNED.md missing (required when SHOPANDA_PLATFORM=1)" >&2
  exit 1
else
  echo "NOTE: docs/PRODUCT_OWNED.md absent — skipping product-owned check."
fi

# Product-owned CI checks (optional). Sorted; same trust as scripts/ci/*.
# See docs/PRODUCT_OWNED.md / PRODUCT_SITES.md.
if [[ -d "${ROOT}/product/checks" ]]; then
  mapfile -t product_checks < <(find "${ROOT}/product/checks" -maxdepth 1 -type f -name '*.sh' | sort)
  for check in "${product_checks[@]:-}"; do
    [[ -n "${check}" ]] || continue
    echo "==> product check: ${check#"${ROOT}/"}"
    bash "${check}"
  done
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
