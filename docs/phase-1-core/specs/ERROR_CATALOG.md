# ❗ Error Handling & Error Catalog — v0 Specification

## 1. Overview

Defines:

* standard error response format
* error codes (machine-readable)
* mapping to HTTP status codes
* extensibility for plugins

Design goals:

* consistent across entire system
* easy to consume by frontend
* human-readable + machine-readable
* minimal but expandable

---

## 2. Error Response Format

---

```json
{
  "data": null,
  "error": {
    "code": "cart.item.not_found",
    "message": "Cart item not found",
    "details": {}
  }
}
```

---

### Fields:

* `code` → stable identifier (DO NOT change once published)
* `message` → human-readable
* `details` → optional structured data

---

---

## 3. Naming Convention

---

```text
<domain>.<entity>.<reason>
```

---

### Examples:

```text
auth.invalid_credentials
cart.item.not_found
order.payment_failed
product.out_of_stock
```

---

---

## 4. HTTP Mapping

---

| HTTP Status | Usage                   |
| ----------- | ----------------------- |
| 400         | validation errors       |
| 401         | authentication required |
| 403         | forbidden               |
| 404         | not found               |
| 409         | conflict (e.g. stock)   |
| 422         | business rule violation |
| 500         | internal error          |

---

---

## 5. Core Error Catalog (v0)

---

### Auth

```text
auth.invalid_credentials
auth.email_already_exists
auth.token_expired
auth.unauthorized
```

---

### Cart

```text
cart.not_found
cart.item.not_found
cart.item.invalid_quantity
```

---

### Product

```text
product.not_found
product.out_of_stock
product.variant_not_found
```

---

### Order

```text
order.not_found
order.invalid_state
order.creation_failed
```

---

### Payment

```text
payment.failed
payment.provider_error
payment.invalid_method
payment.already_processed
```

---

### Shipping

```text
shipping.method_not_available
shipping.address_invalid
```

---

### Search

```text
search.invalid_query
```

---

### General

```text
validation.failed
internal.error
```

---

---

## 6. Validation Errors

---

### Example:

```json
{
  "code": "validation.failed",
  "message": "Invalid input",
  "details": {
    "fields": {
      "email": "invalid format",
      "password": "too short"
    }
  }
}
```

---

---

## 7. Error Creation (Backend)

---

Helper:

```go
NewError(code string, message string, details map[string]interface{})
```

---

Example:

```go
return NewError("product.out_of_stock", "Product is out of stock", nil)
```

---

---

## 8. Plugin Errors

---

Plugins must:

* follow naming convention
* namespace their errors

---

Example:

```text
stripe.payment_failed
meilisearch.unavailable
```

---

---

## 9. Logging Integration

---

Errors must be logged:

```go
log.Error("payment.failed", err, {
  "payment_id": "...",
})
```

---

---

## 10. Stability Rules

---

* error `code` is part of API contract
* DO NOT change existing codes
* new codes can be added freely

---

---

## 11. Localization (Future)

---

Frontend should map:

```text
code → translated message
```

---

Backend message remains fallback.

---

---

## 12. Non-Goals (v0)

---

* no error registry service
* no automatic translation
* no complex error hierarchies

---

---

## 13. Summary

Error system v0 provides:

> a consistent, stable, and extensible way to represent failures across the entire platform.

It ensures:

* predictable API behavior
* clean frontend integration
* plugin compatibility

---
