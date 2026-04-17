# 💳 Stripe Payment Provider — Specification

## 1. Overview

Plugin implementing `PaymentProvider` interface for Stripe:

* PaymentIntent-based flow
* Webhook signature verification
* Refund support
* SCA / 3D Secure compatible

Design goals:

* isolated plugin (no core modifications)
* idempotent webhook processing
* secure secret management
* production-ready error handling

---

## 2. Provider Implementation (PR-207)

---

### 2.1 Plugin Registration

```go
type StripeProvider struct {
    secretKey string
    client    *stripe.Client
}

func (p *StripeProvider) Name() string { return "stripe" }
```

Registered via plugin system:

```go
func init() {
    payment.RegisterProvider("stripe", NewStripeProvider)
}
```

---

### 2.2 Initiate Payment

```go
func (p *StripeProvider) Initiate(ctx context.Context, req PaymentRequest) (*PaymentResult, error)
```

Flow:

1. Create Stripe PaymentIntent with `amount` (in cents), `currency`, `metadata`
2. Store Stripe `payment_intent_id` in `Payment.Meta`
3. Return `client_secret` for frontend confirmation

---

### 2.3 PaymentRequest

```go
type PaymentRequest struct {
    OrderID     string
    Amount      Money
    Currency    string
    Description string
    CustomerID  string
    Meta        map[string]string
}
```

---

### 2.4 PaymentResult

```go
type PaymentResult struct {
    ProviderRef  string // Stripe PaymentIntent ID
    Status       string // pending, requires_action, succeeded
    ClientSecret string // For frontend SDK
    Meta         map[string]interface{}
}
```

---

## 3. Webhook Handler (PR-208)

---

### 3.1 Endpoint

```http
POST /payments/webhook/stripe
```

---

### 3.2 Signature Verification

Uses existing webhook verification infrastructure (PR-104):

```go
func (p *StripeProvider) VerifyWebhook(payload []byte, signature string) error
```

* Verify `Stripe-Signature` header using `SHOPANDA_PAYMENT_STRIPE_WEBHOOK_SECRET`
* Reject unsigned or invalid requests with 400

---

### 3.3 Handled Events

| Stripe Event                  | Action                                    |
| ----------------------------- | ----------------------------------------- |
| `payment_intent.succeeded`    | Update payment status → `paid`, emit `payment.completed` |
| `payment_intent.payment_failed` | Update payment status → `failed`, emit `payment.failed` |
| `charge.dispute.created`      | Log warning, emit `payment.disputed`      |

---

### 3.4 Idempotency

* Store processed webhook event IDs in `Payment.Meta`
* Skip duplicate events (same Stripe event ID)
* Return 200 for already-processed events

---

## 4. Refunds (PR-209)

---

### 4.1 Admin Endpoint

```http
POST /api/v1/admin/orders/{id}/refund
```

```json
{
  "amount": 1500,
  "reason": "customer_request"
}
```

---

### 4.2 Refund Flow

1. Load payment by order ID
2. Call `stripe.Refund.Create` with PaymentIntent ID + amount
3. Store refund reference in payment meta
4. Update payment status → `refunded` (full) or `partially_refunded`
5. Emit `payment.refunded` event

---

### 4.3 Webhook: `charge.refunded`

* Reconcile refund status from Stripe-initiated refunds
* Update local payment record

---

## 5. Configuration

---

```yaml
payment:
  providers:
    stripe:
      enabled: true
      secret_key: ${SHOPANDA_PAYMENT_STRIPE_SECRET_KEY}
      webhook_secret: ${SHOPANDA_PAYMENT_STRIPE_WEBHOOK_SECRET}
      api_version: "2024-04-10"
```

---

## 6. Checkout Integration

---

### Modified Checkout Steps

Existing `initiate_payment` checkout step calls `StripeProvider.Initiate()`:

```text
validate_cart → recalculate_pricing → select_shipping
  → reserve_inventory → create_order → initiate_payment
```

Frontend receives `client_secret` in checkout response → confirms via Stripe.js.

---

### Frontend Flow (API perspective)

```text
1. POST /checkout → { order_id, payment: { client_secret: "pi_..." } }
2. Frontend: stripe.confirmCardPayment(clientSecret)
3. Stripe → POST /payments/webhook/stripe → payment.completed
4. Frontend polls: GET /orders/{id} → status: paid
```

---

## 7. Testing

---

* Use Stripe test mode keys
* Test card numbers: `4242424242424242` (success), `4000000000000002` (decline)
* Webhook testing via Stripe CLI: `stripe listen --forward-to localhost:8080/payments/webhook/stripe`

---

## 8. Non-Goals (v0)

* No Stripe Connect (marketplace)
* No subscription/recurring billing
* No multiple payment methods per order
* No PayPal (separate plugin, separate PR series)
* No saved cards / customer payment methods
