# ⚡ Performance & CDN Strategy — v0 Specification

## 1. Overview

Provides:

* optional CDN integration
* full-page caching
* asset delivery optimization

---

## 2. Asset CDN

---

Config:

```yaml
cdn:
  base_url: https://cdn.example.com
```

if no cdn key present - default to local storage

---

Assets served via CDN URL.

---

---

## 3. Page Caching

---

### Cacheable

* product pages
* category pages
* CMS pages

---

### Non-cacheable

* cart
* checkout
* account

---

---

## 4. Headers

---

Cacheable:

```http
Cache-Control: public, max-age=300
```

---

Non-cacheable:

```http
Cache-Control: no-store
```

---

---

## 5. Personalization

---

* no personalized HTML in cached pages
* dynamic data fetched via API

---

---

## 6. Invalidation

---

Triggered on:

* product update
* price change

---

---

## 7. Cache Keys

---

Must include:

* store
* language
* currency

---

---

## 8. Constraints

---

* no ESI
* no block-level caching
* no template-level caching

---

---

## 9. Summary

Performance system provides:

> a simple, CDN-first caching strategy that avoids complexity while delivering high performance.

---
