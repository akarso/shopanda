#!/usr/bin/env bash
# Fixture: first promote, edit a phase file on SOURCE, second promote.
# The second run is the modify/delete case that used to die in git merge.
# Also: merge that never starts (untracked overwrite) must not look like success.
set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/promote-dev-to-main.sh"
REPO="$(mktemp -d "${TMPDIR:-/tmp}/shopanda-promote.XXXXXX")"
cleanup() { rm -rf "${REPO}"; }
trap cleanup EXIT

git_init() {
  git init -q -b main
  git config user.email test@example.com
  git config user.name "promote fixture"
  git config commit.gpgsign false
}

run_promote() {
  local source="$1" target="$2" branch="$3"
  PROMOTE_ROOT="${REPO}" \
    SOURCE="${source}" \
    TARGET="${target}" \
    PROMOTE_BRANCH="${branch}" \
    SKIP_FETCH=1 \
    bash "${SCRIPT}"
}

assert_no_phase() {
  local leftover
  leftover="$(git ls-files -- 'docs/phase-*')"
  if [[ -n "${leftover}" ]]; then
    echo "FAIL: phase paths still tracked:" >&2
    printf '%s\n' "${leftover}" >&2
    exit 1
  fi
  if [[ -n "$(git diff --name-only --diff-filter=U)" ]]; then
    echo "FAIL: index still conflicted" >&2
    git diff --name-only --diff-filter=U >&2
    exit 1
  fi
}

assert_two_parents() {
  local line n
  line="$(git rev-list --parents -n 1 HEAD)"
  n="$(awk '{print NF}' <<<"${line}")"
  if [[ "${n}" -ne 3 ]]; then
    echo "FAIL: expected two-parent merge commit, got: ${line}" >&2
    exit 1
  fi
}

cd "${REPO}"
git_init

mkdir -p docs/guides docs/phase-1-core/prs
echo "product" >README.md
echo "guide" >docs/guides/HELLO.md
echo "spec v1" >docs/phase-1-core/prs/PR-001.md
git add README.md docs
git commit -qm "main: product docs plus working history"

git checkout -qb dev
echo "spec v2" >docs/phase-1-core/prs/PR-001.md
echo "extra" >docs/phase-1-core/prs/PR-002.md
git add docs/phase-1-core
git commit -qm "dev: more working history"

# First promote: TARGET still has phase files; strip them.
run_promote dev main promote/1
git checkout -q promote/1
assert_no_phase
assert_two_parents
test -f docs/guides/HELLO.md
test -f README.md

# Second cycle: SOURCE edits a phase file that TARGET deleted.
git checkout -q dev
echo "spec v3" >docs/phase-1-core/prs/PR-001.md
echo "notes" >docs/phase-notes.md
echo "app" >app.go
git add docs/phase-1-core/prs/PR-001.md docs/phase-notes.md app.go
git commit -qm "dev: mutate phase file, add phase-notes.md, add code"

run_promote dev promote/1 promote/2
git checkout -q promote/2
assert_no_phase
assert_two_parents
test -f app.go
test -f docs/guides/HELLO.md

# Already-up-to-date: SOURCE is an ancestor; no MERGE_HEAD is success here.
run_promote promote/1 promote/2 promote/noop
git checkout -q promote/noop
if [[ "$(git rev-parse HEAD)" != "$(git rev-parse promote/2)" ]]; then
  echo "FAIL: already-up-to-date promote changed HEAD" >&2
  exit 1
fi

# Untracked overwrite: SOURCE adds a path that exists untracked on TARGET.
# Merge never starts (no MERGE_HEAD). Must not report success or keep promote/3.
git checkout -q dev
echo "from-dev" >docs/guides/NEW.md
git add docs/guides/NEW.md
git commit -qm "dev: new product guide"

git checkout -q promote/2
p2="$(git rev-parse HEAD)"
echo "local dirt" >docs/guides/NEW.md

if PROMOTE_ROOT="${REPO}" SOURCE=dev TARGET=promote/2 PROMOTE_BRANCH=promote/3 SKIP_FETCH=1 bash "${SCRIPT}"; then
  echo "FAIL: expected untracked overwrite to abort" >&2
  exit 1
fi
current="$(git symbolic-ref --short HEAD)"
if [[ "${current}" != "promote/2" ]]; then
  echo "FAIL: expected restore to promote/2, on ${current}" >&2
  exit 1
fi
if [[ "$(git rev-parse HEAD)" != "${p2}" ]]; then
  echo "FAIL: untracked overwrite moved HEAD" >&2
  exit 1
fi
if git show-ref --verify --quiet refs/heads/promote/3; then
  echo "FAIL: failed promote branch should have been deleted" >&2
  exit 1
fi
if git ls-files --error-unmatch docs/guides/NEW.md >/dev/null 2>&1; then
  echo "FAIL: untracked overwrite still merged NEW.md" >&2
  exit 1
fi
test -f docs/guides/NEW.md
rm -f docs/guides/NEW.md

# PROMOTE_FORCE=1 then abort must put the old promote tip back.
git checkout -q promote/2
git checkout -qb promote/force
echo "old" >force.txt
git add force.txt
git commit -qm "prior promote review"
force_prev="$(git rev-parse HEAD)"
git checkout -q promote/2
echo "local dirt" >docs/guides/NEW.md
if PROMOTE_ROOT="${REPO}" SOURCE=dev TARGET=promote/2 PROMOTE_BRANCH=promote/force PROMOTE_FORCE=1 SKIP_FETCH=1 bash "${SCRIPT}"; then
  echo "FAIL: expected PROMOTE_FORCE untracked overwrite to abort" >&2
  exit 1
fi
if [[ "$(git rev-parse refs/heads/promote/force)" != "${force_prev}" ]]; then
  echo "FAIL: PROMOTE_FORCE abort wiped prior promote commits" >&2
  exit 1
fi
rm -f docs/guides/NEW.md
git branch -D promote/force >/dev/null

# Non-phase conflict must abort and restore the branch we started on.
git checkout -q promote/2
echo "main-readme" >README.md
git add README.md
git commit -qm "promote/2: README drift"
git checkout -q dev
echo "dev-readme" >README.md
git add README.md
git commit -qm "dev: README drift"

git checkout -q promote/2
if PROMOTE_ROOT="${REPO}" SOURCE=dev TARGET=promote/2 PROMOTE_BRANCH=promote/3 SKIP_FETCH=1 bash "${SCRIPT}"; then
  echo "FAIL: expected non-phase conflict to abort" >&2
  exit 1
fi
current="$(git symbolic-ref --short HEAD)"
if [[ "${current}" != "promote/2" ]]; then
  echo "FAIL: expected restore to promote/2, on ${current}" >&2
  exit 1
fi
if git show-ref --verify --quiet refs/heads/promote/3; then
  echo "FAIL: failed promote branch should have been deleted" >&2
  exit 1
fi

# Guard: refuse to target main as the promote branch.
git checkout -q promote/2
if PROMOTE_ROOT="${REPO}" SOURCE=dev TARGET=promote/2 PROMOTE_BRANCH=main SKIP_FETCH=1 bash "${SCRIPT}"; then
  echo "FAIL: expected refuse PROMOTE_BRANCH=main" >&2
  exit 1
fi

echo "OK: first promote, second promote (modify/delete), untracked overwrite, conflict abort, branch guard"
