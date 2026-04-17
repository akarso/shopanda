# 🔌 Plugin Lifecycle Specification (v1 — In-Process)

## 1. Overview

Plugins extend the system via an **in-process model**.

Each plugin:

* is a Go module (compiled or dynamically loaded)
* runs inside the application process
* registers functionality during initialization

---

### Key Principle

> Plugins are initialized at startup and register extensions explicitly.

---

## 2. Plugin Structure

---

### Development (source-based)

```plaintext
/plugins/my-plugin
  plugin.go
  README.md
```

---

### Optional (dynamic loading)

```plaintext
/plugins
  my-plugin.so
```

---

👉 `.so` loading is optional and not required for MVP.

---

---

## 3. Plugin Interface

---

```go
type Plugin interface {
    Name() string
    Init(app *App) error
}
```

---

Optional:

```go
type Plugin interface {
    Name() string
    Version() string
    Init(app *App) error
}
```

---

---

## 4. Lifecycle Stages

---

### 4.1 Registration (Compile-Time or Load-Time)

Plugins are:

* imported (compile-time), OR
* loaded from `.so` (optional)

---

Example:

```go
import (
    _ "plugins/stripe"
    _ "plugins/flat_shipping"
)
```

---

---

### 4.2 Initialization

At startup:

```text
for each plugin:
    plugin.Init(app)
```

---

Responsibilities:

* register providers
* register pipeline steps
* register event handlers
* register config

---

---

### 4.3 Registration Phase

During `Init()`, plugin may call:

```go
RegisterPaymentProvider(...)
RegisterProductStep(...)
RegisterCheckoutStep(...)
On(...)
RegisterConfig(...)
```

---

👉 This is the ONLY time plugins modify system behavior.

---

---

### 4.4 Runtime Operation

After initialization:

* plugins are passive
* invoked via:

  * pipelines
  * workflows
  * events

---

---

## 5. Plugin States

---

* `loaded`
* `initialized`
* `active`
* `failed`

---

---

## 6. Failure Handling

---

### Initialization failure

If `Init()` returns error:

* plugin is disabled
* error is logged
* system continues

---

### Runtime failure

* errors must be returned (not panic)
* failure must not crash system

---

---

## 7. Isolation Model

---

Plugins:

* run in same process
* share memory
* have no sandboxing

---

### Implications

* faster execution
* simpler model
* requires trust in plugin code

---

---

## 8. Safety Rules

---

Plugins:

* must not panic
* must be idempotent where applicable
* must not corrupt shared state
* must not bypass domain logic

---

---

## 9. No Runtime Discovery (v1)

---

Plugins are:

* explicitly imported OR
* explicitly loaded

---

❌ No automatic discovery
❌ No manifest scanning

---

---

## 10. No Process Management

---

This system does NOT include:

* process spawning
* port allocation
* health checks
* IPC protocols

---

👉 Those belong to external systems, not plugins.

---

---

## 11. External Systems (Clarification)

---

External integrations (Stripe, Meilisearch, etc.):

* are NOT plugins
* are accessed via providers

---

Example:

```go
type StripeProvider struct{}
```

---

---

## 12. Development Mode

---

* plugins compiled together with app
* fast iteration
* no orchestration required

---

---

## 13. Future Extensions

---

Optional (not core):

* dynamic plugin loading (.so)
* sandboxed plugins (rare cases)
* process-based plugins (advanced, optional)

---

---

## 14. Summary

Plugin lifecycle is:

> load → init → register → execute (via system)

Plugins are:

* in-process
* explicit
* lightweight

---

## Guiding Principle

> Plugins register behavior at startup and are executed by the system — they do not run independently.

---
