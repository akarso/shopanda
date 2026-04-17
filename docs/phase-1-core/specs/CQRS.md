# 📊 Read Models & Projections — v0 Specification (Postgres-Based, Lightweight)

## 1. Overview

Read models (projections) provide:

* optimized data for queries (PDP, PLP, search, admin)
* denormalized views of domain data
* improved performance and simpler queries

Design goals:

* optional (not required for MVP)
* Postgres-based
* simple to implement and maintain
* event-driven updates (future)

---

## 2. Core Concept

---

### Write Model (source of truth)

* normalized tables
* domain-driven
* used for commands

---

### Read Model (projection)

* denormalized tables
* optimized for reads
* derived from write model

---

---

## 3. MVP Stage (Implement NOW)

---

### ❗ IMPORTANT

> No separate read models yet.

---

### Use:

* direct queries on normalized tables
* composition pipeline for shaping responses

---

### Example:

```go
ProductContext {
  Product
  Variants
  Price
}
```

---

👉 This is sufficient for:

* small/medium stores
* early development
* fast iteration

---

---

## 4. When to Introduce Read Models

---

Introduce projections ONLY when:

* queries become too complex
* performance degrades
* repeated joins appear everywhere

---

---

## 5. Projection Model (Future)

---

### Example: Product Projection

```sql
product_view (
  id TEXT,
  name TEXT,
  slug TEXT,

  price NUMERIC,
  currency TEXT,

  in_stock BOOLEAN,

  category_ids TEXT[],

  search_text TEXT
)
```

---

---

## 6. Projection Builder

---

Projections are built from:

* domain events
* or batch jobs

---

### Interface

```go
type Projector interface {
    Name() string

    Handle(event Event) error
}
```

---

---

## 7. Event-Driven Updates (Future)

---

Example:

```go
On("product.updated", func(e Event) {
    projector.UpdateProduct(e)
})
```

---

---

## 8. Batch Rebuild (Important)

---

CLI:

```bash
app projections:rebuild
```

---

Used for:

* initial build
* recovery
* schema changes

---

---

## 9. Query Layer

---

Instead of:

```sql
JOIN products + variants + inventory
```

---

Use:

```sql
SELECT * FROM product_view WHERE ...
```

---

---

## 10. Integration with Composition Pipeline

---

Composition pipeline reads from:

* write model (MVP)
* projection (future)

---

Transparent switch:

```go
repo.GetProduct() // implementation decides source
```

---

---

## 11. Consistency Model

---

### v0:

* eventual consistency (future)
* strong consistency (MVP, no projections)

---

---

## 12. Storage

---

Stored in Postgres:

* regular tables
* optionally materialized views

---

---

## 13. Extensibility

---

Plugins can:

* define projections
* extend projection schema
* hook into events

---

Example:

```go
RegisterProjector("product_view", MyProjector{})
```

---

---

## 14. Constraints

---

* projections must be rebuildable
* projections must not be source of truth
* projection logic must be deterministic

---

---

## 15. Implementation Phases

---

### 🟢 Phase 1 (NOW)

* no projections
* composition pipeline only
* direct DB queries

---

### 🟡 Phase 2 (NEXT)

* introduce simple projections (product_view)
* batch rebuild only (no events yet)

---

### 🔴 Phase 3 (LATER)

* event-driven projections
* partial updates
* multiple projections

---

---

## 16. Trade-offs

---

### Benefits:

* faster queries
* simpler API layer
* scalable reads

---

### Costs:

* duplication
* sync complexity
* eventual consistency

---

---

## 17. Summary

Read models v0 provide:

> an optional optimization layer that can be introduced incrementally without affecting core architecture.

Key principle:

> Start without projections. Add them only when pain appears.

---
