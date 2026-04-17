# 📊 Logging & Observability — v0 Specification (Structured, Event-Centric)

## 1. Overview

Logging system provides:

* structured logs (JSON)
* event-level visibility
* context propagation
* minimal noise, maximum signal

Design goals:

* no log spam
* self-contained entries
* easy local debugging
* pluggable exporters (Grafana, New Relic, etc.)

---

## 2. Core Concept

---

### Log Entry

```go id="9r1j2x"
type LogEntry struct {
    Timestamp time.Time

    Level     string // info, warn, error

    Event     string // e.g. "order.created"

    Message   string

    Context   map[string]interface{}
}
```

---

---

## 3. Logging Philosophy

---

### ❌ Avoid:

```text id="7v8k1p"
"starting checkout"
"processing payment"
"done"
```

---

### ✅ Prefer:

```json id="z3h2c9"
{
  "event": "payment.processed",
  "order_id": "ord_123",
  "payment_id": "pay_456",
  "amount": 100,
  "status": "paid"
}
```

---

---

## 4. Logger Interface

---

```go id="1y6d2k"
type Logger interface {
    Info(event string, ctx map[string]interface{})
    Error(event string, err error, ctx map[string]interface{})
}
```

---

---

## 5. Default Implementation

---

### JSON Logger (stdout)

* writes JSON lines
* no external dependencies

---

Example:

```json id="y7l8a1"
{
  "timestamp": "2026-01-01T12:00:00Z",
  "level": "info",
  "event": "order.created",
  "context": {
    "order_id": "ord_123",
    "customer_id": "cus_456"
  }
}
```

---

---

## 6. Context Propagation

---

### Request Context

```go id="k3d2l0"
type RequestContext struct {
    RequestID string
    UserID    string
}
```

---

Automatically included:

```json id="p9d2m1"
{
  "request_id": "...",
  "user_id": "..."
}
```

---

---

## 7. Correlation IDs

---

Each request/job gets:

```text id="v1z8t3"
request_id
job_id
```

---

Used to trace:

* API request → job → webhook

---

---

## 8. Logging in Key Systems

---

### Checkout

```go id="m1c9k2"
log.Info("checkout.completed", {
  "cart_id": "...",
  "order_id": "...",
})
```

---

---

### Payment

```go id="8t2q9w"
log.Info("payment.paid", {
  "payment_id": "...",
  "order_id": "...",
})
```

---

---

### Jobs

```go id="q7w1e5"
log.Info("job.executed", {
  "job_id": "...",
  "type": "...",
})
```

---

---

## 9. Error Logging

---

```go id="d2k7x9"
log.Error("payment.failed", err, {
  "payment_id": "...",
})
```

---

---

## 10. Log Levels

---

* `info` → business events
* `warn` → recoverable issues
* `error` → failures

---

---

## 11. Observability Scope

---

Logs must cover:

* orders
* payments
* jobs
* auth events
* shipping decisions

---

---

## 12. Extensibility

---

Plugins can:

* add fields to context
* hook into logging

---

Example:

```go id="p8k2z7"
log.Info("custom.metric", {...})
```

---

---

## 13. External Integrations

---

Optional plugins:

* Grafana Loki
* New Relic
* Datadog

---

Core remains:

* stdout JSON

---

---

## 14. Performance

---

* logging must be non-blocking (future)
* minimal allocations
* avoid excessive logs

---

---

## 15. Non-Goals (v0)

---

* no tracing system (OpenTelemetry)
* no metrics system
* no dashboards

---

---

## 16. Summary

Logging v0 provides:

> structured, event-driven logs that give a complete picture of system behavior without requiring external tooling.

It ensures:

* clarity
* debuggability
* extensibility

---
