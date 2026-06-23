# Commercial Licensing

Shopanda uses an **open core** model:

- **Open source (GPL v3)** — the full B2C commerce engine in this repository (`internal/`, `plugins/core/`, `plugins/example/`, storefront, admin, and compliance baseline).
- **Commercial B2B module** — optional paid extension in [`plugins/b2b/`](../plugins/b2b/) for wholesale and business-buyer workflows.

This document describes the product split and how licensing is intended to work. **It is not legal advice.** Final terms will be published separately before paid sales.

---

## Editions

| Edition | Audience | License | Source |
| --- | --- | --- | --- |
| **Shopanda (open core)** | D2C merchants, developers, agencies | [GPL v3](../LICENSE) | This repository (excluding `plugins/b2b/`) |
| **Shopanda B2B** | B2B merchants, multi-buyer companies | Commercial (see [`plugins/b2b/LICENSE`](../plugins/b2b/LICENSE)) | `plugins/b2b/` in-repo today; may move to a private module later |

Both editions ship from the **same binary** when B2B is enabled and licensed. There is no separate fork.

---

## What stays open (always)

These capabilities are part of the free, self-hosted product:

- Catalog, cart, checkout (guest + account), orders, inventory, promotions (simple), coupons
- SSR storefront, admin SPA, REST API, CSV import/export
- Core plugins (Meilisearch, Redis, RabbitMQ, Stripe, S3) and external plugin example
- GDPR consent, data export/delete, price history, EU compliance baseline (Omnibus, WEEE, EPR, GPSR — Phase 5 OSS track)
- Returns / RMA, payment ledger admin, navigation builder, content blocks (Phase 5 OSS PRs)
- Multi-admin users, audit export, shipping zones UI (Phase 5 OSS PRs)

**Principle:** If a solo direct-to-consumer shop needs it, it stays open.

---

## What requires a B2B license

The commercial module adds business-buyer workflows. Planned features live in `plugins/b2b/` and are tagged `[b2b]` in the [Phase 5 roadmap](phase-5-maturity/ROADMAP.md):

| Feature | Phase 5 PR | Notes |
| --- | --- | --- |
| Customer groups | PR-500 | Segmentation foundation |
| Group-aware pricing | PR-501 | Contract / tier prices |
| Quotes / RFQ → order | Backlog | Converts negotiated carts |
| Purchase orders & net terms | Backlog | PO number, payment on account |
| Multi-buyer company accounts | Backlog | Buyer, approver, finance roles |
| Order approval workflows | Backlog | Cart/order gates before submit |
| B2B catalog visibility | Backlog | Hide SKUs, MOQ, login-gated prices |
| ERP / accounting connectors | Backlog | Premium integrations |

Advanced promotion rules (PR-510) ship in OSS; **conditions that depend on customer groups** require the B2B module once groups exist.

---

## How licensing works (technical)

1. Build or run the standard Shopanda binary.
2. Enable the B2B plugin in config:

   ```yaml
   plugins:
     b2b:
       enabled: true
       license_key: "YOUR-KEY"
   ```

   Or via environment:

   ```env
   SHOPANDA_PLUGINS_B2B_ENABLED=true
   SHOPANDA_PLUGINS_B2B_LICENSE_KEY=YOUR-KEY
   ```

3. On startup, `plugins/b2b` validates the license key during `Init`.
4. Invalid or missing key → B2B plugin fails init; **open core continues to run**.
5. Valid key → B2B routes, permissions, and pipeline steps register (as they are implemented).

### Development keys (stub)

Until online license validation ships, local development accepts keys prefixed with `DEV-` (e.g. `DEV-local`). Production keys will be issued on purchase and validated against a license service in a follow-up PR.

**Do not ship `DEV-` keys to production.**

---

## Architecture rules

To keep editions maintainable:

| Rule | Rationale |
| --- | --- |
| B2B code lives only under `plugins/b2b/` | Clear boundary for licensing |
| Core exposes **ports** (interfaces); B2B implements adapters | No `import "plugins/b2b"` from `internal/domain` |
| No `#ifdef` / scattered `if b2b` in core | Prevents accidental GPL contamination of commercial logic |
| OSS PRs must not require B2B to compile or test | CI runs without a license key |
| B2B PRs may depend on OSS ports added in prior PRs | One-way dependency |

---

## Pricing (product intent)

Target model: **small monthly fee per self-hosted deployment** with the B2B module. Exact pricing, trials, and support tiers will be announced before general availability.

Contact for early access and commercial terms: *(add your sales/contact address before launch)*.

---

## Contributing

Contributions to the open core (GPL) are welcome under the project's contribution guidelines.

The B2B module may accept contributions under a separate contributor agreement once commercial terms are finalized. Until then, treat `plugins/b2b/` as source-available scaffolding.

---

## FAQ

**Can I use the open core for free in production?**  
Yes, under GPL v3. You must comply with GPL obligations (source offer, etc.).

**Can I fork and remove B2B?**  
Yes. B2B is optional at runtime.

**Can I run B2B without paying?**  
No. Enabling `plugins.b2b` without a valid license is not permitted once commercial terms are in effect. The stub accepts `DEV-*` keys for development only.

**Will B2B move to a private repository?**  
Possibly, after design partners and legal review. The `plugin.Plugin` contract and ports in core will remain stable either way.

**Is hosted SaaS planned?**  
Not defined in this document. A future hosted offering may bundle license + operations separately from self-hosted.

---

## Related docs

- [Phase 5 Roadmap — OSS vs B2B tags](phase-5-maturity/ROADMAP.md)
- [B2B plugin README](../plugins/b2b/README.md)
- [Plugin authoring (three tiers)](guides/DEVELOPER.md#three-tier-extension-model)
- [EU compliance (OSS track)](phase-5-maturity/specs/COMPLIANCE_EU.md)
