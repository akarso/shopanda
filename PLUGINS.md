# Plugin Authoring Guide

Shopanda extends through **in-process Go plugins** — no separate services, no dynamic discovery in v1.

This document describes the **three-tier model**, extension boundaries, and what authors can do today. For step-by-step integration, see [Developer Guide](docs/guides/DEVELOPER.md). For the original Phase 1 design spec, see [docs/phase-1-core/specs/PLUGINS.md](docs/phase-1-core/specs/PLUGINS.md).

---

## Three tiers

| Tier | Package | Enablement | Purpose |
| --- | --- | --- | --- |
| **Core** | `internal/*` | always on | Domain, application, default Postgres adapters |
| **Core plugin** | `plugins/core/*` | config driver switches | Optional backends (Meilisearch, Redis, RabbitMQ, Stripe, S3, …) |
| **External plugin** | author module, e.g. `plugins/example/` | compile-time register + config | Custom pipeline steps, events, permissions |
| **B2B module** | `plugins/b2b/` | compile-time register + license key | Commercial wholesale / business-buyer features ([COMMERCIAL.md](docs/COMMERCIAL.md)) |

All tiers implement the same interface:

```go
type Plugin interface {
    Name() string
    Init(app *App) error
}
```

Registration is **compile-time**:

- Core plugins: `plugins/core/register.go` (called from `cmd/api/register_plugins.go`)
- External plugins: add `registry.Register(yours.New())` in `cmd/api/register_plugins.go`

There is no runtime plugin discovery, `.so` loading, or marketplace in the current release.

---

## Core vs external boundaries

### Core plugins SHOULD

- Implement infrastructure ports (search, cache, queue, storage, payment)
- Live under `plugins/core/`
- Activate from a single driver switch per resource slot
- Fail init gracefully without crashing startup

### External plugins SHOULD

- Extend behavior via `plugin.App` hooks (pipelines, events, permissions)
- Use a vendor-prefixed unique name (e.g. `acme/loyalty`)
- Avoid modifying `internal/domain` or bypassing domain ports
- Keep optional config under `plugins.<name>.*` in YAML/env

### External plugins MUST NOT

- Override core logic or mutate core schema
- Access the database directly outside domain repositories (plugin-owned tables via `Bootstrap.DB` are OK when scoped to the plugin)
- Import `internal/infrastructure` or `internal/interfaces` (enforced by `TestImportBoundary`)
- Register competing infrastructure backends (use or contribute a core plugin instead)
- Block request lifecycle — use async events or jobs for slow work

### Import allowlist (PR-1017)

Plugins under `plugins/**` (except `plugins/core/**`) may import:

| Allowed | Examples |
| --- | --- |
| `pkg/...` | `pkg/extapi`, `pkg/integrationhttp` |
| `internal/domain/...` | repositories, value types |
| `internal/application/...` | hooks, slots, composition ports |
| `internal/platform/...` | `plugin.App`, `httpx`, config, events |
| Sibling packages under the **same** top-level plugin | e.g. `plugins/b2b/groups` from `plugins/b2b` — not other plugins, not `plugins/core/*` |

Plugins must **not** import:

- `internal/infrastructure/...` — adapters are wired by the composition root / core plugins
- `internal/interfaces/...` — HTTP helpers for plugins live in `internal/platform/httpx`
- `plugins/core/...` — would re-open infrastructure via core driver packages
- Other top-level plugins (e.g. `b2b` must not import `example`)

**Core plugins** (`plugins/core/*`) are the intentional exception: they wrap infrastructure drivers and may import `internal/infrastructure`. External/B2B plugins receive domain ports via `plugin.Bootstrap` (e.g. `Customers`, `Variants`) instead of constructing postgres repos themselves.

Enforce with:

```bash
go test ./plugins/ -run ImportBoundary -count=1
```

This is an **in-module** plugin model (same Go module, compile-time registration). Isolation is import-boundary honesty, not a separate SDK module or `.so` loading.

---

## Extension mechanisms

Available today through `plugin.App`:

| Mechanism | API | Use for |
| --- | --- | --- |
| **Pricing pipeline** | `RegisterPricingStep(step, position...)` | Fees, discounts; `before:`/`after:` core steps (`pkg/extapi`) |
| **Checkout workflow** | `RegisterCheckoutStep` | Extra validation or side effects during checkout |
| **Composition pipelines** | `RegisterCompositionStep("pdp"\|"plp", …)` | Enrich API/storefront product responses |
| **Events** | `Bus.On` / `Bus.OnAsync` | React to domain changes |
| **Permissions** | `RegisterPermission` | Admin RBAC strings on the app-owned `rbac.Registry` |
| **Admin config** | `RegisterConfig` | Simple settings on Integrations page (`GET/PUT /admin/config?group=plugins`) |
| **CLI commands** | `RegisterCommand` | Operational subcommands (`domain:action`) |
| **Public HTTP routes** | `RegisterPublicRoute` | Register public HTTP handlers (e.g. an alternative API surface); mounted by `main.go` after `InitAll` |
| **UI slots** | `Slots(registrant).RegisterRenderer` | Inject HTML at named theme anchors (see below) |
| **Asset manifest** | `Assets().RegisterManifest` | Route-gated CSS/JS in layout head/footer without theme forks |
| **Promotion rule evaluators** | `PromotionRules(registrant).RegisterCatalogCondition/Action` (+ cart variants) | Custom JSON `"type"` values for catalog/cart promotions (PR-862) |

**Infrastructure providers** (typed ports on `plugin.App`; wrong types fail at compile time):

| Port | Register | Interface |
| --- | --- | --- |
| Search | `RegisterSearchProvider(search.SearchEngine)` | `internal/domain/search` |
| Cache | `RegisterCache(cache.Cache)` | `internal/domain/cache` |
| Queue | `RegisterQueue(jobs.Queue)` | `internal/domain/jobs` |
| Payment | `RegisterPaymentProvider(payment.Provider)` | `internal/domain/payment` (multi by method) |
| Media | `RegisterMediaStorage(media.Storage)` | `internal/domain/media` |
| Tax | `RegisterTaxCalculator(tax.Calculator)` | `internal/domain/tax` |
| Shipping | `RegisterShippingRateProvider(shipping.Provider)` | `internal/domain/shipping` (multi by method) |
| Mail | `RegisterMailSender(mail.Mailer)` | `internal/domain/mail` |

Core plugins register these during init; `cmd/api` resolves them after `InitAll` (with core defaults when unset).

**Permission lifecycle (PR-1016):** composition root creates an empty `rbac.Registry` and calls `SetPermissionRegistry` **before** `InitAll` → plugins call `RegisterPermission` only during `Init` → after `InitAll`, freeze the registry. Serve also `BindRuntime` so package-level `HasPermission` / catalog helpers use that same frozen instance. CLI/worker/import/export freeze without binding (avoids multi-App collisions in one process). Duplicate permission codes fail plugin init. There is no package-level mutable plugin permission map.

**Bootstrap requirement (promotion rules):** `cmd/api/main.go` must call `SetPromotionEvaluatorRegistry` with the same registry instance passed to `NewCatalogPromotionStep` / `NewCartPromotionStep` **before** `InitAll`. Calling `PromotionRules()` without prior wiring panics at plugin init so misconfigured bootstraps fail loudly.

### UI slots (storefront)

Plugins register HTML renderers against **anchor names** declared in theme templates. Each anchor supports four placements: `before`, `after`, `prepend`, `append` (see `internal/application/slots`).

```go
app.Slots("acme/badges").RegisterRenderer("pdp.info", slots.PlacementAppend, 100, renderEcoBadge)
```

**Default theme anchors** (custom themes should preserve these names or document their own). Source of truth: `slots.StandardAnchors()` in `internal/application/slots/catalog.go`.

| Anchor | Location |
| --- | --- |
| `layout.head` | End of `<head>` (meta tags, inline snippets) |
| `layout.body_start` | Start of `<body>` |
| `layout.header` | Site header shell |
| `layout.nav` | Primary navigation |
| `layout.category_nav` | Category navigation (when categories exist) |
| `layout.main` | Main content wrapper |
| `layout.footer` | Site footer |
| `layout.body_end` | End of `<body>` (after footer scripts) |
| `pdp.gallery` | PDP media area |
| `pdp.info` | PDP product info column |
| `pdp.actions` | PDP add-to-cart actions |
| `plp.toolbar` | Category / product list toolbar |
| `cart.items` | Cart line items table |
| `cart.summary` | Cart summary aside |
| `checkout.progress` | Checkout step indicator |
| `checkout.panel` | Checkout main form panel |
| `checkout.summary` | Checkout order summary aside |
| `home.hero` | Home page hero area |
| `account.nav` | Signed-in account section navigation |

Theme markers use `{{slot_container "anchor"}}…{{/slot_container}}` or explicit `{{slot . "anchor" "placement"}}`. Nested `slot_container` blocks are supported (depth-aware matching). Missing markers are a silent no-op — plugins do not auto-inject. Global CSS/JS belongs in the **asset manifest**, not slots.

**Stable v0 contracts:** use [`pkg/extapi`](pkg/extapi) for hook/slot names, placements, and handler types (`HookHandler`, `SlotRenderer`). See [Extension API guide](docs/guides/EXTENSION_API.md). Theme authors: [Theme slots & inheritance guide](docs/guides/THEME_SLOTS.md) (`parent:` in `theme.yaml`, partials, preserving anchors).

**Reference plugin:** [plugins/slotsdemo](plugins/slotsdemo/README.md) — layout + PDP slot renderers with integration tests.

For combining multiple plugins (ordering, shared context, cart/import/ERP patterns): [Multi-Plugin Composition](docs/guides/PLUGIN_COMPOSITION.md).

**Integrator / ERP work (Phase 8):** cart rules, CSV import transforms, SAP-style inbound REST, warehouse/PIM sync — see [Integrator Platform spec](docs/phase-8-integrator-platform/specs/INTEGRATOR_PLATFORM.md) and [Phase 8 Roadmap](docs/phase-8-integrator-platform/ROADMAP.md). Use explicit ports and pipeline hooks; do not fork core importers or patch `main.go` when `RegisterPublicRoute` suffices.

---

## Enable core plugins

Change config — no code changes required for shipped core plugins:

```yaml
search:
  engine: meilisearch          # default: postgres
cache:
  driver: redis                # default: postgres
queue:
  driver: rabbitmq             # default: postgres; mutually exclusive with redis
storage:
  driver: s3                   # default: local
payment:
  stripe:
    enabled: true
plugins:
  graphql:
    enabled: true              # default: false — read-only catalog API at POST /api/v1/graphql
```

See `configs/config.example.yaml` and [Deployment Guide](docs/guides/DEPLOYMENT.md).

---

## Add an external plugin

1. Create a package implementing `plugin.Plugin` (copy from [`plugins/example/`](plugins/example/)).
2. Register extensions in `Init` via `plugin.App`.
3. Add `registry.Register(yours.New())` in `cmd/api/register_plugins.go` (optionally behind a config flag).
4. Rebuild and restart.

Example registration:

```go
// cmd/api/register_plugins.go
func registerPlugins(registry *plugin.Registry, cfg *config.Config) {
    core.Register(registry, cfg)
    if cfg.Plugins.Example.Enabled {
        registry.Register(example.New())
    }
}
```

---

## Core-owned capabilities (no main.go plugin wiring)

These live in core and do not require `register_plugins.go` / route-table changes for authors:

| Capability | Notes |
| --- | --- |
| Merchant outbound webhook destinations | Core `application/webhook` + `platform/ssrf` (private/link-local/special-purpose blocked; DNS-rebinding-safe dial; no env proxy) |

---

## What still requires main.go changes

Honest list of gaps for external authors:

| Task | Where to wire |
| --- | --- |
| Register external plugin | `cmd/api/register_plugins.go` |
| New payment webhook HTTP route | `cmd/api/main.go` route table |
| New CLI subcommand (core) | `cmd/api/main.go` subcommand switch + `printHelp()` |
| New CLI subcommand (plugin) | `app.RegisterCommand` in plugin `Init` + compile-time registration in `register_plugins.go` |
| New infrastructure backend | contribute under `plugins/core/` + driver switch |

---

## Deferred capabilities

Not implemented; do not assume these exist:

- Go plugin `.so` dynamic loading — **deferred** ([PR-544 research](docs/phase-5-maturity/specs/DYNAMIC_PLUGIN_LOADING.md)); use compile-time `register_plugins.go`
- Plugin marketplace or version resolver
- Hot reload

Plugin settings (string, int, bool) can be registered with `RegisterConfig` and edited on the admin Integrations page when the plugin is enabled at boot.

---

## Reference links

- [Developer Guide](docs/guides/DEVELOPER.md) — architecture, examples, API usage
- [Integrator Platform spec](docs/phase-8-integrator-platform/specs/INTEGRATOR_PLATFORM.md) — cart/pricing, CSV import hooks, ERP integration (Phase 8)
- [Phase 8 Roadmap](docs/phase-8-integrator-platform/ROADMAP.md) — integrator platform PR plan
- [Example external plugin](plugins/example/README.md) — pricing step, event listener, permission
- [Promotion rule reference plugin](plugins/promodemo/README.md) — custom catalog promotion JSON rule types (PR-862)
- [Commercial licensing](docs/COMMERCIAL.md) — OSS vs B2B module boundary
- [B2B plugin scaffold](plugins/b2b/README.md)
- [Dynamic plugin loading research](docs/phase-5-maturity/specs/DYNAMIC_PLUGIN_LOADING.md) — PR-544 verdict
- [Phase 5 Roadmap](docs/phase-5-maturity/ROADMAP.md) — mature commerce (complete)
- [Phase 4 Roadmap — three tiers](docs/phase-4-refactoring/ROADMAP.md#target-architecture-three-tiers)
- [C4 component diagram](docs/diagrams/c4-component.md) — registry wiring
- [Phase 1 authoring spec (historical)](docs/phase-1-core/specs/PLUGINS.md)

---

## Guiding principle

> Core defines contracts. Core plugins provide optional infrastructure. External plugins extend behavior without modifying core.

If a feature can be a plugin, it should not be in core. If a plugin requires infrastructure, it must be optional.
