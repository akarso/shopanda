# 🔐 Authorization & Access Control — v0 Specification

## 1. Overview

Defines minimal authorization model:

* identity (user + role)
* route protection
* ownership checks

Design goals:

* simple and explicit
* no ACL complexity
* extensible later

---

## 2. Identity

---

```go
type Identity struct {
    UserID string
    Role   string
}
```

---

### Roles:

```text
guest
customer
admin
```

---

---

## 3. Middleware

---

### AuthMiddleware

* parses token
* injects identity into request context

---

### RequireAuth

* ensures user is authenticated
* rejects guests

---

### RequireRole

```go
RequireRole("admin")
```

* ensures specific role

---

---

## 4. Route Protection

---

Example:

```go
router.GET("/orders/{id}", RequireAuth(OrderHandler))

router.GET("/admin/products", RequireRole("admin")(AdminHandler))
```

---

---

## 5. Ownership Checks

---

Performed in application/handler:

```go
if order.CustomerID != identity.UserID {
    return NewError("auth.forbidden", "Access denied", nil)
}
```

---

---

## 6. API Access Rules

---

### Public

* products
* categories
* search

---

### Auth Required

* orders
* user profile

---

### Admin Only

* admin endpoints
* configuration

---

---

## 7. Error Codes

---

```text
auth.unauthorized
auth.forbidden
auth.invalid_token
```

---

---

## 8. Extensibility (Future)

---

* permissions system
* RBAC
* scopes
* policy engine

---

---

## 9. Non-Goals (v0)

---

* no ACL
* no permission matrix
* no dynamic roles

---

---

## 10. Summary

Authorization v0 provides:

> a minimal, explicit system for protecting routes and enforcing access.

It ensures:

* security
* simplicity
* future extensibility

---
