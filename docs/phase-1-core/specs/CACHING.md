# ⚡ Caching System — v0 Specification (Postgres-Backed)

## 1. Overview

Caching system provides:

* simple, built-in cache using Postgres
* optional external cache via plugins

---

## 2. Default Implementation

---

Uses Postgres UNLOGGED table:

```sql
CREATE UNLOGGED TABLE cache (
  key TEXT PRIMARY KEY,
  value JSONB NOT NULL,
  expires_at TIMESTAMP,
  created_at TIMESTAMP DEFAULT NOW()
);
```

---

---

## 3. Cache Interface

---

```go
type Cache interface {
    Get(key string, dest any) (bool, error)
    Set(key string, value any, ttl time.Duration) error
    Delete(key string) error
}
```

---

---

## 4. Usage

---

Used for:

* composition results
* search results
* shipping rates

---

---

## 5. Invalidation

---

* explicit deletion preferred
* TTL as fallback

---

---

## 6. Cleanup

---

Periodic job:

```sql
DELETE FROM cache WHERE expires_at < NOW();
```

---

---

## 7. Configuration

---

```yaml
cache:
  driver: postgres
```

---

---

## 8. Extensibility

---

Plugins can provide:

* Redis cache
* CDN integration

---

---

## 9. Constraints

---

* not for critical transactional data
* eventual consistency acceptable

---

---

## 10. Summary

Caching v0 provides:

> a zero-dependency caching layer using Postgres, with optional upgrades for high-scale systems.

---
