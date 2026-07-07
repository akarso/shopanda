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
- Access the database directly outside domain repositories
- Register competing infrastructure backends (use or contribute a core plugin instead)
- Block request lifecycle — use async events or jobs for slow work

---

## Extension mechanisms

Available today through `plugin.App`:

| Mechanism | API | Use for |
| --- | --- | --- |
| **Pricing pipeline** | `RegisterPricingStep` | Fees, discounts, custom line adjustments |
| **Checkout workflow** | `RegisterCheckoutStep` | Extra validation or side effects during checkout |
| **Composition pipelines** | `RegisterCompositionStep("pdp"\|"plp", …)` | Enrich API/storefront product responses |
| **Events** | `Bus.On` / `Bus.OnAsync` | React to domain changes |
| **Permissions** | `RegisterPermission` | Admin RBAC strings |
| **Admin config** | `RegisterConfig` | Simple settings on Integrations page (`GET/PUT /admin/config?group=plugins`) |
| **CLI commands** | `RegisterCommand` | Operational subcommands (`domain:action`) |
| **Public HTTP routes** | `RegisterPublicRoute` | Register public HTTP handlers (e.g. an alternative API surface); mounted by `main.go` after `InitAll` |
| **UI slots** | `Slots(registrant).RegisterRenderer` | Inject HTML at named theme anchors (see below) |
| **Asset manifest** | `Assets().RegisterManifest` | Route-gated CSS/JS in layout head/footer without theme forks |

Core plugins additionally expose providers on `plugin.App` during init (search engine, job queue, cache store, media storage, payment registry entries) which `main.go` resolves after `InitAll`.

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
| `checkout.summary` | Checkout order summary aside |

Theme markers use `{{slot_container "anchor"}}…{{/slot_container}}` or explicit `{{slot . "anchor" "placement"}}`. **Do not nest `slot_container` blocks** — the preprocessor cannot match inner closings; use explicit `slot` markers inside a container instead. Missing markers are a silent no-op — plugins do not auto-inject. Global CSS/JS belongs in the **asset manifest**, not slots.

For combining multiple plugins (ordering, shared context, checkout-field walkthrough): [Multi-Plugin Composition](docs/guides/PLUGIN_COMPOSITION.md).

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
- [Example external plugin](plugins/example/README.md) — pricing step, event listener, permission
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
