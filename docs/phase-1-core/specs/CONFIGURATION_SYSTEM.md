# ⚙️ Configuration System — v0 Specification

## 1. Overview

Configuration system provides:

* layered configuration (env + file + DB)
* runtime access via unified API
* export/import capability
* plugin-extensible config

Design goals:

* simple and predictable
* environment-aware
* supports admin + code workflows

---

## 2. Configuration Layers

---

### 2.1 Environment (.env)

Used for:

* secrets
* environment-specific values

---

Example:

```env
DB_URL=postgres://...
REDIS_URL=...
STRIPE_KEY=...
```

---

---

### 2.2 Static Config (config.yaml)

Located at:

```plaintext id="7kavt6"
/configs/config.yaml
```

---

Example:

```yaml id="7mj6k5"
app:
  env: dev

plugins:
  - manual_payment
  - flat_shipping

currency:
  default: EUR
```

---

---

### 2.3 Database Config

Stored in:

```sql id="4n7paz"
config (
  key UUID PRIMARY KEY,
  value JSONB
)
```

---

Example:

```json id="v6l7kl"
{
  "key": "tax.rate",
  "value": 0.20
}
```

---

---

## 3. Access API

---

### Read config

```go id="7l8qk1"
config.Get("currency.default")
```

---

### With default

```go id="5lq7dr"
config.GetOrDefault("tax.rate", 0.0)
```

---

### Typed access (recommended)

```go id="3w6lpe"
config.GetString("app.env")
config.GetFloat("tax.rate")
```

---

---

## 4. Resolution Order

---

```text id="ft3o7n"
.env → config.yaml → database
```

---

Later layers override earlier ones.

---

---

## 5. Export / Import

---

### Export

```bash id="paz7j1"
app config:export
```

Produces:

```yaml id="9lgnw4"
tax:
  rate: 0.20

shipping:
  flat_rate: 10.00
```

---

---

### Import

```bash id="dfp3jk"
app config:import config.yaml
```

---

👉 Updates database config

---

---

## 6. Plugin Configuration

---

Plugins can define config schema:

```go id="v7qk9x"
RegisterConfig("stripe", ConfigDefinition{
    Fields: []Field{
        { Name: "api_key", Type: "string", Secret: true },
    },
})
```

---

---

## 7. Admin Integration

---

Admin UI:

* reads config schema
* renders form dynamically
* stores values in DB

---

---

## 8. Namespacing

---

Use dot notation:

```text id="9gq2u7"
payment.stripe.api_key
shipping.flat.rate
tax.vat.rate
```

---

---

## 9. Caching

---

* config loaded once at startup
* optionally cached in memory
* reload on demand (future)

---

---

## 10. Validation

---

v0:

* minimal validation

Future:

* schema-based validation

---

---

## 11. Security

---

* secrets MUST come from `.env`
* DB config must not store secrets (optional enforcement)

---

---

## 12. Extensibility

---

Plugins can:

* define config fields
* read config
* react to config changes (future events)

---

---

## 13. Non-Goals (v0)

---

* no UI builder
* no dynamic reload
* no config versioning
* no distributed config sync

---

---

## 14. Summary

Configuration system v0 provides:

> a layered, predictable configuration model that supports both developer workflows and admin-driven changes.

It enables:

* environment separation
* plugin configuration
* export/import workflows

---
