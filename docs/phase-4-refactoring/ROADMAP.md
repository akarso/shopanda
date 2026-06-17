# Phase 4 — Product Complete

## Strategy

* Close the gap between **a hardened commerce engine** (Phases 1–3) and a **drop-in, merchant-operable product** with a clear three-tier extension model
* Prefer **vertical slices** that replace admin placeholders with working UI over existing APIs before adding new domain concepts
* Keep **Postgres-first defaults**: optional services (Meilisearch, Redis, RabbitMQ, Stripe, S3) stay off until explicitly enabled
* One PR = one responsibility; each PR must be runnable and reviewable in ~10–20 minutes
* Full PR specs live under `prs/` (PR-400–434)

---

## Starting Point (verified after Phase 3)

Phases 1–2 delivered the engine, storefront SSR, admin shell, integrations, and Postgres-backed queue/cache/search. Phase 3 (Tracks 1–4, PR-300–399) hardened guest checkout, admin UX, scoped editing, and customer account/security flows.

**What works end-to-end today**

| Area | Status |
| --- | --- |
| Storefront | Catalog, search, cart, checkout (guest + account), CMS pages, full account area |
| Admin | Products, orders, customers, pages, media, stores, shipping/payment settings, dashboard |
| Backend | Pricing pipeline (incl. promotions/coupons in cart), inventory reservations, invoicing, notifications, multi-store, translations |
| Ops | `install.sh`, `app setup`, migrate/seed, CSV import/export, Docker Compose (app + postgres) |

**Known gaps motivating Phase 4**

| Gap | Impact |
| --- | --- |
| Admin placeholders | Promotions, coupons, attributes, inventory screens show "coming soon" while backend exists |
| CLI-first workflows | Bulk prices/categories still often CSV/CLI; merchants need more in-admin coverage |
| Plugin model drift | Registry exists but registers **zero** plugins; Stripe/Meili/S3 wired in `main.go` as infrastructure |
| Missing optional drivers | Redis cache/queue and RabbitMQ queue documented but not implemented |
| Runtime friction | Emails require `worker`; Compose runs `serve` only; no dev single-process mode |
| Headless parity | REST guest checkout deferred; SSR is the validated path |

---

## Target Architecture (three tiers)

```text
┌─────────────────────────────────────────────────────────────┐
│  Core (always on)                                           │
│  Postgres · job queue · cache · FTS search · SMTP mail      │
│  Domain · application · SSR storefront · admin shell        │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│  Core plugins (shipped in repo, disabled by default)        │
│  meilisearch · redis-cache · redis-queue · rabbitmq-queue   │
│  stripe · s3-storage · …                                    │
│  Enabled via config/env; register through plugin registry   │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│  External plugins (author-owned, compile-time register)     │
│  Custom pipeline/checkout steps · events · permissions      │
│  Example package + docs; no core modification               │
└─────────────────────────────────────────────────────────────┘
```

Phase 4 Track B moves today's `internal/infrastructure/*` adapters toward **core plugins** without changing default behavior.

---

## Runtime Modes

Background jobs and email delivery require a **worker**; recurring tasks require a **scheduler**. See [`specs/RUNTIME_MODES.md`](specs/RUNTIME_MODES.md) for the full dev vs production contract.

| Mode | Processes | When |
| --- | --- | --- |
| **Development** | Single process (`app dev` — PR-430) or `serve` + `worker` | Local work, CI |
| **Production** | `serve` + `worker` + `scheduler` (separate services, same image) | Staging, live stores |

**PR-431** updates Docker Compose and deployment docs so production layout is the documented default; development stays one-command after PR-430.

---

## Tracks

| Track | Goal | PR range | Delivers |
| --- | --- | --- | --- |
| **A** | Merchant-complete admin | PR-400–405 | Replace high-impact admin placeholders; reduce CLI-only merchant workflows |
| **B** | Core plugin packaging | PR-410–416 | Config-gated core plugins; registry wiring; Redis + RabbitMQ optional backends |
| **C** | External plugin ergonomics | PR-420–422 | Example plugin, three-tier docs, extension contract clarity |
| **D** | Runtime & platform polish | PR-430–434 | Dev/prod process model, headless guest parity, auth email normalization, optional audit persistence |

Recommended execution order: **D (430–431) early** so local dev and deploy docs match reality, then **A** in parallel with **B**, then **C**, then remaining **D**.

---

## Track A — Merchant-Complete Admin Surfaces

**Goal:** A merchant can run day-to-day catalog, pricing, stock, and simple promotions from the admin UI without shell access. No customer groups, no advanced promotion DSL, no returns/transactions/navigation/blocks CMS.

| PR | Title | Short description |
| --- | --- | --- |
| PR-400 | Admin coupons CRUD | List/create/edit/delete coupons over existing `CouponRepository` and admin API; wire `/admin/marketing/coupons` off placeholder |
| PR-401 | Admin promotions CRUD | List/create/edit/delete promotions for **simple rules** (fixed/percentage off, optional date range, optional min cart); wire `/admin/marketing/promotions`; no new rule engine |
| PR-402 | Admin attributes UI | Attribute and attribute-group management over existing catalog attribute registry; wire `/admin/catalog/attributes`; align with product form usage |
| PR-403 | Admin inventory visibility | Stock list per SKU with search/filter and single-SKU adjust; wire `/admin/operations/inventory` over `stock` repo; read reservation context where useful |
| PR-404 | Scoped price editing in product admin | Edit variant prices in the product edit screen (store/currency scope via context switcher); reduces `app import:prices` for routine changes |
| PR-405 | Phase 3 doc closure | Archive stale "Remaining PRs" table in `phase-3-testing/ROADMAP.md`; add cross-links from README to Phase 4 |

**Out of scope for Track A:** Customer groups, returns, payment transactions ledger UI, navigation builder, content blocks, complex cart-rule builder, admin category tree rewrite (create route already exists).

---

## Track B — Core Plugin Packaging

**Goal:** Optional integrations register through the plugin registry and activate from config — same behavior as today, clearer packaging. Defaults unchanged (Postgres search, Postgres cache, manual/flat-rate pay, local storage).

| PR | Title | Short description |
| --- | --- | --- |
| PR-410 | Core plugin enablement contract | Config schema for `plugins.core.*` enabled flags; `main.go` registers core plugin packages; failed init disables plugin, logs, continues |
| PR-411 | Search core plugin (Meilisearch) | Extract Meilisearch wiring from `main.go` into `plugins/core/meilisearch`; enable when `search.engine=meilisearch` |
| PR-412 | Payment core plugins (Stripe, manual) | Extract Stripe + manual pay registration into `plugins/core/stripe` and `plugins/core/manualpay`; preserve env-gated Stripe behavior |
| PR-413 | Storage core plugins (local, S3) | Extract `localfs` / `s3store` selection into `plugins/core/storage-*`; config-driven media backend |
| PR-414 | Redis cache driver + core plugin | Implement `cache.Cache` Redis backend; core plugin registers when `cache.driver=redis`; Postgres remains default |
| PR-415 | Redis queue driver + core plugin | Implement `jobs.Queue` Redis backend; core plugin registers when `queue.driver=redis`; Postgres queue remains default |
| PR-416 | RabbitMQ queue driver + core plugin | Implement `jobs.Queue` AMQP backend; core plugin registers when `queue.driver=rabbitmq`; Docker `queue` profile; Postgres queue remains default |

**Note:** Postgres job queue remains the default. Redis and RabbitMQ are alternative **core plugins** — enable one via `queue.driver`, never more than one active backend.

---

## Track C — External Plugin Ergonomics

**Goal:** A third-party or in-house author can add behavior without editing core — via compile-time plugin package and documented registration.

| PR | Title | Short description |
| --- | --- | --- |
| PR-420 | Example external plugin | `plugins/example/` with pricing step, event listener, permission; registered in `main.go` behind `plugins.example.enabled` (default off) |
| PR-421 | Three-tier plugin docs | Align `README.md`, `DEVELOPER.md`, `PLUGINS.md`, and C4 diagram with core / core-plugin / external model; honest list of what still requires `main.go` wiring |
| PR-422 | Plugin config registration (stretch) | `RegisterConfig` usage in example plugin; admin integrations page surfaces plugin-defined settings — only if PR-420/421 expose a clear need |

**Out of scope:** Dynamic `.so` loading, plugin marketplace, CLI commands registered by plugins (document as future).

---

## Track D — Runtime & Platform Polish

**Goal:** Frictionless local dev, explicit production layout, and selected parity fixes. See [`specs/RUNTIME_MODES.md`](specs/RUNTIME_MODES.md).

| PR | Title | Short description |
| --- | --- | --- |
| PR-430 | Dev single-process mode | `app dev` (or `serve --with-worker`) runs HTTP + embedded worker in one process; optional embedded scheduler; documented in RUNTIME_MODES |
| PR-431 | Production compose & deploy layout | Add `worker` and `scheduler` services to `docker-compose.yml`; update `DEPLOYMENT.md` and `MERCHANT.md` with "emails require worker" prominently |
| PR-432 | REST guest checkout parity | Headless `POST /api/v1/checkout` guest path aligned with SSR guest checkout (contact email, no forced registration); closes Phase 3 deferral |
| PR-433 | Auth email normalization | Lowercase/trim email at register, login, password reset, and `FindByEmail` lookups; prevents case-variant duplicates (cross-cutting, deferred from PR-399 review) |
| PR-434 | Persistent audit log (stretch) | `admin_audit_log` table + write path from `Auditor`; read-only admin list; logger remains fallback; skip if timeboxed |

---

## Explicitly Deferred (beyond Phase 4)

* Customer groups, returns workflow, transaction ledger admin
* Navigation builder, content blocks, marketing automation
* Login-time TOTP / authenticator apps
* Admin user/role CRUD beyond read-only Users & Roles
* Store Management menu reshuffle (stores already under Settings + Store routes)
* Other message brokers beyond RabbitMQ (e.g. Kafka, SQS)
* Plugin `.so` hot-loading
* Complex promotion rule builder (tiered discounts, buy-X-get-Y, customer segments)

---

## Validation Target — "Product Complete"

When Phase 4 ships, the following should be true:

### Merchant (no shell required for routine work)

- Create products, variants, media, simple promotions and coupons from admin
- Adjust stock and scoped prices from admin
- Configure shipping, payments, stores, languages, currencies from admin
- Process orders from admin

### Customer

- Browse, search, cart, checkout as guest or account holder
- Receive order and account emails when SMTP + worker are configured

### Developer / operator

- `./install.sh` → `app setup` → `app dev` → working store with emails in Mailpit
- Production deploy documented as three processes (or compose services) from one image
- Enable Meilisearch, Redis, or RabbitMQ by config + compose profile without code changes
- Author an external plugin from the example package without touching domain core

### Architecture

- Core plugins register via registry; `main.go` shrinks to bootstrap + `registry.Register` list
- External extension path is documented and exemplified

---

## PR Index (quick reference)

| PR | Track | One-liner |
| --- | --- | --- |
| 400 | A | Coupons admin CRUD |
| 401 | A | Promotions admin CRUD (simple rules) |
| 402 | A | Attributes admin UI |
| 403 | A | Inventory list + adjust |
| 404 | A | Scoped prices on product edit |
| 405 | A | Phase 3 roadmap doc closure |
| 410 | B | Core plugin enablement contract |
| 411 | B | Meilisearch core plugin |
| 412 | B | Stripe/manual payment core plugins |
| 413 | B | Local/S3 storage core plugins |
| 414 | B | Redis cache driver + plugin |
| 415 | B | Redis queue driver + core plugin |
| 416 | B | RabbitMQ queue driver + core plugin |
| 420 | C | Example external plugin |
| 421 | C | Three-tier plugin documentation |
| 422 | C | Plugin config in admin (stretch) |
| 430 | D | `app dev` single-process mode |
| 431 | D | Prod compose: worker + scheduler |
| 432 | D | REST guest checkout parity |
| 433 | D | Auth email case normalization |
| 434 | D | Persistent audit table (stretch) |

---

## Implementation Order

Suggested first PRs: **PR-430** (dev ergonomics) and **PR-400** (coupons admin) in parallel. Track B queue plugins (PR-415, PR-416) should land after **PR-410** (enablement contract) and can follow **PR-431** (compose layout) so Docker profiles are in place.

PR specs: [`prs/`](prs/)
