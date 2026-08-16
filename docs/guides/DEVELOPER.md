# Developer Extension Guide

This guide is for engineers extending Shopanda itself.

It documents the extension points that exist in the current codebase, including where the platform is already plugin-friendly and where extension is still a code-level integration task.

For deployment and operational setup, see [Deployment Guide](DEPLOYMENT.md). For merchant-facing operations, see [Merchant Guide](MERCHANT.md).

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Three-Tier Extension Model](#three-tier-extension-model)
- [Enable Core Plugins](#enable-core-plugins)
- [Create an External Plugin](#create-an-external-plugin)
- [Add a Payment Provider](#add-a-payment-provider)
- [Add a Shipping Provider](#add-a-shipping-provider)
- [Add a Storage Backend](#add-a-storage-backend)
- [Add Custom Pipeline Steps](#add-custom-pipeline-steps)
- [Add Custom Event Listeners](#add-custom-event-listeners)
- [Multi-Plugin Composition](PLUGIN_COMPOSITION.md)
- [Theme Slots & Inheritance](THEME_SLOTS.md)
- [Add Custom CLI Commands](#add-custom-cli-commands)
- [Use the API Reference](#use-the-api-reference)
- [Integrator Platform (Phase 8)](#integrator-platform-phase-8)
- [Roadmap and Future Work](#roadmap-and-future-work)
- [Continuous Integration](#continuous-integration)
- [Supply chain (Dependabot + govulncheck)](#supply-chain-dependabot--govulncheck)
- [Practical Advice](#practical-advice)

## Architecture Overview

Shopanda follows a hexagonal structure.

```text
interfaces -> application -> domain
               |
               v
         infrastructure
```

Use that dependency direction when you add new behavior:

- `domain` defines core models, policies, and ports
- `application` orchestrates use cases and workflows
- `infrastructure` implements ports such as storage, payment, search, and mail
- `interfaces` adapts HTTP, admin, storefront, and other external entrypoints

When extending the system:

- add interfaces at boundaries, not everywhere
- keep business rules out of HTTP handlers and storage adapters
- prefer composition, events, pipelines, and workflows over direct core overrides

## Three-Tier Extension Model

Shopanda has three extension tiers. All use the same `plugin.Plugin` interface; the difference is packaging and enablement.

| Tier | What it is | Where it lives | How it is enabled |
| --- | --- | --- | --- |
| **Core** | Always-on commerce engine and default Postgres adapters | `internal/domain`, `internal/application`, `internal/infrastructure/postgres` | No config switch — runs on every deployment |
| **Core plugin** | Optional backends shipped in this repository | `plugins/core/*` | Driver switches in config (`search.engine`, `cache.driver`, `queue.driver`, `storage.driver`, `payment.stripe.enabled`) |
| **External plugin** | Author-owned behavior extensions | Your module or `plugins/example/` | Compile-time registration in `cmd/api/register_plugins.go` + optional config flag |

```text
Core (always on)
  └── Core plugins (config-gated, plugins/core/)
        └── External plugins (compile-time register)
```

**Core plugins** replace infrastructure ports (search engine, cache store, job queue, media storage, payment providers). Only one backend is active per resource slot — for example, `queue.driver` is `postgres`, `redis`, `rabbitmq`, `kafka`, or `sqs`, never more than one.

**External plugins** extend behavior through well-defined hooks: pricing, checkout, and composition pipeline steps; sync/async event listeners; admin permissions. They should not reimplement infrastructure adapters — use a core plugin or contribute one under `plugins/core/`.

**What still requires compile-time wiring:**

- Registering any plugin (core plugins are registered from `plugins/core/register.go`; external plugins from `cmd/api/register_plugins.go`)
- Adding payment/shipping providers that are not yet packaged as core plugins
- Adding HTTP webhook routes for new payment providers
- Adding CLI subcommands — core commands in `cmd/api/main.go`; plugin commands via `RegisterCommand` in `Init`

**Deferred (not implemented):** dynamic `.so` loading ([research](../phase-5-maturity/specs/DYNAMIC_PLUGIN_LOADING.md)), plugin marketplace, hot reload.

Plugin-defined settings can be registered via `RegisterConfig` and edited on the admin Integrations page (`group=plugins` API). Plugin enable/disable remains compile-time + config file.

See also: [PLUGINS.md](../../PLUGINS.md) · [Phase 4 Roadmap](../phase-4-refactoring/ROADMAP.md) · [plugins/example/README.md](../../plugins/example/README.md)

## Enable Core Plugins

Core plugins register through `plugins/core/register.go`, called from `registerPlugins()` in `cmd/api/register_plugins.go`. You do not import individual core plugins in your own code — change config and restart.

### Driver switches

| Resource | Config key | Default | Alternatives |
| --- | --- | --- | --- |
| Search | `search.engine` | `postgres` | `meilisearch` |
| Cache | `cache.driver` | `postgres` | `redis` |
| Queue | `queue.driver` | `postgres` | `redis`, `rabbitmq`, `kafka`, `sqs` |
| GraphQL API | `plugins.graphql.enabled` | off | read-only catalog at `POST /api/v1/graphql` |
| Storage | `storage.driver` | `local` | `s3` |
| Stripe payments | `payment.stripe.enabled` | `false` | set `true` + Stripe env vars |

Example (`configs/config.example.yaml`):

```yaml
search:
  engine: meilisearch
  meilisearch:
    host: "http://localhost:7700"
    index: products

cache:
  driver: redis
  redis:
    url: "redis://localhost:6379/0"

queue:
  driver: rabbitmq
  rabbitmq:
    url: "amqp://guest:guest@localhost:5672/"

storage:
  driver: s3
  s3:
    bucket: my-bucket
    region: eu-west-1

payment:
  stripe:
    enabled: true
```

Environment overlays use the `SHOPANDA_` prefix (e.g. `SHOPANDA_SEARCH_ENGINE=meilisearch`, `SHOPANDA_QUEUE_DRIVER=redis`).

### Startup behavior

- Core plugins register only when their driver switch matches.
- Failed plugin `Init` does not crash the process — the plugin is marked failed and skipped; check logs for `plugin.init.summary`.
- After `InitAll`, `main.go` resolves providers from `plugin.App` (search engine, job queue, cache, storage, payment registry).

Manual pay is always registered as a core plugin. Stripe registers additionally when enabled.

## Create an External Plugin

The current plugin contract lives in `internal/platform/plugin`.

Actual interface today:

```go
type Plugin interface {
    Name() string
    Init(app *plugin.App) error
}
```

The plugin app currently exposes:

- `Logger`
- `Bus`
- `Config`
- `RegisterPricingStep`
- `RegisterCheckoutStep`
- `RegisterCompositionStep`
- `RegisterPermission` — writes the app-owned `rbac.Registry` (wired before `InitAll`, frozen after; serve binds it for auth)
- `Bootstrap` — `DB` plus domain ports (`Customers`, `Variants`, …) injected by the composition root

Non-core plugins must not import `internal/infrastructure` or `internal/interfaces` (see [PLUGINS.md import allowlist](../../PLUGINS.md#import-allowlist-pr-1017)); use `platform/httpx` for admin JSON helpers.

Minimal example:

```go
package myplugin

import (
    "context"

    "github.com/akarso/shopanda/internal/application/composition"
    "github.com/akarso/shopanda/internal/domain/pricing"
    "github.com/akarso/shopanda/internal/platform/event"
    "github.com/akarso/shopanda/internal/platform/plugin"
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "my-plugin" }

func (p *Plugin) Init(app *plugin.App) error {
    app.RegisterPricingStep(priceStep{})
    app.RegisterCompositionStep("pdp", productStep{})

    app.Bus.OnAsync("catalog.product.created", func(ctx context.Context, evt event.Event) error {
        app.Logger.Info("myplugin.catalog.product.created", map[string]interface{}{
            "event_id": evt.ID,
        })
        return nil
    })

    return nil
}

type priceStep struct{}

func (priceStep) Name() string { return "my_plugin_price" }
func (priceStep) Apply(ctx context.Context, pctx *pricing.PricingContext) error {
    return nil
}

type productStep struct{}

func (productStep) Name() string { return "my_plugin_pdp" }
func (productStep) Apply(ctx *composition.ProductContext) error {
    return nil
}
```

### Register the plugin at startup

External plugins are registered in `cmd/api/register_plugins.go` (not discovered at runtime):

```go
func registerPlugins(registry *plugin.Registry, cfg *config.Config) {
    core.Register(registry, cfg)
    if cfg.Plugins.Example.Enabled {
        registry.Register(example.New())
    }
    // registry.Register(acme.New())  // your plugin
}
```

`main.go` creates the registry, calls `registerPlugins`, builds `plugin.App`, and runs `registry.InitAll(pluginApp)`.

Reference implementation: [`plugins/example/`](../../plugins/example/) — pricing step, `order.created` listener, admin permission.

Current behavior to keep in mind:

- duplicate plugin names panic at registration time
- nil plugins panic at registration time
- plugin `Init` errors do not crash the app; the plugin is marked failed and skipped
- panics inside `Init` are recovered and reported as failed plugin initialization

## Add a Payment Provider

The payment port is `internal/domain/payment.Provider`:

```go
type Provider interface {
    Method() payment.PaymentMethod
    Initiate(ctx context.Context, p *payment.Payment) (payment.ProviderResult, error)
}
```

If your provider supports refunds, also implement:

```go
type Refunder interface {
    Refund(ctx context.Context, providerRef string, amount int64, currency string) (payment.RefundResult, error)
}
```

The built-in Stripe adapter is a useful reference because it implements both initiation and refunds. In production deployments, Stripe is packaged as a **core plugin** (`plugins/core/stripe`) and enabled via `payment.stripe.enabled`.

### Provider skeleton

```go
type Provider struct {
    apiKey string
}

func NewProvider(apiKey string) (*Provider, error) {
    if apiKey == "" {
        return nil, fmt.Errorf("myprovider: api key must not be empty")
    }
    return &Provider{apiKey: apiKey}, nil
}

func (p *Provider) Method() payment.PaymentMethod {
    return payment.PaymentMethod("my_provider")
}

func (p *Provider) Initiate(ctx context.Context, py *payment.Payment) (payment.ProviderResult, error) {
    return payment.ProviderResult{
        ProviderRef: "external-ref",
        Pending:     true,
    }, nil
}
```

### Wire the provider

Payment providers ship as **core plugins** under `plugins/core/` (manual pay always on; Stripe when enabled). The active provider is selected at checkout from the payment registry populated during plugin init.

Adding a **new** payment provider that is not yet a core plugin still requires:

1. implementing the adapter (see skeleton below)
2. registering it — either as a new core plugin in `plugins/core/` or temporarily in `register_plugins.go`
3. optionally exposing refund support via `payment.Refunder`
4. adding any provider-specific webhook handler in `cmd/api/wire_services.go` and route in `cmd/api/wire_routes.go`

### Handle webhooks

Providers with asynchronous confirmation should expose an HTTP webhook adapter. Stripe is the reference pattern:

- exact route: `/api/v1/payments/webhook/stripe`
- generic route exists for provider-based webhook handling: `/api/v1/payments/webhook/{provider}`

If your provider needs webhooks, implement the HTTP adapter under `internal/interfaces/http` and wire the handler in `cmd/api/wire_services.go` / the route in `cmd/api/wire_routes.go`.

### Test a payment provider

Use adapter-level tests that isolate the external API boundary.

Recommended pattern:

- use `httptest.Server` for outbound HTTP providers
- assert request method, headers, body, and idempotency keys
- test both transport errors and business-level failures
- keep domain/application behavior separate from provider transport tests

### Integrate configuration

Provider-specific settings are currently part of application config, not a generic plugin config registry. For a new provider, extend:

- `internal/platform/config/config.go`
- `.env.example`
- `configs/config.example.yaml`

## Add a Shipping Provider

The shipping port is `internal/domain/shipping.Provider`:

```go
type Provider interface {
    Method() shipping.ShippingMethod
    CalculateRate(ctx context.Context, orderID string, currency string, itemCount int) (shipping.ShippingRate, error)
}
```

The built-in flat-rate provider is the simplest reference implementation.

### Shipping provider skeleton

```go
type Provider struct{}

func (p *Provider) Method() shipping.ShippingMethod {
    return shipping.ShippingMethod("my_shipping")
}

func (p *Provider) CalculateRate(ctx context.Context, orderID string, currency string, itemCount int) (shipping.ShippingRate, error) {
    return shipping.ShippingRate{
        ProviderRef: "quote:" + orderID,
        Cost:        shared.MustNewMoney(900, currency),
        Label:       "My Shipping",
    }, nil
}
```

### Zone integration note

Shopanda’s weight-based and zone-aware logic currently exists as domain/application code rather than a second shipping-provider interface. If your provider needs zone-aware quoting, an adapter can call the zone calculator internally before returning a `shipping.ShippingRate`.

### Wire the provider

Current shipping integration is explicit:

- checkout workflow wiring selects a provider when building checkout steps
- storefront SSR checkout accepts a list of providers for rate display
- shipping-zone admin APIs manage configuration state, not provider registration

That means a new provider usually needs both adapter code and `main.go` wiring.

## Add a Storage Backend

The media storage port is `internal/domain/media.Storage`:

```go
type Storage interface {
    Name() string
    Save(path string, file io.Reader) error
    Delete(path string) error
    URL(path string) string
}
```

The built-in local filesystem backend and the S3-compatible backend are good references. Both are **core plugins** (`plugins/core/storagelocal`, `plugins/core/storages3`) selected by `storage.driver`.

### Storage backend skeleton

```go
type Storage struct {
    baseURL string
}

func (s *Storage) Name() string { return "my-storage" }

func (s *Storage) Save(path string, file io.Reader) error {
    return nil
}

func (s *Storage) Delete(path string) error {
    return nil
}

func (s *Storage) URL(path string) string {
    return strings.TrimRight(s.baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}
```

### Upload flow integration

Storage adapters are used by the media service and HTTP media handlers. A new backend must preserve the same contract:

- `Save` writes a stream to a stable storage-relative path
- `Delete` removes the previously saved object
- `URL` returns the public URL seen by clients and storefront/admin UIs

### Configuration integration

Storage selection is config-driven via `storage.driver`. Core plugins register the active backend during startup. Follow the local and S3 core plugin patterns when adding another storage backend.

## Add Custom Pipeline Steps

Shopanda has three relevant extension pipelines today.

### Pricing pipeline

Contract:

```go
type PricingStep interface {
    Name() string
    Apply(ctx context.Context, pctx *pricing.PricingContext) error
}
```

Register through the plugin app:

```go
app.RegisterPricingStep(myPricingStep{})
```

Current order in `main.go`:

1. core pricing steps
2. plugin pricing steps
3. `pricing.NewFinalizeStep()`

That means plugin steps run before the final totals are locked in.

### Composition pipelines

Contract:

```go
type Step[T any] interface {
    Name() string
    Apply(ctx *T) error
}
```

Available pipeline names:

- `pdp` for product detail composition
- `plp` for product listing composition

Register with:

```go
app.RegisterCompositionStep("pdp", myPDP{})
app.RegisterCompositionStep("plp", myPLP{})
```

Current order in `main.go` is core SEO/composition steps first, plugin steps after.

### Checkout workflow steps

Contract:

```go
type Step interface {
    Name() string
    Execute(ctx *checkout.Context) error
}
```

Register with:

```go
app.RegisterCheckoutStep(myCheckoutStep{})
```

Current order in `main.go` is core checkout steps first, then plugin checkout steps appended to the workflow.

### Type-safety note

Plugin step registration methods accept `any`, but `main.go` later type-asserts them into concrete step interfaces. Invalid types are logged as `plugin.step.invalid_type` and skipped.

## Add Custom Event Listeners

Shopanda uses an in-process event bus with synchronous and asynchronous registration.

```go
type Handler func(ctx context.Context, evt event.Event) error
```

Registration methods:

```go
app.Bus.On("catalog.product.created", handler)
app.Bus.OnAsync("catalog.product.created", handler)
```

Semantics:

- `On` runs handlers synchronously in registration order
- sync handler errors abort the current publish operation
- `OnAsync` runs handlers in separate goroutines after sync handlers succeed
- async handler errors are logged and do not propagate back to the caller

### Representative event names

Examples already shipped in the codebase:

- catalog: `catalog.product.created`, `catalog.product.updated`, `catalog.variant.created`, `catalog.variant.updated`
- orders: `order.created`, `order.confirmed`, `order.paid`, `order.cancelled`, `order.failed`
- payments: `payment.created`, `payment.completed`, `payment.failed`, `payment.refunded`
- media: `asset.uploaded`, `asset.deleted`
- customers: `customer.created`, `customer.deleted`, `customer.password_reset.requested`
- invoices: `invoice.created`, `credit_note.created`
- checkout workflow: `checkout.step.started`, `checkout.step.completed`, `checkout.failed`, `checkout.completed`

For the current full set, inspect `internal/domain/**/events.go` and `internal/application/checkout/workflow.go`.

### Example listener

```go
app.Bus.OnAsync(order.EventOrderPaid, func(ctx context.Context, evt event.Event) error {
    data, ok := evt.Data.(order.OrderStatusChangedData)
    if !ok {
        return fmt.Errorf("unexpected event payload type %T", evt.Data)
    }

    app.Logger.Info("myplugin.order.paid", map[string]interface{}{
        "order_id": data.OrderID,
    })
    return nil
})
```

## Add Custom CLI Commands

Core commands are defined in `cmd/api/main.go`. Plugins register additional commands during `Init`:

```go
import "github.com/akarso/shopanda/internal/platform/cli"

func (p *Plugin) Init(app *plugin.App) error {
    app.RegisterCommand(cli.Command{
        Name:        "acme:sync",
        Description: "Sync vendor catalog",
        Run: func(ctx cli.Context, args []string) error {
            // ctx.Config, ctx.Logger, ctx.DB available
            return nil
        },
    })
    return nil
}
```

Commands use the `domain:action` naming convention (e.g. `example:ping`). Names must be unique across all plugins. Registered commands appear in `app help` and dispatch through the plugin CLI registry.

For core-owned operational commands, add a `case` in the subcommand switch and a `runXxx` helper in `main.go` (unchanged pattern from earlier phases).

See `plugins/example/cli.go` for a minimal working command.

## Use the API Reference

The live API docs are served from the application itself:

- Swagger-style UI: `/docs`
- OpenAPI spec: `/docs/openapi.yaml`
- repository source: `openapi.yaml`

### Test authenticated endpoints

Use the auth API to obtain a bearer token:

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"change-me-now"}'
```

Then send it to protected endpoints:

```bash
curl http://localhost:8080/api/v1/admin/orders \
  -H 'Authorization: Bearer <token>'
```

### Admin UI note

The embedded admin SPA stores its JWT in browser local storage and sends it as a bearer token on API requests. Extension work against admin APIs should continue to use the same bearer-token model rather than inventing a separate admin auth flow.

## Integrator Platform (Phase 8)

**Audience:** agencies and integrators connecting ERP, warehouse, PIM, or custom price/cart rules.

Phase 8 adds first-class seams for **commerce behavior** (positioned pricing steps, cart hooks) and **external systems** (CSV import transforms, inbound REST, outbound sync jobs) — without core forks or Magento-style override folders.

| Resource | Purpose |
| --- | --- |
| [Integrator Platform spec](../phase-8-integrator-platform/specs/INTEGRATOR_PLATFORM.md) | Design position, port catalog, import/integration patterns, precedence policy |
| [Phase 8 Roadmap](../phase-8-integrator-platform/ROADMAP.md) | Tracks A–F + post-phase PR-854 (PR-800–855) |

**Available today (core + Phase 8):**

- Infrastructure ports (typed): search, cache, queue, payment, media, tax (`RegisterSearchProvider(search.SearchEngine)`, `RegisterTaxCalculator(tax.Calculator)`, …), mail (`RegisterMailSender(mail.Mailer)`), shipping rates (`RegisterShippingRateProvider(shipping.Provider)`)
- Behavioral: positioned `RegisterPricingStep`, positioned `RegisterCheckoutStep`, `RegisterCompositionStep`, cart hook chain — see `pkg/extapi`
- Promotion rules: `app.PromotionRules(registrant).RegisterCatalogCondition/Action` (+ cart variants) for custom JSON rule `"type"` values evaluated in catalog/cart promotion pricing steps (PR-862). Requires `SetPromotionEvaluatorRegistry` in bootstrap before `InitAll`.
- HTTP: `RegisterPublicRoute`, `RegisterAdminRoute`, `app.Integration(slug).RegisterRoute` / `RegisterSecureRoute`
- CSV import: CLI `import:*` with row hooks via `app.Import().RegisterRowHook` (PR-820–823)
- CSV export: CLI `export:*` with row hooks via `app.Export().RegisterRowHook` (PR-856–858)
- Outbound sync: `app.Integration(slug).RegisterSyncJob` + `pkg/integrationsdk` (PR-840–841)
- Registration report: `./app plugins report` (PR-851)
- Async: events + optional queue drivers

**Reference plugins** (opt-in via config): `cartdemo`, `importdemo`, `exportdemo`, `integrationdemo`, `warehousedemo`, `pimdemo`, `taxdemo`, `maildemo`, `promodemo` — copy patterns; do not import from production plugins.

When choosing an extension mechanism, start with [Multi-Plugin Composition](PLUGIN_COMPOSITION.md) for ordering rules; use the integrator spec when the task involves ERP/PIM/warehouse wiring or CSV pre-persist transforms.

## Roadmap and Future Work

Phases 1–8 are **complete**. **Phase 9 — Integrator Backlog & Merchant Discovery** is **in progress** (PR-856+).

| Phase | Focus |
| --- | --- |
| 8 (done) | Integrator platform (PR-800–855) |
| 9 (in progress) | Integrator backlog + Phase 7 carryover; then **PRIORITY** merchant discovery (PR-900–903) |

Full plans: [Phase 5 Roadmap](../phase-5-maturity/ROADMAP.md) · [Phase 6 Roadmap](../phase-6-merchant-complete/ROADMAP.md) · [Phase 7 Roadmap](../phase-7-customization-platform/ROADMAP.md) · [Phase 8 Roadmap](../phase-8-integrator-platform/ROADMAP.md) · [Phase 9 Roadmap](../phase-9-merchant-discovery/ROADMAP.md) · [Integrator Platform spec](../phase-8-integrator-platform/specs/INTEGRATOR_PLATFORM.md). EU mapping: [Compliance Reference](../phase-5-maturity/specs/COMPLIANCE_EU.md). Plugin loading: [Dynamic loading research](../phase-5-maturity/specs/DYNAMIC_PLUGIN_LOADING.md).

When extending the platform, keep hexagonal rules: domain ports first, explicit wiring, plugin only when behavior is optional or author-owned.

## Continuous Integration

PRs targeting `main` / `dev`, and pushes to those branches, run [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml). Checks:

| Check | What it runs |
| --- | --- |
| **`CI / unit`** | `go mod verify`, `gofmt`, `go vet`, `go test ./...` (no DSN — Postgres tests skip) |
| **`CI / integration`** | Postgres 17 service + `SHOPANDA_TEST_DSN`; DSN-gated packages; fails if those tests skip |
| **`CI / govuln`** | Pinned `govulncheck` (fail-closed + optional baseline) |

The workflow **reports** the checks; it does not by itself block merges. A repository admin must require **`CI / unit`**, **`CI / integration`**, and **`CI / govuln`** on `main` and `dev` (Settings → Rules / Branch protection → required status checks).

Before opening a PR, run the unit checks locally:

```bash
export GOFLAGS=-mod=readonly
go mod verify
test -z "$(gofmt -l .)"
go vet ./...
go test ./...
go build ./cmd/api/
```

### Integration tests (local)

Requires a Postgres 17 instance and `SHOPANDA_TEST_DSN` (see `.env.example`). Example with Docker:

```bash
docker run --rm -d --name shopanda-test-pg \
  -e POSTGRES_USER=shopanda \
  -e POSTGRES_PASSWORD=shopanda \
  -e POSTGRES_DB=shopanda_test \
  -p 5432:5432 \
  postgres:17-alpine

export SHOPANDA_TEST_DSN='postgres://shopanda:shopanda@localhost:5432/shopanda_test?sslmode=disable'
export GOFLAGS=-mod=readonly

# Same gate as CI (requires jq): fails if DSN empty, any test skips, or canaries miss.
bash .github/scripts/run-integration-tests.sh
```

Repo tests apply root `migrations/` themselves. The script runs `go test -p 1` so packages do not race on `migrate.Run`.

## Supply chain (Dependabot + govulncheck)

### Dependabot

[`.github/dependabot.yml`](../../.github/dependabot.yml) opens weekly PRs for:

| Ecosystem | What it updates |
| --- | --- |
| `gomod` | Go module versions in `go.mod` / `go.sum` |
| `github-actions` | Action pins in `.github/workflows/*` |
| `docker` | Digests / tags in `Dockerfile` (and root dockerfiles Dependabot discovers) |

Review CI on each Dependabot PR before merge. Prefer small, reviewable upgrades over bulk merges.

### govulncheck (fail-closed)

CI job **`CI / govuln`** installs a **pinned** `govulncheck` (`GOVULNCHECK_VERSION` in [`ci.yml`](../../.github/workflows/ci.yml)) and runs [`.github/scripts/run-govulncheck.sh`](../../.github/scripts/run-govulncheck.sh).

Policy:

- Any vulnerability that affects reachable code fails the job (**fail-closed**).
- Temporary exceptions live in [`GOVULN_BASELINE.md`](../phase-10-platform-excellence/GOVULN_BASELINE.md) with **owner + expiry**; IDs not on that list always fail.
- Prefer upgrading Go / dependencies over extending the baseline.

Local:

```bash
go install golang.org/x/vuln/cmd/govulncheck@v1.7.0   # match GOVULNCHECK_VERSION
bash .github/scripts/run-govulncheck.sh
```

### Confirm fail-closed (do not merge)

To verify CI would catch a newly introduced vuln (throwaway branch only):

1. Add a temporary `require` of a known-vulnerable module version (or pin Go below a fixed stdlib advisory).
2. Run `bash .github/scripts/run-govulncheck.sh` — expect a non-zero exit listing an OSV ID not on the baseline.
3. Discard the change; never merge the fixture.

## Practical Advice

When adding an extension, prefer this order:

1. define or reuse the domain port
2. implement the adapter in infrastructure or interfaces
3. wire it explicitly in `main.go`
4. add tests at the adapter boundary
5. only then consider whether the pattern should be generalized into a reusable plugin hook

Shopanda already has real extension points for plugins, events, pipelines, workflows, infrastructure ports, and plugin CLI commands. Core and external plugins register at compile time through `register_plugins.go`; there is no dynamic plugin discovery.

### Admin SPA navigation policy

The embedded admin SPA (`internal/interfaces/http/admin/dist/`) must not expose sidebar links that render generic “coming soon” placeholders for shipped backend APIs.

When adding a new admin API:

1. ship the admin UI in the same phase (or hide the nav item until the UI PR lands)
2. register a real `render*` handler in `admin.js` `routes`
3. do not add `renderPlaceholder` routes for features advertised in the sidebar

Regression guard: `TestAdminHandler_SidebarNavNotPlaceholder` in `admin_handler_test.go` parses sidebar `data-link` hrefs from `index.html` and asserts each maps to a real route. During migration only, paths may be listed in `adminNavPlaceholderAllowlist` — **keep that list empty** once the UI PR ships.

Linked screens that are not in the sidebar (for example webhooks under Integrations) follow the same rule: either implement the screen or do not link to it from a visible admin surface.