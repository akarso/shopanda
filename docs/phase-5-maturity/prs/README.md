# Phase 5 PR Specs

Planned specs for **PR-500–549**. Individual PR markdown files will be added when implementation starts (same format as Phase 4: Summary, Why, Scope, Out of scope, Validation).

| PR | Track | License | Status | Spec |
| --- | --- | --- | --- | --- |
| PR-500 | A | [b2b] | planned | Customer groups domain |
| PR-501 | A | [b2b] | planned | Group-aware pricing |
| PR-502 | A | [oss] | done | [Returns domain + workflow](PR-502.md) |
| PR-503 | A | [oss] | planned | Returns admin + account UI |
| PR-504 | A | [oss] | planned | Payment transaction ledger admin |
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
| PR-531 | D | [oss] | planned | WEEE product fields |
| PR-532 | D | [oss] | planned | EPR / packaging data |
| PR-533 | D | [oss] | planned | GPSR product safety |
| PR-534 | D | [oss] | planned | OSS / e-invoicing (stretch) |
| PR-540 | E | [oss] | planned | Merchant outbound webhooks |
| PR-541 | E | [oss] | planned | Plugin CLI registration |
| PR-542 | E | [oss] | planned | Kafka / SQS queue plugins |
| PR-543 | E | [oss] | planned | GraphQL API plugin (stretch) |
| PR-544 | E | [oss] | planned | Dynamic plugin loading research |

Roadmap: [`../ROADMAP.md`](../ROADMAP.md) · EU compliance: [`../specs/COMPLIANCE_EU.md`](../specs/COMPLIANCE_EU.md) · Licensing: [`../../COMMERCIAL.md`](../../COMMERCIAL.md)

**Suggested next PRs:** PR-503 [oss] (returns admin + account UI) and PR-500 [b2b] (customer groups) in parallel.

[b2b] PRs implement in [`plugins/b2b/`](../../../plugins/b2b/). [oss] PRs stay in open core.
