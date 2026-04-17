# 🌐 API Design — REST Endpoints & Flows (WEB_API.md, v2)

## 1. Overview

REST-first API designed for:

* simplicity
* performance
* extensibility
* device-agnostic usage (SSR + headless)

---

## 2. Conventions

---

### Base URL

```http
/api/v1
```

---

### Headers

```http
Content-Type: application/json
Authorization: Bearer <token>
Idempotency-Key: <optional>
```

---

### Response Format

```json
{
  "data": {},
  "error": null
}
```

---

---

## 3. Auth

---

### Register

```http
POST /auth/register
```

---

### Login

```http
POST /auth/login
```

---

### Current user

```http
GET /auth/me
```

---

### Logout

```http
POST /auth/logout
```

---

---

## 4. Products

---

```http
GET /products
GET /products/{id}
GET /products/{id}/variants
```

---

---

## 5. Categories & Collections

---

```http
GET /categories
GET /categories/{id}
GET /categories/{id}/products

GET /collections
GET /collections/{id}/products
```

---

---

## 6. Search

---

```http
GET /search?q=shoes&limit=20
```

---

---

## 7. Cart

---

```http
POST /carts
GET /carts/{id}

POST /carts/{id}/items
PATCH /carts/{id}/items/{item_id}
DELETE /carts/{id}/items/{item_id}

POST /carts/{id}/recalculate
```

---

---

## 8. Checkout (Creates Order)

---

```http
POST /checkout
```

---

Behavior:

* validates cart
* calculates pricing
* selects shipping
* creates order
* initiates payment

---

---

## 9. Orders (Read Only)

---

```http
GET /orders/{id}
GET /orders
POST /orders/{id}/cancel
```

---

> Orders are created ONLY via checkout.

---

---

## 10. Payments

---

```http
POST /payments/webhook/{provider}
```

---

---

## 11. Shipping

---

### Get available shipping methods

```http
POST /shipping/rates
```

```json
{
  "cart_id": "cart_123",
  "address": { ... }
}
```

---

Response:

```json
{
  "methods": [
    {
      "id": "flat_rate",
      "label": "Flat Rate",
      "price": 5.00
    }
  ]
}
```

---

---

## 12. Media

---

```http
POST /media/upload
```

---

---

## 13. Admin (Schema-Driven)

---

> Admin UI is schema-driven (forms & grids).

---

### Get form schema

```http
GET /admin/forms/{resource}
```

---

### Submit form

```http
POST /admin/forms/{resource}
```

---

---

### Get grid schema

```http
GET /admin/grids/{resource}
```

---

### Fetch grid data

```http
GET /admin/grids/{resource}/data
```

---

---

## 14. Access Control

---

### Public

* products
* categories
* search

---

### Auth Required

* orders
* profile
* checkout (optional guest support)

---

### Admin Only

* `/admin/*`
* product management
* configuration

---

---

## 15. Idempotency

---

Used for:

* checkout
* payments

---

```http
Idempotency-Key: <unique>
```

---

---

## 16. Core Flow

---

```text
cart → checkout → order → payment webhook
```

---

---

## 17. Status Codes

---

* 200 OK
* 201 Created
* 400 Bad Request
* 401 Unauthorized
* 403 Forbidden
* 404 Not Found
* 409 Conflict
* 422 Unprocessable Entity
* 500 Internal Server Error

---

---

## 18. Non-Goals

---

* no GraphQL (MVP)
* no generic query DSL
* no over-engineered endpoints

---

---

## 19. Summary

API is:

> simple, explicit, and aligned with domain workflows.

Key rule:

> Orders are created ONLY via checkout.

---
