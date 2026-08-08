# Phase 10 PR Specs

| PR | Track | Status | Spec |
| --- | --- | --- | --- |
| PR-1000 | A | done | [Unbreak the test suite](PR-1000.md) |
| PR-1001 | A | done | [Repo hygiene](PR-1001.md) |
| PR-1002 | A | done | [GitHub Actions CI (unit)](PR-1002.md) |
| PR-1003 | A | in progress | [CI integration job](PR-1003.md) |
| PR-1004 | B | done | [Login abuse protection](PR-1004.md) |
| PR-1005 | B | done | [JWT secret strength](PR-1005.md) |
| PR-1006 | B | done | [HTTP boundary hardening](PR-1006.md) |
| PR-1007 | B | done | [Webhook SSRF guard](PR-1007.md) |
| PR-1008 | B | planned | [Secure-by-default config](PR-1008.md) |
| PR-1009 | C | planned | [Readiness probe](PR-1009.md) |
| PR-1010 | C | planned | [Release to GHCR](PR-1010.md) |
| PR-1011 | C | planned | [Migration hygiene CI](PR-1011.md) |
| PR-1012 | C | planned | [Supply-chain basics](PR-1012.md) |
| PR-1013 | D | planned | [Split serve wiring](PR-1013.md) |
| PR-1014 | D | planned | [Collapse import/export CLI](PR-1014.md) |
| PR-1015 | D | planned | [Typed plugin providers](PR-1015.md) |
| PR-1016 | D | planned | [RBAC registry injection](PR-1016.md) |
| PR-1017 | D | planned | [Plugin boundary honesty](PR-1017.md) |
| PR-1018 | D | planned | [Checkout context](PR-1018.md) |
| PR-1019 | D | planned | [Event bus drain](PR-1019.md) |
| PR-1020 | E | planned | [Prometheus metrics](PR-1020.md) |
| PR-1021 | E | planned | [HTTP package split (shared)](PR-1021.md) |
| PR-1022 | E | planned | [HTTP package split (admin)](PR-1022.md) |
| PR-1023 | E | planned | [HTTP package split (storefront)](PR-1023.md) |
| PR-1024 | E | planned | [OpenTelemetry traces](PR-1024.md) |
| PR-1025 | E | planned | [pgx driver migration](PR-1025.md) |
| PR-1026 | E | planned | [Extension guide + runbook](PR-1026.md) |

Roadmap: [`../ROADMAP.md`](../ROADMAP.md)

**Hard rule:** Track A (PR-1000–1003) before D/E. Track B may overlap late A once tests compile.

Planned-spec tightenings (PR-1002–1026 requirements text) are tracked separately in [`../PLAN_CR.md`](../PLAN_CR.md) — **not** part of [PR-1001](PR-1001.md) hygiene.

### Intentionally excluded from this index

| PR | Reason |
| --- | --- |
| [PR-383](../../phase-3-testing/prs/PR-383.md) | Phase 3 (testing) work — de-duplicate local ordered category insertion. Not a Phase 10 deliverable; kept in [`docs/phase-3-testing/prs/`](../../phase-3-testing/prs/). |
