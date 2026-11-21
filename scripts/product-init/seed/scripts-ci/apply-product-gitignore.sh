#!/usr/bin/env bash
# Sync .gitignore.product into the marked section of .gitignore.
#
# Usage:
#   bash scripts/ci/apply-product-gitignore.sh
#
# Missing .gitignore.product clears the marked section (upstream default).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}"

START='# >>> shopanda-product-gitignore'
END='# <<< shopanda-product-gitignore'
GITIGNORE="${ROOT}/.gitignore"
FRAGMENT="${ROOT}/.gitignore.product"

if [[ ! -f "${GITIGNORE}" ]]; then
  echo "ERROR: missing ${GITIGNORE}" >&2
  exit 1
fi
if ! grep -qF -- "${START}" "${GITIGNORE}" || ! grep -qF -- "${END}" "${GITIGNORE}"; then
  echo "ERROR: ${GITIGNORE} missing product fragment markers" >&2
  exit 1
fi

FRAG_BODY=""
if [[ -f "${FRAGMENT}" ]]; then
  FRAG_BODY="$(cat "${FRAGMENT}")"
fi

python3 - "${GITIGNORE}" "${START}" "${END}" "${FRAG_BODY}" <<'PY'
import sys
path, start, end, body = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
lines = open(path, encoding="utf-8").read().splitlines(keepends=True)
out = []
i = 0
while i < len(lines):
    line = lines[i]
    if line.rstrip("\n") == start:
        out.append(line if line.endswith("\n") else line + "\n")
        if body:
            if not body.endswith("\n"):
                body += "\n"
            out.append(body)
        # skip until end marker
        i += 1
        while i < len(lines) and lines[i].rstrip("\n") != end:
            i += 1
        if i >= len(lines):
            raise SystemExit(f"missing end marker {end!r}")
        out.append(lines[i] if lines[i].endswith("\n") else lines[i] + "\n")
        i += 1
        continue
    out.append(line)
    i += 1
open(path, "w", encoding="utf-8").writelines(out)
PY

echo "apply-product-gitignore OK"
