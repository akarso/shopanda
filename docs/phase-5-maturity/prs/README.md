# Phase 5 PR Specs

Planned specs for **PR-500–549**. Individual PR markdown files will be added when implementation starts (same format as Phase 4: Summary, Why, Scope, Out of scope, Validation).

| PR | Track | License | Status | Spec |
| --- | --- | --- | --- | --- |
| PR-500 | A | [b2b] | done | [Customer groups domain](PR-500.md) |
| PR-501 | A | [b2b] | done | [Group-aware pricing](PR-501.md) |
| PR-502 | A | [oss] | done | [Returns domain + workflow](PR-502.md) |
| PR-503 | A | [oss] | done | [Returns admin + account UI](PR-503.md) |
| PR-504 | A | [oss] | done | [Payment transaction ledger admin](PR-504.md) |
| PR-505 | A | [oss] | planned | Store credit / gift cards (stretch) |
| PR-510 | B | [oss] | planned | Advanced promotion rules |
| PR-511 | B | [oss] | planned | Navigation builder |
| PR-512 | B | [oss] | planned | Content blocks |
| PR-513 | B | [oss] | planned | Abandoned cart recovery (stretch) |
| PR-514 | B | [oss] | planned | Product reviews (stretch) |
| PR-520 | C | [oss] | planned | Admin user CRUD |
| PR-521 | C | [oss] | planned | Custom roles editor |
| PR-522 | C | [oss] | planned | Admin TOTP / MFA |
| PR-523 | C | [oss] | planned | Audit export + retention |
| PR-524 | C | [oss] | planned | Shipping zones admin UI |
| PR-530 | D | [oss] | done | [Omnibus storefront verification](PR-530.md) |
| PR-535 | D | [oss] | done | [Omnibus listing batch reads](PR-535.md) |
| PR-531 | D | [oss] | done | [WEEE product fields](PR-531.md) |
| PR-532 | D | [oss] | done | [EPR / packaging data](PR-532.md) |
| PR-533 | D | [oss] | done | [GPSR product safety](PR-533.md) |
| PR-534 | D | [oss] | done | [OSS / IOSS tax export](PR-534.md) |
| PR-540 | E | [oss] | planned | Merchant outbound webhooks |
| PR-541 | E | [oss] | planned | Plugin CLI registration |
| PR-542 | E | [oss] | planned | Kafka / SQS queue plugins |
| PR-543 | E | [oss] | planned | GraphQL API plugin (stretch) |
| PR-544 | E | [oss] | planned | Dynamic plugin loading research |

Roadmap: [`../ROADMAP.md`](../ROADMAP.md) · EU compliance: [`../specs/COMPLIANCE_EU.md`](../specs/COMPLIANCE_EU.md) · Licensing: [`../../COMMERCIAL.md`](../../COMMERCIAL.md)

**Suggested next PRs:** PR-505 [oss] (store credit stretch) or Track B/C items.

[b2b] PRs implement in [`plugins/b2b/`](../../../plugins/b2b/). [oss] PRs stay in open core.
