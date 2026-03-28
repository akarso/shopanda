# 💸 Pricing Pipeline Specification

## 1. Overview

Pricing is computed via a **deterministic, ordered pipeline**.

The pipeline:

* transforms a pricing context step-by-step
* is extensible via plugins
* produces final totals for cart and order

Design goals:

* predictability
* extensibility
* auditability
* minimal core complexity

---

## 2. Core Concepts

---

### 2.1 Pricing Context

The central object passed through the pipeline.

```go
type PricingContext struct {
    Currency string

    Items []PricingItem

    Subtotal        Money
    DiscountsTotal  Money
    TaxTotal        Money
    FeesTotal       Money
    GrandTotal      Money

    Adjustments []Adjustment

    Meta map[string]interface{}
}
```

---

### 2.2 Pricing Item

```go
type PricingItem struct {
    VariantID string
    Quantity  int

    UnitPrice Money
    Total     Money

    Adjustments []Adjustment
}
```

---

### 2.3 Adjustment

Represents any modification to price.

```go
type Adjustment struct {
    Type        string // discount, tax, fee
    Code        string // e.g. VAT, PROMO10
    Description string

    Amount      Money
    Included    bool // included in price or added on top

    Meta        map[string]interface{}
}
```

---

## 3. Pipeline Structure

Pipeline is an **ordered list of steps**:

```go
type PricingStep interface {
    Name() string
    Apply(ctx *PricingContext) error
}
```

---

## 4. Default Pipeline (MVP)

```text
1. Base Price
2. Discounts
3. Taxes
4. Fees
5. Finalization
```

---

### Step Responsibilities

---

#### 4.1 Base Price

* fetch prices from DB
* populate items

---

#### 4.2 Discounts

* apply promotions
* update item totals
* add adjustments

---

#### 4.3 Taxes

* calculate tax per item
* support inclusive/exclusive models

---

#### 4.4 Fees

* WEEE, payment fees, etc.

---

#### 4.5 Finalization

* compute totals:

  * subtotal
  * grand total
* rounding

---

## 5. Execution Model

* steps execute **sequentially**
* each step mutates context
* failure stops pipeline

---

## 6. Plugin Integration

Plugins can:

---

### 6.1 Register New Steps

```go
RegisterPricingStep(step, position)
```

Positions:

* `before:taxes`
* `after:discounts`
* `end`

---

---

### 6.2 Modify Existing Steps

Plugins may:

* replace a step
* decorate a step

---

---

### 6.3 Add Adjustments Only

Simplest extension:

```go
ctx.Adjustments = append(ctx.Adjustments, Adjustment{...})
```

---

## 7. Determinism

Pipeline must produce the same result given the same input.

Rules:

* no randomness
* no external calls without caching
* stable ordering

---

## 8. Idempotency

Running pipeline multiple times must yield same result.

---

## 9. Rounding Strategy

* rounding happens only at finalization
* intermediate calculations use high precision

---

## 10. Tax Models

Support both:

---

### Inclusive Tax

* price includes tax
* tax extracted

---

### Exclusive Tax

* tax added on top

---

Handled entirely in tax step.

---

## 11. Multi-Currency

* pipeline operates in single currency per context
* conversion happens before pipeline (or as a step)

---

## 12. Observability

Pipeline should produce trace:

```json
[
  { "step": "base", "subtotal": 100 },
  { "step": "discount", "subtotal": 90 },
  { "step": "tax", "total": 108 }
]
```

---

## 13. Error Handling

* any step can return error
* pipeline aborts
* error propagated to caller

---

## 14. Performance

* must run in < few ms per cart
* avoid DB calls inside steps where possible
* batch operations

---

## 15. Security Constraints

Plugins:

* cannot mutate unrelated fields
* should only operate on pricing context

---

## 16. Future Extensions

* parallel steps (if safe)
* cached pricing snapshots
* rule engines

---

## 17. Summary

The pricing pipeline is:

> a deterministic transformation engine that turns raw cart data into a final, auditable price.

It enables:

* taxes
* discounts
* fees
* regional logic

without polluting the core.

---
