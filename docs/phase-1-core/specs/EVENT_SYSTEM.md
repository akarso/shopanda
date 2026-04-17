# 📡 Event System Specification

## 1. Overview

The system is **event-driven by default**.

Events are:

* immutable facts describing something that happened
* the primary mechanism for extensibility and integration
* used internally and externally (plugins, services)

Design goals:

* simplicity
* consistency
* forward compatibility
* debuggability

---

## 2. Event Naming Convention

Events use **dot notation**:

```
<domain>.<entity>.<action>
```

### Examples:

* `catalog.product.created`
* `cart.item.added`
* `order.placed`
* `inventory.reserved`
* `customer.registered`

---

### Rules:

* lowercase only
* no abbreviations
* action is always past tense
* avoid vague names (`updated` is allowed, but prefer specific when possible)

---

## 3. Event Structure

All events follow a strict envelope:

```json
{
  "id": "evt_01HXYZ...",
  "name": "order.placed",
  "version": 1,
  "timestamp": "2026-01-01T12:00:00Z",
  "source": "core.order",
  "data": { ... },
  "meta": { ... }
}
```

---

### Fields:

#### `id`

* unique event identifier
* ULID or UUID
* used for tracing and deduplication

---

#### `name`

* event name (see naming rules)

---

#### `version`

* version of the event schema
* integer, starts at `1`

---

#### `timestamp`

* ISO 8601 format (UTC)

---

#### `source`

* origin of the event
* examples:

  * `core.order`
  * `plugin.stripe`
  * `system.scheduler`

---

#### `data`

* event payload (business data)
* must be minimal but sufficient

---

#### `meta`

* optional metadata
* examples:

  * request_id
  * user_id
  * correlation_id

---

## 4. Payload Design

### Principles:

* include identifiers, not full objects (unless necessary)
* avoid deep nesting
* keep payload stable

---

### Example:

```json
{
  "id": "evt_123",
  "name": "order.placed",
  "version": 1,
  "timestamp": "...",
  "source": "core.order",
  "data": {
    "order_id": "ord_123",
    "customer_id": "cus_456",
    "total": 129.99,
    "currency": "EUR"
  }
}
```

---

### Anti-pattern:

❌ dumping entire order object
❌ including irrelevant data
❌ changing structure frequently

---

## 5. Event Versioning

### Rules:

* breaking change → increment version
* non-breaking change → keep version

---

### Breaking changes:

* removing fields
* renaming fields
* changing data types

---

### Non-breaking changes:

* adding optional fields

---

### Versioning strategy:

* never modify old versions
* support multiple versions if needed
* plugins must declare supported versions

---

## 6. Sync vs Async Events

### 6.1 Synchronous Events

Used for:

* critical flows
* validation
* decision making

Example:

* `order.placing` (before order is finalized)

---

### Behavior:

* executed in request lifecycle
* can block or reject operation

---

### 6.2 Asynchronous Events

Used for:

* side effects
* integrations
* non-critical processes

Example:

* `order.placed`
* `email.send`

---

### Behavior:

* fire-and-forget
* processed by:

  * in-process handlers (default)
  * message broker (optional)

---

## 7. Event Types (Convention)

To clarify intent:

---

### Pre-events (sync)

```
<domain>.<entity>.<action>ing
```

Examples:

* `order.placing`
* `cart.updating`

Used for:

* validation
* modification
* cancellation

---

### Post-events (async)

```
<domain>.<entity>.<action>ed
```

Examples:

* `order.placed`
* `cart.updated`

Used for:

* side effects
* integrations

---

## 8. Event Bus Behavior

### Default:

* in-process event bus
* synchronous + asynchronous support

---

### Optional:

* external broker (e.g. NATS)

---

### Guarantees:

* at-least-once delivery (async)
* in-order per entity (best effort)

---

## 9. Idempotency

All event consumers must be idempotent.

Reason:

* events may be delivered more than once

---

### Recommended approach:

* use `event.id`
* track processed events if needed

---

## 10. Error Handling

### Sync events:

* error → abort operation

---

### Async events:

* error → log + retry (optional)
* retries must be configurable

---

## 11. Observability

Events must be:

* logged (debug mode)
* traceable via `correlation_id`
* inspectable (future tooling)

---

## 12. Event Registry

System should expose:

```bash
app events:list
```

Example output:

```
order.placing (v1)
order.placed (v1)
cart.updated (v2)
```

---

## 13. Design Principles

* events are contracts, not implementation details
* stability over convenience
* clarity over cleverness
* minimal payloads

---

## 14. Summary

Events are:

> the backbone of extensibility, communication, and system evolution.

A well-designed event system enables:

* plugins
* integrations
* scalability
* maintainability

---
