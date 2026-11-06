#!/usr/bin/env bash
# Scaffold a product external plugin (Phase 17).
#
# Usage:
#   bash scripts/product-init.sh --slug myproduct
#   bash scripts/product-init.sh --slug myproduct --with-ci-samples
#   bash scripts/product-init.sh --slug myproduct --force
#   bash scripts/product-init.sh --slug myproduct --with-ci-samples --force-ci-samples
#
# Does not install git hooks. Does not enable live .github/workflows.
# --with-ci-samples restores scripts/ci + ci-samples from seed (+ GOVULN stub).
# If live trees differ from seed, refuses unless --force-ci-samples (backs up first).
#
# Staging lives under ${ROOT}/.product-init-stage/ (same filesystem as the repo)
# so installs are rename-atomic; TMPDIR/tmpfs is never used for plugin/CI trees.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TEMPLATE_ROOT="${ROOT}/scripts/product-init/templates"
SEED_ROOT="${ROOT}/scripts/product-init/seed"
RESERVED_FILE="${ROOT}/scripts/product-init/reserved-slugs.txt"
STAGE_ROOT="${ROOT}/.product-init-stage"

SLUG=""
WITH_CI_SAMPLES=0
FORCE=0
FORCE_CI_SAMPLES=0
DISPLAY_NAME=""

# Go keywords (cannot be package names / import aliases).
GO_KEYWORDS='break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var'

usage() {
  cat <<'EOF'
Usage: bash scripts/product-init.sh --slug SLUG [options]

  --slug                 Plugin directory / URL slug (lowercase [a-z0-9_], required)
  --name                 Display name for README (default: slug; safe charset only)
  --with-ci-samples      Restore scripts/ci + ci-samples from seed + GOVULN stub
  --force-ci-samples     Allow overwriting customized scripts/ci or ci-samples (backs up first)
  --force                Overwrite an existing non-reserved plugins/<slug>/

Requires: python3
EOF
}

need_arg() {
  local flag="$1"
  if [[ $# -lt 2 || -z "${2:-}" || "${2:-}" == --* ]]; then
    echo "ERROR: ${flag} requires a value" >&2
    usage >&2
    exit 1
  fi
}

if ! command -v python3 >/dev/null 2>&1; then
  echo "ERROR: python3 is required for product-init template rendering (not found on PATH)" >&2
  exit 1
fi

while [[ $# -gt 0 ]]; do
  case "$1" in
    --slug)
      need_arg "$@"
      SLUG="$2"
      shift 2
      ;;
    --name)
      need_arg "$@"
      DISPLAY_NAME="$2"
      shift 2
      ;;
    --with-ci-samples) WITH_CI_SAMPLES=1; shift ;;
    --force-ci-samples) FORCE_CI_SAMPLES=1; shift ;;
    --force) FORCE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "ERROR: unknown argument: $1" >&2; usage >&2; exit 1 ;;
  esac
done

if [[ -z "${SLUG}" ]]; then
  echo "ERROR: --slug is required" >&2
  usage >&2
  exit 1
fi

if [[ ! "${SLUG}" =~ ^[a-z][a-z0-9_]*$ ]]; then
  echo "ERROR: --slug must match ^[a-z][a-z0-9_]*$ (got ${SLUG})" >&2
  exit 1
fi

for kw in ${GO_KEYWORDS}; do
  if [[ "${SLUG}" == "${kw}" ]]; then
    echo "ERROR: slug '${SLUG}' is a Go keyword and cannot be used as a package name" >&2
    exit 1
  fi
done

if [[ ! -f "${RESERVED_FILE}" ]]; then
  echo "ERROR: missing ${RESERVED_FILE} — run: bash scripts/product-init/sync-seed.sh" >&2
  exit 1
fi

while IFS= read -r reserved || [[ -n "${reserved}" ]]; do
  # Strip CR so CRLF checkouts (core.autocrlf) still match.
  reserved="${reserved%$'\r'}"
  [[ -z "${reserved}" || "${reserved}" == \#* ]] && continue
  if [[ "${SLUG}" == "${reserved}" ]]; then
    echo "ERROR: slug '${SLUG}' is a reserved first-party plugin name and cannot be overwritten by product-init" >&2
    exit 1
  fi
done <"${RESERVED_FILE}"

PACKAGE="${SLUG}"
PLUGIN_NAME="${SLUG}"
TABLE_PREFIX="${SLUG}"
DISPLAY_NAME="${DISPLAY_NAME:-${SLUG}}"

name_re='^[A-Za-z0-9][A-Za-z0-9 _.-]*$'
if [[ ! "${DISPLAY_NAME}" =~ $name_re ]]; then
  echo "ERROR: --name must match ${name_re} (got ${DISPLAY_NAME})" >&2
  exit 1
fi

DEST="${ROOT}/plugins/${SLUG}"
PRODUCT_README_OUT="${ROOT}/PRODUCT_README.${SLUG}.md"
if [[ -e "${DEST}" && "${FORCE}" -ne 1 ]]; then
  echo "ERROR: ${DEST} already exists (pass --force to overwrite a non-reserved plugin)" >&2
  exit 1
fi

render_file() {
  local src="$1"
  local dst="$2"
  mkdir -p "$(dirname "${dst}")"
  SLUG="${SLUG}" PACKAGE="${PACKAGE}" PLUGIN_NAME="${PLUGIN_NAME}" \
    TABLE_PREFIX="${TABLE_PREFIX}" DISPLAY_NAME="${DISPLAY_NAME}" \
    python3 - "${src}" "${dst}" <<'PY'
import os, sys
src, dst = sys.argv[1], sys.argv[2]
text = open(src, encoding="utf-8").read()
repl = {
    "{{.Slug}}": os.environ["SLUG"],
    "{{.Package}}": os.environ["PACKAGE"],
    "{{.PluginName}}": os.environ["PLUGIN_NAME"],
    "{{.TablePrefix}}": os.environ["TABLE_PREFIX"],
    "{{.DisplayName}}": os.environ["DISPLAY_NAME"],
}
for k, v in repl.items():
    text = text.replace(k, v)
open(dst, "w", encoding="utf-8").write(text)
PY
}

# Same-FS stage then rename into place (no rm-before-copy window).
install_dir_from_stage() {
  local stage="$1"
  local dst="$2"
  local parent old
  parent="$(dirname "${dst}")"
  mkdir -p "${parent}"
  old="${STAGE_ROOT}/$(basename "${dst}").prev.$$"
  rm -rf "${old}"
  if [[ -e "${dst}" ]]; then
    mv "${dst}" "${old}"
  fi
  if ! mv "${stage}" "${dst}"; then
    echo "ERROR: failed to install ${dst} from staged tree" >&2
    if [[ -e "${old}" ]]; then
      mv "${old}" "${dst}" || true
    fi
    exit 1
  fi
  rm -rf "${old}"
}

# Stage seed on the repo filesystem, then rename-swap into dst.
replace_tree() {
  local src="$1"
  local dst="$2"
  local stage
  if [[ ! -d "${src}" ]]; then
    echo "ERROR: seed/source missing: ${src}" >&2
    exit 1
  fi
  stage="${STAGE_ROOT}/tree-$(basename "${dst}").$$"
  rm -rf "${stage}"
  mkdir -p "${stage}"
  cp -a "${src}/." "${stage}/"
  install_dir_from_stage "${stage}" "${dst}"
}

trees_differ() {
  local live="$1"
  local seed="$2"
  [[ -d "${live}" ]] || return 1
  ! diff -rq "${live}" "${seed}" >/dev/null 2>&1
}

backup_tree() {
  local live="$1"
  local stamp="$2"
  local dest="${ROOT}/.product-init-backup/${stamp}/$(basename "${live}")"
  mkdir -p "$(dirname "${dest}")"
  cp -a "${live}" "${dest}"
  echo "    backed up ${live} → ${dest}"
}

restore_ci_samples_from_seed() {
  local src_ci="${SEED_ROOT}/scripts-ci"
  local src_samples="${SEED_ROOT}/ci-samples"
  if [[ ! -f "${src_ci}/unit.sh" || ! -f "${src_samples}/README.md" ]]; then
    echo "ERROR: product-init seed incomplete under ${SEED_ROOT}" >&2
    echo "On a Shopanda tree run: bash scripts/product-init/sync-seed.sh" >&2
    exit 1
  fi

  local dirty=0
  if trees_differ "${ROOT}/scripts/ci" "${src_ci}"; then
    dirty=1
  fi
  if trees_differ "${ROOT}/ci-samples" "${src_samples}"; then
    dirty=1
  fi
  if [[ "${dirty}" -eq 1 && "${FORCE_CI_SAMPLES}" -ne 1 ]]; then
    echo "ERROR: scripts/ci or ci-samples differ from seed (customized or out of date)." >&2
    echo "Refusing to overwrite. Pass --force-ci-samples to back up then restore from seed." >&2
    echo "Or refresh seed from live on a Shopanda pin: bash scripts/product-init/sync-seed.sh" >&2
    exit 1
  fi
  # Backup whenever a live tree exists and we are about to replace it
  # (including the clean-tree path) so interrupt after swap still has a copy.
  local stamp
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  if [[ "${dirty}" -eq 1 && "${FORCE_CI_SAMPLES}" -eq 1 ]]; then
    echo "NOTE: live CI trees differ from seed — backing up then full replace"
  fi
  [[ -d "${ROOT}/scripts/ci" ]] && backup_tree "${ROOT}/scripts/ci" "${stamp}"
  [[ -d "${ROOT}/ci-samples" ]] && backup_tree "${ROOT}/ci-samples" "${stamp}"

  echo "==> restoring portable CI scripts and samples from seed"
  replace_tree "${src_ci}" "${ROOT}/scripts/ci"
  replace_tree "${src_samples}" "${ROOT}/ci-samples"
  mkdir -p "${ROOT}/docs"
  if [[ ! -f "${ROOT}/docs/GOVULN_BASELINE.md" ]]; then
    render_file "${TEMPLATE_ROOT}/docs/GOVULN_BASELINE.md.tmpl" "${ROOT}/docs/GOVULN_BASELINE.md"
    echo "    wrote docs/GOVULN_BASELINE.md stub"
  fi
  echo "    restored scripts/ci/ and ci-samples/ from seed (full tree replace)"
  echo "    (not installing live .github/workflows — copy from ci-samples/ if desired)"
}

mkdir -p "${STAGE_ROOT}"
cleanup() { rm -rf "${STAGE_ROOT}"; }
trap cleanup EXIT

STAGE="${STAGE_ROOT}/plugins-${SLUG}.new"
rm -rf "${STAGE}"
mkdir -p "${STAGE}/migrations"

echo "==> scaffolding plugins/${SLUG} (staging on repo filesystem)"
render_file "${TEMPLATE_ROOT}/plugin/plugin.go.tmpl" "${STAGE}/plugin.go"
render_file "${TEMPLATE_ROOT}/plugin/migrations.go.tmpl" "${STAGE}/migrations.go"
render_file "${TEMPLATE_ROOT}/plugin/plugin_test.go.tmpl" "${STAGE}/plugin_test.go"
render_file "${TEMPLATE_ROOT}/plugin/README.md.tmpl" "${STAGE}/README.md"
render_file "${TEMPLATE_ROOT}/migrations/001_create_meta.sql.tmpl" "${STAGE}/migrations/001_create_meta.sql"
render_file "${TEMPLATE_ROOT}/docs/PRODUCT_README.md.tmpl" "${STAGE_ROOT}/PRODUCT_README.${SLUG}.md.new"

if [[ ! -s "${STAGE}/plugin.go" ]] || ! grep -q "${SLUG}" "${STAGE}/plugin.go"; then
  echo "ERROR: staged plugin.go render failed" >&2
  exit 1
fi
if [[ ! -s "${STAGE_ROOT}/PRODUCT_README.${SLUG}.md.new" ]]; then
  echo "ERROR: staged PRODUCT_README render failed" >&2
  exit 1
fi

# Preflight CI restore (refuse dirty before mutating plugin destination).
if [[ "${WITH_CI_SAMPLES}" -eq 1 ]]; then
  src_ci="${SEED_ROOT}/scripts-ci"
  src_samples="${SEED_ROOT}/ci-samples"
  if [[ ! -f "${src_ci}/unit.sh" || ! -f "${src_samples}/README.md" ]]; then
    echo "ERROR: product-init seed incomplete under ${SEED_ROOT}" >&2
    exit 1
  fi
  dirty=0
  if trees_differ "${ROOT}/scripts/ci" "${src_ci}"; then
    echo "ERROR: scripts/ci differs from seed (customized or out of date)." >&2
    dirty=1
  fi
  if trees_differ "${ROOT}/ci-samples" "${src_samples}"; then
    echo "ERROR: ci-samples differs from seed (customized or out of date)." >&2
    dirty=1
  fi
  if [[ "${dirty}" -eq 1 && "${FORCE_CI_SAMPLES}" -ne 1 ]]; then
    echo "Refusing to overwrite. Pass --force-ci-samples to back up then restore from seed." >&2
    echo "Or refresh seed from live on a Shopanda pin: bash scripts/product-init/sync-seed.sh" >&2
    exit 1
  fi
fi

install_dir_from_stage "${STAGE}" "${DEST}"
# PRODUCT_README is a suggested root banner (does not overwrite README.md).
mv "${STAGE_ROOT}/PRODUCT_README.${SLUG}.md.new" "${PRODUCT_README_OUT}"

# Restore CI trees only after the plugin is in place.
if [[ "${WITH_CI_SAMPLES}" -eq 1 ]]; then
  restore_ci_samples_from_seed
fi

# Success: drop EXIT cleanup of STAGE_ROOT contents we already moved; remove leftovers.
trap - EXIT
rm -rf "${STAGE_ROOT}"

cat <<EOF

OK: created ${DEST}
    wrote ${PRODUCT_README_OUT} (optional root README banner — review before replacing README.md)

Next steps:
  1. Add registration in cmd/api/register_plugins.go:

       import "github.com/akarso/shopanda/plugins/${SLUG}"

       // inside registerPlugins:
       registry.Register(${PACKAGE}.New())

  2. Optional config gate: add plugins.${SLUG}.enabled to internal/platform/config
     and wrap the Register call.

  3. Build and test:

       go test ./plugins/${SLUG}/ -count=1
       make check

  4. Update the root README banner for your product
     (start from PRODUCT_README.${SLUG}.md or plugins/${SLUG}/README.md).

  5. CI is optional. Local/FTP: do nothing.
     Samples: see ci-samples/README.md and docs/guides/PRODUCT_SITES.md

Job types (canonical only): integration.sync.${SLUG}.<name>
EOF
