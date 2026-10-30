#!/usr/bin/env bash
# Merge SOURCE into a new branch from TARGET, then delete docs/phase-* so
# working history does not land on main.
#
# Usage:
#   bash scripts/promote-dev-to-main.sh
#   SOURCE=my-branch PROMOTE_BRANCH=promote/docs-split bash scripts/promote-dev-to-main.sh
#
# Does not push. Leaves you on the promote branch. Never raw-merge dev into main.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SOURCE="${SOURCE:-origin/dev}"
TARGET="${TARGET:-origin/main}"
STAMP="$(date -u +%Y-%m-%d)"
BRANCH="${PROMOTE_BRANCH:-promote/${STAMP}}"

usage() {
  cat <<EOF
Merge SOURCE into a new branch from TARGET, then delete docs/phase-*.

Environment:
  SOURCE          git ref to merge (default: origin/dev)
  TARGET          git ref to branch from (default: origin/main)
  PROMOTE_BRANCH  local branch name (default: promote/YYYY-MM-DD)

Does not push. Review the branch, then:
  git push -u origin ${BRANCH}
  gh pr create --base main --head ${BRANCH}
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
  echo "ERROR: tracked files have uncommitted changes. Commit or stash first." >&2
  exit 1
fi

git fetch origin dev main

resolve_ref() {
  local ref="$1"
  if git rev-parse --verify "${ref}" >/dev/null 2>&1; then
    printf '%s\n' "${ref}"
    return
  fi
  if git rev-parse --verify "origin/${ref}" >/dev/null 2>&1; then
    printf 'origin/%s\n' "${ref}"
    return
  fi
  echo "ERROR: unknown ref ${ref}" >&2
  exit 1
}

SOURCE="$(resolve_ref "${SOURCE}")"
TARGET="$(resolve_ref "${TARGET}")"

git checkout -B "${BRANCH}" "${TARGET}"
git merge --no-ff --no-edit -m "Merge ${SOURCE} into ${BRANCH} for main promotion." "${SOURCE}"

stripped=0
for dir in docs/phase-*; do
  if [[ ! -d "${dir}" ]]; then
    continue
  fi
  # Skip untracked working-tree dirs (e.g. a local phase folder not on SOURCE).
  if git ls-files -- "${dir}" | grep -q .; then
    git rm -r "${dir}"
    stripped=1
  fi
done

if [[ "${stripped}" -eq 1 ]]; then
  git commit -m "$(cat <<'EOF'
Strip working-history docs from main.

Phase folders, PR specs, and in-progress roadmaps stay on dev.
See docs/README.md.

EOF
)"
fi

echo "Promote branch: ${BRANCH} (SOURCE=${SOURCE} TARGET=${TARGET})"
echo "Review, then: git push -u origin ${BRANCH}"
echo "Open a PR targeting main. Do not raw-merge dev into main."
