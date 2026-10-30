# PR-DOCS-SPLIT — Product docs vs working history

Status: done

## Summary

Keep product docs (guides, current C4, runbook) on `main`. Keep phase folders, PR specs, and in-progress roadmaps on `dev` only. Git will not do this by itself — promotion strips `docs/phase-*`, and CI fails if those paths land on `main`.

The promote script must replay. After the first strip, `main` has deletes and `dev` keeps editing the same files. The next merge is modify/delete on almost every phase path. The script continues past that, force-removes every `docs/phase-*` index/unmerged path, and refuses to commit if anything matching the prefix remains.

## Why

`docs/phase-*` is hundreds of PR writeups and historical specs. A clone of `main` should be operable without that archaeology. A raw `dev` → `main` merge re-adds the folders every time.

## Scope

- Lift living artifacts out of phase folders: runtime modes, EU compliance, integrator spec, govuln baseline
- Rewrite product-doc links so they do not depend on `docs/phase-*` (prose `dev` paths, not GitHub `blob/dev` URLs)
- Add `docs/README.md` describing the split, including that old `main` bookmarks under `docs/phase-*` 404 by design
- Add `scripts/promote-dev-to-main.sh` (merge `--no-commit`, require `MERGE_HEAD` or ancestor, force-remove `docs/phase-*`, abort/restore on non-phase conflicts)
- Add `scripts/promote-dev-to-main_test.sh` (first/second promote, two-parent merge, untracked overwrite abort, conflict abort, branch guard)
- Add CI jobs `docs-policy` (always) and `docs-main` (`main` / PRs to `main` / merge queue to `main`)
- Rename lifted stubs to `MOVED.md` so they cannot be mistaken for the living file
- Shrink the DEVELOPER integrator section to a pointer at `INTEGRATOR_PLATFORM.md`
- Update `AGENTS.md` and `.cursor/rules/pr-documentation-checklist.mdc` to lead with the `main` hotfix path

## Out of scope

- Wiki or a second docs repo
- Deleting working history from `dev`
- Changing C4 architecture (diagrams stay; inbound phase-roadmap links rewritten)
- Pushing or merging `promote/docs-split` — that branch is a full `dev` dump and must be recreated with this script after this lands on `dev`
- Requiring `docs-main` in GitHub branch protection from this PR (an admin must tick the box on `main` only; the workflow comment and `DEVELOPER.md` say so)

## Validation

```bash
bash -n scripts/promote-dev-to-main.sh
bash scripts/promote-dev-to-main_test.sh
bash .github/scripts/check-product-docs-links.sh
test -f docs/GOVULN_BASELINE.md
go test ./internal/domain/scheduler/ ./internal/platform/plugin/ ./internal/platform/config/ ./cmd/api/ -count=1
```

- Fixture covers: first promote, second promote after a phase-file edit (modify/delete), two-parent merge commits, a tracked `docs/phase-notes.md` file (not only directories), untracked overwrite (merge never starts → non-zero, restore, no leftover promote branch), ancestor no-op does not create a branch, refuse when SOURCE is already in TARGET and `docs/phase-*` is still tracked (not a raw-merge recovery tool), `PROMOTE_FORCE` abort restores the old tip (including when already on that branch), non-phase conflict abort + restore, refuse `PROMOTE_BRANCH=main`.
- Fetch updates `origin/dev` and `origin/main` only; SOURCE is resolved locally afterward.
- After `git merge ... || true`, the script requires `MERGE_HEAD`; otherwise it dies. Ancestor no-op is decided before `checkout -b`.
- `docs-main` uses `leftover="$(git ls-files -- 'docs/phase-*')"` with no `|| true` (fail-closed).
- Product surfaces contain no relative `docs/phase-*` hrefs and no hardcoded `blob/dev` / `tree/dev` working-history URLs (`docs/GOVULN_BASELINE.md` is in the scan set).
- Comment-only Go changes in scheduler, plugin registry, config, and `wire_routes.go`.

## Documentation updates

| Artifact | Change |
| --- | --- |
| `docs/README.md` | split policy; 404-by-design for old `docs/phase-*` bookmarks |
| `docs/guides/RUNTIME_MODES.md` | moved from phase-4 specs |
| `docs/guides/COMPLIANCE_EU.md` | moved from phase-5 specs |
| `docs/guides/INTEGRATOR_PLATFORM.md` | moved from phase-8 specs; product lead |
| `docs/GOVULN_BASELINE.md` | moved from phase-10 |
| `docs/phase-*/MOVED.md` | stubs; living files are not duplicated under the old names |
| Product docs listed above | links rewritten to in-tree guides or prose `dev` paths |
| C4 diagrams | no architecture change — container diagram now points at `docs/guides/RUNTIME_MODES.md` |
| `AGENTS.md` / PR checklist | lead with target branch; `main` hotfixes must not recreate `docs/phase-*` |
| `docs/guides/DEVELOPER.md` | integrator section is a pointer; CI table includes `docs-policy` / `docs-main` |
| `docs/phase-10-platform-excellence/ROADMAP.md` | PR-DOCS-SPLIT process row |
