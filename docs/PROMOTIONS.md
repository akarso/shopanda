# 🎯 Promotions & Discounts — v0 Specification

## 1. Overview

Provides:

* catalog-level pricing rules
* cart-level discount rules
* coupon support

---

## 2. Types

---

### Catalog Rules

* modify product price

---

### Cart Rules

* modify cart totals

---

---

## 3. Model

---

```go
type Promotion struct {
    ID     uuid.UUID
    Type   string // catalog | cart
    Active bool
}
```

---

---

## 4. Rule Structure

---

```go
type Rule[T any] struct {
    Condition Condition[T]
    Action    func(ctx *T)
}
```

---

---

## 5. Execution

---

Catalog:

```text
base → rules → final
```

Cart:

```text
items → rules → totals
```

---

---

## 6. Coupons

---

```go
type Coupon struct {
    Code string
}
```

---

---

## 7. API

---

```http
POST /cart/{id}/coupon
DELETE /cart/{id}/coupon
```

---

---

## 8. Constraints

---

* no global rule engine
* no DSL
* deterministic execution

---

---

## 9. Extensibility

---

* custom conditions
* custom actions

---

---

## 10. Summary

Promotions system provides:

> a simple, explicit, and extensible way to implement discounts without introducing unnecessary complexity.

---
