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
| PR-822 | C | [oss] | done | [Import context + errors](PR-822.md) |
| PR-823 | C | [oss] | done | [Reference plugin: CSV remap](PR-823.md) |
| PR-830 | D | [oss] | done | [Integration route conventions](PR-830.md) |
| PR-831 | D | [oss] | done | [Integration auth middleware](PR-831.md) |
| PR-832 | D | [oss] | done | [Idempotency store](PR-832.md) |
| PR-833 | D | [oss] | done | [Reference plugin: order status inbound](PR-833.md) |
| PR-840 | E | [oss] | done | [Sync job registration](PR-840.md) |
| PR-841 | E | [oss] | done | [Integration client bootstrap](PR-841.md) |
| PR-842 | E | [oss] | done | [Reference plugin: warehouse stock](PR-842.md) |
| PR-843 | E | [oss] | planned | Reference plugin: PIM GraphQL PDP |
| PR-850 | F | [oss] | planned | Plugin SDK package |
| PR-851 | F | [oss] | planned | Registration report |
| PR-852 | F | [oss] | planned | Replace-by-name steps |
| PR-853 | F | [oss] | planned | Reference port replacement |

Roadmap: [`../ROADMAP.md`](../ROADMAP.md) · Architecture: [`../specs/INTEGRATOR_PLATFORM.md`](../specs/INTEGRATOR_PLATFORM.md) · Upstream: [Phase 7](../../phase-7-customization-platform/ROADMAP.md)

**Rule for Phase 8:** Integrators extend via registered seams only — no core service overrides, no override folders, no ad hoc importer forks.

**Phase 8 Track A complete** (PR-800–802). **Track B complete** (PR-810–814). **Track C complete** (PR-820–823). **Track D complete** (PR-830–833).

Individual PR spec files (`PR-812.md`, …) are added when implementation starts.
