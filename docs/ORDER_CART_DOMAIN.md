# 🛒 Cart & Order Domain Specification

## 1. Design Principles

* cart is **mutable**, order is **immutable**
* cart is a working state, order is a finalized snapshot
* pricing is recalculated, not trusted blindly
* inventory is reserved, not deducted immediately
* system must tolerate retries and partial failures

---

## 2. Cart Domain

---

### 2.1 Carts

```sql
CREATE TABLE carts (
    id              UUID PRIMARY KEY,
        customer_id     UUID,
    status          TEXT NOT NULL DEFAULT 'active',
    currency        TEXT NOT NULL,
    meta            JSONB DEFAULT '{}'::jsonb,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW()
);
```

---

### 2.2 Cart Items

```sql
CREATE TABLE cart_items (
    id              UUID PRIMARY KEY,
        cart_id         UUID NOT NULL REFERENCES carts(id) ON DELETE CASCADE,
    variant_id      TEXT NOT NULL,
    quantity        INT NOT NULL,
    unit_price      NUMERIC(12,2),
    total_price     NUMERIC(12,2),
    meta            JSONB DEFAULT '{}'::jsonb,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW()
);
```

---

### Key Notes

* `unit_price` is optional cache (recalculated often)
* cart is **not a source of truth for pricing**
* supports anonymous carts (`customer_id` nullable)

---

## 3. Cart Behavior

---

### Adding Item

Flow:

1. validate variant exists
2. check inventory availability
3. create/update cart item
4. recalculate cart totals

Event:

* `cart.item.adding` (sync)
* `cart.item.added` (async)

---

### Updating Quantity

* must revalidate stock
* must trigger recalculation

---

### Cart Recalculation

Triggered on:

* item change
* currency change
* checkout start

Includes:

* base prices
* discounts (via plugins)
* taxes (optional)

---

## 4. Inventory Integration

Cart does NOT lock inventory permanently.

Optional:

* short-lived reservation (configurable)

---

## 5. Checkout Flow

---

### Step 1: Validate Cart

* items still exist
* prices recalculated
* stock available

---

### Step 2: Reserve Inventory

* create reservations for each item
* set expiration (e.g. 15 min)

Event:

* `inventory.reserving`
* `inventory.reserved`

---

### Step 3: Create Order (pending)

---

## 6. Orders

---

### 6.1 Orders Table

```sql
CREATE TABLE orders (
    id              UUID PRIMARY KEY,
        customer_id     UUID,
    status          TEXT NOT NULL,
    currency        TEXT NOT NULL,
    total_amount    NUMERIC(12,2) NOT NULL,
    meta            JSONB DEFAULT '{}'::jsonb,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW()
);
```

---

### Statuses:

* `pending`
* `confirmed`
* `paid`
* `cancelled`
* `failed`

---

### 6.2 Order Items

```sql
CREATE TABLE order_items (
    id              UUID PRIMARY KEY,
        order_id        UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    variant_id      TEXT NOT NULL,
    sku             TEXT NOT NULL,
    name            TEXT,
    quantity        INT NOT NULL,
    unit_price      NUMERIC(12,2) NOT NULL,
    total_price     NUMERIC(12,2) NOT NULL,
    meta            JSONB DEFAULT '{}'::jsonb
);
```

---

### Key Rule:

> Order items are a snapshot — never recomputed.

---

## 7. Order Lifecycle

---

### 7.1 Order Creation

Event:

* `order.creating` (sync)
* `order.created` (async)

---

### 7.2 Payment

Handled via plugin:

Flow:

1. call payment plugin
2. update order status

Events:

* `payment.processing`
* `payment.succeeded`
* `payment.failed`

---

### 7.3 Confirmation

On successful payment:

* finalize inventory (deduct stock)
* mark order as `paid`

Event:

* `order.paid`

---

### 7.4 Cancellation / Failure

* release reservations
* mark order `cancelled` or `failed`

Event:

* `order.cancelled`

---

## 8. Inventory Interaction

---

### Reservation → Confirmation Model

1. cart → no lock (or soft)
2. checkout → reserve
3. payment success → deduct
4. failure → release

---

## 9. Idempotency

Critical for:

* payment callbacks
* retries

Strategy:

* unique constraints (order_id, payment_id)
* idempotency keys in meta

---

## 10. Pricing Strategy

---

### Source of Truth

* pricing service (not cart)

---

### Flow

1. fetch base prices
2. apply rules (plugins)
3. compute totals

---

### Cart vs Order

| Stage | Pricing         |
| ----- | --------------- |
| Cart  | recalculated    |
| Order | frozen snapshot |

---

## 11. Events Overview

---

### Cart

* `cart.created`
* `cart.item.added`
* `cart.updated`

---

### Checkout

* `checkout.started`
* `inventory.reserved`

---

### Order

* `order.created`
* `order.paid`
* `order.failed`
* `order.cancelled`

---

## 12. Failure Scenarios

---

### Payment fails

* release inventory
* mark order failed

---

### Timeout

* expire reservations
* invalidate checkout

---

### Partial failure

* system must retry safely
* no double charges

---

## 13. Extensibility Points

---

Plugins can:

* modify pricing
* inject discounts
* handle payments
* add metadata
* listen to lifecycle events

---

## 14. Non-Goals (MVP)

* multi-shipment orders
* subscription billing
* complex tax engines

---

## 15. Summary

This model ensures:

* clean separation of cart vs order
* safe inventory handling
* extensibility via events
* resilience to real-world failures

> Keep cart flexible, keep order immutable, and let events do the heavy lifting.

---
