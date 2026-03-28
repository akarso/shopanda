# 🌐 API Design — REST Endpoints & Flows

## 1. Overview

The system exposes a **REST-first API** designed for:

* simplicity
* predictability
* performance
* compatibility with multiple clients (web, mobile, headless)

Guidelines:

* pragmatic REST (not dogmatic)
* action-based endpoints allowed
* JSON only
* stateless

---

## 2. Conventions

### Base URL

```http
/api/v1
```

---

### Headers

```http
Content-Type: application/json
Idempotency-Key: <optional>
Authorization: Bearer <token>
```

---

### Response Format

```json
{
  "data": {},
  "error": null
}
```

Error:

```json
{
  "data": null,
  "error": {
    "code": "invalid_request",
    "message": "Something went wrong"
  }
}
```

---

## 3. Products

---

### List products

```http
GET /products
```

Query params:

* `search`
* `limit`
* `offset`
* filters (e.g. `color=red`)

---

### Get product

```http
GET /products/{id}
```

---

### Get variants

```http
GET /products/{id}/variants
```

---

### Optional: Field selection

```http
GET /products/{id}?fields=name,slug
```

---

## 4. Cart

---

### Create cart

```http
POST /carts
```

Response:

```json
{
  "id": "cart_123",
  "currency": "EUR"
}
```

---

### Get cart

```http
GET /carts/{id}
```

---

### Add item

```http
POST /carts/{id}/items
```

```json
{
  "variant_id": "var_123",
  "quantity": 2
}
```

---

### Update item

```http
PATCH /carts/{id}/items/{item_id}
```

---

### Remove item

```http
DELETE /carts/{id}/items/{item_id}
```

---

### Recalculate cart (optional explicit)

```http
POST /carts/{id}/recalculate
```

---

## 5. Checkout

---

### Start checkout

```http
POST /checkout
```

```json
{
  "cart_id": "cart_123"
}
```

Response:

* validated cart
* pricing snapshot
* next actions

---

## 6. Orders

---

### Create order

```http
POST /orders
```

```json
{
  "cart_id": "cart_123"
}
```

---

### Get order

```http
GET /orders/{id}
```

---

### List orders

```http
GET /orders?customer_id=cus_123
```

---

### Cancel order

```http
POST /orders/{id}/cancel
```

---

## 7. Payments (Plugin-driven)

---

### Initiate payment

```http
POST /payments
```

```json
{
  "order_id": "ord_123",
  "provider": "stripe"
}
```

---

### Payment webhook

```http
POST /payments/webhook/{provider}
```

---

## 8. Inventory (Optional exposure)

---

### Get stock

```http
GET /inventory/{variant_id}
```

---

## 9. Query Enhancements

---

### Expand related resources

```http
GET /orders/{id}?expand=items
```

---

### Field selection

```http
GET /orders/{id}?fields=id,total_amount,status
```

---

## 10. Idempotency

Used for:

* order creation
* payments

Header:

```http
Idempotency-Key: unique-key
```

---

## 11. Core Flow: Cart → Order

---

### 1. Create cart

```http
POST /carts
```

---

### 2. Add items

```http
POST /carts/{id}/items
```

---

### 3. Recalculate (optional)

```http
POST /carts/{id}/recalculate
```

---

### 4. Checkout

```http
POST /checkout
```

---

### 5. Create order

```http
POST /orders
```

---

### 6. Initiate payment

```http
POST /payments
```

---

### 7. Payment webhook updates order

```http
POST /payments/webhook/{provider}
```

---

## 12. Status Codes

* `200 OK`
* `201 Created`
* `400 Bad Request`
* `401 Unauthorized`
* `404 Not Found`
* `409 Conflict`
* `500 Internal Server Error`

---

## 13. Non-Goals

* No GraphQL in MVP
* No overly generic query language
* No RPC-style API explosion

---

## 14. Future Extensions

* GraphQL (read-only layer)
* bulk endpoints
* streaming / realtime updates

---

## 15. Summary

The API is:

> simple, explicit, and aligned with domain flows—not abstracted for its own sake.

It prioritizes:

* clarity over flexibility
* stability over trendiness
* real-world workflows over theoretical purity

---
