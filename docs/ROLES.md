# 🔐 Admin RBAC — v0 Specification

## 1. Overview

Provides:

* role-based access control for admin users
* permission-based route protection
* integration with admin UI

---

## 2. Model

---

### Roles

* admin
* manager
* editor
* support

---

---

### Permissions

Examples:

```text
products.read
products.write
orders.read
orders.write
invoices.read
```

---

---

## 3. Assignment

---

* each user has one role
* role defines permissions

---

---

## 4. Enforcement

---

Middleware:

```go
RequirePermission("products.write")
```

---

---

## 5. Admin UI

---

* forms and grids respect permissions
* actions hidden if not allowed

---

---

## 6. Extensibility

---

Plugins may:

* define new permissions
* extend roles

---

---

## 7. Constraints

---

* no ACL tree
* no inheritance
* no per-field permissions

---

---

## 8. Summary

RBAC system provides:

> a simple and explicit way to control admin access without introducing unnecessary complexity.

---
