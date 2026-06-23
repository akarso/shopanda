# Phase 5 — Mature Commerce

## Strategy

* Build on the **product-complete** baseline delivered in Phases 1–4 (catalog, checkout, admin, three-tier plugins, runtime modes)
* Close **operational gaps** merchants expect from a mature self-hosted platform (returns, segments, ledger visibility)
* Add **EU and compliance depth** without turning core into enterprise bloat — compliance features stay explicit, testable, and mostly opt-in per store/market
* Prefer **vertical slices** (domain → application → admin/storefront → tests) over speculative abstractions
* One PR = one responsibility; each PR must be runnable and reviewable in ~10–20 minutes
* PR specs will live under `prs/` (planned range **PR-500–549**)

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
| Compliance partial | Omnibus data exists; WEEE/EPR/GPSR/e-invoicing not modeled |
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
| **D** | EU compliance | PR-530–534 | WEEE/EPR/GPSR fields, Omnibus storefront verification, OSS/e-invoicing stretch |
| **E** | Platform & integrations | PR-540–544 | Merchant webhooks, plugin CLI, extra queue brokers (stretch) |

Recommended order: **D (530)** early for EU merchants already on Shopanda, **A (500–503)** in parallel for operational maturity, then **B**, **C**, **E**.

Full compliance overview: [`specs/COMPLIANCE_EU.md`](specs/COMPLIANCE_EU.md).

---

## Track A — Post-Sale Operations & Customer Segments

**Goal:** Merchants handle returns and customer tiers without spreadsheets or external OMS tools.

| PR | Title | Short description |
| --- | --- | --- |
| PR-500 | Customer groups domain | `customer_groups` table, group membership, repository port, admin API list/create/update |
| PR-501 | Group-aware pricing | Optional price rows or promotion conditions scoped to customer group |
| PR-502 | Returns domain + workflow | RMA entity, states (requested → approved → received → refunded/restocked), links to order lines |
| PR-503 | Returns admin + account UI | Admin list/detail/actions; customer "request return" on eligible orders |
| PR-504 | Payment transaction ledger admin | Read-only admin grid over payments/refunds/chargebacks with order link |
| PR-505 | Store credit / gift cards (stretch) | Issued credit balance, redemption at checkout — only if returns (502–503) expose clear need |

**Out of scope for Track A:** Full OMS/WMS, drop-ship vendor portals, marketplace split payouts.

---

## Track B — Marketing & CMS Depth

**Goal:** Merchants run richer campaigns and structure storefront content without code changes.

| PR | Title | Short description |
| --- | --- | --- |
| PR-510 | Advanced promotion rules | Tiered discounts, buy-X-get-Y, min qty, optional customer group + date window; admin rule builder (simple UI) |
| PR-511 | Navigation builder | Menu entities (header/footer), nested items, link to category/page/URL, storefront render |
| PR-512 | Content blocks | Reusable block types (hero, rich text, product carousel), assign to pages or layouts |
| PR-513 | Abandoned cart recovery (stretch) | Scheduled job + email template for stale carts |
| PR-514 | Product reviews (stretch) | Moderated reviews on PDP; optional syndication later |

**Carried from Phase 4 deferred:** complex promotion builder, navigation builder, content blocks, marketing automation (513 covers minimal automation).

---

## Track C — Admin Platform & Operator Tools

**Goal:** Teams operate Shopanda as a multi-user admin product with stronger security and visibility.

| PR | Title | Short description |
| --- | --- | --- |
| PR-520 | Admin user CRUD | Create/disable admin users, assign role, force password reset |
| PR-521 | Custom roles editor | Edit role → permission matrix (within core permission catalog + plugin permissions) |
| PR-522 | Admin TOTP / MFA | Optional second factor at admin login; recovery codes |
| PR-523 | Audit export + retention | CSV/JSON export of `admin_audit_log`; configurable retention job |
| PR-524 | Shipping zones admin UI | Embed existing shipping zone/rate API into settings (closes MERCHANT.md API-only gap) |

**Carried from Phase 4 deferred:** admin user/role CRUD beyond read-only, login-time TOTP, audit persistence follow-ups (export/retention).

---

## Track D — EU & Legal Compliance

**Goal:** EU-facing merchants meet common product, pricing, and reporting obligations with explicit data models — not legal advice baked into code.

| PR | Title | Short description |
| --- | --- | --- |
| PR-530 | Omnibus storefront verification | Ensure discounted PDP/PLP shows lowest prior price (30d) using existing `price_history`; admin toggle per store |
| PR-531 | WEEE product fields | Producer registration number, WEEE category, take-back info; PDP/footer display helpers |
| PR-532 | EPR / packaging data | Packaging material weights, recyclability flags, registration IDs per market (config-driven) |
| PR-533 | GPSR product safety | Manufacturer/importer contact, safety warnings, product identifiers for applicable catalogs |
| PR-534 | Cross-border tax & e-invoicing (stretch) | OSS/IOSS summary exports; Peppol/e-invoice adapter as core plugin |

**Already in core (Phase 1–4):** cookie consent, GDPR export/delete, tax modes, credit notes domain, price history recording.

See [`specs/COMPLIANCE_EU.md`](specs/COMPLIANCE_EU.md) for directive mapping and non-goals.

---

## Track E — Platform & Integrations (Stretch)

**Goal:** Optional scale-out and integrator ergonomics without changing Postgres-first defaults.

| PR | Title | Short description |
| --- | --- | --- |
| PR-540 | Merchant outbound webhooks | Subscribe to order/payment events; signed delivery + retry |
| PR-541 | Plugin CLI registration | Plugins register subcommands via registry (document + example) |
| PR-542 | Kafka / SQS queue plugins | Alternative `jobs.Queue` backends as core plugins |
| PR-543 | GraphQL API plugin (stretch) | Read-heavy headless layer; REST remains canonical |
| PR-544 | Dynamic plugin loading (research) | Spike only — document why `.so` loading stays deferred if rejected |

**Carried from Phase 4 deferred:** Kafka/SQS brokers, plugin `.so` hot-loading (544 is explicit research gate).

---

## Additional Maturity Backlog (unscheduled)

Items worth tracking but not yet assigned PR numbers:

| Theme | Examples | Notes |
| --- | --- | --- |
| **Catalog depth** | Bundles, configurable products, downloadable goods, back-in-stock alerts | Prefer plugins where possible |
| **Fulfillment** | Partial shipments, multi-warehouse, pick-up in store (BOPIS) | Needs inventory model extensions |
| **B2B** | Quotes, purchase orders, net payment terms, shared carts | Often pairs with customer groups (500) |
| **Subscriptions** | Recurring billing | Stripe Billing as core plugin candidate; out of scope until payment team need |
| **Analytics** | Cohort reports, funnel, merchandising dashboards | Extend stats repo + admin charts |
| **Marketplace** | Multi-vendor, split cart, seller KYC | Large architectural fork — defer beyond Phase 5 |
| **Localization** | RTL, locale-aware formats, translation workflows | Incremental PRs on existing i18n |
| **Store admin UX** | Store management menu reshuffle | Phase 4 deferred cosmetic item |

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

| PR | Track | One-liner |
| --- | --- | --- |
| 500 | A | Customer groups domain |
| 501 | A | Group-aware pricing |
| 502 | A | Returns domain + workflow |
| 503 | A | Returns admin + account UI |
| 504 | A | Payment ledger admin |
| 505 | A | Store credit / gift cards (stretch) |
| 510 | B | Advanced promotion rules |
| 511 | B | Navigation builder |
| 512 | B | Content blocks |
| 513 | B | Abandoned cart recovery (stretch) |
| 514 | B | Product reviews (stretch) |
| 520 | C | Admin user CRUD |
| 521 | C | Custom roles editor |
| 522 | C | Admin TOTP / MFA |
| 523 | C | Audit export + retention |
| 524 | C | Shipping zones admin UI |
| 530 | D | Omnibus storefront verification |
| 531 | D | WEEE product fields |
| 532 | D | EPR / packaging data |
| 533 | D | GPSR product safety |
| 534 | D | OSS / e-invoicing (stretch) |
| 540 | E | Merchant webhooks |
| 541 | E | Plugin CLI registration |
| 542 | E | Kafka / SQS queue plugins |
| 543 | E | GraphQL plugin (stretch) |
| 544 | E | Dynamic plugin loading research |

PR specs: [`prs/`](prs/) (to be added as work starts).

---

## Relationship to Prior Phases

| Phase | Focus | Status |
| --- | --- | --- |
| [Phase 1](../phase-1-core/ROADMAP.md) | Core engine | Shipped |
| [Phase 2](../phase-2-merchant-ready/ROADMAP.md) | Merchant-ready surfaces | Shipped |
| [Phase 3](../phase-3-testing/ROADMAP.md) | Hardening & guest checkout | Shipped |
| [Phase 4](../phase-4-refactoring/ROADMAP.md) | Product complete | Shipped |
| **Phase 5** | **Mature commerce** | **Planned** |

Phase 4 "Explicitly Deferred" items are mapped to tracks above; see Phase 4 roadmap for the original list.
