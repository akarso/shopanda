# 🌍 Multi-Store & Multi-Currency — v0 Specification

## 1. Overview

Provides:

* multiple store contexts
* per-store pricing and tax
* domain-based store resolution

---

## 2. Store Model

---

```go
type Store struct {
    ID       uuid.UUID
    Code     string
    Currency string
    Country  string
    Domain   string
}
```

---

---

## 3. Currency

---

* one currency per store (v0)
* no conversion

---

---

## 4. Resolution

---

Store is resolved by:

* domain (default)
* header (optional)

---

---

## 5. Pricing

---

Prices are scoped per store:

```go
variant_id + store_id → price
```

---

---

## 6. Tax

---

Tax is derived from:

```text
store.country
```

---

---

## 7. Constraints

---

* no multi-currency per store
* no store hierarchy
* no store views

---

---

## 8. Extensibility

---

Plugins may add:

* currency conversion
* multi-language
* advanced store logic

---

---

## 9. Summary

Multi-store system provides:

> a simple and flexible way to support multiple business contexts without unnecessary hierarchy.

---
