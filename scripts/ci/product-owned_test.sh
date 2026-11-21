#!/usr/bin/env bash
# Tests for check-product-owned.sh + apply-product-gitignore.sh (Phase 17).
#
# Never leaves a product fork's .gitignore / .gitignore.product dirty: stash both
# at start and restore on EXIT (including mid-run failure).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "OK: $*"; }

HAD_FRAG=0
[[ -f "${ROOT}/.gitignore.product" ]] && HAD_FRAG=1
FRAG_BEFORE=""
[[ "${HAD_FRAG}" -eq 1 ]] && FRAG_BEFORE="$(cat "${ROOT}/.gitignore.product")"

STASH="$(mktemp -d "${TMPDIR:-/tmp}/product-owned-test.XXXXXX")"
cp "${ROOT}/.gitignore" "${STASH}/gitignore"
if [[ "${HAD_FRAG}" -eq 1 ]]; then
  cp "${ROOT}/.gitignore.product" "${STASH}/gitignore.product"
fi

restore_gitignore_files() {
  mv "${STASH}/gitignore" "${ROOT}/.gitignore"
  if [[ "${HAD_FRAG}" -eq 1 ]]; then
    mv "${STASH}/gitignore.product" "${ROOT}/.gitignore.product"
  else
    rm -f "${ROOT}/.gitignore.product"
  fi
  rm -rf "${STASH}"
}
trap restore_gitignore_files EXIT

bash "${ROOT}/scripts/ci/check-product-owned.sh" >/dev/null
pass "check-product-owned on live tree"

# Swapped markers fail check
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
  fail "expected check-product-owned to fail on swapped markers"
fi
# Restore markers from stash copy for next cases (keep trap for final restore).
cp "${STASH}/gitignore" "${ROOT}/.gitignore"
pass "check-product-owned fails on swapped markers"

# Unapplied .gitignore.product fails check (markers alone are not enough)
printf '%s\n' "unapplied-product-ignore" >"${ROOT}/.gitignore.product"
cp "${STASH}/gitignore" "${ROOT}/.gitignore"
if bash "${ROOT}/scripts/ci/check-product-owned.sh" >/dev/null 2>&1; then
  fail "expected check-product-owned to fail when fragment not applied"
fi
if [[ "${HAD_FRAG}" -eq 1 ]]; then
  cp "${STASH}/gitignore.product" "${ROOT}/.gitignore.product"
else
  rm -f "${ROOT}/.gitignore.product"
fi
pass "check-product-owned fails when .gitignore.product not applied"

# apply: embedded end marker in fragment fails closed
printf '%s\n' "foo" "# <<< shopanda-product-gitignore" "bar" >"${ROOT}/.gitignore.product"
if bash "${ROOT}/scripts/ci/apply-product-gitignore.sh" >/dev/null 2>&1; then
  fail "expected apply-product-gitignore to reject embedded end marker"
fi
cp "${STASH}/gitignore" "${ROOT}/.gitignore"
if [[ "${HAD_FRAG}" -eq 1 ]]; then
  cp "${STASH}/gitignore.product" "${ROOT}/.gitignore.product"
else
  rm -f "${ROOT}/.gitignore.product"
fi
pass "apply-product-gitignore rejects embedded end marker"

# apply: happy path + idempotent (disposable fragment; trap restores owned file)
printf '%s\n' "product-owned-test-ignore" >"${ROOT}/.gitignore.product"
bash "${ROOT}/scripts/ci/apply-product-gitignore.sh" >/dev/null
grep -q "product-owned-test-ignore" "${ROOT}/.gitignore" || fail "fragment not applied"
bash "${ROOT}/scripts/ci/apply-product-gitignore.sh" >/dev/null
count="$(grep -c "product-owned-test-ignore" "${ROOT}/.gitignore" || true)"
[[ "${count}" -eq 1 ]] || fail "second apply duplicated fragment (count=${count})"
# Clear via empty fragment (do not rely on deleting a pre-existing owned file).
: >"${ROOT}/.gitignore.product"
bash "${ROOT}/scripts/ci/apply-product-gitignore.sh" >/dev/null
grep -q "product-owned-test-ignore" "${ROOT}/.gitignore" && fail "cleared fragment still present" || true
pass "apply-product-gitignore idempotent apply + clear"

# Pre-existing owned fragment must be unchanged after restore.
restore_gitignore_files
trap - EXIT
if [[ "${HAD_FRAG}" -eq 1 ]]; then
  [[ -f "${ROOT}/.gitignore.product" ]] || fail ".gitignore.product missing after restore"
  [[ "$(cat "${ROOT}/.gitignore.product")" == "${FRAG_BEFORE}" ]] \
    || fail ".gitignore.product content changed by product-owned_test"
else
  [[ ! -e "${ROOT}/.gitignore.product" ]] || fail ".gitignore.product litter after test"
fi
pass "gitignore / .gitignore.product restored unchanged"

echo "product-owned_test OK"
