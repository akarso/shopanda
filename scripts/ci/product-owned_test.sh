#!/usr/bin/env bash
# Tests for check-product-owned.sh + apply-product-gitignore.sh (Phase 17).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "OK: $*"; }

bash "${ROOT}/scripts/ci/check-product-owned.sh" >/dev/null
pass "check-product-owned on live tree"

# Swapped markers fail check
GITIGNORE_BAK="$(mktemp)"
cp "${ROOT}/.gitignore" "${GITIGNORE_BAK}"
python3 - "${ROOT}/.gitignore" <<'PY'
import sys
path = sys.argv[1]
start = "# >>> shopanda-product-gitignore"
end = "# <<< shopanda-product-gitignore"
lines = open(path, encoding="utf-8").read().splitlines()
si, ei = lines.index(start), lines.index(end)
lines[si], lines[ei] = end, start
open(path, "w", encoding="utf-8").write("\n".join(lines) + "\n")
PY
if bash "${ROOT}/scripts/ci/check-product-owned.sh" >/dev/null 2>&1; then
  mv "${GITIGNORE_BAK}" "${ROOT}/.gitignore"
  fail "expected check-product-owned to fail on swapped markers"
fi
mv "${GITIGNORE_BAK}" "${ROOT}/.gitignore"
pass "check-product-owned fails on swapped markers"

# apply: embedded end marker in fragment fails closed
FRAG_BAK=""
[[ -f "${ROOT}/.gitignore.product" ]] && FRAG_BAK="$(mktemp)" && cp "${ROOT}/.gitignore.product" "${FRAG_BAK}"
printf '%s\n' "foo" "# <<< shopanda-product-gitignore" "bar" >"${ROOT}/.gitignore.product"
if bash "${ROOT}/scripts/ci/apply-product-gitignore.sh" >/dev/null 2>&1; then
  [[ -n "${FRAG_BAK}" ]] && mv "${FRAG_BAK}" "${ROOT}/.gitignore.product" || rm -f "${ROOT}/.gitignore.product"
  fail "expected apply-product-gitignore to reject embedded end marker"
fi
[[ -n "${FRAG_BAK}" ]] && mv "${FRAG_BAK}" "${ROOT}/.gitignore.product" || rm -f "${ROOT}/.gitignore.product"
pass "apply-product-gitignore rejects embedded end marker"

# apply: happy path + idempotent
printf '%s\n' "product-owned-test-ignore" >"${ROOT}/.gitignore.product"
bash "${ROOT}/scripts/ci/apply-product-gitignore.sh" >/dev/null
grep -q "product-owned-test-ignore" "${ROOT}/.gitignore" || fail "fragment not applied"
bash "${ROOT}/scripts/ci/apply-product-gitignore.sh" >/dev/null
count="$(grep -c "product-owned-test-ignore" "${ROOT}/.gitignore" || true)"
[[ "${count}" -eq 1 ]] || fail "second apply duplicated fragment (count=${count})"
# clear fragment section
rm -f "${ROOT}/.gitignore.product"
bash "${ROOT}/scripts/ci/apply-product-gitignore.sh" >/dev/null
grep -q "product-owned-test-ignore" "${ROOT}/.gitignore" && fail "cleared fragment still present" || true
pass "apply-product-gitignore idempotent apply + clear"

echo "product-owned_test OK"
