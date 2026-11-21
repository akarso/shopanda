#!/usr/bin/env bash
# Validate docs/PRODUCT_OWNED.md manifest + hook expectations (Phase 17).
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

python3 - "${ROOT}" "${MANIFEST}" <<'PY'
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
manifest = pathlib.Path(sys.argv[2])
text = manifest.read_text(encoding="utf-8").splitlines()

in_section = False
in_fence = False
rows = []
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
    raw = line.rstrip("\r")
    if not raw or raw.lstrip().startswith("#"):
        continue
    if "\t" not in raw:
        raise SystemExit(
            f"ERROR: PRODUCT_OWNED row must be tab-separated (got {raw!r})"
        )
    parts = raw.split("\t")
    if len(parts) < 4:
        raise SystemExit(
            f"ERROR: PRODUCT_OWNED row needs path, kind, expect, note (got {raw!r})"
        )
    path, kind, expect, note = parts[0], parts[1], parts[2], "\t".join(parts[3:])
    rows.append((path, kind, expect, note))

owned = sum(1 for _, k, _, _ in rows if k == "owned")
hook = sum(1 for _, k, _, _ in rows if k == "hook")
companion = sum(1 for _, k, _, _ in rows if k == "companion")
if owned < 1 or hook < 1:
    raise SystemExit(
        f"ERROR: PRODUCT_OWNED must list owned and hook rows "
        f"(owned={owned} hook={hook} companion={companion})"
    )

def require_file(rel: str) -> pathlib.Path:
    p = root / rel
    if not p.is_file():
        raise SystemExit(f"ERROR: hook file missing: {rel}")
    return p

def check_gitignore_markers(rel: str) -> None:
    start = "# >>> shopanda-product-gitignore"
    end = "# <<< shopanda-product-gitignore"
    lines = require_file(rel).read_text(encoding="utf-8").splitlines()
    starts = [i for i, ln in enumerate(lines) if ln == start]
    ends = [i for i, ln in enumerate(lines) if ln == end]
    if len(starts) != 1 or len(ends) != 1:
        raise SystemExit(
            f"ERROR: {rel} must have exactly one ordered marker pair "
            f"(start={len(starts)} end={len(ends)})"
        )
    if starts[0] >= ends[0]:
        raise SystemExit(f"ERROR: {rel} start marker must appear before end marker")

for path, kind, expect, _note in rows:
    if kind in ("owned", "companion"):
        if expect != "-":
            raise SystemExit(
                f"ERROR: {path}: owned/companion expect must be '-' (got {expect!r})"
            )
        continue
    if kind != "hook":
        raise SystemExit(f"ERROR: unknown kind {kind!r} for {path}")

    if expect == "exists":
        require_file(path)
        continue
    if expect == "gitignore_markers":
        check_gitignore_markers(path)
        continue
    if expect.startswith("contains:"):
        needle = expect[len("contains:") :]
        body = require_file(path).read_text(encoding="utf-8")
        if needle not in body:
            raise SystemExit(f"ERROR: {path} missing required substring {needle!r}")
        continue
    if expect.startswith("contains_all:"):
        body = require_file(path).read_text(encoding="utf-8")
        for needle in expect[len("contains_all:") :].split("|"):
            if not needle:
                raise SystemExit(f"ERROR: empty contains_all needle for {path}")
            if needle not in body:
                raise SystemExit(
                    f"ERROR: {path} missing required substring {needle!r}"
                )
        continue
    raise SystemExit(f"ERROR: unknown expect {expect!r} for hook {path}")

print(f"product-owned manifest OK (owned={owned} hook={hook} companion={companion})")
PY
