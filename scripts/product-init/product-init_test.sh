#!/usr/bin/env bash
# Regression tests for scripts/product-init.sh (Phase 17).
#
# Live product/ overlays stay stashed for the whole script. Disposable product/
# trees are created and removed under that isolation; EXIT always restores bak.
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

HAD_PRODUCT=0
[[ -d "${ROOT}/product" ]] && HAD_PRODUCT=1
PRODUCT_SNAP=""
if [[ "${HAD_PRODUCT}" -eq 1 ]]; then
  PRODUCT_SNAP="$(mktemp)"
  (
    cd "${ROOT}/product"
    find . -type f -print0 | sort -z | xargs -0 sha256sum
  ) >"${PRODUCT_SNAP}"
fi
PLANTED_PRECIOUS=0
mkdir -p "${ROOT}/product"
if [[ ! -f "${ROOT}/product/precious" ]]; then
  echo "preserve-me" >"${ROOT}/product/precious"
  PLANTED_PRECIOUS=1
fi

PRODUCT_BAK=""
RESERVED_BAK=""
restore_all() {
  if [[ -n "${RESERVED_BAK}" && -f "${RESERVED_BAK}" ]]; then
    mv "${RESERVED_BAK}" "${RESERVED_FILE}"
    RESERVED_BAK=""
  fi
  if [[ -n "${PRODUCT_BAK}" && -d "${PRODUCT_BAK}/product" ]]; then
    rm -rf "${ROOT}/product"
    mv "${PRODUCT_BAK}/product" "${ROOT}/product"
    rm -rf "${PRODUCT_BAK}"
    PRODUCT_BAK=""
  fi
}
trap restore_all EXIT

# Stash for the entire remaining script — disposable overlays only on live path.
PRODUCT_BAK="$(mktemp -d "${TMPDIR:-/tmp}/product-bak.XXXXXX")"
mv "${ROOT}/product" "${PRODUCT_BAK}/product"

drop_disposable_product() {
  rm -rf "${ROOT}/product"
}

# Documented product path: scaffold with promote fixture present and PLATFORM unset.
PROD_SLUG="zzprodcheck"
rm -rf "${ROOT}/plugins/${PROD_SLUG}"
rm -f "${ROOT}/PRODUCT_README.${PROD_SLUG}.md"
upstream_paths=(
  Makefile
  docker-compose.yml
  .env.example
  configs/config.example.yaml
  .gitignore
  scripts/ci/docs-policy.sh
  scripts/product-init/reserved-slugs.txt
  cmd/api/register_plugins.go
)
before_sums="$(mktemp)"
after_sums="$(mktemp)"
(
  cd "${ROOT}"
  sha256sum "${upstream_paths[@]}" >"${before_sums}"
)
(
  unset SHOPANDA_PLATFORM || true
  bash "${INIT}" --slug "${PROD_SLUG}" --name 'Prod Check' >/dev/null
  SHOPANDA_PLATFORM=0 bash "${SYNC}" --check
)
(
  cd "${ROOT}"
  sha256sum "${upstream_paths[@]}" >"${after_sums}"
)
if ! diff -u "${before_sums}" "${after_sums}" >/dev/null; then
  echo "FAIL: product-init dirtyed upstream-owned root files:" >&2
  diff -u "${before_sums}" "${after_sums}" >&2 || true
  rm -f "${before_sums}" "${after_sums}"
  exit 1
fi
rm -f "${before_sums}" "${after_sums}"
[[ -d "${ROOT}/plugins/${PROD_SLUG}" ]] || fail "zzprodcheck missing after product-path scaffold"
[[ -d "${ROOT}/product" ]] || fail "product/ overlay missing after product-init"
[[ -f "${ROOT}/product/reserved-slugs.txt" ]] || fail "product/reserved-slugs.txt missing"
rm -rf "${ROOT}/plugins/${PROD_SLUG}"
rm -f "${ROOT}/PRODUCT_README.${PROD_SLUG}.md"
drop_disposable_product
pass "product path: scaffold + sync-seed --check; no upstream root dirty"

# Overlay reserved-slugs refuse on a disposable product/ only.
OVERLAY_SLUG="zzoverlayreserved"
mkdir -p "${ROOT}/product"
printf '%s\n' "# disposable" "${OVERLAY_SLUG}" >"${ROOT}/product/reserved-slugs.txt"
if bash "${INIT}" --slug "${OVERLAY_SLUG}" --force 2>/dev/null; then
  fail "expected overlay reserved slug '${OVERLAY_SLUG}' to be refused"
fi
drop_disposable_product
pass "product/reserved-slugs.txt refuses overlay slug"

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

# CRLF reserved-slugs.txt still refuses reserved names (trap restores file).
RESERVED_BAK="$(mktemp)"
cp "${RESERVED_FILE}" "${RESERVED_BAK}"
python3 - "${RESERVED_FILE}" <<'PY'
import sys
path = sys.argv[1]
data = open(path, "rb").read().replace(b"\r\n", b"\n").replace(b"\n", b"\r\n")
open(path, "wb").write(data)
PY
if bash "${INIT}" --slug core --force 2>/dev/null; then
  fail "expected reserved slug 'core' to be refused with CRLF reserved-slugs.txt"
fi
mv "${RESERVED_BAK}" "${RESERVED_FILE}"
RESERVED_BAK=""
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
bash "${INIT}" --slug "${SLUG}" --force --with-ci-samples --force-ci-samples >/dev/null
diff -rq "${ROOT}/ci-samples" "${ROOT}/scripts/product-init/seed/ci-samples" >/dev/null \
  || fail "ci-samples not restored from seed after --force-ci-samples"
[[ ! -f "${ROOT}/ci-samples/.orphan-live-only" ]] || fail "orphan live file survived restore"
[[ -d "${ROOT}/.product-init-backup" ]] || fail "expected .product-init-backup after force"
pass "--with-ci-samples refuses dirty trees; --force-ci-samples full replace + backup"

# Cleanup plugin litter + disposable product/ from happy-path init.
rm -rf "${ROOT}/plugins/${SLUG}"
rm -f "${ROOT}/PRODUCT_README.${SLUG}.md"
rm -rf "${ROOT}/.product-init-backup"
rm -rf "${ROOT}/.product-init-stage"
drop_disposable_product
[[ ! -f "${ROOT}/PRODUCT_README.${SLUG}.md" ]] || fail "PRODUCT_README litter after cleanup"
[[ ! -d "${ROOT}/product" ]] || fail "disposable product/ still present before restore"

# Restore live overlay and assert preservation.
restore_all
trap - EXIT
[[ -f "${ROOT}/product/precious" ]] || fail "product/precious must survive full product-init_test"
if [[ "${HAD_PRODUCT}" -eq 1 ]]; then
  if [[ "${PLANTED_PRECIOUS}" -eq 1 ]]; then
    rm -f "${ROOT}/product/precious"
  fi
  after_snap="$(mktemp)"
  (
    cd "${ROOT}/product"
    find . -type f -print0 | sort -z | xargs -0 sha256sum
  ) >"${after_snap}"
  if ! diff -u "${PRODUCT_SNAP}" "${after_snap}" >/dev/null; then
    echo "FAIL: pre-existing product/ overlay changed unexpectedly:" >&2
    diff -u "${PRODUCT_SNAP}" "${after_snap}" >&2 || true
    rm -f "${PRODUCT_SNAP}" "${after_snap}"
    exit 1
  fi
  rm -f "${PRODUCT_SNAP}" "${after_snap}"
  pass "pre-existing product/ overlay byte-identical after full test"
else
  rm -rf "${ROOT}/product"
  pass "planted overlay removed; no live product/ left on upstream tree"
fi

echo "product-init_test OK"
