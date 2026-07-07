# Phase 7 PR Specs

Planned specs for **PR-700–710** (Summary, Why, Scope, Out of scope, Validation — same format as Phase 6).

| PR | Track | License | Status | Spec |
| --- | --- | --- | --- | --- |
| PR-700 | A | [oss] | done | [Extension field domain + registry](PR-700.md) |
| PR-701 | A | [oss] | done | [Field definition persistence](PR-701.md) |
| PR-702 | A | [oss] | done | [Admin field registry API](PR-702.md) |
| PR-703 | B | [oss] | done | [Extension value storage + API](PR-703.md) |
| PR-704 | B | [oss] | done | [Product Extensions admin panel](PR-704.md) |
| PR-705 | C | [oss] | done | [Cart item extension capture](PR-705.md) |
| PR-706 | C | [oss] | done | [Checkout order-item snapshot](PR-706.md) |
| PR-707 | D | [oss] | done | [Dynamic hook registry](PR-707.md) |
| PR-708 | D | [oss] | done | [Slot registry + storefront markers](PR-708.md) |
| PR-709 | E | [oss] | done | [Plugin asset manifest injection](PR-709.md) |
| PR-710 | E | [oss] | done | [GraphQL extension parity](PR-710.md) |

Roadmap: [`../ROADMAP.md`](../ROADMAP.md) · Architecture: [`../specs/CUSTOMIZATION_PLATFORM.md`](../specs/CUSTOMIZATION_PLATFORM.md) · Upstream: [Phase 6](../../phase-6-merchant-complete/ROADMAP.md)

**Suggested next PR:** Phase 7 complete (PR-700 to PR-710 done).

**Rule for Phase 7:** Extension field writes must go through the registry service — no ad hoc JSON columns in core entities.
