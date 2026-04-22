# Developer Extension Guide

This guide is for engineers extending Shopanda itself.

It documents the extension points that exist in the current codebase, including where the platform is already plugin-friendly and where extension is still a code-level integration task.

For deployment and operational setup, see [Deployment Guide](DEPLOYMENT.md). For merchant-facing operations, see [Merchant Guide](MERCHANT.md).

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

## Create a Plugin

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
- `RegisterPermission`

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

Plugin registration is currently explicit and compile-time. Add the plugin in `cmd/api/main.go` where the registry is created:

```go
registry := plugin.NewRegistry(log)
registry.Register(myplugin.New())

pluginApp := &plugin.App{
    Logger: log,
    Bus:    bus,
    Config: cfg,
}

summary := registry.InitAll(pluginApp)
```

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

The built-in Stripe adapter is a useful reference because it implements both initiation and refunds.

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

Current provider selection is code-driven in `cmd/api/main.go`, not plugin-discovered. That means adding a provider usually requires:

1. instantiating the adapter from configuration
2. selecting it as the active `payment.Provider`
3. optionally exposing refund support via `payment.Refunder`
4. adding any provider-specific webhook route and handler

### Handle webhooks

Providers with asynchronous confirmation should expose an HTTP webhook adapter. Stripe is the reference pattern:

- exact route: `/api/v1/payments/webhook/stripe`
- generic route exists for provider-based webhook handling: `/api/v1/payments/webhook/{provider}`

If your provider needs webhooks, implement the HTTP adapter under `internal/interfaces/http` and wire the route in `main.go`.

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

The built-in local filesystem backend and the S3-compatible backend are good references.

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

Storage selection is currently configured in app config rather than a dynamic backend registry. Follow the existing local and S3 patterns when introducing another backend.

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

Current limitation: CLI subcommands are not plugin-registered.

Today, the command surface is defined explicitly in `cmd/api/main.go` via:

- the subcommand switch in `run()`
- `printHelp()`
- the individual `runXxx` helpers

That means a custom command currently requires a code change in `main.go`.

Recommended pattern:

1. add a new `case` in the subcommand switch
2. keep parsing and orchestration in a dedicated `runMyCommand(cfg, log)` helper
3. keep domain/application logic outside `main.go`
4. update `printHelp()` so the command appears in built-in help output

Minimal sketch:

```go
case "sync:vendors":
    return runVendorSync(cfg, log)
```

This is not yet a plugin hook, so document it honestly in your own extension work.

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

## Practical Advice

When adding an extension, prefer this order:

1. define or reuse the domain port
2. implement the adapter in infrastructure or interfaces
3. wire it explicitly in `main.go`
4. add tests at the adapter boundary
5. only then consider whether the pattern should be generalized into a reusable plugin hook

Shopanda already has real extension points for plugins, events, pipelines, workflows, and infrastructure ports. It does not yet have dynamic plugin discovery or a plugin-based CLI registry, so keep the guide and the code honest about where extension is explicit rather than magical.