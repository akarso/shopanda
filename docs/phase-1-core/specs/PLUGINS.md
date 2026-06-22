# 🔌 Plugins — Authoring Guide (PLUGINS.md, v1)

> **Current model:** Shopanda now documents a **three-tier** extension model (core / core plugin / external plugin). See the up-to-date [root PLUGINS.md](../../../PLUGINS.md) and [Developer Guide](../../guides/DEVELOPER.md). This Phase 1 spec remains as historical design context; some API examples below predate Track B packaging.

## Three tiers (Phase 4)

| Tier | Location | Enabled by |
| --- | --- | --- |
| Core | `internal/` | always |
| Core plugin | `plugins/core/` | config drivers (`search.engine`, `cache.driver`, …) |
| External plugin | author packages | compile-time `registry.Register` in `register_plugins.go` |

Reference external plugin: [`plugins/example/`](../../../plugins/example/README.md).

---

## 1. Overview

Plugins are the primary mechanism for extending the system.

They enable:

* optional complexity (payments, search, UI, etc.)
* customization without modifying core
* ecosystem growth

Design goals:

* simple to write
* safe to run
* explicit behavior (no hidden magic)
* **no additional infrastructure required**

---

## 2. Philosophy

---

> Core defines contracts. Plugins provide implementations.

---

### Rules:

* plugins extend — **never override core**
* behavior must be explicit (registered, not discovered magically)
* plugins run **in-process**
* complexity must be opt-in

---

## 3. Execution Model (CRITICAL)

---

### In-Process Plugins (Default)

Plugins run **inside the application process**.

They:

* share memory with the application
* call core APIs directly
* have zero network overhead
* require no separate services

---

### Example

```go id="n9i9qj"
func (p MyPlugin) Init(app *App) error {
    RegisterPaymentProvider("stripe", StripeProvider{})
    return nil
}
```

---

---

### ❌ What Plugins Are NOT

Plugins are NOT:

* separate executables
* microservices
* HTTP/gRPC servers

---

👉 This system is **not a microservice plugin architecture**.

---

---

## 4. Plugin Structure

---

### Development (source-based)

```plaintext id="9v1rts"
/plugins/my-plugin
  plugin.go
  README.md
```

---

### Optional (dynamic loading)

```plaintext id="7z5b6y"
/plugins
  my-plugin.so
```

---

👉 `.so` plugins are optional and not required for MVP.

---

---

## 5. Plugin Interface

---

```go id="b6ykxj"
type Plugin interface {
    Name() string
    Init(app *App) error
}
```

---

### Optional (versioning)

```go id="6c5l0w"
type Plugin interface {
    Name() string
    Version() string
    Init(app *App) error
}
```

---

---

## 6. Loading Strategy

---

### 🟢 Compile-Time Registration (Current)

Core plugins register from `plugins/core/register.go`. External plugins register from `cmd/api/register_plugins.go`:

```go
func registerPlugins(registry *plugin.Registry, cfg *config.Config) {
    core.Register(registry, cfg)
    if cfg.Plugins.Example.Enabled {
        registry.Register(example.New())
    }
}
```

Pros:

* simplest and safest
* no runtime magic
* failed init disables a plugin without crashing the app

---

### 🟡 Go Plugins (.so) — Deferred

* dynamically loaded
* still in-process

---

Limitations:

* OS-dependent
* Go version compatibility
* **not implemented** in Shopanda v1

---

### 🔴 Process-Based Plugins (NOT DEFAULT)

Reserved for:

* sandboxing
* untrusted code
* different language runtimes

---

👉 Not part of core design.

---

---

## 7. What Plugins CAN Do

---

### Core plugins — register infrastructure providers

Core plugins under `plugins/core/` set search, cache, queue, storage, or payment providers on `plugin.App` during `Init`. See `plugins/core/register.go`.

---

### External plugins — extend pipelines and events

```go
app.RegisterPricingStep(myStep{})
app.RegisterCheckoutStep(myStep{})
app.RegisterCompositionStep("pdp", myStep{})
app.Bus.OnAsync("order.created", handler)
app.RegisterPermission("myplugin.reports.read", []string{"Admin"})
```

---

### Extend pipelines (legacy examples — see Developer Guide for current contracts)

```go id="3znw3g"
RegisterCompositionStep("product", MyStep{}, "after:pricing")
```

---

### Extend checkout workflow

```go id="7b9r6g"
RegisterCheckoutStep(MyStep{}, "before:initiate_payment")
```

---

### Listen to events

```go id="9a1m5y"
On("order.created", func(e Event) {
    // custom logic
})
```

---

### Extend admin

```go id="0m6b9s"
RegisterFormField("product.form", Field{...})
```

---

### Add config

```go id="7tx9vi"
RegisterConfig("my_plugin", ConfigDefinition{...})
```

---

---

## 8. What Plugins MUST NOT Do

---

❌ Modify core code
❌ Access database directly bypassing domain
❌ Introduce hidden side effects
❌ Depend on implicit execution order
❌ Block request lifecycle (use jobs)

---

---

## 9. External Integrations (Important Distinction)

---

External systems are NOT plugins.

---

### Examples:

* Stripe API
* Meilisearch
* S3 storage

---

These are accessed via:

* providers
* adapters

---

### Example:

```go id="h7m8jk"
type StripeProvider struct{}
```

---

👉 Runs in-process, calls external API.

---

---

## 10. Communication Between Plugins

---

Plugins communicate via:

* events
* pipelines
* shared context

---

❌ Direct plugin-to-plugin calls are forbidden

---

---

## 11. Configuration

---

Plugins define config:

```go id="h9y1r3"
RegisterConfig("stripe", ConfigDefinition{
    Fields: [...]
})
```

---

Access:

```go id="z3v2tx"
config.Get("stripe.api_key")
```

---

---

## 12. Error Handling

---

Plugins must:

* return errors (no panic)
* use standard error codes

---

```go id="1yq5u7"
return NewError("stripe.payment_failed", "Payment failed", nil)
```

---

---

## 13. Logging

---

Use structured logging:

```go id="w2r4n8"
log.Info("stripe.payment.initiated", {...})
```

---

---

## 14. Safety Rules

---

* plugins must be idempotent where applicable
* must not corrupt shared state
* must fail gracefully

---

---

## 15. Optional Complexity Pattern

---

Plugins may introduce dependencies:

---

Examples:

* Meilisearch plugin → requires Meilisearch
* Redis queue plugin → requires Redis

---

👉 Core remains dependency-free

---

---

## 16. Testing

---

Recommended:

* unit test plugin logic
* integration test via events/pipelines

---

---

## 17. Non-Goals (v1)

---

* no plugin marketplace
* no hot reloading
* no dependency resolver

---

---

## 18. Summary

Plugins provide:

> a simple, in-process extension mechanism based on Go interfaces.

They enable:

* modularity
* extensibility
* performance without overhead

---

## Guiding Principle

> If a feature can be a plugin, it should not be in core.
> If a plugin requires infrastructure, it must be optional.

---
