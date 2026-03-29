# 🔗 URL Routing & Rewrites — v0 Specification

## 1. Overview

Provides:

* SEO-friendly URLs
* entity resolution from paths
* decoupling of routing from content

---

## 2. URL Model

---

Entities define:

* slug (URL key)

---

Example:

```text
/nike-air-max-90
```

---

---

## 3. URL Rewrite Table

---

```sql
CREATE TABLE url_rewrites (
  path TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  entity_id UUID NOT NULL
);
```

---

---

## 4. Resolution

---

```text
HTTP → resolve(path) → entity → handler
```

---

---

## 5. Catch-All Route

---

```http
GET /*
```

---

---

## 6. Constraints

---

* paths must be unique
* resolution must be fast (indexed)
* no business logic in resolver

---

---

## 7. Summary

URL system provides:

> a simple and extensible mechanism for mapping human-readable URLs to system entities.

---
