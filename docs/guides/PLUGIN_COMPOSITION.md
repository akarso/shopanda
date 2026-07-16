# Multi-Plugin Composition

How multiple plugins combine behavior without inheriting core classes or calling each other directly.

Audience: plugin authors, integrators  
Status: design guide (Phase 7 hooks/slots/extensions shipped; Phase 8 cart/import/integration patterns documented — some APIs still planned)

Related:

- [Plugin Authoring Guide](../../PLUGINS.md)
- [Developer Extension Guide](DEVELOPER.md)
- [Integrator Platform spec](../phase-8-integrator-platform/specs/INTEGRATOR_PLATFORM.md) — architecture, port catalog, acceptance criteria
- [Customization Platform spec](../phase-6-merchant-complete/specs/CUSTOMIZATION_PLATFORM.md)
- [Extension API policy](EXTENSION_API.md)

---

## Core rule

**Chain handlers, not plugins.**

Plugins are independent registration units. Composition happens inside **ordered pipelines, workflows, and hook chains** via a shared context object.

Do **not**:

- import another plugin's internal packages
- build a runtime plugin dependency graph
- use events for synchronous "run B after A in this request"

Do:

- register steps/handlers on a named pipeline or hook
- declare explicit ordering (`priority`, `before:`, `after:`)
- share data through typed context, extension field codes, or documented meta keys

---

## Three composition layers

| Layer | Ordering | Shared state | Use when |
| --- | --- | --- | --- |
| **Pipelines / workflows** | Deterministic, sequential | Typed context (`PricingContext`, `checkout.Context`, `ProductContext`) | Same request; step B needs step A's output |
| **Extension fields** | Lifecycle rules (define → store → snapshot) | Persisted values on entities | Data survives across requests (cart → order) |
| **Events** | No order guarantee | Event payload only | Side effects, notifications, async work |

```text
Same HTTP request?  ──yes──> pipeline or hook chain
        │
        no
        │
        └── Data must persist? ──yes──> extension fields + snapshot policy
                        │
                        no
                        └── events (async reactions)
```

---

## What exists today

### Pipelines and workflows

Plugins register steps during `Init`:

- `RegisterPricingStep` — fees, discounts, adjustments
- `RegisterCheckoutStep` — validation or side effects during checkout
- `RegisterCompositionStep("pdp"|"plp", …)` — enrich API/storefront responses

Each step receives a **mutable context**. Plugin steps use `RegisterPricingStep(step, position...)` with `before:<step>` / `after:<step>` (aliases: `promotions`, `taxes`). Default position is `after:base`. Checkout step positioning is still append-only (planned).

### Hooks (Phase 7)

Dynamic hook registry is **shipped**. Plugins register during `Init` via `app.Hooks("<registrant>")`:

```go
app.Hooks("acme/rules").Register(extapi.HookCartAddItemAfter, 100, func(hctx *extapi.HookContext) error {
    // read/write hctx.Payload; lower priority runs first
    return nil
})
```

Stable v0 hook points: see [`pkg/extapi`](../../pkg/extapi) and `GET /api/v1/admin/extensions/hooks`. Cart lifecycle hooks (`cart.add_item.before`, `cart.validate`, …) ship in Phase 8 Track B.

Use hooks when the extension point is not already a first-class pipeline (e.g. reacting after add-to-cart, composing checkout fields in one render pass).

### Extension fields, slots, assets (Phase 7)

- **Extension fields** — `app.Extensions().RegisterField(...)`; values via REST/GraphQL; cart → order snapshot at checkout
- **Slots** — `app.Slots("<registrant>").RegisterRenderer(anchor, placement, priority, fn)`
- **Assets** — `app.Assets().RegisterManifest(...)` for route-gated CSS/JS

Catalog endpoints: `GET /api/v1/admin/extensions/fields`, `/hooks`, `/slots`. Infrastructure ports: `GET /api/v1/admin/extensions/ports` (PR-801).

### Plugin init order

`InitAll` runs plugins in compile-time registration order (`cmd/api/register_plugins.go`). That order affects **who registers what at startup** (field definitions, permissions, routes). It does **not** define runtime handler sequence.

Optional future enhancement: `depends_on` in plugin config to guarantee init order when plugin B registers handlers that assume plugin A's field definitions exist. Still not a runtime chain.

Checkout and pricing contexts support cross-step data via `Meta` maps — the B2B group-price step reads `customer_id` from `PricingContext.Meta` without importing another plugin.

---

## Integrator task → mechanism

Use this table before writing code. Full design rationale: [Integrator Platform spec §2–§7](../phase-8-integrator-platform/specs/INTEGRATOR_PLATFORM.md).

| Task | Mechanism | Ordering | Status |
| --- | --- | --- | --- |
| Custom fee / cart price rule | `RegisterPricingStep` | `before:` / `after:` anchors (PR-810) | Shipped |
| Block or validate cart mutation | Cart hook chain (`cart.add_item.before`, `cart.validate`, …) | Lower priority runs first | Shipped |
| Custom checkout validation | `RegisterCheckoutStep` | Anchor positions planned | Shipped (append-only) |
| ERP CSV column remap before DB write | Import row hook (`import.product.row`, …) | Lower priority runs first | Shipped (PR-820–823) |
| SAP / ERP inbound REST callback | `app.Integration(slug).RegisterSecureRoute` | Route per plugin under `/api/v1/integrations/{plugin}/…` | Shipped (PR-830–832) |
| Warehouse / PIM outbound sync | `app.Integration(slug).RegisterSyncJob` | Cron or event → queue retry | Shipped (PR-840) |
| Replace search / cache / tax backend | Infrastructure port (`RegisterSearchProvider`, `RegisterTaxCalculator`, …) | Config picks one winner | Partial (tax shipped PR-813; mail/shipping planned) |
| Enrich PDP from external PIM | `RegisterCompositionStep("pdp", …)` + cached fetch | Pipeline order | Shipped |
| Durable custom line data | Extension field on `cart_item` → snapshot `order_item` | Registry + ACL | Shipped (Phase 7) |
| Notify after order placed | `Bus.OnAsync("order.created", …)` | No order guarantee | Shipped |

**Rule:** Same HTTP request → pipeline or hook chain. Cross-request durable data → extension fields. Side effects / fan-out → events or sync jobs.

---

## Cart and pricing composition

Cart mutations follow a fixed core flow: **mutate cart → recalculate → pricing pipeline → persist** (add-item also runs extension upsert before persist when values are supplied). On add, the shipped `cart.add_item.after` hook runs **after persist**. Plugins extend this without patching `cart.Service`.

```text
add item
  └─ [shipped] cart.add_item.before hook
  └─ core mutation (in memory)
  └─ recalculate
       └─ [shipped] cart.recalculate.before → inject PricingContext.Meta via pricing_meta map
       └─ pricing pipeline (core steps + RegisterPricingStep)
  └─ [shipped] cart.validate → structured validation_errors (blocks persist on error-level issues)
  └─ extension value upsert (when provided)
  └─ persist
  └─ [shipped] cart.add_item.after hook

update item quantity
  └─ [shipped] cart.update_item.before hook
  └─ core mutation (in memory)
  └─ recalculate
       └─ [shipped] cart.recalculate.before
       └─ pricing pipeline
  └─ persist

remove item
  └─ core mutation (in memory)
  └─ recalculate
       └─ [shipped] cart.recalculate.before
       └─ pricing pipeline
  └─ persist
  └─ [shipped] cart.remove_item.after hook

apply / remove coupon
  └─ core mutation (in memory)
  └─ recalculate
       └─ [shipped] cart.recalculate.before
       └─ pricing pipeline
  └─ persist
```

### Pattern: custom price rule (two plugins)

| Plugin | Mechanism | Priority / position |
| --- | --- | --- |
| `acme/volume-discount` | `RegisterPricingStep` — quantity tiers | `after:promotions` |
| `acme/handling-fee` | `RegisterPricingStep` — flat fee | `after:promotions` or `before:tax` |

Both steps read `PricingContext` and append `Adjustments`. They must **not** import each other — share context via `PricingContext.Meta` keys documented in README (e.g. `acme.assortment_tier`).

### Pattern: block add-to-cart (shipped)

Register validation on `cart.add_item.before` that returns an error to stop the mutation:

```go
app.Hooks("acme/assortment").Register(extapi.HookCartAddItemBefore, 100, func(hctx *extapi.HookContext) error {
    // read variant_id, quantity from hctx.Payload; return error to block
    return nil
})
```

For update/remove, use `HookCartUpdateItemBefore` and `HookCartRemoveItemAfter` respectively.

### Pattern: structured cart validation (shipped)

Register `cart.validate` to append machine-readable issues. Error-level issues block mutations (HTTP 422); `level: "warning"` issues are returned on successful reads and mutations without blocking.

Reference implementation: [`plugins/cartdemo`](../../plugins/cartdemo) (PR-814).

```go
app.Hooks("acme/assortment").Register(extapi.HookCartValidate, 100, func(hctx *extapi.HookContext) error {
    cart, _ := hctx.Get("cart")
    // inspect cart snapshot; append issues (do not return business errors from handler)
    hctx.AppendValidationError(extapi.CartValidationIssue{
        Code:    "acme.min_qty",
        Message: "minimum quantity is 5 per line",
    })
    return nil
})
```

Storefront cart responses include `data.validation_errors` alongside `data.cart`.

### Pattern: capture side effect after add (shipped)

`cart.add_item.after` runs after successful add, recalculate, and persist — use for post-add side effects (extension capture, cross-sell meta). The hook receives the saved cart in payload (`cart` key).

```go
app.Hooks("acme/engraving").Register(extapi.HookCartAddItemAfter, 100, handler)
```

Inspect active handlers: `GET /api/v1/admin/extensions/hooks`.

---

## Import composition (Track C)

**Status:** shipped (PR-820–823). Reference implementation: [`plugins/importdemo`](../../plugins/importdemo).

**Problem:** ERP CSV files use foreign column names; forking `internal/application/importer` breaks on upgrades.

**Model:** one ordered row hook chain per entity, invoked after header validation and **before** repository write.

```text
CLI import:products file.csv
  └─ parse headers
  └─ for each row:
       └─ ImportContext{Entity, Row map, RowIndex, Meta}
       └─ plugin row hooks (priority order)
       └─ core persist (or skip row / collect errors)
```

| Entity | CLI | Hook name |
| --- | --- | --- |
| Products | `import:products` | `import.product.row` |
| Prices | `import:prices` | `import.price.row` |
| Stock | `import:stock` | `import.stock.row` |
| Categories | `import:categories` | `import.category.row` |
| Customers | `import:customers` | `import.customer.row` |
| Attributes | `import:attributes` | `import.attribute.row` |

**Plugin registration (shipped PR-820):**

```go
app.Import("acme/erp").RegisterRowHook(extapi.ImportEntityProduct, 100, func(ctx *extapi.ImportRowContext) error {
    ctx.Row["sku"] = ctx.Row["MATNR"]
    return nil
})
```

**Composition rules:**

- Handlers mutate `Row map[string]string` in place — remap ERP columns to core columns (`MATNR` → `sku`).
- Use `Meta` for job-scoped scratch (store ID, import batch ID); do not store row data only in Meta.
- Lower priority runs first; later hooks see earlier normalizations.
- Return error to fail the row immediately; use `AppendError(code, msg)` to collect validation issues (row fails after chain)
- Use `SkipRow()` to skip a row without recording an error (PR-822)
- **Do not** register competing importers — extend the chain.

**Multi-plugin example:** Plugin A maps column names (priority 100); Plugin B validates attribute enums (priority 200); Plugin C enriches from external ID lookup via Meta cache (priority 300).

---

## Integration composition

### Inbound (ERP → Shopanda)

**Today:** `app.Integration("acme").RegisterSecureRoute(...)` mounts authenticated handlers under `/api/v1/integrations/acme/…` (PR-830–833). Use `integrationhttp.AuthConfig` with API key and optional HMAC secret from plugin config. Mutating requests with `Idempotency-Key` are deduplicated automatically when the API wires the Postgres idempotency store. See `plugins/integrationdemo` for a reference order-status callback.

| Concern | Composition approach |
| --- | --- |
| Route namespace | Prefix `/api/v1/integrations/acme/…` — one plugin owns its tree |
| Duplicate POST retries | Idempotency store keyed by `Idempotency-Key` |
| Replay attacks on signed requests | HMAC middleware: timestamp + nonce + replay store |
| Business logic | Thin handler → application service; no direct `postgres` imports from `plugins/*` |

Multiple ERP plugins each register **disjoint route prefixes** — no shared handler chain. Conflicts on the same pattern fail at startup (duplicate route registration).

### Outbound (Shopanda → warehouse / PIM)

**Today:** `Bus.OnAsync` + queue port for retries; `RegisterCompositionStep` for read-path enrichment; `app.Integration(slug).RegisterSyncJob(...)` for cron/event outbound sync (PR-840); `pkg/integrationsdk/http` and `pkg/integrationsdk/graphql` for outbound REST/GraphQL clients (PR-841); reference warehouse stock plugin (`plugins/warehousedemo`, PR-842). Cron fires from the scheduler process; events enqueue from the API server; the worker executes jobs with queue retry.

**Planned (Track E):** PIM GraphQL PDP reference plugin (PR-843).

| Pattern | Mechanism | Ordering |
| --- | --- | --- |
| Push order to ERP on create | `order.created` event → async HTTP POST | Events unordered — handler must be idempotent |
| Pull stock every 5m | `RegisterSyncJob` + `extapi.Cron("@every 5m")` | One job per plugin registration |
| Enrich PDP from PIM GraphQL | `RegisterCompositionStep("pdp", …)` | Pipeline order; cache externally |

Outbound plugins **must not** import each other's clients. Share lookup tables via extension fields or documented DB views exposed through application services — not cross-plugin Go imports.

---

## Multi-team precedence

When two teams (or plugins) extend the **same seam**, resolution is explicit — there is no Magento-style preference XML.

| Seam type | Who wins | Integrator action |
| --- | --- | --- |
| **Infrastructure port** | One implementation per port; second `Register*` panics at startup | Pick driver in config; enable one core plugin per slot. Inspect: `GET /api/v1/admin/extensions/ports` |
| **Pricing / checkout step** | Ordered chain; first core steps then plugin steps | Set priority / `before:`/`after:` when API ships; document order in project README |
| **Hook chain** | Lower priority number runs first | Assign non-overlapping priority bands per team (e.g. 100–199 team A, 200–299 team B) |
| **Import row hook** | Lower priority runs first (PR-820) | Same priority band discipline |
| **Extension field** | Field codes are global; first registration wins | Namespace codes (`vendor.feature.field`); coordinate via registry API |
| **Slot renderer** | Registration order within placement | Priority integer on `RegisterRenderer` |
| **Theme** | Child theme overrides parent templates | Slot markers preserved by convention — see [THEME_SLOTS.md](THEME_SLOTS.md) |
| **Public/admin route** | First registered pattern wins; duplicate panics | Use `/api/v1/integrations/{plugin}/…` prefix per vendor |
| **Conflicting business rules** | No automatic merge | Integrator adjusts priorities or disables one plugin |

**Compile-time vs runtime:** `register_plugins.go` order affects **Init** (who registers fields, routes, handlers). It does **not** assign hook priorities. For **append-only pipelines** (pricing, checkout today), the order each plugin calls `RegisterPricingStep` / `RegisterCheckoutStep` during Init is the runtime execution order for plugin steps. For **hooks** (and pipelines once positioning ships), use explicit **priority** or `before:`/`after:` APIs — lower hook priority runs first.

**Fail fast:** Double infrastructure registration and duplicate HTTP patterns panic at startup rather than silently overriding.

---

## Reference pattern: checkout custom field

**Scenario:** Plugin `acme/gift-wrap` adds a "Gift wrap" field to checkout. Plugin `acme/loyalty` adds a "Loyalty message" field that only appears when gift wrap is selected.

### Split the problem

| Concern | Mechanism |
| --- | --- |
| Field definition + stored value on cart line | Extension field `acme.gift.wrap_level` on `cart_item`, snapshot to `order_item` |
| Compose fields for one checkout render | Hook chain `checkout.fields.compose` (proposed) |
| Validate before order creation | `RegisterCheckoutStep` positioned `before:create_order` |
| React after order exists | `order.created` event (async) |

### Step 1 — Plugin A defines the durable contract

During `Init`, register the field:

```go
app.Extensions().RegisterField(domainext.FieldDef{
    Code:        "acme.gift.wrap_level",
    Label:       "Gift wrap",
    Type:        "enum",
    Scope:       "cart_item",
    StorageMode: "snapshot",
    Visibility:  "public",
    // ...
})
```

The **field code** is the public contract. Plugin B depends on `acme.gift.wrap_level`, not on plugin A's Go package.

### Step 2 — Plugin A adds the checkout UI field (same request)

Register a hook handler when `checkout.fields.compose` ships, or use extension fields on the cart/checkout API today:

```go
// Planned hook (not yet in extapi v0):
app.Hooks("acme/gift-wrap").Register("checkout.fields.compose", 100, func(hctx *extapi.HookContext) error {
    // mutate hctx.Payload["fields"]
    return nil
})
```

Priority `100` runs before plugin B's handler at `200`.

### Step 3 — Plugin B reads A's output from shared context

```go
// Planned hook (not yet in extapi v0):
app.Hooks("acme/loyalty").Register("checkout.fields.compose", 200, func(hctx *extapi.HookContext) error {
    wrapLevel := currentWrapLevel(hctx) // from cart_item extension value
    if wrapLevel == "none" || wrapLevel == "" {
        return nil // fail open when upstream contract absent
    }
    // append loyalty field to hctx.Payload["fields"]
    return nil
})
```

Plugin B **reads** composed state from `hctx.Payload`; it never calls plugin A.

### Step 4 — Persist selection on the cart line

When the customer submits checkout (or earlier on add-to-cart), write the value through the extension service:

```go
app.Extensions().SetValues(extensions.Target{Type: "cart_item", ID: lineID}, []extensions.Value{
    {FieldCode: "acme.gift.wrap_level", StringValue: "premium"},
})
```

### Step 5 — Validate in the checkout workflow

```go
app.RegisterCheckoutStep(NewValidateGiftWrapStep(app.Extensions()), "before:create_order")
```

The step reads extension values from cart items. Failure returns a validation error and stops the workflow — same pattern as core `validate_cart`.

### Step 6 — Snapshot happens in core

At `create_order`, core copies `cart_item` extension values with `storage_mode: snapshot` onto `order_item`. Neither plugin patches order creation.

### End-to-end flow

```text
PDP / cart
  └─ customer selects gift wrap → extension value on cart_item

Checkout page render
  └─ hook checkout.fields.compose
       ├─ [100] acme/gift-wrap: append gift wrap field
       └─ [200] acme/loyalty: append message field if wrap != none

Checkout submit
  └─ workflow
       ├─ validate_cart
       ├─ recalculate_pricing
       ├─ …
       ├─ [before:create_order] acme/gift-wrap: validate enum + pricing side effects
       ├─ create_order (snapshots acme.gift.wrap_level → order_item)
       └─ initiate_payment

After order
  └─ order.created event → acme/loyalty sends notification (async)
```

---

## Ordering handlers

Use one of these (implement across pipelines and hooks consistently):

| Style | Example | When |
| --- | --- | --- |
| **Numeric priority** | `100`, `200` | Many plugins; coarse ordering |
| **Anchor position** | `before:create_order`, `after:recalculate_pricing` | Insert relative to core steps |
| **Named handler** | `after:acme.gift_wrap_field` | Plugin B explicitly follows plugin A's handler |

Checkout workflow spec already documents anchor positions; `RegisterCheckoutStep` should gain the same API as the spec (today: append-only).

---

## Plugin dependencies (init only)

| Approach | Coupling | Recommendation |
| --- | --- | --- |
| Go import between plugins | Tight | Avoid for third-party plugins |
| `depends_on` in config | Loose (startup only) | Optional; ensures field defs exist before dependent `Init` |
| Contract (field codes, hook names, meta keys) | Loosest | **Preferred** for runtime behavior |

Example config (hypothetical):

```yaml
plugins:
  acme.loyalty:
    enabled: true
    depends_on: [acme.gift-wrap]  # init order only
```

Runtime behavior still relies on field codes and hook priorities, not on `depends_on`.

---

## PHP / Magento mapping

| PHP instinct | Shopanda equivalent |
| --- | --- |
| Extend `CheckoutController` | `RegisterCheckoutStep` or hook on `checkout.fields.compose` |
| Override `getCheckoutFields()` | Hook chain mutating shared `Payload["fields"]` |
| `$this->setData()` for child blocks | `ctx.Meta` / `HookContext.Payload` |
| Plugin `sequence` / `sortOrder` | Handler priority or `before:`/`after:` |
| Observer (unordered) | `Bus.OnAsync` — not for synchronous chaining |

Go favors **composition through shared context**, not class inheritance.

---

## Checklist for plugin authors

1. Publish a **contract** (field codes, hook names, Meta keys) in your plugin README.
2. Pick the **mechanism** from [Integrator task → mechanism](#integrator-task--mechanism) — do not fork core importers or cart service.
3. Register **handlers** on the right layer (pipeline, hook, port, route, or event).
4. Set explicit **ordering** when your handler must run before/after another (priority bands for multi-team projects).
5. Read sibling output from **shared context or extension values**, not from another plugin's package.
6. **Fail open** when an optional upstream contract is absent (e.g. loyalty field skipped if gift wrap plugin disabled).
7. Use **events** and **sync jobs** only for post-factum or async work, not intra-request sequencing.
8. Inspect registrations: `/api/v1/admin/extensions/{hooks,slots,fields,ports}`.
