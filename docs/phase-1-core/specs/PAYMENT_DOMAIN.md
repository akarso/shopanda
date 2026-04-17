# 💳 Payments Domain — v0 Specification

## 1. Overview

Payments module handles:

* payment initiation
* provider integration
* payment state tracking
* webhook processing

Design goals:

* provider-agnostic core
* plugin-based providers
* idempotent and retry-safe
* supports multiple providers

---

## 2. Core Concepts

---

### 2.1 Payment

Represents a payment attempt for an order.

```go
type Payment struct {
    ID          string
    OrderID     string

    Provider    string

    Status      string // pending, authorized, paid, failed

    Amount      Money
    Currency    string

    Meta        map[string]interface{}

    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

---

### 2.2 Payment Status

---

```text
pending → authorized → paid
        ↘ failed
```

---

### Definitions:

* `pending` → created, not processed
* `authorized` → funds reserved (optional)
* `paid` → confirmed
* `failed` → error or rejection

---

## 3. Payment Provider Interface

---

```go
type PaymentProvider interface {
    Name() string

    Initiate(ctx PaymentContext) (PaymentResult, error)

    HandleWebhook(payload []byte) (WebhookResult, error)
}
```

---

### PaymentContext

```go
type PaymentContext struct {
    PaymentID string
    OrderID   string

    Amount    Money
    Currency  string

    ReturnURL string
    Meta      map[string]interface{}
}
```

---

### PaymentResult

```go
type PaymentResult struct {
    Status      string

    RedirectURL string // optional

    Meta        map[string]interface{}
}
```

---

### WebhookResult

```go
type WebhookResult struct {
    PaymentID string
    Status    string
}
```

---

## 4. Core Flow

---

### 4.1 Initiate Payment

```http
POST /payments
```

---

Flow:

1. create payment (status: pending)
2. resolve provider
3. call provider.Initiate()
4. update payment status
5. return response

---

---

### 4.2 Webhook

```http
POST /payments/webhook/{provider}
```

---

Flow:

1. route to provider
2. parse payload
3. return WebhookResult
4. update payment
5. emit events

---

---

## 5. Events

---

* `payment.created`
* `payment.initiated`
* `payment.authorized`
* `payment.paid`
* `payment.failed`

---

---

## 6. Default Provider (Core)

---

### Manual Provider

---

```go
type ManualProvider struct{}
```

---

Behavior:

* immediately marks payment as `paid`
* no external calls

---

Use cases:

* testing
* offline payments
* cash on delivery

---

---

## 7. Multiple Providers

---

System must support:

```json
[
  "manual",
  "stripe",
  "paypal"
]
```

---

Selection:

```json
{
  "provider": "stripe"
}
```

---

---

## 8. Idempotency

---

Critical for:

* payment creation
* webhook processing

---

Strategy:

* unique constraint on (order_id, provider)
* idempotency key support

---

---

## 9. Failure Handling

---

### Initiation failure

* mark payment as `failed`
* return error

---

### Webhook failure

* retry-safe
* idempotent updates

---

---

## 10. Order Integration

---

On `payment.paid`:

* update order status → `paid`
* finalize inventory

---

---

## 11. Storage (Postgres)

---

### Payments

```sql
payments (
  id UUID PRIMARY KEY,
  order_id TEXT,
  provider TEXT,
  status TEXT,
  amount NUMERIC,
  currency TEXT,
  meta JSONB,
  created_at TIMESTAMP,
  updated_at TIMESTAMP
)
```

---

---

## 12. Security

---

* verify webhook signatures (provider-specific)
* never trust client payment status
* validate amounts server-side

---

---

## 13. Extensibility

---

Plugins can:

* register providers

```go
RegisterPaymentProvider("stripe", StripeProvider{})
```

---

* extend metadata
* add validation

---

---

## 14. Non-Goals (v0)

---

* no subscriptions
* no partial payments
* no refunds (future)
* no payment UI

---

---

## 15. Summary

Payments v0 provides:

> a provider-agnostic, plugin-based system that supports real-world payment flows without locking into any provider.

It ensures:

* drop-in usability (manual provider)
* extensibility (plugins)
* safety (idempotency, events)

---
