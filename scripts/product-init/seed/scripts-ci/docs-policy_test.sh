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

# Platform mode: one root + promote → OK
echo '# hi' >"${TMP}/README.md"
(
  cd "${TMP}"
  SHOPANDA_PLATFORM=1 bash scripts/ci/docs-policy.sh >/dev/null
) || fail "platform mode should succeed with README + promote"
pass "platform mode OK with README + promote"

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
