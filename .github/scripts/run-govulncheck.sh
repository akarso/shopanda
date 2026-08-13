#!/usr/bin/env bash
# Fail-closed govulncheck with optional OSV baseline from GOVULN_BASELINE.md.
#
# Usage (CI installs the pinned binary first):
#   bash .github/scripts/run-govulncheck.sh
#
# Portable: avoids bash-4-only features (mapfile) so macOS /bin/bash works.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

BASELINE_DOC="${ROOT}/docs/phase-10-platform-excellence/GOVULN_BASELINE.md"
MAX_BASELINE_DAYS=90

if [[ ! -f "${BASELINE_DOC}" ]]; then
  echo "ERROR: missing baseline doc ${BASELINE_DOC}" >&2
  exit 1
fi

if ! command -v govulncheck >/dev/null 2>&1; then
  echo "ERROR: govulncheck not on PATH (install the pinned version from CI)" >&2
  exit 1
fi

# Extract OSV IDs from the machine-readable fenced block after "Machine-readable allowlist".
BASELINE_IDS="$(
  awk '
    BEGIN { inblock=0; seen=0 }
    /^Machine-readable allowlist/ { seen=1; next }
    seen && /^```/ {
      if (inblock) exit
      inblock=1
      next
    }
    inblock && /^#/ { next }
    inblock && NF { gsub(/[[:space:]]/,""); print }
  ' "${BASELINE_DOC}" | sort -u
)"

# Table rows: "| GO-… | module | owner | YYYY-MM-DD | notes |"
TABLE_ROWS="$(grep -E '^\| GO-[0-9]+-[0-9]+' "${BASELINE_DOC}" || true)"
TABLE_IDS="$(
  printf '%s\n' "${TABLE_ROWS}" \
    | awk -F'|' '{gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2); print $2}' \
    | sort -u
)"

# Governance: fenced allowlist and accountability table must match.
missing_table=""
if [[ -n "${BASELINE_IDS}" ]]; then
  while IFS= read -r id; do
    [[ -z "${id}" ]] && continue
    if ! printf '%s\n' "${TABLE_IDS}" | grep -Fxq -- "${id}"; then
      missing_table="${missing_table}${id}"$'\n'
    fi
  done <<EOF
${BASELINE_IDS}
EOF
fi
missing_fence=""
if [[ -n "${TABLE_IDS}" ]]; then
  while IFS= read -r id; do
    [[ -z "${id}" ]] && continue
    if ! printf '%s\n' "${BASELINE_IDS}" | grep -Fxq -- "${id}"; then
      missing_fence="${missing_fence}${id}"$'\n'
    fi
  done <<EOF
${TABLE_IDS}
EOF
fi
if [[ -n "${missing_table}" || -n "${missing_fence}" ]]; then
  echo "ERROR: GOVULN baseline fenced allowlist and table are out of sync:" >&2
  if [[ -n "${missing_table}" ]]; then
    echo "  In fenced list but missing table row (owner+expiry required):" >&2
    printf '%s' "${missing_table}" | sed 's/^/    - /' >&2
  fi
  if [[ -n "${missing_fence}" ]]; then
    echo "  In table but missing from fenced allowlist:" >&2
    printf '%s' "${missing_fence}" | sed 's/^/    - /' >&2
  fi
  exit 1
fi

today="$(date -u +%Y-%m-%d)"
# Portable max expiry = today + MAX_BASELINE_DAYS (UTC).
if date -u -d "+${MAX_BASELINE_DAYS} days" +%Y-%m-%d >/dev/null 2>&1; then
  max_expiry="$(date -u -d "+${MAX_BASELINE_DAYS} days" +%Y-%m-%d)"
elif date -u -v+"${MAX_BASELINE_DAYS}"d +%Y-%m-%d >/dev/null 2>&1; then
  max_expiry="$(date -u -v+"${MAX_BASELINE_DAYS}"d +%Y-%m-%d)"
else
  echo "ERROR: cannot compute max baseline expiry (+${MAX_BASELINE_DAYS}d)" >&2
  exit 1
fi

expired=""
too_far=""
missing_owner=""
bad_expiry=""
while IFS= read -r line; do
  [[ -z "${line}" ]] && continue
  osv="$(echo "${line}" | awk -F'|' '{gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2); print $2}')"
  owner="$(echo "${line}" | awk -F'|' '{gsub(/^[[:space:]]+|[[:space:]]+$/, "", $4); print $4}')"
  expiry="$(echo "${line}" | awk -F'|' '{gsub(/^[[:space:]]+|[[:space:]]+$/, "", $5); print $5}')"
  case "${osv}" in
    GO-*) ;;
    *) continue ;;
  esac
  if [[ -z "${owner}" || "${owner}" == "—" || "${owner}" == "-" ]]; then
    missing_owner="${missing_owner}${osv}"$'\n'
  fi
  case "${expiry}" in
    [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9])
      if [[ "${expiry}" < "${today}" ]]; then
        expired="${expired}${osv} (expired ${expiry})"$'\n'
      elif [[ "${expiry}" > "${max_expiry}" ]]; then
        too_far="${too_far}${osv} (expiry ${expiry} > max ${max_expiry} / ${MAX_BASELINE_DAYS}d)"$'\n'
      fi
      ;;
    *)
      bad_expiry="${bad_expiry}${osv} (expiry ${expiry:-empty})"$'\n'
      ;;
  esac
done <<EOF
${TABLE_ROWS}
EOF

if [[ -n "${missing_owner}" || -n "${bad_expiry}" || -n "${expired}" || -n "${too_far}" ]]; then
  echo "ERROR: invalid GOVULN baseline table row(s):" >&2
  if [[ -n "${missing_owner}" ]]; then
    echo "  Missing owner:" >&2
    printf '%s' "${missing_owner}" | sed 's/^/    - /' >&2
  fi
  if [[ -n "${bad_expiry}" ]]; then
    echo "  Missing/invalid expiry (need YYYY-MM-DD):" >&2
    printf '%s' "${bad_expiry}" | sed 's/^/    - /' >&2
  fi
  if [[ -n "${expired}" ]]; then
    echo "  Past expiry:" >&2
    printf '%s' "${expired}" | sed 's/^/    - /' >&2
  fi
  if [[ -n "${too_far}" ]]; then
    echo "  Expiry beyond ${MAX_BASELINE_DAYS} days from today:" >&2
    printf '%s' "${too_far}" | sed 's/^/    - /' >&2
  fi
  exit 1
fi

TMP="$(mktemp)"
trap 'rm -f "${TMP}"' EXIT

set +e
govulncheck ./... >"${TMP}" 2>&1
status=$?
set -e

# 0 = clean, 3 = vulnerabilities found, anything else = scanner/tool failure.
if [[ "${status}" -ne 0 && "${status}" -ne 3 ]]; then
  echo "ERROR: govulncheck failed with exit ${status}" >&2
  cat "${TMP}" >&2
  exit "${status}"
fi

FOUND_IDS="$(
  # Lines look like: "Vulnerability #1: GO-2026-5856"
  grep -E '^Vulnerability #[0-9]+: GO-[0-9]+-[0-9]+' "${TMP}" \
    | sed -E 's/^Vulnerability #[0-9]+: (GO-[0-9]+-[0-9]+).*/\1/' \
    | sort -u || true
)"

# Fail-closed: scanner reported findings but we could not parse any OSV IDs.
if [[ "${status}" -eq 3 && -z "${FOUND_IDS}" ]]; then
  echo "ERROR: govulncheck reported findings (exit 3) but none could be parsed from text output" >&2
  echo "Update the parser in .github/scripts/run-govulncheck.sh or pin an older govulncheck." >&2
  cat "${TMP}" >&2
  exit 1
fi

unexpected=""
if [[ -n "${FOUND_IDS}" ]]; then
  while IFS= read -r id; do
    [[ -z "${id}" ]] && continue
    if ! printf '%s\n' "${BASELINE_IDS}" | grep -Fxq -- "${id}"; then
      unexpected="${unexpected}${id}"$'\n'
    fi
  done <<EOF
${FOUND_IDS}
EOF
fi

if [[ -n "${unexpected}" ]]; then
  echo "ERROR: govulncheck found vulnerabilities not on the baseline:" >&2
  printf '%s' "${unexpected}" | sed 's/^/  - /' >&2
  echo >&2
  cat "${TMP}" >&2
  echo >&2
  echo "See docs/phase-10-platform-excellence/GOVULN_BASELINE.md" >&2
  exit 1
fi

if [[ -n "${FOUND_IDS}" ]]; then
  count="$(printf '%s\n' "${FOUND_IDS}" | grep -c . || true)"
  echo "govulncheck: ${count} finding(s) allowed by baseline:"
  printf '%s\n' "${FOUND_IDS}" | sed 's/^/  - /'
else
  echo "govulncheck: no vulnerabilities found (fail-closed; baseline empty)."
fi
