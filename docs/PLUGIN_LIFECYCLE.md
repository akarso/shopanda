# 🔌 Plugin Lifecycle Specification

## 1. Overview

Plugins extend the system via a **process-based model**.

Each plugin:

* is a standalone executable (binary)
* resides in the `/plugins` directory
* communicates with the core via a defined protocol (HTTP/gRPC/JSON)

Design goals:

* zero coupling to core internals
* safe execution and isolation
* simple installation (drop-in)
* deterministic lifecycle

---

## 2. Plugin Structure

Example:

```
/plugins/
  stripe/
    plugin        # executable
    manifest.json
```

---

## 3. Manifest

Each plugin must provide a `manifest.json`:

```json
{
  "name": "stripe",
  "version": "1.0.0",
  "description": "Stripe payment integration",
  "entry": "./plugin",
  "capabilities": {
    "events": ["order.placed"],
    "services": ["payment.processor"]
  }
}
```

---

## 4. Lifecycle Stages

### 4.1 Discovery

On startup, the core:

1. Scans `/plugins` directory
2. Validates structure
3. Reads `manifest.json`

Invalid plugins are skipped with warnings.

---

### 4.2 Boot

Core starts each plugin process:

* assigns a port or communication channel
* passes environment variables:

```
PLUGIN_NAME=stripe
PLUGIN_PORT=4101
CORE_URL=http://localhost:8080
```

---

### 4.3 Handshake

Plugin must expose a `/health` or `/init` endpoint.

Core performs handshake:

```
GET /init
```

Expected response:

```json
{
  "status": "ok",
  "name": "stripe",
  "version": "1.0.0"
}
```

Failure → plugin is disabled.

---

### 4.4 Registration

After handshake, plugin registers capabilities:

```
POST /register
```

Response:

```json
{
  "events": ["order.placed"],
  "services": ["payment.processor"]
}
```

Core updates internal registry.

---

### 4.5 Runtime Operation

#### Event Flow

Core emits events:

```
POST /event
```

Payload:

```json
{
  "event": "order.placed",
  "data": { ... }
}
```

Plugin processes asynchronously or synchronously.

---

#### Service Invocation

Core calls plugin services:

```
POST /service/payment.processor
```

Payload:

```json
{
  "action": "charge",
  "data": { ... }
}
```

---

### 4.6 Health Monitoring

Core periodically checks:

```
GET /health
```

Expected:

```json
{
  "status": "ok"
}
```

If unhealthy:

* mark plugin degraded
* optionally restart

---

### 4.7 Shutdown

On core shutdown:

1. send signal (SIGTERM)
2. allow graceful shutdown window
3. force kill if timeout exceeded

---

## 5. Plugin States

* `discovered`
* `booting`
* `active`
* `degraded`
* `failed`
* `disabled`

---

## 6. Failure Handling

* plugin crash → restart (configurable)
* repeated failure → disable plugin
* errors must not crash core

---

## 7. Isolation Rules

Plugins:

* must not access core database directly
* must not modify core state outside APIs/events
* operate as external systems

---

## 8. Security Considerations

* plugins run with limited permissions
* future:

  * sandboxing (jails, containers)
  * signed plugins
  * permission scopes

---

## 9. Versioning

Plugins must declare:

```json
"engine_version": ">=1.0.0"
```

Core may:

* reject incompatible plugins
* warn on mismatch

---

## 10. Development Mode

In development:

* plugins may run from source
* hot reload supported (optional)
* verbose logging enabled

---

## 11. CLI Integration (Future)

Examples:

```
app plugins:list
app plugins:enable stripe
app plugins:disable stripe
app plugins:logs stripe
```

---

## 12. Design Principles

* fail-safe: plugin failure must not affect core
* observable: every plugin action is traceable
* explicit: no hidden behavior
* minimal: protocol should stay simple

---

## 13. Future Extensions

* WASM plugin support
* remote plugins (network)
* plugin marketplace
* permission system (scopes)

---

## 14. Summary

Plugins are:

> isolated, event-driven extensions that communicate via explicit contracts and can be installed by dropping a binary into a directory.

---
