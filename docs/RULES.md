# ⚙️ Rule System — v0 Specification

## 1. Overview

Provides:

* reusable condition primitives
* domain-specific rule execution

---

## 2. Design

---

Rules are NOT global.

Each domain defines its own rule system.

---

---

## 3. Condition Interface

---

```go
type Condition[T any] interface {
    Evaluate(ctx T) bool
}
```

---

---

## 4. Rule Structure

---

Example:

```go
type Rule[T any] struct {
    Condition Condition[T]
    Apply     func(ctx *T) error
}
```

---

---

## 5. Execution

---

```go
for _, rule := range rules {
    if rule.Condition.Evaluate(ctx) {
        rule.Apply(ctx)
    }
}
```

---

---

## 6. Usage

---

### Pricing

* discounts
* promotions

---

### Cart

* validation rules

---

### Shipping

* method eligibility

---

---

## 7. Constraints

---

* no global rule engine
* no DSL
* no expression parsing

---

---

## 8. Extensibility

---

Plugins may:

* add conditions
* add rules
* extend pipelines

---

---

## 9. Summary

Rule system provides:

> a simple, explicit, and type-safe way to implement conditional logic across domains without introducing a complex rule engine.

---
