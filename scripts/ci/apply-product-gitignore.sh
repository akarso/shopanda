#!/usr/bin/env bash
# Sync .gitignore.product into the marked section of .gitignore.
#
# Usage:
#   bash scripts/ci/apply-product-gitignore.sh
#
# Missing .gitignore.product clears the marked section (upstream default).
# Fragment lines equal to the start/end markers are rejected (fail closed).
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

FRAG_FILE="$(mktemp "${TMPDIR:-/tmp}/gitignore-product.XXXXXX")"
cleanup() { rm -f "${FRAG_FILE}"; }
trap cleanup EXIT

if [[ -f "${FRAGMENT}" ]]; then
  cp "${FRAGMENT}" "${FRAG_FILE}"
else
  : >"${FRAG_FILE}"
fi

python3 - "${GITIGNORE}" "${START}" "${END}" "${FRAG_FILE}" <<'PY'
import sys

path, start, end, frag_path = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
raw = open(path, encoding="utf-8").read().splitlines(keepends=True)
lines = [ln.rstrip("\n") for ln in raw]

starts = [i for i, ln in enumerate(lines) if ln == start]
ends = [i for i, ln in enumerate(lines) if ln == end]
if len(starts) != 1 or len(ends) != 1:
    raise SystemExit(
        f"ERROR: {path} must contain exactly one start and one end marker "
        f"(start={len(starts)} end={len(ends)})"
    )
si, ei = starts[0], ends[0]
if si >= ei:
    raise SystemExit(f"ERROR: {path} start marker must appear before end marker")

frag_lines = open(frag_path, encoding="utf-8").read().splitlines()
for ln in frag_lines:
    if ln == start or ln == end:
        raise SystemExit(
            f"ERROR: .gitignore.product must not contain marker line {ln!r}"
        )

out = lines[: si + 1] + frag_lines + lines[ei:]
text = "\n".join(out) + "\n"
open(path, "w", encoding="utf-8").write(text)
PY

echo "apply-product-gitignore OK"
