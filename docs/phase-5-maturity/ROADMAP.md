# Phase 5 — Mature Commerce

## Strategy

* Build on the **product-complete** baseline delivered in Phases 1–4 (catalog, checkout, admin, three-tier plugins, runtime modes)
* Close **operational gaps** merchants expect from a mature self-hosted platform (returns, segments, ledger visibility)
* Add **EU and compliance depth** without turning core into enterprise bloat — compliance features stay explicit, testable, and mostly opt-in per store/market
* Prefer **vertical slices** (domain → application → admin/storefront → tests) over speculative abstractions
* One PR = one responsibility; each PR must be runnable and reviewable in ~10–20 minutes
* PR specs will live under `prs/` (planned range **PR-500–549**)

Each PR is tagged **`[oss]`** (GPL open core) or **`[b2b]`** (commercial `plugins/b2b/`). See [Commercial licensing](../COMMERCIAL.md).

---

## Licensing split (OSS vs B2B)

| Tag | Where it ships | License |
| --- | --- | --- |
| **`[oss]`** | `internal/`, `plugins/core/`, storefront, admin | GPL v3 |
| **`[b2b]`** | `plugins/b2b/` only | Commercial |

Open core must build and pass CI **without** a B2B license key. B2B PRs implement ports exposed by prior OSS PRs.

---

## Starting Point (after Phase 4)

Phases 1–4 delivered a runnable commerce engine with SSR storefront, schema-driven admin, Postgres-first ops, core plugins, external plugin example, guest checkout, and persistent audit log.

**What works end-to-end today**

| Area | Status |
| --- | --- |
| Storefront | Catalog, search, cart, guest + account checkout, CMS pages, account area, GDPR consent |
| Admin | Products, orders, customers, promotions, coupons, attributes, inventory, media, stores, settings, audit log |
| Pricing | Pipeline with simple promotions/coupons; price history for Omnibus-style lookups |
| Legal (baseline) | Cookie consent, account data export/delete, EU price indication composition step |
| Ops | `app dev`, compose with worker + scheduler, CSV import/export, core plugin drivers |

**Gaps motivating Phase 5**

| Gap | Impact |
| --- | --- |
| No returns / RMA workflow | Post-sale operations stay manual or external |
| No customer groups | Cannot target pricing, promotions, or B2B tiers |
| Simple promotions only | No tiered rules, buy-X-get-Y, or segment conditions |
| Transaction ledger not in admin | Payment/refund history hard to reconcile in UI |
| CMS limits | No navigation builder or reusable content blocks |
| Admin identity frozen | Users & roles read-only; no MFA for admin |
| Compliance partial | Omnibus done (PR-530/535); WEEE (PR-531); EPR (PR-532); GPSR (PR-533); OSS export (PR-534); Peppol e-invoicing not modeled |
| Platform stretch items | No merchant webhooks, plugin CLI, alternate brokers |

Phase 4 deferred items are **in scope** for Phase 5 unless marked stretch below.

---

## Target Outcome — "Mature Commerce"

When Phase 5 ships, a merchant operating in the EU should be able to:

- Process **returns** with traceable status and inventory restock rules
- Maintain **customer groups** and apply group-aware pricing or promotions
- Reconcile **payments and refunds** from admin without shell access
- Configure **navigation and reusable page blocks** without developer edits
- Display **Omnibus-compliant price indication** on discounted products (verified on storefront)
- Attach **WEEE / EPR / GPSR** compliance metadata to relevant products and surface it on PDP/checkout where required
- Manage **admin users and roles** in-app (not seed-only)
- Export **audit history** for compliance reviews

Developers should still extend via plugins without core overrides. Optional infrastructure (Kafka, SQS, GraphQL) remains **stretch** and plugin-shaped.

---

## Tracks

| Track | Goal | PR range | Delivers |
| --- | --- | --- | --- |
| **A** | Post-sale & segments | PR-500–505 | Returns/RMA, customer groups, payment ledger admin |
| **B** | Marketing & CMS depth | PR-510–514 | Advanced promotions, navigation builder, content blocks |
| **C** | Admin platform | PR-520–524 | Admin user/role CRUD, MFA, audit export, shipping UI |
| **D** | EU compliance | PR-530–535 | Omnibus storefront + PLP batch reads, WEEE/EPR/GPSR fields, OSS/e-invoicing stretch |
| **E** | Platform & integrations | PR-540–544 | Merchant webhooks, plugin CLI, extra queue brokers (stretch) |

Recommended order: **D (530, then 535)** early for EU merchants already on Shopanda, **A (500–503)** in parallel for operational maturity, then **B**, **C**, **E**.

Full compliance overview: [`specs/COMPLIANCE_EU.md`](specs/COMPLIANCE_EU.md).

---

## Track A — Post-Sale Operations & Customer Segments

**Goal:** Merchants handle returns and customer tiers without spreadsheets or external OMS tools.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-500 | [b2b] | Customer groups domain | **Done (PR-500)** — `customer_groups` table, membership, admin API |
| PR-501 | [b2b] | Group-aware pricing | **Done (PR-501)** — group price rows + pricing pipeline step + admin API |
| PR-502 | [oss] | Returns domain + workflow (done) | RMA entity, states (requested → approved → received → refunded/restocked), links to order lines |
| PR-503 | [oss] | Returns admin + account UI (done) | Admin list/detail/actions; customer "request return" on eligible orders |
| PR-504 | [oss] | Payment transaction ledger admin (done) | Read-only admin grid over payment records with order links and status filter |
| PR-505 | [oss] | Store credit / gift cards (stretch) | Issued credit balance, redemption at checkout — only if returns (502–503) expose clear need |

**Out of scope for Track A:** Full OMS/WMS, drop-ship vendor portals, marketplace split payouts.

---

## Track B — Marketing & CMS Depth

**Goal:** Merchants run richer campaigns and structure storefront content without code changes.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-510 | [oss] | Advanced promotion rules | Tiered discounts, buy-X-get-Y, min qty, date window; group conditions when B2B licensed |
| PR-511 | [oss] | Navigation builder | Menu entities (header/footer), nested items, link to category/page/URL, storefront render |
| PR-512 | [oss] | Content blocks | Reusable block types (hero, rich text, product carousel), assign to pages or layouts |
| PR-513 | [oss] | Abandoned cart recovery (stretch) | Scheduled job + email template for stale carts |
| PR-514 | [oss] | Product reviews (stretch) | Moderated reviews on PDP; optional syndication later |

**Carried from Phase 4 deferred:** complex promotion builder, navigation builder, content blocks, marketing automation (513 covers minimal automation).

---

## Track C — Admin Platform & Operator Tools

**Goal:** Teams operate Shopanda as a multi-user admin product with stronger security and visibility.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-520 | [oss] | Admin user CRUD | Create/disable admin users, assign role, force password reset |
| PR-521 | [oss] | Custom roles editor | Edit role → permission matrix (within core permission catalog + plugin permissions) |
| PR-522 | [oss] | Admin TOTP / MFA | Optional second factor at admin login; recovery codes |
| PR-523 | [oss] | Audit export + retention | CSV/JSON export of `admin_audit_log`; configurable retention job |
| PR-524 | [oss] | Shipping zones admin UI | Embed existing shipping zone/rate API into settings (closes MERCHANT.md API-only gap) |

**Carried from Phase 4 deferred:** admin user/role CRUD beyond read-only, login-time TOTP, audit persistence follow-ups (export/retention).

---

## Track D — EU & Legal Compliance

**Goal:** EU-facing merchants meet common product, pricing, and reporting obligations with explicit data models — not legal advice baked into code.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-530 | [oss] | Omnibus storefront verification (done) | Ensure discounted PDP/PLP shows lowest prior price (30d) using existing `price_history`; admin toggle per store |
| PR-535 | [oss] | Omnibus listing batch reads (done) | Batch variant/price/history repo methods; refactor `ListingPriceIndicationStep` to avoid PLP N+1 |
| PR-531 | [oss] | WEEE product fields (done) | Producer registration number, WEEE category, take-back info; PDP/footer display helpers |
| PR-532 | [oss] | EPR / packaging data (done) | Packaging material weights, recyclability flags, registration IDs per market (config-driven) |
| PR-533 | [oss] | GPSR product safety (done) | Manufacturer/importer contact, safety warnings, product identifiers for applicable catalogs |
| PR-534 | [oss] | Cross-border tax & e-invoicing (stretch) | **Done (PR-534)** — OSS/IOSS CSV exports; Peppol adapter deferred |

**Already in core (Phase 1–4):** cookie consent, GDPR export/delete, tax modes, credit notes domain, price history recording.

See [`specs/COMPLIANCE_EU.md`](specs/COMPLIANCE_EU.md) for directive mapping and non-goals.

---

## Track E — Platform & Integrations (Stretch)

**Goal:** Optional scale-out and integrator ergonomics without changing Postgres-first defaults.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-540 | [oss] | Merchant outbound webhooks | Subscribe to order/payment events; signed delivery + retry |
| PR-541 | [oss] | Plugin CLI registration | Plugins register subcommands via registry (document + example) |
| PR-542 | [oss] | Kafka / SQS queue plugins | Alternative `jobs.Queue` backends as core plugins |
| PR-543 | [oss] | GraphQL API plugin (stretch) | Read-heavy headless layer; REST remains canonical |
| PR-544 | [oss] | Dynamic plugin loading (research) | Spike only — document why `.so` loading stays deferred if rejected |

**Carried from Phase 4 deferred:** Kafka/SQS brokers, plugin `.so` hot-loading (544 is explicit research gate).

---

## Additional Maturity Backlog (unscheduled)

Items worth tracking but not yet assigned PR numbers:

| Theme | Examples | License | Notes |
| --- | --- | --- | --- |
| **Catalog depth** | Bundles, configurable products, downloadable goods, back-in-stock alerts | [oss] | Prefer plugins where possible |
| **Fulfillment** | Partial shipments, multi-warehouse, pick-up in store (BOPIS) | [oss] | Needs inventory model extensions |
| **B2B** | Quotes, purchase orders, net payment terms, shared carts, approvals | [b2b] | Lives in `plugins/b2b/`; pairs with PR-500–501 |
| **Subscriptions** | Recurring billing | [oss] plugin | Stripe Billing as core plugin candidate |
| **Analytics** | Cohort reports, funnel, merchandising dashboards | [oss] | Extend stats repo + admin charts |
| **Marketplace** | Multi-vendor, split cart, seller KYC | — | Defer beyond Phase 5 |
| **Localization** | RTL, locale-aware formats, translation workflows | [oss] | Incremental PRs on existing i18n |
| **Store admin UX** | Store management menu reshuffle | [oss] | Phase 4 deferred cosmetic item |

---

## Explicitly Out of Scope (Phase 5)

* SaaS multi-tenancy / hosted merchant isolation
* Full marketplace / vendor onboarding
* Native mobile apps
* Visual theme builder (keep SSR + theme templates)
* Replacing Postgres as default for search/queue/cache
* Legal compliance as a substitute for merchant legal counsel

---

## Validation Target

### Merchant

- Create customer groups and assign customers
- Initiate and complete a return from admin; customer can request return on eligible order
- View payment/refund history per order in admin
- Build header navigation and assign content blocks to a page
- Configure WEEE/EPR fields on an electronics SKU and see PDP disclosure

### Customer

- See prior lowest price when a product is on promotion (EU store)
- Request return and track status in account area

### Operator

- Add a new admin user with Manager role; enforce MFA when enabled
- Export audit log for a date range
- Configure shipping zones in admin UI without API calls

### Architecture

- Returns and groups live in domain + application; no handler bypass
- Compliance fields are optional product/store metadata, not hard-coded per country in core
- Stretch integrations remain core plugins or external plugins

---

## PR Index (quick reference)

| PR | Track | License | One-liner |
| --- | --- | --- | --- |
| 500 | A | [b2b] | Customer groups domain (done) |
| 501 | A | [b2b] | Group-aware pricing (done) |
| 502 | A | [oss] | Returns domain + workflow (done) |
| 503 | A | [oss] | Returns admin + account UI (done) |
| 504 | A | [oss] | Payment ledger admin (done) |
| 505 | A | [oss] | Store credit / gift cards (stretch) |
| 510 | B | [oss] | Advanced promotion rules |
| 511 | B | [oss] | Navigation builder |
| 512 | B | [oss] | Content blocks |
| 513 | B | [oss] | Abandoned cart recovery (stretch) |
| 514 | B | [oss] | Product reviews (stretch) |
| 520 | C | [oss] | Admin user CRUD |
| 521 | C | [oss] | Custom roles editor |
| 522 | C | [oss] | Admin TOTP / MFA |
| 523 | C | [oss] | Audit export + retention |
| 524 | C | [oss] | Shipping zones admin UI |
| 530 | D | [oss] | Omnibus storefront verification (done) |
| 535 | D | [oss] | Omnibus listing batch reads (done) |
| 531 | D | [oss] | WEEE product fields (done) |
| 532 | D | [oss] | EPR / packaging data (done) |
| 533 | D | [oss] | GPSR product safety (done) |
| 534 | D | [oss] | OSS / IOSS tax export (done) |
| 540 | E | [oss] | Merchant webhooks |
| 541 | E | [oss] | Plugin CLI registration |
| 542 | E | [oss] | Kafka / SQS queue plugins |
| 543 | E | [oss] | GraphQL plugin (stretch) |
| 544 | E | [oss] | Dynamic plugin loading research |

PR specs: [`prs/`](prs/).

---

## Relationship to Prior Phases

| Phase | Focus | Status |
| --- | --- | --- |
| [Phase 1](../phase-1-core/ROADMAP.md) | Core engine | Shipped |
| [Phase 2](../phase-2-merchant-ready/ROADMAP.md) | Merchant-ready surfaces | Shipped |
| [Phase 3](../phase-3-testing/ROADMAP.md) | Hardening & guest checkout | Shipped |
| [Phase 4](../phase-4-refactoring/ROADMAP.md) | Product complete | Shipped |
| **Phase 5** | **Mature commerce** | **Shipped** |
| [Phase 6](../phase-6-merchant-complete/ROADMAP.md) | Merchant-complete admin | Planned |

Phase 4 "Explicitly Deferred" items are mapped to tracks above; see Phase 4 roadmap for the original list.

---

## Admin UI deferred to Phase 6

Several Phase 5 PRs shipped **admin APIs only** (same pattern as PR-524 before its UI PR). Phase 6 wires these into `admin.js`:

| Phase 5 PR | Capability | Phase 6 UI PR |
| --- | --- | --- |
| PR-511 | Navigation builder API | [PR-600](../phase-6-merchant-complete/prs/PR-600.md) |
| PR-512 | Content blocks API | [PR-601](../phase-6-merchant-complete/prs/PR-601.md), [PR-602](../phase-6-merchant-complete/prs/PR-602.md) |
| PR-505 | Store credit API | [PR-611](../phase-6-merchant-complete/prs/PR-611.md) |
| PR-500/501 | Customer groups (B2B) | [PR-610](../phase-6-merchant-complete/prs/PR-610.md) |
| PR-540 | Outbound webhooks API | [PR-620](../phase-6-merchant-complete/prs/PR-620.md) |
| PR-513/514/510 | Marketing stretch APIs | [PR-641](../phase-6-merchant-complete/prs/PR-641.md), [PR-640](../phase-6-merchant-complete/prs/PR-640.md), [PR-642](../phase-6-merchant-complete/prs/PR-642.md) (stretch) |

Phase 5 stretch **PR-543–544** (GraphQL, dynamic plugin loading) remain **integrator/platform** work — planned for Phase 7 unless explicitly promoted. See [Phase 6 roadmap](../phase-6-merchant-complete/ROADMAP.md) for the full merchant UI sequence.
