# 🧩 Composition Pipeline Specification

## 1. Overview

The Composition Pipeline is responsible for **building API responses** in a deterministic and extensible way.

It replaces:

* controller-heavy logic
* ad-hoc data enrichment
* backend-driven UI layouts (Magento-style blocks)

Design goals:

* extensibility (plugins can enrich responses)
* determinism (predictable output)
* separation of concerns (data vs presentation)
* frontend flexibility (headless-first)

---

## 2. Core Concept

Instead of returning raw entities:

```text
controller → fetch → return
```

We use:

```text
controller → context → composition pipeline → enriched response
```

---

## 3. Context Objects

Each composition pipeline operates on a **context struct**.

---

### 3.1 Product Context (PDP)

```go id="5m1l0f"
type ProductContext struct {
    Product     *Product
    Variants    []Variant

    Currency    string
    Country     string

    Blocks      []Block
    Meta        map[string]interface{}
}
```

---

### 3.2 Listing Context (PLP)

```go id="9w4zjz"
type ListingContext struct {
    Products    []Product

    Filters     []Filter
    SortOptions []SortOption

    Blocks      []Block

    Currency    string
    Country     string

    Meta        map[string]interface{}
}
```

---

## 4. Block Model

Blocks represent UI-agnostic components.

```go id="2yq7o2"
type Block struct {
    Type string
    Data map[string]interface{}
}
```

---

### Example:

```json id="tw4sl8"
{
  "type": "shipping_estimator",
  "data": {
    "cost": 12.99
  }
}
```

---

## 5. Composition Step Interface

```go id="qz9t1m"
type CompositionStep interface {
    Name() string
    Apply(ctx interface{}) error
}
```

---

## 6. Pipeline Execution

* steps execute sequentially
* each step mutates context
* order is deterministic

---

## 7. Step Registration

```go id="i6p3cs"
RegisterCompositionStep(target string, step CompositionStep, position string)
```

---

### Targets:

* `product`
* `listing`

---

### Positions:

* `start`
* `end`
* `before:<step>`
* `after:<step>`

---

### Example:

```go id="9k4yyt"
RegisterCompositionStep("product", ShippingEstimator{}, "after:base")
```

---

## 8. Default Steps (MVP)

---

### Product Pipeline

```text id="5tcx2t"
1. base
2. pricing
3. availability
```

---

### Listing Pipeline

```text id="m0ckbp"
1. base
2. filters
3. sorting
```

---

## 9. Step Responsibilities

---

### base

* load core data
* populate context

---

### pricing

* attach price data (via pricing pipeline)

---

### availability

* attach stock info

---

### filters (listing)

* build filter options

---

## 10. Plugin Use Cases

---

### Add computed block

```go id="q3rsm9"
type ShippingEstimator struct{}

func (s ShippingEstimator) Apply(ctx interface{}) error {
    pctx := ctx.(*ProductContext)

    cost := calculateShipping(pctx.Product.Weight, pctx.Country)

    pctx.Blocks = append(pctx.Blocks, Block{
        Type: "shipping_estimator",
        Data: map[string]interface{}{
            "cost": cost,
        },
    })

    return nil
}
```

---

---

### Modify filters (listing)

```go id="i7h9kj"
type CustomFilter struct{}

func (f CustomFilter) Apply(ctx interface{}) error {
    lctx := ctx.(*ListingContext)

    lctx.Filters = append(lctx.Filters, Filter{
        Name: "custom",
    })

    return nil
}
```

---

## 11. Determinism Rules

* steps must not depend on execution timing
* no randomness
* same input → same output

---

## 12. Idempotency

* running pipeline multiple times must yield same result

---

## 13. Error Handling

* step failure stops pipeline
* error propagated to caller

---

## 14. Events Integration

Optional:

```text id="g3l6w5"
composition.step.started
composition.step.completed
```

---

## 15. Performance

* avoid DB calls inside steps when possible
* prefer preloading data
* batch operations

---

## 16. Security Constraints

Plugins:

* must only modify context
* must not perform side effects

---

## 17. Frontend Contract

Backend returns:

```json id="c9k0y1"
{
  "product": { ... },
  "blocks": [
    { "type": "price" },
    { "type": "shipping_estimator" }
  ]
}
```

Frontend:

* decides layout
* renders blocks

---

## 18. Non-Goals

* no backend-controlled layout system
* no template engine complexity
* no XML configuration

---

## 19. Future Extensions

* caching per pipeline stage
* partial pipeline execution
* GraphQL integration (optional)

---

## 20. Summary

Composition pipeline is:

> a structured way to build API responses by incrementally enriching context objects.

It enables:

* PDP/PLP customization
* backend-driven logic
* frontend flexibility

without:

* controller bloat
* core modification
* template chaos

---
