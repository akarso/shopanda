# Phase 8 PR Specs

Specs for Phase 8 PRs (Summary, Why, Scope, Out of scope, Validation — same format as Phase 6/7).

| PR | Track | License | Status | Spec |
| --- | --- | --- | --- | --- |
| PR-800 | A | [oss] | done | [Integrator platform spec publish](PR-800.md) |
| PR-801 | A | [oss] | done | [Port catalog + introspection](PR-801.md) |
| PR-802 | A | [oss] | done | [Precedence policy + composition guide](PR-802.md) |
| PR-810 | B | [oss] | done | [Pricing step positioning](PR-810.md) |
| PR-811 | B | [oss] | done | [Cart lifecycle hooks](PR-811.md) |
| PR-812 | B | [oss] | done | [Cart validate chain](PR-812.md) |
| PR-813 | B | [oss] | done | [Tax calculator port](PR-813.md) |
| PR-814 | B | [oss] | done | [Reference plugin: cart rule](PR-814.md) |
| PR-820 | C | [oss] | done | [Import row hook registry](PR-820.md) |
| PR-821 | C | [oss] | done | [Wire core importers](PR-821.md) |
| PR-822 | C | [oss] | planned | Import context + errors |
| PR-823 | C | [oss] | planned | Reference plugin: CSV remap |
| PR-830 | D | [oss] | planned | Integration route conventions |
| PR-831 | D | [oss] | planned | Integration auth middleware |
| PR-832 | D | [oss] | planned | Idempotency store |
| PR-833 | D | [oss] | planned | Reference plugin: order status inbound |
| PR-840 | E | [oss] | planned | Sync job registration |
| PR-841 | E | [oss] | planned | Integration client bootstrap |
| PR-842 | E | [oss] | planned | Reference plugin: warehouse stock |
| PR-843 | E | [oss] | planned | Reference plugin: PIM GraphQL PDP |
| PR-850 | F | [oss] | planned | Plugin SDK package |
| PR-851 | F | [oss] | planned | Registration report |
| PR-852 | F | [oss] | planned | Replace-by-name steps |
| PR-853 | F | [oss] | planned | Reference port replacement |

Roadmap: [`../ROADMAP.md`](../ROADMAP.md) · Architecture: [`../specs/INTEGRATOR_PLATFORM.md`](../specs/INTEGRATOR_PLATFORM.md) · Upstream: [Phase 7](../../phase-7-customization-platform/ROADMAP.md)

**Rule for Phase 8:** Integrators extend via registered seams only — no core service overrides, no override folders, no ad hoc importer forks.

**Phase 8 Track A complete** (PR-800–802). **Track B complete** (PR-810–814).

Individual PR spec files (`PR-812.md`, …) are added when implementation starts.
