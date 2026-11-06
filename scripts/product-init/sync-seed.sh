#!/usr/bin/env bash
# Derive product-init seed from live trees (single source of truth = scripts/ci + ci-samples).
#
# Usage:
#   bash scripts/product-init/sync-seed.sh          # refresh seed + reserved-slugs.txt
#   bash scripts/product-init/sync-seed.sh --check  # exit 1 if seed/reserved drifted
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SEED="${ROOT}/scripts/product-init/seed"
RESERVED_FILE="${ROOT}/scripts/product-init/reserved-slugs.txt"
CHECK=0

if [[ "${1:-}" == "--check" ]]; then
  CHECK=1
elif [[ -n "${1:-}" ]]; then
  echo "Usage: bash scripts/product-init/sync-seed.sh [--check]" >&2
  exit 1
fi

list_plugin_dirs() {
  local d name
  for d in "${ROOT}/plugins"/*/; do
    [[ -d "${d}" ]] || continue
    name="$(basename "${d}")"
    # Require at least one .go file in the plugin package.
    if compgen -G "${d}*.go" >/dev/null; then
      printf '%s\n' "${name}"
    fi
  done | sort -u
}

if [[ "${CHECK}" -eq 1 ]]; then
  diff -rq "${SEED}/scripts-ci" "${ROOT}/scripts/ci" >/dev/null \
    || { echo "ERROR: seed/scripts-ci drifted from scripts/ci — run: bash scripts/product-init/sync-seed.sh" >&2; exit 1; }
  diff -rq "${SEED}/ci-samples" "${ROOT}/ci-samples" >/dev/null \
    || { echo "ERROR: seed/ci-samples drifted from ci-samples — run: bash scripts/product-init/sync-seed.sh" >&2; exit 1; }
  if [[ ! -f "${RESERVED_FILE}" ]]; then
    echo "ERROR: missing ${RESERVED_FILE}" >&2
    exit 1
  fi
  # Every reserved slug must still exist (first-party not deleted without list update).
  while IFS= read -r name || [[ -n "${name}" ]]; do
    # Strip CR so CRLF checkouts still resolve plugins/<slug>/.
    name="${name%$'\r'}"
    [[ -z "${name}" || "${name}" == \#* ]] && continue
    if [[ ! -d "${ROOT}/plugins/${name}" ]]; then
      echo "ERROR: reserved slug '${name}' has no plugins/${name}/ — run sync-seed or restore the plugin" >&2
      exit 1
    fi
  done <"${RESERVED_FILE}"
  # On Shopanda platform CI: every plugins/*/ package must be reserved (no wipeable orphans).
  if [[ "${SHOPANDA_PLATFORM:-0}" == "1" ]]; then
    tmp="$(mktemp)"
    list_plugin_dirs >"${tmp}"
    if ! diff -u "${RESERVED_FILE}" "${tmp}" >/dev/null; then
      echo "ERROR: reserved-slugs.txt drifted from plugins/*/ — run: bash scripts/product-init/sync-seed.sh" >&2
      diff -u "${RESERVED_FILE}" "${tmp}" >&2 || true
      rm -f "${tmp}"
      exit 1
    fi
    rm -f "${tmp}"
  fi
  echo "sync-seed --check OK"
  exit 0
fi

mkdir -p "${SEED}/scripts-ci" "${SEED}/ci-samples"
rm -rf "${SEED}/scripts-ci" "${SEED}/ci-samples"
mkdir -p "${SEED}/scripts-ci" "${SEED}/ci-samples"
cp -a "${ROOT}/scripts/ci/." "${SEED}/scripts-ci/"
cp -a "${ROOT}/ci-samples/." "${SEED}/ci-samples/"
list_plugin_dirs >"${RESERVED_FILE}"
echo "sync-seed OK (seed + reserved-slugs.txt refreshed from live trees)"
