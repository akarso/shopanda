# 🌱 Seed Data Specification (v0)

## 1. Overview

Seed data initializes the system with a **minimal working dataset**.

It ensures:

* immediate usability after install
* consistent environments
* reproducible setups

---

## 2. Design Goals

* deterministic (same result every time)
* idempotent (safe to run multiple times)
* simple (no complex fixtures)
* environment-aware (dev vs prod)

---

## 3. Seed Command

---

CLI:

```bash
app seed
```

---

Optional:

```bash
app seed --env=dev
app seed --env=prod
```

---

---

## 4. Seed Structure

---

```plaintext
/internal/seed
  seed.go
  catalog.go
  users.go
  config.go
```

---

---

## 5. Execution Flow

---

```text
1. load config
2. run seed modules
3. ensure idempotency
```

---

---

## 6. Idempotency Rules

---

Seed must:

* check if data exists before inserting
* use stable identifiers (slug/code)
* avoid duplicates

---

Example:

```go
if !repo.ExistsByEmail("admin@example.com") {
    createAdminUser()
}
```

---

---

## 7. Core Seed Data

---

### 7.1 Admin User

```text
email: admin@example.com
password: admin123
```

---

---

### 7.2 Store Config

* default currency (EUR)
* default country
* basic settings

---

---

### 7.3 Categories

```text
- electronics
- clothing
```

---

---

### 7.4 Products

Minimal set:

* 2–5 products
* simple variants

---

---

### 7.5 Shipping

* flat rate shipping

---

---

### 7.6 Payment

* manual payment (core)

---

---

## 8. Environment Modes

---

### dev

* includes demo products
* includes sample data

---

---

### prod

* minimal setup only
* no demo products

---

---

## 9. Plugin Integration

---

Plugins may register seed logic:

```go
RegisterSeeder(func(ctx SeedContext) error {
    // plugin-specific seed
})
```

---

---

## 10. Rebuild / Reset

---

Optional:

```bash
app seed --reset
```

---

Behavior:

* clears data (optional)
* re-seeds from scratch

---

---

## 11. Constraints

---

* must not overwrite user data unintentionally
* must not depend on execution order
* must be safe to run multiple times

---

---

## 12. Non-Goals (v0)

---

* no migration-based seeding
* no test fixture system
* no random data generation

---

---

## 13. Summary

Seed system provides:

> a simple, deterministic way to initialize a working store.

It enables:

* fast onboarding
* consistent environments
* reliable demos

---
