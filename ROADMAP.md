# Implementation Roadmap

## Strategy

* Bottom-up foundation, then vertical slices
* Each PR touches one responsibility across all layers (domain → application → infrastructure → interfaces)
* Every PR must compile, and ideally be runnable
* No unused abstractions — build only what the current PR needs

---

## Phase 0 — Project Scaffold

Goal: empty but runnable binary with tooling in place.

| PR  | Title                        | Scope                                                        |
| --- | ---------------------------- | ------------------------------------------------------------ |
| 001 | Go module + project skeleton | `go.mod`, `/cmd/api/main.go`, `/internal` dirs, `.gitignore` |
| 002 | Platform: config loader      | `config.yaml` parsing, env overlay, `config.Get()`           |
| 003 | Platform: structured logger  | JSON logger to stdout, `Logger` interface                    |
| 004 | Request context + correlation IDs | `RequestID` middleware, correlation ID propagation, context-aware logger |
| 005 | Platform: ID generation      | UUID helper (`id.New()`)                                     |
| 006 | Database connection + migrations | Postgres connection, migration runner, `app migrate` CLI  |
| 007 | HTTP server skeleton         | Router, health endpoint, middleware chain, graceful shutdown  |
| 008 | Error handling foundation    | `AppError` type, error-to-HTTP mapping, standard response    |

---

## Phase 1 — Catalog (First Vertical Slice)

Goal: products stored, retrieved, and served via API. Composition pipeline introduced early so handlers use it from day one.

| PR  | Title                           | Scope                                                   |
| --- | ------------------------------- | ------------------------------------------------------- |
| 009 | Money type                      | `domain/shared/money.go` — `Money` struct + helpers     |
| 010 | Product domain                  | Entity, repository interface, events definitions        |
| 011 | Product Postgres repository     | `infrastructure/db/postgres/product_repo.go` + migration |
| 012 | Composition pipeline (typed)    | Generic `Pipeline[T]`, step interface, product + listing contexts |
| 013 | Product HTTP handlers           | List, Get — served via composition pipeline             |
| 014 | Product write handlers          | Create, Update — admin endpoints                        |
| 015 | Variant domain + repository     | Entity, repo interface, Postgres impl, migration        |
| 016 | Variant HTTP handlers           | CRUD under `/products/{id}/variants`                    |

---

## Phase 2 — Minimal Import

Goal: load product data from CSV so every subsequent phase has test data.

| PR  | Title                        | Scope                                          |
| --- | ---------------------------- | ---------------------------------------------- |
| 017 | Basic product import         | CSV parser, direct map to product + variant entities, `app import:products` CLI |

---

## Phase 3 — Pricing

Goal: deterministic pricing pipeline, starting minimal.

| PR  | Title                        | Scope                                        |
| --- | ---------------------------- | -------------------------------------------- |
| 018 | Pricing domain models        | `PricingContext`, `Adjustment`, `PricingStep` |
| 019 | Price storage                | `prices` table, repo, migration              |
| 020 | Pricing pipeline (base + finalize) | Base price step, finalization step, pipeline executor |

---

## Phase 4 — Minimal Auth

Goal: identity middleware and roles so all subsequent handlers can be protected from the start.

| PR  | Title                        | Scope                                              |
| --- | ---------------------------- | -------------------------------------------------- |
| 021 | Identity middleware + roles  | `Identity` type, role enum, token parsing, `RequireAuth` middleware |
| 022 | Dev auth provider            | Dev token generation, test helpers for authenticated requests |

---

## Phase 5 — Inventory

Goal: stock tracking and reservation model.

| PR  | Title                        | Scope                                        |
| --- | ---------------------------- | -------------------------------------------- |
| 023 | Inventory domain + storage   | Entity, repo, migration, basic set/get stock |
| 024 | Inventory reservations       | Reservation table, reserve/release logic     |

---

## Phase 6 — Cart

Goal: working cart with pricing integration. Protected by auth middleware.

| PR  | Title                        | Scope                                             |
| --- | ---------------------------- | ------------------------------------------------- |
| 025 | Cart domain                  | Entity, cart items, repository interface           |
| 026 | Cart Postgres repository     | Storage + migration                                |
| 027 | Cart application service     | Add/update/remove items, recalculate via pipeline  |
| 028 | Cart HTTP handlers           | Create, get, add item, update item, remove item — behind `RequireAuth` |

---

## Phase 7 — Event System

Goal: wire up the event bus now that domains exist to emit and react.

| PR  | Title                     | Scope                                          |
| --- | ------------------------- | ---------------------------------------------- |
| 029 | Event bus (in-process)    | Event envelope, sync + async dispatch, `On()`  |
| 030 | Wire domain events        | Emit events on product, cart, inventory changes |

---

## Phase 8 — Customers & Registration

Goal: real customer accounts, replacing dev tokens with proper auth flow.

| PR  | Title                        | Scope                                            |
| --- | ---------------------------- | ------------------------------------------------ |
| 031 | Customer domain + storage    | Entity, repo, migration                          |
| 032 | Registration + login         | Password hashing, JWT creation, `/auth/*` endpoints |
| 033 | Wire customer to auth        | Replace dev token provider with real JWT flow    |

---

## Phase 9 — Orders & Checkout

Goal: cart → order via checkout workflow.

| PR  | Title                        | Scope                                            |
| --- | ---------------------------- | ------------------------------------------------ |
| 034 | Order domain + storage       | Entity, items, repo, migration                   |
| 035 | Checkout workflow engine     | Step interface, sequential executor              |
| 036 | Checkout steps: validate + price | `validate_cart`, `recalculate_pricing`        |
| 037 | Checkout steps: reserve + create | `reserve_inventory`, `create_order`           |
| 038 | Checkout HTTP endpoint       | `POST /checkout` — full flow                     |
| 039 | Order HTTP handlers          | Get order, list orders                           |

---

## Phase 10 — Minimal Admin

Goal: basic admin UI endpoints so write APIs are testable without curl. No schema abstraction yet.

| PR  | Title                        | Scope                                            |
| --- | ---------------------------- | ------------------------------------------------ |
| 040 | Admin route group + role guard | `/admin/*` prefix, `RequireRole(admin)` middleware |
| 041 | Entity admin endpoints       | Product list/create/update, Order list/view — plain CRUD behind admin guard |

---

## Phase 11 — Payments & Shipping

Goal: complete the checkout loop.

| PR  | Title                        | Scope                                            |
| --- | ---------------------------- | ------------------------------------------------ |
| 042 | Payment domain + storage     | Entity, provider interface, repo, migration      |
| 043 | Manual payment provider      | In-process default, marks payment as paid        |
| 044 | Payment webhook endpoint     | `POST /payments/webhook/{provider}`              |
| 045 | Shipping domain + storage    | Entity, provider interface, repo, migration      |
| 046 | Flat rate shipping provider  | Default provider, `/shipping/rates`              |
| 047 | Checkout: payment + shipping | Add `initiate_payment` + `select_shipping` steps |

---

## Phase 12 — Plugin System

Goal: formalize extension mechanism. Composition pipeline already exists (PR 012); this phase adds plugin loading and registration.

| PR  | Title                        | Scope                                            |
| --- | ---------------------------- | ------------------------------------------------ |
| 048 | Plugin interface + loader    | `Plugin` interface, `Init()`, registration       |
| 049 | Pipeline registration API    | `RegisterPricingStep()`, `RegisterCheckoutStep()`, `RegisterCompositionStep()` |

---

## Phase 13 — Catalog Organization

Goal: browsable store.

| PR  | Title                        | Scope                                          |
| --- | ---------------------------- | ---------------------------------------------- |
| 050 | Categories domain + storage  | Entity, tree, repo, migration                  |
| 051 | Category HTTP handlers       | Tree, get, products-by-category                |
| 052 | Collections domain + storage | Manual + dynamic collections                   |

---

## Phase 14 — Search

Goal: products are searchable.

| PR  | Title                        | Scope                                          |
| --- | ---------------------------- | ---------------------------------------------- |
| 053 | Search engine interface      | `SearchEngine`, `SearchQuery`, `SearchResult`  |
| 054 | Postgres search impl         | `tsvector`, full-text search, facets           |
| 055 | Search HTTP endpoint         | `GET /search`                                  |

---

## Phase 15 — Supporting Systems

Goal: production-readiness features.

| PR  | Title                        | Scope                                          |
| --- | ---------------------------- | ---------------------------------------------- |
| 056 | Jobs: queue interface + Postgres impl | `Job`, `Queue`, `Worker`, migration     |
| 057 | Scheduler (cron)             | `Scheduler` interface, in-process impl, `app scheduler` CLI |
| 058 | Email notifications          | `Notifier` interface, SMTP impl, templates     |
| 059 | Wire email to order events   | `order.paid` → send confirmation email         |
| 060 | Media: storage interface     | `Storage`, `Asset`, local filesystem impl      |
| 061 | Media: upload endpoint       | `POST /media/upload`                           |
| 062 | Caching layer                | `Cache` interface, Postgres UNLOGGED impl      |
| 063 | Configuration: DB layer      | DB config table, export/import CLI             |
| 064 | Cache cleanup scheduled job  | Register `cache.cleanup` cron → enqueue job    |

---

## Phase 16 — Admin: Schema Registry

Goal: upgrade minimal admin (PR 040–041) to schema-driven forms and grids.

| PR  | Title                        | Scope                                          |
| --- | ---------------------------- | ---------------------------------------------- |
| 065 | Admin schema registry        | Form, Grid, Field types, registration API      |
| 066 | Product admin schema         | Register product form + grid, migrate admin endpoints to schema-driven |

---

## Phase 17 — Theme & Frontend

Goal: optional SSR layer.

| PR  | Title                        | Scope                                          |
| --- | ---------------------------- | ---------------------------------------------- |
| 067 | Theme engine (basic SSR)     | Template loading, `Render()`, layout           |
| 068 | Product page template        | `product.html` consuming composition pipeline  |

Spec: [`docs/PAGES_RENDERING.md`](docs/PAGES_RENDERING.md), [`docs/LAYOUTS.md`](docs/LAYOUTS.md)

---

## Phase 18 — Data Exchange (Advanced)

Goal: flexible attribute mapping and export. Basic import already exists (PR 017).

| PR  | Title                        | Scope                                          |
| --- | ---------------------------- | ---------------------------------------------- |
| 069 | Attribute model              | `Attribute` type, attribute registry           |
| 070 | Import pipeline upgrade      | Attribute mapping, validation rules, error reporting |
| 071 | Export pipeline              | Entity → CSV, `app export:products` CLI        |

> **Note:** PR-069 should also introduce `AttributeGroup` — products can be assigned one or more attribute groups, and each group defines which attributes apply. Until then, attributes are stored as flat JSONB with no schema enforcement.

---

## Phase 19 — Seeding & CLI

Goal: drop-in usability.

| PR  | Title                        | Scope                                          |
| --- | ---------------------------- | ---------------------------------------------- |
| 072 | Seed framework               | Seeder interface, `app seed` command           |
| 073 | Core seed data               | Admin user, categories, sample products        |
| 074 | CLI: full command set        | `serve`, `migrate`, `seed`, `worker`, `scheduler`, `search:reindex`, `config:export`, `import:*`, `export:*` |

---

## Phase 20 — Taxes

Goal: country-based VAT calculation integrated into the pricing pipeline.

| PR  | Title                        | Scope                                              |
| --- | ---------------------------- | -------------------------------------------------- |
| 075 | Tax domain + rate storage    | `TaxClass`, `TaxRate`, repo, migration             |
| 076 | Tax pipeline step            | Tax calculation step in pricing pipeline, per-item + total, exclusive/inclusive modes |

Spec: [`docs/TAXES.md`](docs/TAXES.md)

---

## Phase 21 — Rules & Promotions

Goal: reusable rule primitives powering catalog and cart discounts.

| PR  | Title                        | Scope                                              |
| --- | ---------------------------- | -------------------------------------------------- |
| 077 | Rule primitives              | `Condition[T]`, `Rule[T]`, sequential executor     |
| 078 | Promotion domain + storage   | Catalog rules, cart rules, coupon entity, repo, migration |
| 079 | Promotion HTTP endpoints     | Apply catalog rules in pricing pipeline, `POST/DELETE /cart/{id}/coupon` |

Specs: [`docs/RULES.md`](docs/RULES.md), [`docs/PROMOTIONS.md`](docs/PROMOTIONS.md)

---

## Phase 22 — Invoicing

Goal: immutable invoice generation from orders with credit note support.

| PR  | Title                        | Scope                                              |
| --- | ---------------------------- | -------------------------------------------------- |
| 080 | Invoice domain + storage     | Invoice entity, credit notes, DB sequence numbering, repo, migration |
| 081 | Invoice generation + PDF     | Order → invoice snapshot, HTML → PDF, events (`invoice.created`) |

Spec: [`docs/INVOICING.md`](docs/INVOICING.md)

---

## Phase 23 — URL Routing & CMS

Goal: SEO-friendly URLs and static content pages.

| PR  | Title                        | Scope                                              |
| --- | ---------------------------- | -------------------------------------------------- |
| 082 | URL rewrite system           | `url_rewrites` table, migration, resolver middleware, catch-all route |
| 083 | Wire entity slugs            | Product/category slugs registered in rewrite table on create/update |
| 084 | CMS pages                    | Page domain, storage, `GET /pages/{slug}`, admin integration |

Specs: [`docs/ROUTING.md`](docs/ROUTING.md), [`docs/CMS.md`](docs/CMS.md)

---

## Phase 24 — SEO

Goal: structured data and discoverability.

| PR  | Title                        | Scope                                              |
| --- | ---------------------------- | -------------------------------------------------- |
| 085 | Meta + structured data       | Composition step for meta tags, JSON-LD for products (price, availability) |
| 086 | Sitemap + robots             | `GET /sitemap.xml` generation, `GET /robots.txt`, canonical URLs |

Spec: [`docs/SEO.md`](docs/SEO.md)

---

## Phase 25 — Multi-Store & Currency

Goal: multiple store contexts with scoped pricing and tax.

| PR  | Title                        | Scope                                              |
| --- | ---------------------------- | -------------------------------------------------- |
| 087 | Store domain + resolution    | `Store` entity, repo, migration, domain-based resolution middleware |
| 088 | Scoped pricing + tax         | Prices per store (`variant_id + store_id → price`), tax by `store.country` |

Spec: [`docs/STORES_CURRENCIES.md`](docs/STORES_CURRENCIES.md)

---

## Phase 26 — Localization

Goal: unified translation system across backend and frontend.

| PR  | Title                        | Scope                                              |
| --- | ---------------------------- | -------------------------------------------------- |
| 089 | Translation system           | System translations table, `t()` function, language resolution (param → header → default) |
| 090 | Content translations         | Entity translation table (`entity_id + language + field → value`), API integration |

Spec: [`docs/I18N.md`](docs/I18N.md)

---

## Phase 27 — Legal & Compliance

Goal: essential EU compliance features.

| PR  | Title                        | Scope                                              |
| --- | ---------------------------- | -------------------------------------------------- |
| 091 | Cookie consent + GDPR        | Consent model, `GET /account/data`, `GET /account/export`, `DELETE /account` |
| 092 | Price history                | Price tracking table, lowest-price-in-30-days display for discounted products |

Spec: [`docs/LEGAL.md`](docs/LEGAL.md)

---

## Phase 28 — Performance & CDN

Goal: caching strategy and CDN integration.

| PR  | Title                        | Scope                                              |
| --- | ---------------------------- | -------------------------------------------------- |
| 093 | Cache headers + CDN config   | `Cache-Control` middleware (public vs no-store), `cdn.base_url` config |
| 094 | Cache invalidation           | Product/price change → invalidation trigger, cache keys include store + language + currency |

Spec: [`docs/PERFORMANCE.md`](docs/PERFORMANCE.md)

---

## Phase 29 — Admin RBAC

Goal: permission-based access control for admin users.

| PR  | Title                        | Scope                                              |
| --- | ---------------------------- | -------------------------------------------------- |
| 095 | Roles + permissions model    | Role entity (admin/manager/editor/support), permission strings, role-permission mapping, migration |
| 096 | Permission middleware + UI   | `RequirePermission()` middleware, admin forms/grids respect permissions, plugin permission registration |

Spec: [`docs/ROLES.md`](docs/ROLES.md)

---

## Milestone Summary

| Milestone                 | After PR | What works                                  |
| ------------------------- | -------- | ------------------------------------------- |
| **Skeleton runs**         | 008      | Binary starts, health check, migrations     |
| **Catalog API**           | 016      | Products + variants via composition pipeline |
| **CSV import**            | 017      | `app import:products` loads test data        |
| **Auth foundation**       | 022      | Identity middleware, roles, dev tokens       |
| **Priced cart**           | 028      | Cart with pricing, behind auth              |
| **Events wired**          | 030      | Domains emit events, bus dispatches          |
| **Real auth**             | 033      | Customer accounts, registration, login      |
| **Testable admin**        | 041      | Product/order CRUD behind admin guard       |
| **End-to-end checkout**   | 047      | Cart → checkout → order → payment           |
| **Pluggable**             | 049      | Plugins can extend pipelines                |
| **Searchable store**      | 055      | Full-text product search                    |
| **Production-ready**      | 064      | Jobs, scheduler, email, media, caching      |
| **Schema-driven admin**   | 066      | Form/grid registry, schema-driven UI        |
| **Data exchange**         | 071      | Attribute mapping, export                   |
| **Drop-in usable**        | 074      | `migrate → seed → serve` = working store    |
| **Tax-aware pricing**     | 076      | VAT calculation in pricing pipeline         |
| **Discounts & coupons**   | 079      | Catalog rules, cart rules, coupon support   |
| **Invoicing**             | 081      | Invoice + credit note generation, PDF       |
| **SEO-ready storefront**  | 086      | URL rewrites, CMS pages, sitemap, JSON-LD   |
| **Multi-store**           | 088      | Store resolution, scoped pricing + tax      |
| **Localized**             | 090      | System + content translations               |
| **EU-compliant**          | 092      | GDPR, cookie consent, price history         |
| **CDN-optimized**         | 094      | Cache headers, CDN config, invalidation     |
| **Admin RBAC**            | 096      | Role-based permissions for admin routes     |

---

## Rules

* Each PR must compile
* Each PR should be reviewable in ~15 minutes
* PR: e2e smoke test (cart → checkout)
* No forward references — don't use what hasn't been built
* Domain tests required from Phase 1
* Refactoring PRs are allowed but must be standalone

---
