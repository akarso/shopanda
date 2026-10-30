# PR-DOCS-SPLIT — Product docs vs working history

Status: done

## Summary

Keep product docs (guides, current C4, runbook) on `main`. Keep phase folders, PR specs, and in-progress roadmaps on `dev` only. Git will not do this by itself — promotion strips `docs/phase-*`, and CI fails if those paths land on `main`.

## Why

`docs/phase-*` is hundreds of PR writeups and historical specs. A clone of `main` should be operable without that archaeology. A raw `dev` → `main` merge re-adds the folders every time.

## Scope

- Lift living artifacts out of phase folders: runtime modes, EU compliance, integrator spec, govuln baseline
- Rewrite product-doc links so they do not depend on `docs/phase-*`
- Add `docs/README.md` describing the split
- Add `scripts/promote-dev-to-main.sh` (merge then strip)
- Add CI job `docs-main` (PRs/pushes to `main` only)
- Update `AGENTS.md` and `.cursor/rules/pr-documentation-checklist.mdc`

## Out of scope

- Wiki or a second docs repo
- Deleting working history from `dev`
- Changing C4 architecture (diagrams stay; inbound phase-roadmap links rewritten)

## Validation

- `bash -n scripts/promote-dev-to-main.sh`
- `bash .github/scripts/run-govulncheck.sh` path now reads `docs/GOVULN_BASELINE.md` (file exists)
- Grep of product docs (`README.md`, `PLUGINS.md`, `RUNBOOK.md`, `docs/guides/`, `docs/diagrams/`, `docs/COMMERCIAL.md`) for in-repo `docs/phase-*` links — remaining hits are GitHub `blob/dev/...` working-note URLs
- `go test ./internal/domain/scheduler/ ./internal/platform/plugin/ -count=1` — comment-only Go changes

## Documentation updates

| Artifact | Change |
| --- | --- |
| `docs/README.md` | new — split policy |
| `docs/guides/RUNTIME_MODES.md` | moved from phase-4 specs |
| `docs/guides/COMPLIANCE_EU.md` | moved from phase-5 specs |
| `docs/guides/INTEGRATOR_PLATFORM.md` | moved from phase-8 specs |
| `docs/GOVULN_BASELINE.md` | moved from phase-10 |
| Product docs listed above | links rewritten |
| C4 diagrams | no architecture change — container diagram now points at `docs/guides/RUNTIME_MODES.md` |
| `AGENTS.md` / PR checklist | `dev` vs `main` rules |
