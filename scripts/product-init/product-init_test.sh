#!/usr/bin/env bash
# Regression tests for scripts/product-init.sh (Phase 17).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"
INIT="${ROOT}/scripts/product-init.sh"
SYNC="${ROOT}/scripts/product-init/sync-seed.sh"
RESERVED_FILE="${ROOT}/scripts/product-init/reserved-slugs.txt"

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "OK: $*"; }

bash "${SYNC}" --check
pass "seed + reserved-slugs in sync with live trees"

# Documented product path: scaffold with promote fixture present and PLATFORM unset.
# Must not fail sync-seed --check (platform reserved coverage is CI-env only).
PROD_SLUG="zzprodcheck"
rm -rf "${ROOT}/plugins/${PROD_SLUG}"
rm -f "${ROOT}/PRODUCT_README.${PROD_SLUG}.md"
(
  unset SHOPANDA_PLATFORM || true
  bash "${INIT}" --slug "${PROD_SLUG}" --name 'Prod Check' >/dev/null
  SHOPANDA_PLATFORM=0 bash "${SYNC}" --check
)
[[ -d "${ROOT}/plugins/${PROD_SLUG}" ]] || fail "zzprodcheck missing after product-path scaffold"
rm -rf "${ROOT}/plugins/${PROD_SLUG}"
rm -f "${ROOT}/PRODUCT_README.${PROD_SLUG}.md"
pass "product path: scaffold + sync-seed --check with PLATFORM unset (fixture may exist)"

# Missing --slug value
if bash "${INIT}" --slug 2>/dev/null; then
  fail "expected error for --slug without value"
fi
pass "--slug without value errors"

# Go keyword slug refused (would emit invalid package clause)
if bash "${INIT}" --slug chan 2>/dev/null; then
  fail "expected Go keyword slug 'chan' to be refused"
fi
if [[ -e "${ROOT}/plugins/chan" ]]; then
  fail "keyword slug left plugins/chan behind"
fi
pass "Go keyword slug refused"

# Reserved slug refused even with --force
if bash "${INIT}" --slug core --force 2>/dev/null; then
  fail "expected reserved slug 'core' to be refused"
fi
if bash "${INIT}" --slug sitekit --force 2>/dev/null; then
  fail "expected reserved slug 'sitekit' to be refused"
fi
pass "reserved slugs refused"

# CRLF reserved-slugs.txt still refuses reserved names
RESERVED_BAK="$(mktemp)"
cp "${RESERVED_FILE}" "${RESERVED_BAK}"
python3 - "${RESERVED_FILE}" <<'PY'
import sys
path = sys.argv[1]
data = open(path, "rb").read().replace(b"\r\n", b"\n").replace(b"\n", b"\r\n")
open(path, "wb").write(data)
PY
if bash "${INIT}" --slug core --force 2>/dev/null; then
  mv "${RESERVED_BAK}" "${RESERVED_FILE}"
  fail "expected reserved slug 'core' to be refused with CRLF reserved-slugs.txt"
fi
mv "${RESERVED_BAK}" "${RESERVED_FILE}"
pass "CRLF reserved-slugs still refuses core"

# Unsafe --name must not wreck destination
SLUG="zztestinit"
rm -rf "${ROOT}/plugins/${SLUG}"
rm -f "${ROOT}/PRODUCT_README.${SLUG}.md"
if bash "${INIT}" --slug "${SLUG}" --name 'A/B' 2>/dev/null; then
  fail "expected unsafe --name to fail"
fi
if [[ -e "${ROOT}/plugins/${SLUG}" ]]; then
  fail "failed render left plugins/${SLUG} behind"
fi
if [[ -e "${ROOT}/PRODUCT_README.${SLUG}.md" ]]; then
  fail "failed render left PRODUCT_README litter"
fi
pass "unsafe --name fails without leaving wreckage"

# Happy path + --force overwrite of non-reserved
rm -rf "${ROOT}/plugins/${SLUG}"
rm -f "${ROOT}/PRODUCT_README.${SLUG}.md"
bash "${INIT}" --slug "${SLUG}" --name 'Acme Store' >/dev/null
[[ -s "${ROOT}/plugins/${SLUG}/plugin.go" ]] || fail "plugin.go empty"
grep -q 'Acme Store' "${ROOT}/plugins/${SLUG}/README.md" || fail "display name not rendered"
grep -q "GET /api/${SLUG}/health" "${ROOT}/plugins/${SLUG}/plugin.go" || fail "health route not rendered"
[[ -s "${ROOT}/PRODUCT_README.${SLUG}.md" ]] || fail "PRODUCT_README.${SLUG}.md missing"
grep -q 'pluginsdk.New(app,' "${ROOT}/plugins/${SLUG}/README.md" \
  || fail "README missing two-arg pluginsdk.New(app, …)"
if grep -qE 'pluginsdk\.New\(app\)' "${ROOT}/plugins/${SLUG}/README.md"; then
  fail "README still has one-arg pluginsdk.New(app)"
fi
go test "./plugins/${SLUG}/" -count=1 >/dev/null \
  || fail "go test ./plugins/${SLUG}/ failed"
bash "${INIT}" --slug "${SLUG}" --name 'Acme Store' --force >/dev/null
pass "scaffold + go test + README arity + --force on non-reserved"

# --with-ci-samples when already in sync is OK (also creates backup of live trees)
bash "${INIT}" --slug "${SLUG}" --force --with-ci-samples >/dev/null
[[ -f "${ROOT}/scripts/ci/unit.sh" ]] || fail "scripts/ci/unit.sh missing"
[[ -f "${ROOT}/ci-samples/README.md" ]] || fail "ci-samples/README.md missing"
[[ -d "${ROOT}/.product-init-backup" ]] || fail "expected .product-init-backup after CI restore"
pass "--with-ci-samples when clean restores from seed"

# Customized live tree refuses without --force-ci-samples
echo '# customized' >>"${ROOT}/ci-samples/README.md"
echo 'orphan-only-on-live' >"${ROOT}/ci-samples/.orphan-live-only"
if bash "${INIT}" --slug "${SLUG}" --force --with-ci-samples >/dev/null 2>&1; then
  fail "expected refuse when ci-samples dirty"
fi
# restore via force-ci-samples (backs up + full tree replace)
bash "${INIT}" --slug "${SLUG}" --force --with-ci-samples --force-ci-samples >/dev/null
diff -rq "${ROOT}/ci-samples" "${ROOT}/scripts/product-init/seed/ci-samples" >/dev/null \
  || fail "ci-samples not restored from seed after --force-ci-samples"
[[ ! -f "${ROOT}/ci-samples/.orphan-live-only" ]] || fail "orphan live file survived restore"
[[ -d "${ROOT}/.product-init-backup" ]] || fail "expected .product-init-backup after force"
pass "--with-ci-samples refuses dirty trees; --force-ci-samples full replace + backup"

# Cleanup
rm -rf "${ROOT}/plugins/${SLUG}"
rm -f "${ROOT}/PRODUCT_README.${SLUG}.md"
rm -rf "${ROOT}/.product-init-backup"
rm -rf "${ROOT}/.product-init-stage"
[[ ! -f "${ROOT}/PRODUCT_README.${SLUG}.md" ]] || fail "PRODUCT_README litter after cleanup"

echo "product-init_test OK"
