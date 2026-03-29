# 🧾 Tax System (VAT) — v0 Specification

## 1. Overview

Provides:

* basic tax calculation
* country-based VAT support
* integration with pricing pipeline

---

## 2. Model

---

### Tax Class

```go
type TaxClass struct {
    Code string
}
```

---

### Tax Rate

```go
type TaxRate struct {
    Country string
    Class   string
    Rate    float64
}
```

---

---

## 3. Calculation

---

```text
price → tax → total
```

---

---

## 4. Modes

---

* exclusive (default)
* inclusive

---

---

## 5. Input

---

* product tax class
* customer country

---

---

## 6. Rules

---

* country-based rate lookup
* no complex rule engine

---

---

## 7. Order

---

Tax is stored in order snapshot:

* per item
* total

---

---

## 8. Extensibility

---

External providers may override calculation.

---

---

## 9. Constraints

---

* no tax zones
* no complex jurisdiction logic
* deterministic calculation

---

---

## 10. Summary

Tax system provides:

> a minimal, correct, and extensible approach to VAT without introducing unnecessary complexity.

---
