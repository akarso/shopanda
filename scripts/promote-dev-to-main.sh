#!/usr/bin/env bash
# Promote SOURCE onto TARGET without bringing docs/phase-* (working history).
#
# Merge conflicts under docs/phase-* are expected on every promote after the
# first (modify/delete). They must not stop the promote. Conflicts outside
# that prefix abort and restore the previous branch.
#
# A merge that never starts (untracked overwrite, unrelated histories) is not
# a successful promote. After `git merge ... || true`, MERGE_HEAD must exist
# unless SOURCE is already an ancestor of HEAD.
#
# Usage:
#   bash scripts/promote-dev-to-main.sh
#   SOURCE=dev TARGET=main SKIP_FETCH=1 PROMOTE_BRANCH=promote/t bash scripts/promote-dev-to-main.sh
#
# Does not push. Never raw-merge dev into main.
set -euo pipefail

ROOT="${PROMOTE_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
cd "${ROOT}"

SOURCE="${SOURCE:-origin/dev}"
TARGET="${TARGET:-origin/main}"
STAMP="$(date -u +%Y-%m-%d)"
BRANCH="${PROMOTE_BRANCH:-promote/${STAMP}}"

ORIG_REF="$(git symbolic-ref --short HEAD 2>/dev/null || git rev-parse HEAD)"
CREATED_BRANCH=0
WIPED_PREV=""
FINISHED=0

usage() {
  cat <<EOF
Promote SOURCE onto TARGET, then force-remove every docs/phase-* path
(including unmerged entries) so working history does not land on main.

Environment:
  SOURCE          git ref to merge (default: origin/dev)
  TARGET          git ref to branch from (default: origin/main)
  PROMOTE_BRANCH  local branch name (default: promote/YYYY-MM-DD)
  PROMOTE_FORCE   set to 1 to reset an existing promote branch
  SKIP_FETCH      set to 1 to skip git fetch (offline / fixture)
  PROMOTE_ROOT    repository root (default: parent of scripts/)

Does not push. Review the branch, then push and open a PR targeting main.
EOF
}

die() {
  echo "ERROR: $*" >&2
  exit 1
}

restore() {
  git merge --abort >/dev/null 2>&1 || true
  if ! git checkout "${ORIG_REF}"; then
    echo "ERROR: could not checkout ${ORIG_REF}; not deleting ${BRANCH}." >&2
    return 1
  fi
  if [[ "${CREATED_BRANCH}" -eq 1 ]]; then
    git branch -D "${BRANCH}"
  elif [[ -n "${WIPED_PREV}" ]]; then
    if [[ "$(git rev-parse --abbrev-ref HEAD)" == "${BRANCH}" ]]; then
      git reset --hard "${WIPED_PREV}"
    else
      git branch -f "${BRANCH}" "${WIPED_PREV}"
    fi
  fi
}

on_exit() {
  local ec=$?
  if [[ "${FINISHED}" -eq 1 ]]; then
    return 0
  fi
  if restore; then
    echo "ERROR: promote failed; restored ${ORIG_REF}" >&2
  else
    echo "ERROR: promote failed; restore incomplete" >&2
  fi
  exit "${ec}"
}

strip_phase_paths() {
  local tmp path unmerged
  tmp="$(mktemp)"
  git ls-files -z -- 'docs/phase-*' >"${tmp}"
  if [[ -s "${tmp}" ]]; then
    while IFS= read -r -d '' path || [[ -n "${path}" ]]; do
      git rm -f --ignore-unmatch -- "${path}" >/dev/null
    done <"${tmp}"
  fi
  rm -f "${tmp}"
  unmerged="$(git diff --name-only --diff-filter=U)"
  if [[ -n "${unmerged}" ]]; then
    while IFS= read -r path; do
      [[ -z "${path}" ]] && continue
      case "${path}" in
        docs/phase-*) git rm -f --ignore-unmatch -- "${path}" >/dev/null ;;
      esac
    done <<<"${unmerged}"
  fi
}

assert_no_phase_paths() {
  local leftover
  leftover="$(git ls-files -- 'docs/phase-*')"
  if [[ -n "${leftover}" ]]; then
    printf '%s\n' "${leftover}" >&2
    die "docs/phase-* still in the index"
  fi
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

case "${BRANCH}" in
  main|master|dev|HEAD|origin/*)
    die "PROMOTE_BRANCH cannot be ${BRANCH}"
    ;;
esac

if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
  die "tracked files have uncommitted changes. Commit or stash first."
fi

trap on_exit EXIT

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
  die "unknown ref ${ref}"
}

if [[ "${SKIP_FETCH:-}" != "1" ]]; then
  if git remote get-url origin >/dev/null 2>&1; then
    src_fetch="${SOURCE#origin/}"
    tgt_fetch="${TARGET#origin/}"
    git fetch origin "${src_fetch}" "${tgt_fetch}"
  fi
fi

SOURCE="$(resolve_ref "${SOURCE}")"
TARGET="$(resolve_ref "${TARGET}")"

if git show-ref --verify --quiet "refs/heads/${BRANCH}"; then
  if [[ "${PROMOTE_FORCE:-}" != "1" ]]; then
    die "branch ${BRANCH} already exists. Pick another PROMOTE_BRANCH or set PROMOTE_FORCE=1"
  fi
  WIPED_PREV="$(git rev-parse "refs/heads/${BRANCH}")"
  git checkout -B "${BRANCH}" "${TARGET}"
else
  git checkout -b "${BRANCH}" "${TARGET}"
  CREATED_BRANCH=1
fi

# Merge may fail on docs/phase-* modify/delete. Continue; strip those paths.
# A merge that never starts is not success — see MERGE_HEAD / ancestor check.
git merge --no-ff --no-commit "${SOURCE}" || true

if [[ ! -f "$(git rev-parse --git-path MERGE_HEAD)" ]]; then
  if git merge-base --is-ancestor "${SOURCE}" HEAD; then
    leftover="$(git ls-files -- 'docs/phase-*')"
    if [[ -n "${leftover}" ]]; then
      printf '%s\n' "${leftover}" >&2
      die "SOURCE already contained in ${BRANCH} but docs/phase-* is still tracked"
    fi
    echo "SOURCE already contained in ${BRANCH}; nothing to promote."
    FINISHED=1
    trap - EXIT
    echo "Promote branch: ${BRANCH} (SOURCE=${SOURCE} TARGET=${TARGET})"
    echo "Review, then push and open a PR targeting main. Do not raw-merge dev into main."
    exit 0
  fi
  die "merge did not start (untracked overwrite, unrelated histories, or similar). Not promoting."
fi

strip_phase_paths
assert_no_phase_paths

unmerged="$(git diff --name-only --diff-filter=U)"
if [[ -n "${unmerged}" ]]; then
  printf '%s\n' "${unmerged}" >&2
  die "unresolved conflicts remain outside docs/phase-*"
fi

msg="$(cat <<EOF
Promote ${SOURCE} onto ${TARGET} without working-history docs.

Phase folders, PR specs, and in-progress roadmaps stay on dev.
See docs/README.md.
EOF
)"

git commit -m "${msg}"
assert_no_phase_paths

FINISHED=1
trap - EXIT

echo "Promote branch: ${BRANCH} (SOURCE=${SOURCE} TARGET=${TARGET})"
echo "Review, then push and open a PR targeting main. Do not raw-merge dev into main."
