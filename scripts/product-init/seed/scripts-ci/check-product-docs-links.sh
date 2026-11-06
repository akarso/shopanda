#!/usr/bin/env bash
# Fail if product-facing docs use in-repo relative links into docs/phase-*
# or hardcoded GitHub blob/tree URLs into working history on `dev`.
#
# Missing optional roots are skipped on product trees. On Shopanda platform CI
# (SHOPANDA_PLATFORM=1), zero existing roots is a hard failure.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"

candidate_paths=(
  README.md
  PLUGINS.md
  RUNBOOK.md
  AGENTS.md
  docs/README.md
  docs/COMMERCIAL.md
  docs/GOVULN_BASELINE.md
  docs/guides
  docs/runbooks
  docs/diagrams
  plugins/*/README.md
)

paths=()
for p in "${candidate_paths[@]}"; do
  # shellcheck disable=SC2086
  for expanded in ${p}; do
    if [[ -e "${expanded}" ]]; then
      paths+=("${expanded}")
    fi
  done
done

if [[ ${#paths[@]} -eq 0 ]]; then
  if [[ "${SHOPANDA_PLATFORM:-0}" == "1" ]]; then
    echo "ERROR: no product-doc roots present (required on Shopanda platform CI)" >&2
    exit 1
  fi
  echo "NOTE: no product-doc roots present — skipping link scan."
  exit 0
fi

scan() {
  local pattern="$1"
  local label="$2"
  local hits ec
  set +e
  hits="$(grep -RInE "${pattern}" -- "${paths[@]}")"
  ec=$?
  set -e
  if [[ "${ec}" -gt 1 ]]; then
    echo "ERROR: grep failed while scanning product docs (exit ${ec})" >&2
    exit "${ec}"
  fi
  if [[ -n "${hits}" ]]; then
    echo "ERROR: product docs ${label}" >&2
    printf '%s\n' "${hits}" >&2
    echo "Name the path in prose (dev branch) or lift the file into docs/guides/." >&2
    exit 1
  fi
}

scan '\]\((\.\./)*(docs/)?phase-[0-9]' "contain relative links into docs/phase-* (those paths do not exist on main)."
scan 'blob/dev/docs/phase-|tree/dev/docs/phase-' "contain hardcoded GitHub blob/tree URLs into docs/phase-* on dev."

echo "product-doc link scan OK (${#paths[@]} root(s))"
