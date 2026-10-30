#!/usr/bin/env bash
# Fail if product-facing docs use in-repo relative links into docs/phase-*
# or hardcoded GitHub blob/tree URLs into working history on `dev`.
# Working-history paths are named in prose; they must not be markdown hrefs
# on the product surface (those 404 on main after promote).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"

paths=(
  README.md
  PLUGINS.md
  RUNBOOK.md
  AGENTS.md
  docs/README.md
  docs/COMMERCIAL.md
  docs/GOVULN_BASELINE.md
  docs/guides
  docs/diagrams
  plugins/*/README.md
)

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
