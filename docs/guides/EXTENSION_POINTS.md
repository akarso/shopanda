# Extension Points — Which Mechanism?

Shopanda has grown five different "run some code at a certain point" mechanisms (events, hooks, the pricing pipeline, the checkout workflow, composition steps) plus close to twenty registries for wiring in infrastructure and metadata. Each one was added for a real reason, but a plugin author facing all of them at once has no single place that says *"you want this one, not that one."* This guide is that place.

Audience: plugin authors, integrators
Status: reference guide (Phase 10 architecture audit follow-up, PR-1026)

Related:

- [Multi-Plugin Composition](PLUGIN_COMPOSITION.md) — how to coordinate *multiple* plugins using these mechanisms (ordering, precedence, task→mechanism table). Read this guide first to pick a mechanism; read that one once you're sharing a seam with another plugin.
- [Plugin Authoring Guide](../../PLUGINS.md) — the full API reference table and infrastructure ports
- [Developer Extension Guide](DEVELOPER.md) — tutorial-style walkthroughs
- [Extension API policy](EXTENSION_API.md) — `pkg/extapi` stability contract

---

## Pick a mechanism

Answer these in order:

```text
Does this run as part of one request/workflow, where step B needs step A's
output right now (same call stack)?
  │
  ├─ no ──> Does it react to something that already happened (an entity was
  │         created/updated/deleted), fire-and-forget, no return value needed?
  │           │
  │           ├─ yes ──> EVENTS (Bus.On / Bus.OnAsync)
  │           │
  │           └─ no ───> you probably want a registry (infra port, config,
  │                      CLI command, admin route — see "Registries" below)
  │
  └─ yes ─> Which request/workflow is it?
              │
              ├─ Cart mutation (add/update/remove item, validate)
              │     ──> HOOKS (app.Hooks(name).Register(...))
              │
              ├─ Price calculation (fee, discount, tax)
              │     ──> PRICING PIPELINE (app.RegisterPricingStep)
              │
              ├─ Checkout (validate, reserve, pay, create order)
              │     ──> CHECKOUT WORKFLOW (app.RegisterCheckoutStep)
              │
              └─ Storefront response shaping (PDP/PLP — SEO, price
                 indication, reviews, compliance blocks)
                    ──> COMPOSITION STEPS (app.RegisterCompositionStep)
```

The four "same request" branches (hooks, pricing pipeline, checkout workflow, composition steps) all look similar — ordered handlers over a mutable context, first error stops the chain — but they are **four separately-typed mechanisms, not one generic system with four call sites**. Picking the wrong one usually still compiles (most take `step any`), so this guide exists because the compiler won't catch the mistake for you.

---

## The four "same request" mechanisms

### Hooks — `internal/application/hooks`

```go
// internal/application/hooks/context.go
type Handler func(ctx *Context) error
type Context struct{ Name string; /* payload, unexported */ }
func (c *Context) Get(key string) (interface{}, bool)
func (c *Context) Set(key string, value interface{})

// internal/application/hooks/registry.go
func (r *Registry) Register(hook string, priority int, registrant string, handler Handler) error
func (r *Registry) Invoke(ctx context.Context, hookCtx *Context) error
```

Plugin-facing (`internal/platform/plugin/hooks.go`, types from `pkg/extapi`):

```go
app.Hooks("acme/assortment").Register(extapi.HookCartValidate, 100, func(hctx *extapi.HookContext) error {
    cart, _ := hctx.Get("cart")
    // inspect / append validation issues; return error to abort the mutation
    return nil
})
```

- **Synchronous, priority-ordered** (lower runs first), against a **mutable shared payload** — later handlers see earlier handlers' writes via `Get`/`Set`.
- A handler can **veto**: returning an error aborts the chain and the triggering request fails.
- Panics inside a handler are recovered and turned into an error — a broken plugin hook can't crash the process, only fail its own chain.
- Fixed set of hook points today, all cart-lifecycle (`HookCartAddItemBefore/After`, `HookCartUpdateItemBefore`, `HookCartRemoveItemAfter`, `HookCartRecalculateBefore`, `HookCartValidate`) — see `pkg/extapi/hooks.go` and `GET /api/v1/admin/extensions/hooks` for the live catalog.
- **Use for:** validating or enriching a cart mutation within the same request, where a plugin needs to read what an earlier plugin already decided.
- **Don't use for:** checkout (there's a dedicated mechanism below), or anything that should keep running after the response is sent (that's an event).

### Pricing pipeline — `internal/domain/pricing`

```go
// internal/domain/pricing/step.go
type PricingStep interface {
    Name() string
    Apply(ctx context.Context, pctx *PricingContext) error
}

// internal/domain/pricing/pipeline.go
type Pipeline struct{ steps []PricingStep }
func NewPipeline(steps ...PricingStep) Pipeline
func (p Pipeline) Execute(ctx context.Context, pctx *PricingContext) error
```

Plugin-facing: `app.RegisterPricingStep(step, position...)` (`internal/platform/plugin/pricing_steps.go`) — `position` is `before:<anchor>`, `after:<anchor>`, or `replace:<anchor>` against `CoreStepCatalog = []string{"base", "catalog_promotions", "cart_promotions", "tax", "finalize"}` (default `after:base` if omitted). Actual merge happens in `internal/application/pricing/position.go`'s `MergePluginSteps`.

- Fixed, non-generic pipeline over exactly one type, `PricingContext` — this is the only same-request mechanism with a `replace:` position (a plugin can swap out a core step entirely, e.g. a custom tax calculation replacing core tax).
- **Use for:** fees, discounts, custom promotion pricing adjustments.
- Consumed from `internal/application/cart/service.go` and `internal/application/checkout/recalculate_pricing_step.go` — the same pipeline runs on every cart mutation *and* again inside checkout, so a pricing step must be idempotent across both call sites.

### Checkout workflow — `internal/application/checkout`

```go
// internal/application/checkout/step.go
type Step interface {
    Name() string
    Execute(ctx context.Context, cctx *Context) error
}

// internal/application/checkout/workflow.go
func NewWorkflow(steps []Step, bus *event.Bus, log logger.Logger) *Workflow
func (w *Workflow) Execute(ctx context.Context, cctx *Context) error
```

Plugin-facing: `app.RegisterCheckoutStep(step, position...)` (`internal/platform/plugin/checkout_steps.go`) — `start`, `end` (default), `before:<anchor>`, `after:<anchor>` against `CoreStepCatalog` (`validate_cart, recalculate_pricing, reserve_inventory, create_order, select_shipping, initiate_payment`; no `replace:` here, unlike pricing).

Looks like a `PricingStep` — ordered steps over a mutable context, halt-on-error — but it is a **separate, checkout-only type** with extra machinery baked in that the pricing pipeline doesn't have:

- Publishes lifecycle events around every step (`checkout.step.started/completed`, `checkout.failed`, `checkout.completed`) — checkout is the one place where "workflow" and "events" overlap on purpose.
- Wraps every step in its own OTel span (`checkout.step.<name>`) and records checkout metrics.
- Recovers panics **per step** — but only to close and annotate that step's own span before re-panicking. This is not isolation: the panic still propagates out of `Execute` unbounded, exactly as if the per-step recover didn't exist. A panicking plugin step still fails the whole checkout request; whatever stops that from crashing the process is recovery further up the call stack (e.g. an HTTP-layer recovery middleware), not anything in `Workflow` itself.

**Use for:** checkout-time validation or side effects (fraud checks, custom compliance holds, third-party fulfillment reservation) that must run inside the checkout transaction, not after it.

**Context rule:** pass the request `ctx` into forward-progress blocking calls (DB, payment initiate, inventory reserve). Do **not** reuse it for compensating rollback or for persisting an already-committed side effect (PSP already charged; order already saved) — those need a detached bounded context (`context.WithoutCancel(parent)` + timeout). See [PLUGIN_COMPOSITION.md](PLUGIN_COMPOSITION.md#cart-and-pricing-composition) for the full compensating-work pattern.

### Composition steps — `internal/application/composition`

```go
// internal/application/composition/step.go
type Step[T any] interface {
    Name() string
    Apply(ctx *T) error
}

// internal/application/composition/pipeline.go
type Pipeline[T any] struct{ steps []Step[T] }
func NewPipeline[T any]() *Pipeline[T]
func (p *Pipeline[T]) AddStep(s Step[T])
func (p *Pipeline[T]) Execute(ctx *T) error
```

Plugin-facing: `app.RegisterCompositionStep(pipeline, step)` (`internal/platform/plugin/plugin.go`) — `pipeline` is `"pdp"` or `"plp"`, `step` is asserted to `composition.Step[ProductContext]` or `composition.Step[ListingContext]` at wiring time (registered untyped as `any`, so a type mismatch fails at plugin init, not at compile time — double-check your `Step[T]`'s `T` matches the pipeline you registered against).

- The only **generic** same-request mechanism, reused for both PDP (`ProductContext`) and PLP (`ListingContext`).
- `Apply(ctx *T) error` takes **no `context.Context`** — unlike every other mechanism above. Composition steps are meant to be response-shaping transforms over an already-fetched, in-memory struct, not steps that make their own blocking network calls. A step that needs an external call (e.g. a PIM enrichment lookup) manages its own timeout internally instead of relying on request cancellation — see `plugins/pimdemo/plugin.go`.
- Concrete core steps live in `internal/application/composition/*_step.go`: SEO meta/JSON-LD/canonical, EU Omnibus price indication (product + listing variants), reviews aggregation, GPSR/WEEE compliance blocks.
- **Use for:** enriching a storefront product/listing API response — extra fields, computed badges, external PIM/review data.

---

## Events — `internal/platform/event`

The one mechanism above that is *not* about the current request.

```go
type Handler func(ctx context.Context, evt Event) error
func (b *Bus) On(name string, h Handler)       // synchronous
func (b *Bus) OnAsync(name string, h Handler)  // asynchronous
func (b *Bus) Publish(ctx context.Context, evt Event) error
```

- `On` handlers run **sequentially, in the publisher's own goroutine** — an error aborts `Publish` (and skips remaining sync handlers *and* all async ones). This is the escape hatch for "must happen before the request completes, but isn't a first-class step in any of the four mechanisms above" — used sparingly (e.g. `internal/application/rewrite/subscriber.go` needs a URL slug to exist before the response returns).
- `OnAsync` handlers each run in their **own goroutine**, after all sync handlers succeed, with the **bus's shutdown context** — not the request context, so a client disconnect doesn't cancel a webhook dispatch that's already started. Errors are logged, not propagated; nothing retries them beyond what the handler itself enqueues.
- No ordering guarantee across handlers for the same event name.
- **Use for:** side effects and notifications that should happen *because of* something, not *as part of* it — sending a webhook, invalidating a cache, notifying another system. `internal/application/webhook/dispatcher.go` is the canonical example: it subscribes `OnAsync` to every event in `domainwebhook.SupportedEvents` and enqueues a delivery job.
- **Don't use for:** same-request sequencing ("run B after A, and B needs A's return value") — that's exactly what the four mechanisms above exist for. An async handler that assumes it runs before some other in-request code will occasionally lose that race.
- Keep `OnAsync` bodies short (enqueue a job, don't call a third-party API inline) — see [RUNBOOK.md's "Event bus drain"](../../RUNBOOK.md#event-bus-drain-sigterm) for the shutdown-grace budget a slow handler eats into.

---

## Gotcha: two things named "Pipeline"

`internal/domain/pricing.Pipeline` and `internal/application/composition.Pipeline[T]` share a name and a shape (ordered steps, halt-on-error) but are **unrelated Go types with no common interface** — there is no `type Pipeline interface` anywhere that both satisfy. If you're reading code and see `Pipeline`, check the import path before assuming which one it is. The checkout `Workflow` (above) is really a third member of this same family — steps + mutable context + halt-on-error — that simply isn't named "Pipeline" at all.

If you're looking for a single unifying "Step" interface across all four same-request mechanisms: there isn't one, and adding one is explicitly out of scope for this platform (see [PLUGIN_COMPOSITION.md's core rule](PLUGIN_COMPOSITION.md#core-rule) — chain handlers, don't build a framework to unify them). This guide's job is to help you pick the right one quickly, not to collapse them.

---

## Registries

Everything above is about *when your code runs*. Registries are about *what your plugin makes available* — infrastructure implementations, metadata, routes, permissions — resolved once at startup (`Init`), not re-invoked per request the way hooks/steps are.

Every plugin-facing registry follows the same shape: a `Register*` method that records **who** registered **what**, usually reached through a scoped wrapper keyed by your plugin's name (`app.Hooks(name)`, `app.Export(name)`, `app.Slots(name)`, `app.PromotionRules(name)`) rather than a raw registry object. Learn that idiom once and every registry below works the same way.

> This table is manually maintained, not generated, and this exact kind of table has drifted from source before (an earlier revision of this table, and of the equivalent one in [PLUGINS.md](../../PLUGINS.md#extension-mechanisms), both documented a nonexistent `Assets().RegisterManifest(...)` call). If a call below doesn't compile, trust the compiler over this page: `grep -n "^func (a \*App) Register" internal/platform/plugin/*.go` lists every real plugin-facing `Register*` signature directly from source.

| Registry | File | Registers | Plugin-facing API |
| --- | --- | --- | --- |
| Hooks | `internal/application/hooks/registry.go` | `hooks.Handler` (priority + registrant) | `app.Hooks(name).Register(...)` |
| Extension fields | `internal/application/extension/registry.go` | `domainext.FieldDef` (custom fields on entities) | `app.Extensions().RegisterField(...)` |
| Export row hooks | `internal/application/exportctx/registry.go` | export `RowHandler` per entity | `app.Export(name).RegisterRowHook(...)` |
| Import row hooks | `internal/application/importctx/registry.go` | import `RowHandler` per entity | `app.Import(name).RegisterRowHook(...)` |
| Assets | `internal/application/assets/registry.go` | route-gated CSS/JS manifest | `app.Assets(name).Register(manifest)` |
| Slots | `internal/application/slots/registry.go` | HTML `Renderer` per `(anchor, placement)` | `app.Slots(name).RegisterRenderer(...)` |
| Plugin config | `internal/platform/plugin/config.go` | admin-editable settings schema | `app.RegisterConfig(def)` |
| RBAC permissions | `internal/domain/rbac/registry.go` | `rbac.Permission` + default roles | `app.RegisterPermission(perm, roles...)` |
| CLI commands | `internal/platform/cli/registry.go` | `cli.Command` | `app.RegisterCommand(cmd)` |
| Shipping providers | `internal/domain/shipping/registry.go` | `shipping.Provider` keyed by method | `app.RegisterShippingRateProvider(...)` |
| Payment providers | `internal/domain/payment/registry.go` | `payment.Provider` keyed by method | `app.RegisterPaymentProvider(...)` |
| Promotion evaluators | `internal/domain/promotion/evaluator_registry.go` | 4 evaluator func types (catalog/cart × condition/action) | `app.PromotionRules(name).RegisterCatalogCondition/Action/RegisterCartCondition/Action(...)` |
| Public routes | `internal/platform/plugin/public_routes.go` | `http.Handler` per pattern; duplicate pattern panics at startup | `app.RegisterPublicRoute(pattern, handler)` |
| Search provider | `internal/domain/search/search.go` (`SearchEngine`) | one implementation; second `Register*` panics | `app.RegisterSearchProvider(provider)` |
| Cache provider | `internal/domain/cache/cache.go` (`Cache`) | one implementation; second `Register*` panics | `app.RegisterCache(c)` |
| Queue provider | `internal/domain/jobs/job.go` (`Queue`) | one implementation; second `Register*` panics | `app.RegisterQueue(queue)` |
| Media storage provider | `internal/domain/media/storage.go` (`Storage`) | one implementation; second `Register*` panics | `app.RegisterMediaStorage(storage)` |
| Tax calculator provider | `internal/domain/tax/calculator.go` (`Calculator`) | one implementation; second `Register*` panics | `app.RegisterTaxCalculator(calculator)` |
| Mail sender provider | `internal/domain/mail/mail.go` (`Mailer`) | one implementation; second `Register*` panics | `app.RegisterMailSender(mailer)` |

That's 19 rows as this table happens to break them down — shipping and payment providers (above) are multi-implementation-by-method rather than single-winner, so they're listed separately from the strictly-one-implementation ports here. The architecture audit's own "thirteen registries" figure isn't independently checkable from this repo (the audit document isn't in-tree) and may have grouped some of these differently than this table does — treat the row count here as "however many distinct `Register*` surfaces exist today," not a reconciliation against that number. Full API signatures and further detail live in [PLUGINS.md § Extension mechanisms](../../PLUGINS.md#extension-mechanisms) — this table exists to answer *"is there a registry for X"* at a glance; go there once you've found the right row.

Two registries exist but aren't currently exposed through `plugin.App` — `catalog.AttributeRegistry` (`internal/domain/catalog/attribute_registry.go`, PIM attribute schema) and `admin.Registry` (`internal/domain/admin/registry.go`, admin form/grid schemas). If your plugin needs either, check current wiring in `cmd/api/wire_services.go` before assuming it's reachable from `Init` — this may change; this note is a snapshot, not a guarantee.

---

## Where to go next

- Coordinating **with another plugin** on the same seam (priority bands, `before:`/`after:` conventions across teams, what wins on a conflict) → [PLUGIN_COMPOSITION.md § Multi-team precedence](PLUGIN_COMPOSITION.md#multi-team-precedence)
- A ready-made **task → mechanism** table (e.g. "ERP CSV column remap" → import row hook) → [PLUGIN_COMPOSITION.md § Integrator task → mechanism](PLUGIN_COMPOSITION.md#integrator-task--mechanism)
- Full **API signatures** for every mechanism and infrastructure port → [PLUGINS.md § Extension mechanisms](../../PLUGINS.md#extension-mechanisms)
- **Tutorial**-style walkthroughs → [DEVELOPER.md](DEVELOPER.md)
