# Phase 10 — Platform Excellence

Get Shopanda from **B− / B** (audit 2026-08) to **A / A+** on architecture, code quality, security, and infra — without a rewrite.

## Why this phase

Phase 9 finished merchant discovery. The product surface is broad; the **process and ops safety net** is not. The 2026-08 tech audit found:

- Hexagonal architecture and domain model are real and salvageable
- Test suite quality is high (~80% behavioral) but **broken on `dev`** with **zero CI**
- Security primitives are sound; **defaults are production-unsafe**
- Docker/docs are decent; **no pipeline, lying readiness conflation, binary in git**

This phase hardens what already exists. It does **not** add merchant features.

## Code + documentation rule

Per `AGENTS.md` / the PR documentation checklist, finished PRs ship **code and docs** together.

- Phase 10 executable work starts at **PR-1000** (test suite unbreak — done) and continues through PR-1025.
- **Approved exception:** [PR-1026](prs/PR-1026.md) is docs-only (extension-point guide + runbook) after the code tracks have landed. It has no production code surface; validation is link/comprehension review, not `go test`.
- **Plan CR (docs-only):** tightenings to still-planned PR-1002–1026 specs live in [`PLAN_CR.md`](PLAN_CR.md) and must not be bundled into [PR-1001](prs/PR-1001.md) hygiene.

## Grade gates

| Grade | Meaning | Exit when |
| --- | --- | --- |
| **A** | Trustworthy to ship and evolve | Tracks **A–D** done |
| **A+** | Production-grade platform | Track **E** done on top of A |

See [`ROADMAP.md`](ROADMAP.md) for track tables, sequencing, and PR specs under [`prs/`](prs/).

## Relationship

| Phase | Focus | Status |
| --- | --- | --- |
| Phase 9 | Integrator backlog + merchant discovery | Shipped (PR-856–908) |
| **Phase 10** | Platform excellence (quality / security / ops / architecture) | **In progress (PR-1000–1002 done; PR-1003 in progress; Tracks B–D done; next PR-1020 Prometheus metrics)** |
