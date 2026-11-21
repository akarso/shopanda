#!/usr/bin/env bash
# Validate docs/PRODUCT_OWNED.md manifest + upstream hook markers (Phase 17).
#
# Usage:
#   bash scripts/ci/check-product-owned.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"

MANIFEST="${ROOT}/docs/PRODUCT_OWNED.md"
if [[ ! -f "${MANIFEST}" ]]; then
  echo "ERROR: missing ${MANIFEST}" >&2
  exit 1
fi

# Extract the first fenced block after "Manifest (machine-readable)".
mapfile -t LINES < <(python3 - "${MANIFEST}" <<'PY'
import sys
text = open(sys.argv[1], encoding="utf-8").read().splitlines()
in_section = False
in_fence = False
for line in text:
    if not in_section:
        if "Manifest (machine-readable)" in line:
            in_section = True
        continue
    if not in_fence:
        if line.strip().startswith("```"):
            in_fence = True
        continue
    if line.strip().startswith("```"):
        break
    print(line)
PY
)

owned=0
hook=0
while IFS= read -r line || [[ -n "${line}" ]]; do
  line="${line%$'\r'}"
  [[ -z "${line}" || "${line}" == \#* ]] && continue
  # path<TAB>kind<TAB>note  (tabs preferred; also accept 2+ spaces)
  if [[ "${line}" == *$'\t'* ]]; then
    path="${line%%$'\t'*}"
    rest="${line#*$'\t'}"
    kind="${rest%%$'\t'*}"
  else
    path="$(awk '{print $1}' <<<"${line}")"
    kind="$(awk '{print $2}' <<<"${line}")"
  fi
  case "${kind}" in
    owned) owned=$((owned + 1)) ;;
    hook) hook=$((hook + 1)) ;;
    *)
      echo "ERROR: PRODUCT_OWNED manifest unknown kind ${kind@Q} for ${path@Q}" >&2
      exit 1
      ;;
  esac
done < <(printf '%s\n' "${LINES[@]}")

if [[ "${owned}" -lt 1 || "${hook}" -lt 1 ]]; then
  echo "ERROR: PRODUCT_OWNED manifest must list owned and hook rows (owned=${owned} hook=${hook})" >&2
  exit 1
fi

require_contains() {
  local file="$1"
  local needle="$2"
  local label="$3"
  if [[ ! -f "${file}" ]]; then
    echo "ERROR: hook file missing: ${file}" >&2
    exit 1
  fi
  if ! grep -qF -- "${needle}" "${file}"; then
    echo "ERROR: ${file} missing ${label} marker: ${needle}" >&2
    exit 1
  fi
}

require_contains "Makefile" "-include Makefile.product" "Make product include"
require_contains "scripts/ci/docs-policy.sh" "check-product-owned.sh" "docs-policy product-owned check"
require_contains "scripts/ci/docs-policy.sh" "product/checks" "docs-policy product checks dir"
require_contains ".gitignore" ">>> shopanda-product-gitignore" "gitignore product fragment start"
require_contains ".gitignore" "<<< shopanda-product-gitignore" "gitignore product fragment end"
require_contains ".env.example" ".env.example.product" "env product fragment pointer"
require_contains "configs/config.example.yaml" "config.example.product.yaml" "config product fragment pointer"
require_contains "docker-compose.yml" "docker-compose.override.yml" "compose override pointer"
require_contains "scripts/product-init.sh" "product/reserved-slugs.txt" "product-init reserved overlay"

echo "product-owned manifest OK (owned=${owned} hook=${hook})"
