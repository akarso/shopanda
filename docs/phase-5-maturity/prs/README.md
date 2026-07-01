# Phase 5 PR Specs

Planned specs for **PR-500–549**. Individual PR markdown files will be added when implementation starts (same format as Phase 4: Summary, Why, Scope, Out of scope, Validation).

| PR | Track | License | Status | Spec |
| --- | --- | --- | --- | --- |
| PR-500 | A | [b2b] | done | [Customer groups domain](PR-500.md) |
| PR-501 | A | [b2b] | done | [Group-aware pricing](PR-501.md) |
| PR-502 | A | [oss] | done | [Returns domain + workflow](PR-502.md) |
| PR-503 | A | [oss] | done | [Returns admin + account UI](PR-503.md) |
| PR-504 | A | [oss] | done | [Payment transaction ledger admin](PR-504.md) |
| PR-505 | A | [oss] | done | [Store credit (stretch)](PR-505.md) |
| PR-510 | B | [oss] | done | [Advanced promotion rules](PR-510.md) |
| PR-511 | B | [oss] | done | [Navigation builder](PR-511.md) |
| PR-512 | B | [oss] | done | [Content blocks](PR-512.md) |
| PR-513 | B | [oss] | done | [Abandoned cart recovery (stretch)](PR-513.md) |
| PR-514 | B | [oss] | done | [Product reviews (stretch)](PR-514.md) |
| PR-520 | C | [oss] | done | [Admin user CRUD](PR-520.md) |
| PR-521 | C | [oss] | done | [Custom roles editor](PR-521.md) |
| PR-522 | C | [oss] | done | [Admin TOTP / MFA](PR-522.md) |
| PR-523 | C | [oss] | done | [Audit export + retention](PR-523.md) |
| PR-524 | C | [oss] | done | [Shipping zones admin UI](PR-524.md) |
| PR-530 | D | [oss] | done | [Omnibus storefront verification](PR-530.md) |
| PR-535 | D | [oss] | done | [Omnibus listing batch reads](PR-535.md) |
| PR-531 | D | [oss] | done | [WEEE product fields](PR-531.md) |
| PR-532 | D | [oss] | done | [EPR / packaging data](PR-532.md) |
| PR-533 | D | [oss] | done | [GPSR product safety](PR-533.md) |
| PR-534 | D | [oss] | done | [OSS / IOSS tax export](PR-534.md) |
| PR-540 | E | [oss] | done | [Merchant outbound webhooks](PR-540.md) |
| PR-541 | E | [oss] | done | [Plugin CLI registration](PR-541.md) |
| PR-542 | E | [oss] | done | [Kafka / SQS queue plugins](PR-542.md) |
| PR-543 | E | [oss] | done | [GraphQL API plugin (stretch)](PR-543.md) |
| PR-544 | E | [oss] | planned | Dynamic plugin loading research |

Roadmap: [`../ROADMAP.md`](../ROADMAP.md) · EU compliance: [`../specs/COMPLIANCE_EU.md`](../specs/COMPLIANCE_EU.md) · Licensing: [`../../COMMERCIAL.md`](../../COMMERCIAL.md)

**Suggested next PRs:** PR-544 (dynamic plugin loading research). Phase 6 merchant UI starts at [PR-600](../phase-6-merchant-complete/prs/PR-600.md).

[b2b] PRs implement in [`plugins/b2b/`](../../../plugins/b2b/). [oss] PRs stay in open core.
