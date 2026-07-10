# Phase 6 — Merchant-Complete Admin

## Strategy

* Build on **Phase 5 mature commerce** — domain, API, and storefront behavior largely exist; Phase 6 closes **admin UI debt**
* **No new nav placeholders** — every sidebar item either works or is hidden until its PR ships
* Prefer **vertical slices** that wire existing admin APIs into `admin.js` (same pattern as PR-524 shipping UI)
* One PR = one admin surface or one merchant-doc refresh; runnable and reviewable in ~10–20 minutes
* PR specs live under `prs/` (planned range **PR-600–651**)

Each PR is tagged **`[oss]`** or **`[b2b]`**. See [Commercial licensing](../COMMERCIAL.md).

---

## Licensing split (OSS vs B2B)

| Tag | Where it ships | License |
| --- | --- | --- |
| **`[oss]`** | `internal/`, admin SPA, storefront | GPL v3 |
| **`[b2b]`** | `plugins/b2b/` admin screens for groups/pricing | Commercial |

Open core must build and pass CI **without** a B2B license key. B2B UI PRs gate screens on license/config but must not break OSS builds.

---

## Starting Point (after Phase 5)

Phase 5 delivered operational maturity: returns, ledger, promotions, shipping zones UI, MFA, audit export, EU compliance fields, webhooks (API), navigation/blocks (API), store credit (API), and platform stretch (Kafka/SQS, plugin CLI).

**What works for merchants today**

| Area | Status |
| --- | --- |
| Daily ops | Dashboard, products, categories, orders, returns, transactions, inventory, customers, promotions, coupons, pages, media, shipping zones, settings, users/MFA/audit |
| Storefront | Catalog, checkout, account, CMS, compliance disclosures, DB-driven header nav when configured via API |
| Bulk data | CSV import/export via CLI for prices, stock, categories, catalog |

**Gaps motivating Phase 6**

| Gap | Impact |
| --- | --- |
| **Navigation / Blocks / Groups** show `renderPlaceholder` in admin | Merchants click visible menu items and hit dead ends |
| **Webhooks, store credit** API-only | Integrations page shows count + “use the API” |
| **Bulk price edits** CLI-first | Per-variant price on product edit is not enough for catalog managers |
| **Invoices** email-only | Support cannot download PDF from order admin |
| **MERCHANT.md** stale | Documents API-only gaps already fixed (shipping, categories) |
| Marketing stretch (513–514) | Backend exists; little or no admin UI |

Phase 5 explicitly deferred **admin SPA editors** for PR-511, PR-512, PR-540, PR-505, and B2B groups UI — **in scope** for Phase 6.

---

## Target Outcome — "Merchant-Complete Admin"

When Phase 6 ships, a merchant **without shell access** should be able to:

- Edit **header/footer navigation** from Content → Navigation
- Create and assign **content blocks** to home and CMS pages
- Manage **outbound webhooks** (create URL, events, rotate secret) from Integrations
- **Issue store credit** and view balance on a customer record
- Run a **bulk price update** from admin (not only CSV)
- **Download or view invoice PDF** from an order
- See **customer groups** in admin when B2B is licensed (assign members, basic CRUD)
- Never land on a **placeholder** for a feature advertised in the sidebar

Developers keep extending via plugins. Phase 5 platform work (GraphQL, dynamic-loading research) is **complete**; compile-time plugins remain the extension model. See [PR-544 research](../phase-5-maturity/specs/DYNAMIC_PLUGIN_LOADING.md).

---

## Tracks

| Track | Goal | PR range | Delivers |
| --- | --- | --- | --- |
| **A** | CMS UI debt | PR-600–602 | Navigation editor, content blocks CRUD, block placement |
| **B** | Segments & wallet | PR-610–611 | Customer groups UI [b2b], store credit on customer |
| **C** | Integrations & orders | PR-620–622 | Webhooks UI, invoice on order, refund UX polish |
| **D** | Catalog ops | PR-630–631 | Bulk price grid; optional product–category UX polish |
| **E** | Marketing UI (stretch) | PR-640–642 | Reviews moderation, abandoned cart settings, promotion helper UI |
| **F** | Docs & admin quality | PR-650–651 | MERCHANT.md refresh, placeholder policy + empty states |

Recommended order: **A (600–602)** first — highest “broken menu” impact — then **C (620)**, **B (611, 610)**, **D (630)**, **F (650)**, then stretch **E**.

---

## Track A — CMS UI Debt

**Goal:** Replace placeholders for features shipped in Phase 5 (PR-511, PR-512).

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-600 | [oss] | Navigation builder admin UI | List/edit header & footer menus; nested items; link types url/category/page |
| PR-601 | [oss] | Content blocks admin UI | CRUD hero, rich text, product carousel blocks; validated config forms |
| PR-602 | [oss] | Block placement admin UI | Assign ordered blocks to `layout/home` and CMS pages via target API |

**Out of scope:** Drag-and-drop visual page builder, block translations, footer theme wiring beyond existing API.

---

## Track B — Segments & Wallet

**Goal:** Surface B2B groups and store credit without API calls.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-610 | [b2b] | Customer groups admin UI | List/create groups; assign/remove customers; link to group pricing docs when licensed |
| PR-611 | [oss] | Store credit admin UI | Customer detail: balance, issue credit, ledger snippet (PR-505 API) |

**Out of scope:** Gift cards, auto refund-to-credit from returns, storefront credit widget changes.

---

## Track C — Integrations & Orders

**Goal:** Close integrator and support workflows in admin.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-620 | [oss] | Webhook endpoints admin UI | CRUD endpoints, event checkboxes, one-time secret display, active toggle |
| PR-621 | [oss] | Order invoice admin UI | List invoices on order; download PDF or open stored artifact |
| PR-622 | [oss] | Refund UX polish | Clear provider capability messaging, error surfacing, confirm partial/full refund |

**Out of scope:** Webhook delivery log/history UI, outbound retry controls, new payment providers.

---

## Track D — Catalog Operations

**Goal:** Reduce CLI dependency for high-frequency catalog tasks.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-630 | [oss] | Bulk price admin grid | Filterable grid: SKU/variant, currency/store scope, inline edit, save batch |
| PR-631 | [oss] | Product category picker UX (stretch) | Improve category assignment on product form if tree picker still weak |

**Out of scope:** Full PIM, channel syndication, import UI in admin (CLI remains valid).

---

## Track E — Marketing UI (Stretch)

**Goal:** Admin screens for Phase 5 marketing stretch APIs.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-640 | [oss] | Product reviews moderation UI | List pending/approved; approve/reject; link from product |
| PR-641 | [oss] | Abandoned cart settings UI | Enable/threshold preview; recovery email template pick |
| PR-642 | [oss] | Promotion rule helper UI | Form-driven conditions/actions wrapping PR-510 JSON (no raw JSON required) |

**Out of scope:** Visual campaign designer, ad platform integrations.

---

## Track F — Docs & Admin Quality

**Goal:** Merchant docs match product; admin never lies about availability.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-650 | [oss] | MERCHANT.md refresh | Align guide with Phase 5–6 admin; remove CLI-only claims where UI exists |
| PR-651 | [oss] | Admin empty states & nav policy | Remove `renderPlaceholder` pattern; document “nav item requires UI PR” in AGENTS/FRONTEND |

**Out of scope:** Video tutorials, in-app tours, white-label admin theming.

---

## Additional Backlog (unscheduled, Phase 7+)

See [Phase 7 — Customization Platform](../phase-7-customization-platform/ROADMAP.md) for the platform track (**shipped**, PR-700–720).

| Theme | Examples | Notes |
| --- | --- | --- |
| **Platform** | Multi-store admin workflow, analytics dashboards | Extend stats + charts |
| **Fulfillment** | Shipments, labels, partial ship | Needs domain extensions |
| **Analytics** | Sales reports, cohort views | Extend stats repo + charts |
| **Email** | Template editor with preview | Stretch beyond 641 |
| **Multi-store** | Store switcher workflow end-to-end | Operator complexity |
| **Compliance** | Peppol e-invoicing, category-driven GPSR validation UI | EU stretch from Phase 5 |

---

## Explicitly Out of Scope (Phase 6)

* New domain concepts unless required for UI wiring (prefer existing APIs)
* SaaS multi-tenancy / hosted merchant product
* Visual theme builder or full page builder
* Replacing CSV/CLI bulk tools (admin augments, does not replace)
* Marketplace, subscriptions, native mobile apps

---

## Validation Target

### Merchant (30-second test)

- Content → **Navigation** opens a working editor (not placeholder)
- Content → **Blocks** opens block list and create form
- Integrations → **Webhooks** can add an endpoint without curl
- Customer → **Store credit** shows balance and issue form
- Operations or Catalog → **Bulk prices** grid loads and saves one row
- Order detail → **Invoice** downloadable when invoice exists

### Operator

- No sidebar route renders `renderPlaceholder` for a Phase 5–6 shipped feature
- MERCHANT.md describes UI paths, not API-only, for navigation/blocks/shipping/returns

### Architecture

* Admin screens call existing HTTP APIs; no domain logic in `admin.js`
* B2B screens fail gracefully when license disabled (hide nav or show upgrade hint)
* One PR removes at most one placeholder (or one doc gap)

---

## PR Index (quick reference)

| PR | Track | License | One-liner |
| --- | --- | --- | --- |
| 600 | A | [oss] | Navigation builder admin UI |
| 601 | A | [oss] | Content blocks admin UI |
| 602 | A | [oss] | Block placement admin UI |
| 610 | B | [b2b] | Customer groups admin UI |
| 611 | B | [oss] | Store credit admin UI |
| 620 | C | [oss] | Webhook endpoints admin UI |
| 621 | C | [oss] | Order invoice admin UI |
| 622 | C | [oss] | Refund UX polish |
| 630 | D | [oss] | Bulk price admin grid |
| 631 | D | [oss] | Product category picker UX (stretch) |
| 640 | E | [oss] | Reviews moderation UI (stretch) |
| 641 | E | [oss] | Abandoned cart settings UI (stretch) |
| 642 | E | [oss] | Promotion rule helper UI (stretch) |
| 650 | F | [oss] | MERCHANT.md refresh |
| 651 | F | [oss] | Admin empty states & nav policy |

PR specs: [`prs/`](prs/).

---

## Relationship to Prior Phases

| Phase | Focus | Status |
| --- | --- | --- |
| [Phase 1](../phase-1-core/ROADMAP.md) | Core engine | Shipped |
| [Phase 2](../phase-2-merchant-ready/ROADMAP.md) | Merchant-ready surfaces | Shipped |
| [Phase 3](../phase-3-testing/ROADMAP.md) | Hardening & guest checkout | Shipped |
| [Phase 4](../phase-4-refactoring/ROADMAP.md) | Product complete | Shipped |
| [Phase 5](../phase-5-maturity/ROADMAP.md) | Mature commerce | Shipped |
| **Phase 6** | **Merchant-complete admin** | **Shipped** |
| [Phase 7](../phase-7-customization-platform/ROADMAP.md) | Customization platform + storefront DX | Shipped |

Phase 5 items marked “API only; admin UI deferred” map to Phase 6 tracks A–C. Phase 5 is **complete** (including PR-544 dynamic-loading research). Phase 6 is **complete** (PR-600–651).
 