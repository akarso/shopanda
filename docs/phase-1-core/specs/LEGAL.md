# ⚖️ Legal & Compliance — v0 Specification

## 1. Overview

Provides:

* cookie consent management
* GDPR basic compliance
* EU price indication support

---

## 2. Cookies

---

### Consent Model

```go
type Consent struct {
    Necessary bool
    Analytics bool
    Marketing bool
}
```

---

### Rules

* only necessary cookies allowed before consent
* user must be able to accept/reject

---

---

## 3. GDPR

---

### Endpoints

```http
GET /account/data
GET /account/export
DELETE /account
```

---

---

## 4. Price Indication

---

System must store price history:

```text
variant + timestamp → price
```

---

---

### Display

For discounted products:

```text
lowest price in last 30 days
```

---

---

## 5. Integration

---

Legal data is attached during composition pipeline.

---

---

## 6. Constraints

---

* no legal automation engine
* no dynamic compliance rules
* minimal required compliance only

---

---

## 7. Summary

Legal system provides:

> essential compliance features required for operating in EU without introducing unnecessary complexity.

---
