# Phase 6 PR Specs

Planned specs for **PR-600–651** (Summary, Why, Scope, Out of scope, Validation — same format as Phase 5).

| PR | Track | License | Status | Spec |
| --- | --- | --- | --- | --- |
| PR-600 | A | [oss] | done | [Navigation builder admin UI](PR-600.md) |
| PR-601 | A | [oss] | done | [Content blocks admin UI](PR-601.md) |
| PR-602 | A | [oss] | done | [Block placement admin UI](PR-602.md) |
| PR-610 | B | [b2b] | done | [Customer groups admin UI](PR-610.md) |
| PR-611 | B | [oss] | done | [Store credit admin UI](PR-611.md) |
| PR-620 | C | [oss] | done | [Webhook endpoints admin UI](PR-620.md) |
| PR-621 | C | [oss] | done | [Order invoice admin UI](PR-621.md) |
| PR-622 | C | [oss] | done | [Refund UX polish](PR-622.md) |
| PR-630 | D | [oss] | done | [Bulk price admin grid](PR-630.md) |
| PR-631 | D | [oss] | done | [Product category picker UX (stretch)](PR-631.md) |
| PR-640 | E | [oss] | done | [Reviews moderation UI (stretch)](PR-640.md) |
| PR-641 | E | [oss] | planned | [Abandoned cart settings UI (stretch)](PR-641.md) |
| PR-642 | E | [oss] | planned | [Promotion rule helper UI (stretch)](PR-642.md) |
| PR-650 | F | [oss] | planned | [MERCHANT.md refresh](PR-650.md) |
| PR-651 | F | [oss] | planned | [Admin empty states & nav policy](PR-651.md) |

Roadmap: [`../ROADMAP.md`](../ROADMAP.md) · Upstream: [Phase 5](../phase-5-maturity/ROADMAP.md) · Licensing: [`../../COMMERCIAL.md`](../../COMMERCIAL.md)

**Suggested next PRs:** PR-641 (abandoned cart settings UI, stretch).

**Rule for Phase 6:** Do not merge backend/API PRs that add admin nav entries without the corresponding UI PR (or hide the nav item until the UI PR lands).

[b2b] PRs implement in [`plugins/b2b/`](../../../plugins/b2b/) where they touch licensed UX; OSS wiring may live in admin.js with license gates.
