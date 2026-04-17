# 🧩 Composition Pipeline Specification (v1 — Typed)

## 1. Overview

The Composition Pipeline is responsible for **building API responses** in a deterministic and extensible way.

It replaces:

* controller-heavy logic
* ad-hoc data enrichment
* backend-driven UI layouts (Magento-style blocks)

Design goals:

* extensibility (plugins can enrich responses)
* determinism (predictable output)
* **type safety (no runtime casting)**
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

Each composition pipeline operates on a **typed context struct**.

---

### 3.1 Product Context (PDP)

```go
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

```go
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

```go
type Block struct {
    Type string
    Data map[string]interface{}
}
```

---

---

## 5. Pipeline Step Interface (Typed)

---

```go
type PipelineStep[T any] interface {
    Name() string
    Apply(ctx *T) error
}
```

---

### Key Rule

> Steps are strongly typed and must operate on a specific context type.

---

## 6. Pipeline

---

```go
type Pipeline[T any] struct {
    steps []PipelineStep[T]
}

func (p *Pipeline[T]) Execute(ctx *T) error {
    for _, step := range p.steps {
        if err := step.Apply(ctx); err != nil {
            return err
        }
    }
    return nil
}
```

---

---

## 7. Pipeline Registration (Typed)

---

### Product pipeline

```go
func RegisterProductStep(step PipelineStep[ProductContext], position string)
```

---

### Listing pipeline

```go
func RegisterListingStep(step PipelineStep[ListingContext], position string)
```

---

### Positions

* `start`
* `end`
* `before:<step>`
* `after:<step>`

---

---

### Example

```go
RegisterProductStep(ShippingEstimator{}, "after:pricing")
```

---

👉 Type safety is enforced at compile time.

---

---

## 8. Default Steps (MVP)

---

### Product Pipeline

```text
1. base
2. pricing
3. availability
```

---

### Listing Pipeline

```text
1. base
2. filters
3. sorting
```

---

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

---

## 10. Plugin Use Cases (Typed)

---

### Add computed block

```go
type ShippingEstimator struct{}

func (s ShippingEstimator) Name() string {
    return "shipping_estimator"
}

func (s ShippingEstimator) Apply(ctx *ProductContext) error {
    cost := calculateShipping(ctx.Product.Weight, ctx.Country)

    ctx.Blocks = append(ctx.Blocks, Block{
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

```go
type CustomFilter struct{}

func (f CustomFilter) Name() string {
    return "custom_filter"
}

func (f CustomFilter) Apply(ctx *ListingContext) error {
    ctx.Filters = append(ctx.Filters, Filter{
        Name: "custom",
    })

    return nil
}
```

---

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

## 14. Events Integration (Optional)

```text
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

```json
{
  "product": { ... },
  "blocks": [
    { "type": "price" },
    { "type": "shipping_estimator" }
  ]
}
```

---

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

> a **type-safe, extensible system** for building API responses by enriching context objects step-by-step.

It enables:

* PDP/PLP customization
* backend-driven logic
* frontend flexibility

without:

* controller bloat
* runtime casting
* hidden behavior

---

## Guiding Principle

> If it compiles, it should be safe to execute.

---
