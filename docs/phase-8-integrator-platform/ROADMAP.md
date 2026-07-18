# Phase 8 — Integrator Platform

## Strategy

* Build on **Phase 7 customization platform** — extension fields, hooks/slots, and assets exist; Phase 8 makes **commerce behavior** and **external system wiring** first-class
* **Explicit extension only** — ports, ordered pipelines, import hooks, integration routes; no override folders or runtime preferences
* Prefer **vertical slices** (port or hook → one importer or one reference plugin → docs)
* One PR = one responsibility; runnable and reviewable in ~10–20 minutes
* PR specs live under `prs/` (**PR-800–855** shipped)

Each PR is tagged **`[oss]`** unless noted. See [Commercial licensing](../COMMERCIAL.md).

---

## Licensing split (OSS vs plugins)

| Tag | Where it ships | License |
| --- | --- | --- |
| **`[oss]`** | `internal/domain`, `internal/application`, integration middleware | GPL v3 |
| **Plugin** | `plugins/*` reference integrations | Plugin author's license |

Open core must build and pass CI **without** any reference integration plugin enabled.

---

## Starting Point (after Phase 7)

Phase 7 closed customization platform work (PR-700–720). Developers extend via compile-time plugins, extension fields, dynamic hooks, slots, and asset manifests.

**What works today**

| Area | Status |
| --- | --- |
| Pricing | Pipeline with positioned plugin steps; cart recalculate runs pipeline |
| Checkout | Workflow with positioned plugin steps |
| Cart hooks | Full lifecycle + `cart.validate` (stable v0 via extapi) |
| Infrastructure ports | Search, cache, queue, payment, media — single-slot registration |
| HTTP surfaces | `RegisterAdminRoute`, `RegisterPublicRoute` |
| CSV import | CLI importers for catalog, prices, stock, categories, customers |
| Events / queue | Async reactions; optional Kafka/SQS |
| GraphQL | Read + extension field parity |

**Gaps motivating Phase 8**

| Gap | Impact |
| --- | --- |
| **Price rules & cart mods** are the #1 integrator ask | Append-only pricing steps; almost no cart lifecycle hooks |
| **No import transform seam** | ERP CSV layouts require importer forks |
| **Integration routes ad hoc** | No API-key/HMAC/idempotency conventions for SAP-style inbound |
| **Outbound sync unstructured** | Warehouse/PIM clients live outside standard job registration |
| **Port catalog incomplete** | Tax, mail, shipping rating not replaceable |
| **Precedence undocumented** | Multi-plugin projects guess ordering |

See [INTEGRATOR_PLATFORM.md](specs/INTEGRATOR_PLATFORM.md) for the full design position.

---

## Target Outcome — "Integrator Platform"

When Phase 8 ships, an integrator **without core forks** should be able to:

- Register a **positioned pricing step** (custom cart/price rule after promotions)
- **Validate or block cart mutations** via hook chains with structured API errors
- **Transform CSV rows** before product/price/stock import persistence
- Expose **authenticated integration REST** endpoints for ERP callbacks (idempotent)
- Register **outbound sync jobs** that call external GraphQL/REST (warehouse, PIM)
- **Replace** tax calculation (and optionally mail/shipping rating) via infrastructure ports
- Inspect **all registered extensions** at startup (ports, steps, hooks, routes, jobs)

Merchants continue using admin for day-to-day ops; Phase 8 is **integrator- and agency-facing**.

---

## Tracks

| Track | Goal | PR range | Delivers |
| --- | --- | --- | --- |
| **A** | Strategy & catalog | PR-800–802 | Spec, port catalog, precedence policy, composition guide updates — **complete** |
| **B** | Commerce behavior | PR-810–814 | Pricing position API, cart hooks, cart validate chain, reference cart rule plugin — **complete** |
| **C** | Import pipelines | PR-820–823 | Row hook registry wired into CLI importers, reference CSV remap plugin — **complete** |
| **D** | Inbound integration | PR-830–833 | Integration auth middleware, idempotency, reference SAP-style endpoint — **complete** |
| **E** | Outbound integration | PR-840–843 | Sync job registration, client bootstrap, warehouse + PIM reference plugins — **complete** |
| **F** | Wiring ergonomics | PR-850–853 | Plugin SDK helpers, startup report, replace-by-name steps, port replacement template — **complete** |

Post-phase: **PR-854** checkout step positioning (shipped).

Recommended order: **A (800–802)** → **B (810–814)** and **C (820–823)** in parallel → **D (830–833)** + **E (840–843)** → **F (850–853)** → **854** (checkout positioning).

---

## Track A — Extension Strategy & Port Catalog (PR-800–802)

**Goal:** One authoritative answer for “how do I extend X?” and “who wins when two plugins conflict?”

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-800 | [oss] | Integrator platform spec publish | Finalize [INTEGRATOR_PLATFORM.md](specs/INTEGRATOR_PLATFORM.md); link from DEVELOPER.md / PLUGINS.md |
| PR-801 | [oss] | Port catalog + introspection endpoint | `GET /api/v1/admin/extensions/ports` (or CLI) listing active port implementations |
| PR-802 | [oss] | Precedence policy + composition guide | Update PLUGIN_COMPOSITION.md with cart/import/integration patterns; multi-team table |

**Out of scope:** Implementing new ports (Track F), reference plugins (Tracks B–E).

---

## Track B — Commerce Behavior: Cart & Pricing (PR-810–814)

**Goal:** Cover the most common customization pain — **price rules and cart modifications** — without patching core services.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-810 | [oss] | Pricing step positioning | `RegisterPricingStep(step, Position)` — `before:` / `after:` named core steps; stable step name catalog |
| PR-811 | [oss] | Cart lifecycle hooks | `cart.add_item.before`, `cart.update_item.before`, `cart.remove_item.after`, `cart.recalculate.before`; extapi v0 additions |
| PR-812 | [oss] | Cart validate chain | `cart.validate` hook; structured errors surfaced on storefront cart API |
| PR-813 | [oss] | Tax calculator port | `RegisterTaxCalculator`; default core implementation; pricing pipeline calls port |
| PR-814 | [oss] | Reference plugin: cart rule | Demo plugin — min qty + fee step + validate hook; integration test |

**Stretch (backlog within phase):** promotion rule evaluator port; shipping rate provider port.

**Out of scope:** Admin UI for custom rules; visual rule builder.

---

## Track C — Import Pipelines (PR-820–823)

**Goal:** CSV import values can be **read, transformed, and enriched** before DB write.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-820 | [oss] | Import row hook registry | Domain/application registry; `app.Import().RegisterRowHook(entity, priority, fn)` |
| PR-821 | [oss] | Wire core importers | Product, price, stock, category, customer importers invoke row chain pre-persist |
| PR-822 | [oss] | Import context + errors | Mutable row map, skip row, aggregated `ImportResult` errors with row index |
| PR-823 | [oss] | Reference plugin: CSV remap | Demo — ERP column names → core columns on `import:products` |

**Out of scope:** Admin CSV upload UI; non-CSV formats in core (plugins add CLI commands).

---

## Track D — Inbound Integration REST (PR-830–833)

**Goal:** ERP systems (SAP, etc.) push data via **documented, secured REST** on plugin routes.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-830 | [oss] | Integration route conventions | Doc + helper: `/api/v1/integrations/{plugin}/…`; structured error envelope |
| PR-831 | [oss] | Integration auth middleware | API key + optional HMAC signature verification; plugin config fields |
| PR-832 | [oss] | Idempotency store | `Idempotency-Key` dedupe for inbound POST; Postgres repo |
| PR-833 | [oss] | Reference plugin: order status inbound | Public route accepts ERP payload → updates order status idempotently |

**Out of scope:** SAP connector certification; proprietary IDoc parser in core.

---

## Track E — Outbound Integration (PR-840–843)

**Goal:** Plugins query **external GraphQL/REST** (warehouse, PIM) on schedule or on events.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-840 | [oss] | Sync job registration port | `RegisterSyncJob` — cron or event trigger; queue-backed retry |
| PR-841 | [oss] | Integration client bootstrap | `pkg/integrationsdk` — HTTP + GraphQL helpers, timeouts, logging (stdlib-first) |
| PR-842 | [oss] | Reference plugin: warehouse stock | Pull stock from mock HTTP API → upsert via application service |
| PR-843 | [oss] | Reference plugin: PIM GraphQL PDP | Composition step enriches PDP from external GraphQL (cached) |

**Out of scope:** Built-in connectors for specific vendors; credential vault beyond plugin config.

---

## Track F — Wiring Ergonomics (PR-850–853)

**Goal:** Lower ceremony for integrators wiring many plugins; make registration **visible and debuggable**.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-850 | [oss] | Plugin SDK package | Typed wrappers for pricing position, import hooks, sync jobs |
| PR-851 | [oss] | Registration report | Startup log summary + `./app plugins report` CLI |
| PR-852 | [oss] | Replace-by-name pipeline steps | `RegisterPricingStep(step, Replace("tax"))` — one winner per step name |
| PR-853 | [oss] | Reference port replacement | Template plugin replacing tax calculator (or mail sender stretch) |

**Out of scope:** Runtime plugin discovery; `depends_on` graph solver (stretch doc only in PR-802).

---

## Additional Backlog (unscheduled, Phase 8+)

| Theme | Examples | Notes |
| --- | --- | --- |
| **Checkout step positioning** | Same API as pricing for `RegisterCheckoutStep` | Shipped (PR-854) |
| **Export pipelines** | Row hooks on CSV export | Symmetric to Track C |
| **Shipping rate port** | Carrier API replaces zone table math | Stretch in Track B |
| **Mail sender port** | SendGrid, Postmark | Stretch in Track F |
| **Admin integration UI** | View inbound idempotency keys, replay | Merchant-facing; lower priority |
| **Phase 7 backlog** | Extension field admin grid, variant scopes | Unchanged |

---

## Explicitly Out of Scope (Phase 8)

* Runtime Go `.so` plugin loading ([PR-544 research](../phase-5-maturity/specs/DYNAMIC_PLUGIN_LOADING.md))
* Magento-style override folders / `di.xml` / global class preferences
* Low-code visual rule engine or promotion designer
* Layered navigation / faceted search (large standalone track)
* “Install any npm package” or runtime theme marketplace
* SaaS multi-tenancy

---

## Validation Target

### Integrator (acceptance from spec §10)

- Custom **price rule** via positioned pricing step on live cart
- **Block cart add** with user-visible error from validate hook
- **CSV column remap** on product import without editing core importer
- **Inbound REST** endpoint with API key + idempotent retry
- **Outbound sync job** calling external GraphQL with queue retry
- **Tax port replacement** via reference plugin
- **Registration report** lists all active extensions

### Architecture

* Domain has no HTTP/DB imports
* Import hooks run in application layer; repositories unchanged in signature
* Integration auth secrets never logged
* Plugins use documented narrow ports — no new `internal/infrastructure` imports from `plugins/*`
* One PR = one layer or one vertical slice consumer

---

## PR Index (quick reference)

| PR | Track | License | One-liner |
| --- | --- | --- | --- |
| 800 | A | [oss] | Integrator platform spec publish (done) |
| 801 | A | [oss] | Port catalog + introspection (done) |
| 802 | A | [oss] | Precedence policy + composition guide (done) |
| 810 | B | [oss] | Pricing step positioning (done) |
| 811 | B | [oss] | Cart lifecycle hooks (done) |
| 812 | B | [oss] | Cart validate chain (done) |
| 813 | B | [oss] | Tax calculator port (done) |
| 814 | B | [oss] | Reference plugin: cart rule (done) |
| 820 | C | [oss] | Import row hook registry (done) |
| 821 | C | [oss] | Wire core importers (done) |
| 822 | C | [oss] | Import context + errors (done) |
| 823 | C | [oss] | Reference plugin: CSV remap (done) |
| 830 | D | [oss] | Integration route conventions (done) |
| 831 | D | [oss] | Integration auth middleware (done) |
| 832 | D | [oss] | Idempotency store (done) |
| 833 | D | [oss] | Reference plugin: order status inbound (done) |
| 840 | E | [oss] | Sync job registration (done) |
| 841 | E | [oss] | Integration client bootstrap (done) |
| 842 | E | [oss] | Reference plugin: warehouse stock (done) |
| 843 | E | [oss] | Reference plugin: PIM GraphQL PDP (done) |
| 850 | F | [oss] | Plugin SDK package (done) |
| 851 | F | [oss] | Registration report (done) |
| 852 | F | [oss] | Replace-by-name steps (done) |
| 853 | F | [oss] | Reference port replacement (done) |
| 854 | — | [oss] | Checkout step positioning (done) |
| 855 | — | [oss] | Phase 8 closure docs (done) |

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
| [Phase 6](../phase-6-merchant-complete/ROADMAP.md) | Merchant-complete admin | Shipped |
| [Phase 7](../phase-7-customization-platform/ROADMAP.md) | Customization platform | Shipped |
| **Phase 8** | **Integrator platform** | **Shipped (PR-800–854; tracks A–F complete)** |
