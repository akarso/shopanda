# Plugin Authoring Guide

Shopanda extends through **in-process Go plugins** — no separate services, no dynamic discovery in v1.

This document describes the **three-tier model**, extension boundaries, and what authors can do today. For step-by-step integration, see [Developer Guide](docs/guides/DEVELOPER.md). The original Phase 1 authoring dump lives on the `dev` branch under `docs/phase-1-core/specs/PLUGINS.md`.

---

## Three tiers

| Tier | Package | Enablement | Purpose |
| --- | --- | --- | --- |
| **Core** | `internal/*` | always on | Domain, application, default Postgres adapters |
| **Core plugin** | `plugins/core/*` | config driver switches | Optional backends (Meilisearch, Redis, RabbitMQ, Stripe, S3, …) |
| **External plugin** | author module, e.g. `plugins/example/` | compile-time register + config | Custom pipeline steps, events, permissions |
| **B2B module** | `plugins/b2b/` | compile-time register + license key | Commercial wholesale / business-buyer features ([COMMERCIAL.md](docs/COMMERCIAL.md)) |

Core plugins (`plugins/core/*`) implement the core-internal interface directly, since they
implement the driver adapters the public SDK itself wraps:

```go
// internal/platform/plugin
type Plugin interface {
    Name() string
    Init(app *App) error
}
```

External plugins and the B2B module implement the public SDK interface instead (PR-1541–1552) —
they depend only on `pkg/pluginsdk` + `pkg/extapi`, never `internal/*`:

```go
// pkg/pluginsdk
type Plugin interface {
    Name() string
    Init(h Host) error
}
```

Registration is **compile-time**:

- Core plugins: `plugins/core/register.go` (called from `cmd/api/register_plugins.go`)
- In-tree demos / reference (`extref`, `*demo`): `cmd/api/register_plugins.go`
- Product (fork) plugins: `cmd/api/register_products.go` only — do not edit `register_plugins.go`

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
- Keep optional config under `plugins.<name>.*` in YAML/env. Product (fork) plugins
  use an unknown slug map (`PluginsConfig.Extra`) — nested YAML → dotted flatten keys;
  env uses underscores; Host.Config* accepts `.` or `_`;
  do not add a typed field in `internal/platform/config` for product plugins

### External plugins MUST NOT

- Override core logic or mutate core schema
- Access the database directly outside domain repositories (plugin-owned tables via `Bootstrap.DB` + `plugin.Migrator` are OK when scoped to the plugin; do not call raw `migrate.RunFS`)
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

Not sure which mechanism fits your task? [EXTENSION_POINTS.md](docs/guides/EXTENSION_POINTS.md) is a decision guide with a picker, real code snippets, and the full registry catalog. This section is the flat API reference once you already know what you're looking for.

Available today through `plugin.App`:

| Mechanism | API | Use for |
| --- | --- | --- |
| **Hooks** | `Hooks(registrant).Register(hook, priority, handler)` | Synchronous, priority-ordered, payload-mutating cart-lifecycle handlers; can veto by returning an error |
| **Pricing pipeline** | `RegisterPricingStep(step, position...)` | Fees, discounts; `before:`/`after:`/`replace:` core steps (`pkg/extapi`) |
| **Checkout workflow** | `RegisterCheckoutStep` | Extra validation or side effects during checkout |
| **Composition pipelines** | `RegisterCompositionStep("pdp"\|"plp", …)` | Enrich API/storefront product responses |
| **Events** | `Bus.On` / `Bus.OnAsync` | React to domain changes |
| **Extension fields** | `Extensions().RegisterField(...)` (owner = plugin, assigned by the loader after `Init`; `ExtensionsFor(name)` outside `Init`) | Custom fields on entities; values via REST/GraphQL; cart → order snapshot at checkout |
| **Import/export row hooks** | `Import(registrant).RegisterRowHook(...)` / `Export(registrant).RegisterRowHook(...)` | Remap/validate CSV columns per entity, in priority order |
| **Permissions** | `RegisterPermission` | Admin RBAC strings on the app-owned `rbac.Registry` |
| **Admin config** | `RegisterConfig` | Simple settings on Integrations page (`GET/PUT /admin/config?group=plugins`) |
| **CLI commands** | `RegisterCommand` / `pluginsdk.CLICommand` | Operational subcommands (`domain:action`). Seed: plugin must `Need`/`Require(CapCatalogSeed)` **and** register `RequiresDB: true` (+ prefer `Needs: CapCatalogSeed`). `CLICommand.Needs` accepts **only** CapCatalogSeed (unknown/empty fail closed — not a general per-command Cap API). Bind when declared **and** (command Needs seed **or** empty Needs compat over-bind for every RequiresDB of that plugin). No Needs-based opt-out: keep non-seed siblings `RequiresDB: false` (another RequiresDB with empty Needs still gets the write port). Command Needs alone without plugin declare does not open `Host.CatalogSeed()`. Command `Needs: CapCatalogSeed` without `RequiresDB` fails closed. |
| **Public HTTP routes** | `RegisterPublicRoute` | Register public HTTP handlers (e.g. an alternative API surface); mounted by `main.go` after `InitAll`. Pass `pluginsdk.NoCache()` when guest GET/HEAD must not receive the default `public, max-age=300` Cache-Control (path is merged into the HTTP cache denylist). For the storefront session cart use `CartPort.EnsureStorefrontCart` (`pkg/storefrontsession`) — keep `cart_id` out of response JSON (see `plugins/extref` design-line). Auth: opaque (non-JWT) owner bearer → session or guest; failed/JWT-shaped customer token → 401; staff Bearer defers to session; `pluginsdk.CustomerID(r)` is any non-guest actor |
| **Catalog reads** | `Host.Catalog()` | `FindVariant` / `FindVariants` / `FindProduct`; `ProductVisible` + `ProductSlug`; use `extapi.ClassifyProductSlug` in `cart.validate` (Match / Other / Unknown) |
| **CartPort** (Needs `CapCart`) | `Host.Cart()` | `AddItem`, `Get`, `EnsureActive`, `EnsureStorefrontCart`, `DiscardEmptyGuestCart`, `DeleteLineExtension`, `UpsertLineExtension`; trusted plugin-only writes via `WithPluginWrite` on the call ctx (Host pins the plugin name) |
| **JobsPort** (Needs `CapJobs`) | `Host.Jobs()` | `Get`, `CountsByStatus`, `CountsByType(ctx, jobType)`, `Requeue` (+ `WithRequeueMinAge` / `WithRequeueTerminal`, `ErrRequeue*`, `JobStatus*`). Bound via `pluginsdk.NewJobsPort` (or harness stub) after `resolveAndBindJobQueue` when the queue implements `jobs.Reader` (Postgres) on serve (`setupWorker`), worker, scheduler, search-reindex, jobs CLI, schedule CLI — **before** `assertWorkerNeedsMet`. `Get` returns `(nil, nil)` on miss / non-UUID. `CountsByType` rejects empty/whitespace at the port; zero statuses omitted by Reader (port forwards). **Unscoped:** any CapJobs holder may `CountsByType` / `Requeue` any type/id. Broker queues leave CapJobs unbound. Cache/extension-field/discovery skip CapJobs via `assertWorkerLiteNeedsMet`. |
| **MediaPort** (Needs `CapMedia`) | `Host.Media()` | `Name`/`Save`/`Delete`/`URL`; `AsReadableStorage` / `AsSignedURLStorage` when supported. Raw storage — no key-prefix / content-type hardening; plugins that wrap for safety still must. Satisfied by `BindMediaStorage` on the same CapJobs process set (including jobs CLI). Cache/extension-field/discovery skip CapMedia. |
| **UI slots** | `Slots(registrant).RegisterRenderer` | Inject HTML at named theme anchors (see below) |
| **Asset manifest** | `Assets(registrant).Register(manifest)` | Route-gated CSS/JS in layout head/footer without theme forks |
| **Promotion rule evaluators** | `PromotionRules(registrant).RegisterCatalogCondition/Action` (+ cart variants) | Custom JSON `"type"` values for catalog/cart promotions (PR-862) |
| **Plugin schema** | `plugin.Migrator` (`Migration() MigrationSpec`) | One embed FS per plugin. Collected **without** `Init` by `app migrate` / `setup`; applied by `InitAll` before `Init` when `Bootstrap.DB` is set (PR-1086). Versions are `plugin:{Name()}:{basename}`. Set `Options.AdoptBareBasenames` only for a frozen legacy allowlist. |
| **Outbound sync jobs** | `app.Integration(slug).RegisterSyncJob` or `pluginsdk` `RegisterCron` / `RegisterOnEvent` / `RegisterManual` + `Enqueue` | Cron / event / manual → **`integration.sync.{slug}.{name}` only** (no free-form `{slug}.*` job types). **Event-driven:** `RegisterOnEvent` (envelope `trigger=event`). **Idempotent event job id:** `RegisterOnEvent(…, pluginsdk.WithEventJobID(func(payload) uuid))` — UUID required; Conflict resolves like `WithJobID` when `JobsPort` is bound (no dispatcher job). **Run-now:** `Enqueue` after composition-root `BindQueue` (envelope `trigger=manual`). **Idempotent enqueue:** `Enqueue(ctx, name, payload, pluginsdk.WithJobID(uuid))` — UUID required; on Conflict, Enqueue resolves via bound `JobsPort` (same sync `Type`: pending/processing/reclaiming → success/no-op; terminal → `Requeue(WithRequeueTerminal)` with one-shot race handling: `NotTerminal` = success, `WrongAttempt` → Get once + retry, a second `WrongAttempt` hard-fails — best-effort under N-way contention; wrong type → hard error). Second payload is discarded. Bind `JobsPort` (Postgres Reader) for Conflict resolve; without it Conflict stays `IsConflict` but is not auto-resumed. Redis Enqueue duplicates also return Conflict. Handlers must be idempotent. Do not use Manual+`OnAsync`→`Enqueue` as the default event path — that stamps every run as manual. Bus enqueue failures are not retried. Observability: [docs/guides/SYNC_JOB_OBSERVABILITY.md](docs/guides/SYNC_JOB_OBSERVABILITY.md). |

**Plugin-owned tables:** implement `plugin.Migrator` once. Do not call `migrate.RunPluginFS` from `Init` — `InitAllContext` applies the same spec on boot (`PluginMigrateApply`), or verifies pending versions (`PluginMigrateVerify` for short-lived CLIs). Prefer `app migrate` / `setup` before processes start; that path never calls `InitAll`. Calling `Init` directly does **not** create tables. A `depends_on` sort error with any Migrator registered fails every `InitAll` caller (Failed=Registered) — check `plugin.InitSummaryError`. Do not use raw `migrate.RunFS` (shared basename namespace, no lock).

**Infrastructure providers** (typed ports on `plugin.App`; wrong types fail at compile time):

| Port | Register | Interface |
| --- | --- | --- |
| Search | `RegisterSearchProvider(search.SearchEngine)` | `internal/domain/search` — `SuggestQuery.Filters` (PR-1079) uses the same well-known keys as `SearchQuery`; engines apply only caller-supplied filters (no baked-in storefront visibility on Suggest) |
| Cache | `RegisterCache(cache.Cache)` | `internal/domain/cache` |
| Queue | `RegisterQueue(jobs.Queue)` | `internal/domain/jobs` |
| Payment | `RegisterPaymentProvider(payment.Provider)` | `internal/domain/payment` (multi by method) |
| Media | `RegisterMediaStorage(media.Storage)` | `internal/domain/media` |
| Tax | `RegisterTaxCalculator(tax.Calculator)` | `internal/domain/tax` |
| Tax identifier types | `RegisterTaxIdentifierType` (`plugin.App` / `pluginsdk.Host` + `extapi.TaxIdentifierType`) | In-process catalog for store tax IDs (PR-1201); core seeds NIP/VAT/EIN/…; duplicate codes fail closed; registry freezes after `InitAll`. For every-EU coverage pass `Countries: extapi.EUMemberCountries()` (`Region` is UI grouping only) |
| Shipping rates | `RegisterShippingRateProvider(shipping.Provider)` | `internal/domain/shipping` (multi by method) |
| Shipment status (inbound) | `app.IntegrationShipmentStatusUpdater()` | `extapi.IntegrationShipmentStatusUpdater` — ship/deliver/cancel by order id (PR-1400); wired in serve |
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

**Integrator / ERP work:** cart rules, CSV import transforms, SAP-style inbound REST, warehouse/PIM sync — see [Integrator Platform spec](docs/guides/INTEGRATOR_PLATFORM.md). Use explicit ports and pipeline hooks; do not fork core importers or patch `main.go` when `RegisterPublicRoute` suffices.

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

## Product sites

For a separate product repository (own `origin`, Shopanda as fetch-only `upstream`, optional sample CI), see [PRODUCT_SITES.md](docs/guides/PRODUCT_SITES.md) and run:

```bash
bash scripts/product-init.sh --slug myproduct
```

Product behaviour outside `plugins/<slug>/` belongs on **owned** paths or named
hooks — not ad hoc edits of upstream roots. Canonical list:
[PRODUCT_OWNED.md](docs/PRODUCT_OWNED.md) (convention in
[PRODUCT_SITES.md — Product-owned convention](docs/guides/PRODUCT_SITES.md#product-owned-convention)).

Reference scaffold output (unregistered): [`plugins/sitekit/`](plugins/sitekit/). Job types remain `integration.sync.{slug}.{name}` only — see [PLUGIN_RUNTIME_CONTRACT.md](docs/guides/PLUGIN_RUNTIME_CONTRACT.md).

## Add an external plugin

1. Create a package implementing `pluginsdk.Plugin` — `Init(pluginsdk.Host) error` (copy from
   [`plugins/example/`](plugins/example/) or [`plugins/extref/`](plugins/extref/)). Product (non-core)
   plugins depend **only** on `pkg/pluginsdk` + `pkg/extapi` (plus stdlib/external modules and
   same-plugin siblings) — never `internal/*`. `plugins/core/*` is the one exemption (it implements
   the driver adapters the SDK itself wraps).
2. Register extensions and ports in `Init` via the `pluginsdk.Host` / `pluginsdk.SDK` facade
   (pricing/checkout/composition steps, hooks, admin routes, permissions, config fields, cart/
   catalog/order/customer ports, CLI commands — see [DEVELOPER.md](docs/guides/DEVELOPER.md)).
3. Add `registry.Register(pluginsdk.Adapt(yours.New()))`:
   - **Product forks:** `cmd/api/register_products.go` inside `registerProductPlugins`
   - **Upstream demos / reference:** `cmd/api/register_plugins.go` (optionally behind a config flag)
   `pluginsdk.Adapt` bridges the public `Host`-based plugin into the core registry, and also wires
   `Migration()` automatically if the plugin implements `pluginsdk.Migrator`.
4. Rebuild and restart.

Example product registration:

```go
// cmd/api/register_products.go
func registerProductPlugins(registry *plugin.Registry, cfg *config.Config) {
    registry.Register(pluginsdk.Adapt(myproduct.New()))
}
```

Every non-core plugin under `plugins/` (including the commercial [`plugins/b2b/`](plugins/b2b/)
module) follows this pattern; `plugins/plugin_imports_test.go`'s `TestImportBoundary` enforces the
`pkg/*`-only import rule at build time.

### Dropping `internal/platform/plugin` (Host + Adapt)

Product plugins should not import `internal/platform/plugin`. Map the old symbols:

| Was (`internal/platform/plugin`) | Use instead |
| --- | --- |
| `App` in `Init(app *App)` | `pluginsdk.Host` via `pluginsdk.Adapt` / `AsHostFor`; helpers via `pluginsdk.NewHost(h, name)` |
| `SyncJobRegistration` / `RegisterSyncJob` | `pluginsdk.NewHost(h, name).Integration(slug).RegisterCron` / `RegisterOnEvent` / `RegisterManual` |
| `NewSyncJobEnvelope` / raw `BoundQueue().Enqueue` with fixed `job.ID` | `SyncJobs.Enqueue(ctx, name, payload, pluginsdk.WithJobID(uuid))` — Conflict resolve is inside Enqueue when `JobsPort` is bound (type + status aware; do not Get/Requeue yourself) |
| `InjectRequestID` | Automatic in `Enqueue`; or `pluginsdk.InjectRequestID` when stamping a payload yourself |
| `MigrationSpec` / `Migrator` | `pluginsdk.MigrationSpec` / `pluginsdk.Migrator` (+ `ApplyMigrationFS` in tests) |
| `Capability` / `Cap*` | `pluginsdk.Capability` / `CapCart`, `CapJobs`, `CapMedia`, … |
| Request id read | `pluginsdk.RequestIDFromContext` |
| `RegisterSearchProvider` / `Bus` / composition-root binds | Stay on the App in `cmd/api` — not a plugin import |

Reference: [`plugins/extref`](plugins/extref/) production sources import only `pkg/*` (tests may still build a minimal `plugin.App` for Host wiring; prefer `pkg/pluginsdk/harness`).

---

## Core-owned capabilities (no main.go plugin wiring)

These live in core and do not require registration / route-table changes for authors:

| Capability | Notes |
| --- | --- |
| Merchant outbound webhook destinations | Core `application/webhook` + `platform/ssrf` (private/link-local/special-purpose blocked; DNS-rebinding-safe dial; no env proxy) |

---

## What still requires main.go changes

Honest list of gaps for external authors:

| Task | Where to wire |
| --- | --- |
| Register product (fork) plugin | `cmd/api/register_products.go` |
| Register upstream demo / reference plugin | `cmd/api/register_plugins.go` |
| New payment webhook HTTP route | `cmd/api/main.go` route table |
| New CLI subcommand (core) | `cmd/api/main.go` subcommand switch + `printHelp()` |
| New CLI subcommand (plugin) | `app.RegisterCommand` in plugin `Init` + compile-time registration |
| New infrastructure backend | contribute under `plugins/core/` + driver switch |

---

## Deferred capabilities

Not implemented; do not assume these exist:

- Go plugin `.so` dynamic loading — **deferred**. The Go plugin ABI is fragile across compiler versions, and Shopanda plugins need domain ports at process start. Use compile-time registration (`register_products.go` for products, `register_plugins.go` for upstream demos). Full research (PR-544) lives on the `dev` branch under `docs/phase-5-maturity/specs/DYNAMIC_PLUGIN_LOADING.md`.
- Plugin marketplace or version resolver
- Hot reload

Plugin settings (string, int, bool) can be registered with `RegisterConfig` and edited on the admin Integrations page when the plugin is enabled at boot.

---

## Reference links

- [Developer Guide](docs/guides/DEVELOPER.md) — architecture, examples, API usage
- [Integrator Platform spec](docs/guides/INTEGRATOR_PLATFORM.md) — cart/pricing, CSV import hooks, ERP integration
- [Example external plugin](plugins/example/README.md) — pricing step, event listener, permission
- [extref SDK reference](plugins/extref/README.md) — `pluginsdk.Adapt` only; gift_note + design-on-line (`POST /extref/design-line`, PR-1553)
- [Promotion rule reference plugin](plugins/promodemo/README.md) — custom catalog promotion JSON rule types (PR-862)
- [Commercial licensing](docs/COMMERCIAL.md) — OSS vs B2B module boundary
- [B2B plugin scaffold](plugins/b2b/README.md)
- [C4 component diagram](docs/diagrams/c4-component.md) — registry wiring
- Working history (phase roadmaps, PR-544 research, historical specs): the `dev` branch under `docs/phase-*` (those paths are not on `main`)

---

## Guiding principle

> Core defines contracts. Core plugins provide optional infrastructure. External plugins extend behavior without modifying core.

If a feature can be a plugin, it should not be in core. If a plugin requires infrastructure, it must be optional.
