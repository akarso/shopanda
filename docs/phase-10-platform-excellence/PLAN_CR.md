# Phase 10 — Plan CR (docs-only)

Status: done (documentation)

## Summary

Tighten still-**planned** Phase 10 PR specs (PR-1002–1026) and related phase index/README text after the post-audit plan review. **Not part of PR-1001** (repo hygiene).

## Why separate

PR-1001 is mechanical hygiene (`/api` ignore, gofmt, dead migration, deploy note). Bundling requirement/security/allowlist edits into that PR violates one-PR/one-responsibility and buries planning changes under formatting noise.

## Scope (docs only)

| Area | Change |
| --- | --- |
| `prs/PR-1002.md` … `PR-1026.md` (planned) | Validation commands; security/ops constraints from plan CR (readiness gates, JWT parser, SSRF dial, metrics labels, fixed plugin allowlist, etc.) |
| `prs/README.md` | PR-383 exclusion note (lives in Phase 3); index housekeeping |
| `README.md` / `ROADMAP.md` | Code+docs rule; PR-1026 docs-only exception; short-description sync for tightened planned PRs |

## Out Of Scope

- PR-1000 / PR-1001 implementation artifacts
- Implementing any planned PR (1002+)
- Code changes

## Validation

- Docs-only — N/A for `go test` / `go build`
- Spot-check: planned specs still `Status: planned`; PR-1000/1001 remain `done` with implementation validation intact

## Review guidance

Review these files as **requirements tightening for future work**, not as completed engineering. Prefer merging (or reverting) independently of the PR-1001 hygiene diff.
