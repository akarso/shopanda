#!/usr/bin/env bash
# Docs-policy skip / fail-closed behavior (Phase 17).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "OK: $*"; }

TMP="$(mktemp -d "${TMPDIR:-/tmp}/docs-policy-test.XXXXXX")"
cleanup() { rm -rf "${TMP}"; }
trap cleanup EXIT

# Minimal fake tree: only the scripts under test.
mkdir -p "${TMP}/scripts/ci"
cp "${ROOT}/scripts/ci/check-product-docs-links.sh" "${TMP}/scripts/ci/"
cp "${ROOT}/scripts/ci/docs-policy.sh" "${TMP}/scripts/ci/"

# Fake trees omit PRODUCT_OWNED.md; FAKE_TREE opts out of platform fail-closed.
# Clear CI vars — FAKE_TREE is refused whenever CI/GITHUB_ACTIONS is set.
unset CI GITHUB_ACTIONS || true
export SHOPANDA_DOCS_POLICY_FAKE_TREE=1

# Refuse FAKE_TREE under CI even on a stub tree (doc absent).
(
  cd "${TMP}"
  if CI=true SHOPANDA_PLATFORM=1 SHOPANDA_DOCS_POLICY_FAKE_TREE=1 \
    bash scripts/ci/docs-policy.sh >/dev/null 2>&1; then
    exit 1
  fi
  exit 0
) || fail "FAKE_TREE under CI must refuse"
pass "FAKE_TREE refused when CI=true"

# Refuse FAKE_TREE under CI on the real checkout too.
(
  cd "${ROOT}"
  if CI=true GITHUB_ACTIONS=true SHOPANDA_DOCS_POLICY_FAKE_TREE=1 \
    bash scripts/ci/docs-policy.sh >/dev/null 2>&1; then
    exit 1
  fi
  exit 0
) || fail "FAKE_TREE under CI must refuse on real tree"
pass "FAKE_TREE refused under CI when PRODUCT_OWNED.md present"

# Product mode: no roots, no promote fixture → skip OK
(
  cd "${TMP}"
  SHOPANDA_PLATFORM=0 bash scripts/ci/docs-policy.sh >/dev/null
) || fail "product mode should succeed with empty tree"
pass "product mode fail-open on empty roots / missing promote"

# Platform mode: missing promote fixture → fail
(
  cd "${TMP}"
  if SHOPANDA_PLATFORM=1 bash scripts/ci/docs-policy.sh >/dev/null 2>&1; then
    exit 1
  fi
  exit 0
) || fail "platform mode should fail without promote fixture"
pass "platform mode fail-closed without promote fixture"

# Platform mode: promote present but zero roots → fail at link scan
mkdir -p "${TMP}/scripts"
printf '#!/usr/bin/env bash\necho promote-ok\n' >"${TMP}/scripts/promote-dev-to-main_test.sh"
chmod +x "${TMP}/scripts/promote-dev-to-main_test.sh"
(
  cd "${TMP}"
  if SHOPANDA_PLATFORM=1 bash scripts/ci/docs-policy.sh >/dev/null 2>&1; then
    exit 1
  fi
  exit 0
) || fail "platform mode should fail with zero product-doc roots"
pass "platform mode fail-closed with zero roots"

# Platform mode: one root + promote → OK (FAKE_TREE still set)
echo '# hi' >"${TMP}/README.md"
(
  cd "${TMP}"
  SHOPANDA_PLATFORM=1 bash scripts/ci/docs-policy.sh >/dev/null
) || fail "platform mode should succeed with README + promote"
pass "platform mode OK with README + promote"

# Same tree without FAKE_TREE and without PRODUCT_OWNED → fail closed
(
  cd "${TMP}"
  unset SHOPANDA_DOCS_POLICY_FAKE_TREE || true
  if SHOPANDA_PLATFORM=1 bash scripts/ci/docs-policy.sh >/dev/null 2>&1; then
    exit 1
  fi
  exit 0
) || fail "platform mode should fail without PRODUCT_OWNED.md"
pass "platform mode fail-closed without PRODUCT_OWNED.md"
export SHOPANDA_DOCS_POLICY_FAKE_TREE=1

# docs/runbooks is a product-doc root: phase-* relative links must fail
mkdir -p "${TMP}/docs/runbooks"
printf '# runbook\n[bad](../phase-17-product-packaging/ROADMAP.md)\n' \
  >"${TMP}/docs/runbooks/sample.md"
(
  cd "${TMP}"
  if SHOPANDA_PLATFORM=1 bash scripts/ci/docs-policy.sh >/dev/null 2>&1; then
    exit 1
  fi
  exit 0
) || fail "platform mode should fail on phase-* link under docs/runbooks"
pass "docs/runbooks phase-* relative link fails closed"

echo "docs-policy_test OK"
