# Multi-Plugin Composition

How multiple plugins combine behavior without inheriting core classes or calling each other directly.

Audience: plugin authors, integrators  
Status: design guide (partially implemented today; hook registry is proposed)

Related:

- [Plugin Authoring Guide](../../PLUGINS.md)
- [Developer Extension Guide](DEVELOPER.md)
- [Customization Platform spec](../phase-6-merchant-complete/specs/CUSTOMIZATION_PLATFORM.md)

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

Each step receives a **mutable context** and runs in registration order (core steps first, then plugin steps appended today).

Checkout context already supports cross-step data:

```go
// internal/application/checkout/context.go
type Context struct {
    CartID, CustomerID, Currency string
    Cart   *cart.Cart
    Input  Input
    Order  *order.Order
    Meta   map[string]interface{} // shared between steps in one run
}
```

Pricing context works the same way — checkout copies relevant meta into `PricingContext.Meta` before running the pricing pipeline. The B2B group-price step reads `customer_id` from meta without importing another plugin.

### Plugin init order

`InitAll` runs plugins in compile-time registration order (`cmd/api/register_plugins.go`). That order affects **who registers what at startup** (field definitions, permissions, routes). It does **not** define runtime handler sequence.

Optional future enhancement: `depends_on` in plugin config to guarantee init order when plugin B registers handlers that assume plugin A's field definitions exist. Still not a runtime chain.

### Proposed: hook registry

The [Customization Platform spec](../phase-6-merchant-complete/specs/CUSTOMIZATION_PLATFORM.md) adds named hooks with ordered handlers and a shared `HookContext`. Use hooks when the extension point is not already a first-class pipeline (e.g. composing checkout form fields in one render pass).

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

During `Init`, register the field (proposed API):

```go
app.Extensions().RegisterField(extensions.FieldDef{
    Code:        "acme.gift.wrap_level",
    Label:       "Gift wrap",
    Type:        "enum",
    Scope:       "cart_item",
    StorageMode: "snapshot", // cart_item → order_item at checkout
    Visibility:  "public",
    Validation:  extensions.EnumOptions("none", "standard", "premium"),
})
```

The **field code** is the public contract. Plugin B depends on `acme.gift.wrap_level`, not on plugin A's Go package.

### Step 2 — Plugin A adds the checkout UI field (same request)

Register a hook handler (proposed):

```go
app.Hooks().Register("checkout.fields.compose", 100, func(hctx *hooks.Context) error {
    fields := hctx.Payload["fields"].([]CheckoutField) // typed in real impl
    fields = append(fields, CheckoutField{
        Code:  "acme.gift.wrap_level",
        Type:  "select",
        Label: "Gift wrap",
        Options: []Option{{"none", "None"}, {"standard", "Standard"}, {"premium", "Premium"}},
    })
    hctx.Payload["fields"] = fields
    return nil
})
```

Priority `100` runs before plugin B's handler at `200`.

### Step 3 — Plugin B reads A's output from shared context

```go
app.Hooks().Register("checkout.fields.compose", 200, func(hctx *hooks.Context) error {
    fields := hctx.Payload["fields"].([]CheckoutField)
    wrapLevel := currentWrapLevel(hctx) // from submitted form or cart_item extension value

    if wrapLevel == "none" || wrapLevel == "" {
        return nil // skip — no dependency failure if A is disabled
    }

    fields = append(fields, CheckoutField{
        Code:  "acme.loyalty.message",
        Type:  "text",
        Label: "Add a loyalty note",
    })
    hctx.Payload["fields"] = fields
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

1. Publish a **contract** (field codes, hook names) in your plugin README.
2. Register **handlers** on the right layer (pipeline, hook, or event).
3. Set explicit **ordering** when your handler must run before/after another.
4. Read sibling output from **shared context or extension values**, not from another plugin's package.
5. **Fail open** when an optional upstream contract is absent (e.g. loyalty field skipped if gift wrap plugin disabled).
6. Use **events** only for post-factum reactions, not intra-request sequencing.
