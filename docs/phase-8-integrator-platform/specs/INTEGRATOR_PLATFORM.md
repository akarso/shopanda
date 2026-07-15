# Integrator Platform — Extension Strategy & External Systems

Status: **published** (Phase 8 Track A — PR-800)  
Implementation: **in progress** — sections marked **Phase 8** or **Planned** describe APIs not yet shipped; see [ROADMAP](../ROADMAP.md) for PR mapping.  
Audience: core maintainers, integrators, plugin authors

Related:

- [Phase 8 ROADMAP](../ROADMAP.md)
- [Plugin Authoring Guide](../../../PLUGINS.md)
- [Developer Extension Guide](../../guides/DEVELOPER.md)
- [PLUGIN_COMPOSITION.md](../../guides/PLUGIN_COMPOSITION.md)
- [PRICING_PIPELINE.md](../../phase-1-core/specs/PRICING_PIPELINE.md)
- [FOUNDATION.md](../../phase-1-core/specs/FOUNDATION.md) §2.7 — extensibility without chaos

---

## Document map

| Section | Status |
| --- | --- |
| §2 Design position, §9 Precedence | **Published** — policy in spec; practical guide in [PLUGIN_COMPOSITION.md](../../guides/PLUGIN_COMPOSITION.md) (PR-802) |
| §3 Port catalog | **Partial** — search/cache/queue/payment/media/tax shipped; introspection at `GET /api/v1/admin/extensions/ports` (PR-801); mail/shipping planned |
| §4 Behavioral catalog | **Shipped** — pricing positioning + cart lifecycle hooks + `cart.validate` + reference cart plugin (PR-810–814) |
| §5 Import pipelines | **Shipped** — row hooks, importer wiring, skip/errors, reference CSV remap plugin (PR-820–823) |
| §6 Inbound integration | **Partial** — routes + auth + idempotency shipped (PR-830–832); reference plugin planned (PR-833) |
| §7 Outbound integration | **Partial** — events + queue shipped; sync job registration planned (Track E) |
| §8 Wiring ergonomics | **Planned** (Track F) |
| §10 Validation | **Phase 8 exit criteria** — not yet achievable end-to-end |

---

## 1) Problem

Phase 7 made **custom data and storefront injection** first-class. Real-world integrator work still hits friction:

| Pain (observed in the field) | Today | Risk if unaddressed |
| --- | --- | --- |
| **Price rules & cart modifications** | `RegisterPricingStep` exists but ordering is append-only; one cart hook (`cart.add_item.after`) | Teams patch cart service or duplicate promotion logic |
| **CSV import transforms** | Importers are core-owned; attribute columns pass through fixed validation only | ERP CSV layouts require one-off importer forks |
| **Inbound ERP (SAP, etc.)** | Integration routes, auth, and idempotency conventions shipped (PR-830–832) | Reference inbound plugin still needed for end-to-end demo |
| **Outbound warehouse / PIM** | Events + queue exist; no standard sync registration or client bootstrap | Plugins import `internal/*` or run cron outside the binary |
| **“Replace search/cache/tax”** | Search, cache, queue, payment, media ports exist; tax/shipping/mail do not | Integrators assume Magento-style preferences |

Without explicit seams for **behavior change** and **external system wiring**, the platform fails integrators even when storefront customization works.

---

## 2) Design position

### What we keep (Go-native, explicit)

```text
┌─────────────────────────────────────────────────────────────┐
│  Integrator-owned register_plugins.go (compile-time order)  │
└───────────────────────────┬─────────────────────────────────┘
                            │
        ┌───────────────────┼───────────────────┐
        ▼                   ▼                   ▼
  Infrastructure      Behavioral chains    Integration surface
  ports (1 winner)    (ordered steps)        (routes + jobs)
        │                   │                   │
   search, cache,     pricing, checkout,   public/admin routes,
   queue, payment,    cart hooks, import    import pipelines,
   media, (tax…)      pipelines             outbound sync
```

| Mechanism | Precedence | Use when |
| --- | --- | --- |
| **Infrastructure port** | Config picks one implementation | Replace backend (search engine, cache, media store) |
| **Pipeline / workflow step** | Ordered chain; optional `before:` / `after:` / replace-by-name | Same-request transforms (pricing, checkout, import row) |
| **Hook chain** | Priority integer; lower runs first | Cross-cutting injection (cart validate, compose fields) |
| **Events** | No order guarantee | Async reactions, notifications, fan-out |
| **Extension fields** | Registry + ACL | Durable custom data (Phase 7) |
| **Theme / slots** | Child theme wins; slot order by registration | Presentation (Phase 7) |

### What we reject

| Approach | Why |
| --- | --- |
| Override folders / `di.xml` preferences | Hidden precedence, untestable at compile time |
| Runtime `.so` loading | Operational and security cost (see PR-544) |
| Plugins importing each other | Coupling; use shared context keys and documented field codes |
| Core patches per customer | Violates hexagonal boundaries |

**Guiding line:** Match **Magento-level outcomes** (custom price rules, ERP hooks, PIM sync) via **Shopware-like explicit registration**, not reflection magic.

---

## 3) Port catalog (infrastructure replacement)

Single active implementation per port unless noted.

| Port | Register API | Status | Notes |
| --- | --- | --- | --- |
| Search | `RegisterSearchProvider` | Shipped | postgres / meilisearch drivers |
| Cache | `RegisterCache` | Shipped | none / redis |
| Queue | `RegisterQueue` | Shipped | sync / redis / kafka / sqs |
| Payment | `RegisterPaymentProvider` | Shipped | Multi-provider registry by method |
| Media storage | `RegisterMediaStorage` | Shipped | local / s3 |
| Tax calculation | `RegisterTaxCalculator` | Shipped | Default `RateTableCalculator`; plugin replaces |
| Shipping rate | `RegisterShippingRateProvider` | **Phase 8 stretch** | Zone tables stay core; rating port for carriers |
| Mail sender | `RegisterMailSender` | **Phase 8 stretch** | SMTP default; plugin for SendGrid, etc. |
| Address validation | `RegisterAddressValidator` | Backlog | Optional ERP validation |

Config + `register_plugins.go` choose the winner. Conflicting double registration panics at startup (fail fast).

**Introspection:** `GET /api/v1/admin/extensions/ports` (requires `extensions.read`) returns the catalog with runtime status (`active`, `core_default`, `unconfigured`, `planned`) and implementation types. Built at startup from plugin registration + config (mirrors `cmd/api/providers.go`).

---

## 4) Behavioral extension catalog

### 4.1 Pricing & promotions (highest-frequency pain)

**Primary seam:** pricing pipeline (`PricingContext` in, adjustments out).

| Extension | API | Phase 8 work | Status |
| --- | --- | --- | --- |
| Custom fees / discounts | `RegisterPricingStep` | `before:` / `after:` anchors (PR-810); aliases: `promotions`, `taxes` | Shipped |
| Customer/group context | `PricingContext.Meta` | Document keys (`customer_id`, `store_id`); B2B step as reference | Partial |
| Promotion rule types | Promotion pipeline step | Stretch: plugin registers rule evaluator, not admin UI | Planned |
| Audit trail | `Adjustments[]` on context | Ensure plugins set `Code`, `Description`, `Meta` | Partial |

Pricing runs on **cart recalculate** and **checkout recalculate** — one pipeline, deterministic order.

### 4.2 Cart modifications

Cart mutations today: add / update qty / remove / coupon → `recalculate` → pricing pipeline.

| Hook / step | Purpose | Phase 8 |
| --- | --- | --- |
| `cart.add_item.before` | Validate SKU rules, min qty, B2B assortment | Shipped |
| `cart.add_item.after` | Extension capture, cross-sell meta | Shipped (Phase 7) |
| `cart.update_item.before` | Block quantity changes | Shipped |
| `cart.remove_item.after` | Cleanup extension side effects | Shipped |
| `cart.recalculate.before` | Inject meta into pricing context | Shipped |
| `cart.validate` | Structured errors returned to storefront API | Shipped |

Hooks receive mutable payload (variant, qty, cart snapshot refs by ID). Heavy logic stays in application services; hooks orchestrate.

### 4.3 Checkout

`RegisterCheckoutStep` with anchor positions (`before:create_order`, etc.) — **implement positioning API** matching [CHECKOUT_WORKFLOW.md](../../phase-1-core/specs/CHECKOUT_WORKFLOW.md) (today: append-only).

---

## 5) Import pipelines (CSV → DB)

**Goal:** Integrator transforms row values **before** core persistence without copying `internal/application/importer`.

**Status:** shipped (PR-820–823). Reference plugin: [`plugins/importdemo`](../../../plugins/importdemo).

### Model

```text
CSV row → parse headers → ImportContext{entity, row map} → plugin steps → core persist
```

| Entity importers | CLI today | Pipeline hook |
| --- | --- | --- |
| products / variants | `import:products` | `import.product.row` |
| prices | `import:prices` | `import.price.row` |
| stock | `import:stock` | `import.stock.row` |
| categories | `import:categories` | `import.category.row` |
| customers | `import:customers` | `import.customer.row` |
| attributes | `import:attributes` | `import.attribute.row` |

**ImportContext** (application layer, not domain):

- `Entity string` — e.g. `product`
- `Row map[string]string` — mutable; plugins may add/rename/normalize columns
- `RowIndex int` — 1-based data row for error messages
- `Meta map[string]interface{}` — cross-step scratch (store ID, import job ID)
- `Skip bool` — step may skip row (counted in result)
- `Errors []ImportError` — append-only; row fails if any error after chain

Plugins register during `Init`:

```go
app.Import("acme/erp").RegisterRowHook(extapi.ImportEntityProduct, 100, func(ctx *extapi.ImportRowContext) error {
    // Map ERP column "MATNR" → slug, normalize VAT class, etc.
    return nil
})
```

Core importer calls the chain **after** header validation, **before** repository write (PR-821). Transaction boundaries unchanged (one TX per batch or per row per existing importer).

**Non-goals:** Replace admin CSV upload (CLI-first remains OK); arbitrary file formats in core (plugins can add CLI commands via `RegisterCommand`).

---

## 6) Inbound integration (ERP → Shopanda)

**Goal:** SAP (or any ERP) pushes order status, inventory, or master data via REST without editing `cmd/api/main.go`.

### Surfaces

| Surface | API | Auth |
| --- | --- | --- |
| Public integration routes | `RegisterPublicRoute` | API key / HMAC signature (Phase 8 middleware) |
| Admin integration routes | `RegisterAdminRoute` | RBAC + optional service account |
| Inbound webhooks (existing) | Core webhook admin | Outbound today; inbound uses public routes |

### Conventions (Phase 8)

1. **Route prefix:** `/api/v1/integrations/{plugin}/…` — avoids colliding with storefront REST
2. **Idempotency:** `Idempotency-Key` header → dedupe table; safe retries from ERP when the same logical operation is submitted more than once
3. **HMAC replay protection** (HMAC-authenticated routes only; enforced by integration auth middleware, separate from idempotency):
   - Require a signed **timestamp** and unique **nonce** in the request (headers or canonical signature payload — documented per plugin)
   - Reject requests outside a documented **freshness window** (e.g. ±5 minutes from server time)
   - Reject previously seen **nonce** or **signature** combinations via a **replay store** checked before request processing
   - Idempotency-Key dedupe handles duplicate *business operations*; replay protection handles duplicate *authenticated requests* (including replays of distinct payloads)
4. **Structured errors:** `{ "error": "code", "message": "…", "details": {} }` — ERP-parseable
5. **Audit:** integration writes log plugin name + idempotency key (no secrets)

Reference plugin: accept SAP IDoc-shaped JSON (simplified) → update order status.

---

## 7) Outbound integration (Shopanda → warehouse / PIM)

**Goal:** Plugin queries external GraphQL/REST on schedule or on event, using queue for retries.

### Registration

```go
app.Integration().RegisterSyncJob(integration.SyncJob{
    Name:     "acme.pim.enrich",
    Trigger:  integration.OnEvent("catalog.product.updated"),
    Handler:  syncProductToPIM,
})
app.Integration().RegisterSyncJob(integration.SyncJob{
    Name:     "acme.warehouse.stock",
    Trigger:  integration.Cron("@every 5m"),
    Handler:  pullStockLevels,
})
```

- Handler receives `Bootstrap.DB`, plugin config, structured logger
- Uses **`pkg/integrationsdk/http`** or **`graphql`** thin wrappers (stdlib-first; no heavy client framework in core)
- Failures → queue retry with backoff (reuse existing queue port)

### Reference patterns

| Pattern | Example |
| --- | --- |
| PIM GraphQL enrich PDP | `RegisterCompositionStep("pdp", …)` + cached external fetch |
| Warehouse stock pull | Sync job → upsert via stock repo port |
| ERP order export | `order.created` event → async POST to ERP |

Plugins **must not** import `internal/infrastructure/postgres` — use application services or narrow ports exposed to plugins (Phase 8 documents allowed boundaries).

---

## 8) Wiring ergonomics

| Feature | Purpose |
| --- | --- |
| **Registration report** | At startup: log/CLI dump of ports, pipeline steps, hooks, routes, sync jobs |
| **Plugin SDK** | Typed helpers: `sdk.Pricing.Register(step, sdk.After("promotions"))` |
| **Replace-by-name** | One step named `tax` replaces core default instead of stacking duplicates |
| **depends_on** (config) | Optional init order when plugin B registers fields/hooks plugin A defines |

Integrator owns **`cmd/api/register_plugins.go`** ordering for init; runtime handler order comes from pipeline/hook priority APIs.

---

## 9) Multi-team precedence

When two plugins extend the same seam, resolution is **explicit** — no preference XML.

**Practical guide:** [Multi-Plugin Composition §Multi-team precedence](../../guides/PLUGIN_COMPOSITION.md#multi-team-precedence) (PR-802).

| Seam type | Resolution |
| --- | --- |
| Infrastructure port | First registration wins → panic on second (today). Config selects plugin set at compile time. |
| Pipeline step | Explicit priority / `before:` / `after:`; document in plugin README |
| Hook chain | Lower priority number runs first |
| Import row hook | Same as hook chain |
| Theme / slot | Child theme overrides parent; slot renderers ordered by registration |
| Conflicting business rules | Integrator merges plugins or adjusts priority — no core “preference XML” |

---

## 10) Validation (acceptance)

An integrator **without core fork** can:

1. Add a **pricing step** that applies a custom cart rule, positioned after promotions
2. **Reject cart add** with a structured error via `cart.validate`
3. **Remap CSV columns** on product import before DB write
4. Expose **`POST /api/v1/integrations/acme/sap/order-status`** with API-key auth and idempotent handling
5. Register a **sync job** that queries an external GraphQL PIM and enriches PDP responses
6. **Replace tax calculation** via port registration
7. Run **`./app plugins report`** (or equivalent) and see all registered extension points

---

## 11) Relationship to Phase 7 backlog

These remain valid but are **not** Phase 8 blockers:

- Admin registry UI for extension fields
- Variant/customer extension scopes
- Runtime `.so` loading

Phase 8 **supersedes** the informal “Plugin SDK” backlog item with a scoped SDK focused on integrator seams (import, integration, pricing position).
