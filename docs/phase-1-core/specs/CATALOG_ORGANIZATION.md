# 🗂️ Catalog Organization — Categories & Collections v0

## 1. Overview

This module provides:

* hierarchical categories (navigation)
* product assignment to categories
* dynamic collections (optional, rule-based)

Design goals:

* simple browsing structure
* flexible product grouping
* extensibility for advanced merchandising

---

## 2. Categories

---

### 2.1 Category Entity

```go
type Category struct {
    ID        string
    ParentID  *string

    Name      string
    Slug      string

    Position  int

    Meta      map[string]interface{}

    CreatedAt time.Time
    UpdatedAt time.Time
}
```

---

---

### 2.2 Tree Structure

* adjacency list (`parent_id`)
* no nested set / ltree in v0

---

👉 Keep it simple.

---

---

### 2.3 Product Assignment

---

```sql
product_categories (
  product_id TEXT,
  category_id TEXT
)
```

---

* many-to-many
* product can belong to multiple categories

---

---

## 3. Category API

---

### Get category tree

```http
GET /categories
```

---

### Get category

```http
GET /categories/{id}
```

---

### Get category products

```http
GET /categories/{id}/products
```

Supports:

* pagination
* filters
* sorting

---

---

## 4. Category Behavior

---

* categories are **pure structure**
* no pricing logic
* no business rules

---

👉 Important:

> categories do NOT contain logic

---

---

## 5. Collections

---

### 5.1 Collection Entity

```go
type Collection struct {
    ID     string
    Name   string
    Slug   string

    Type   string // "manual" | "dynamic"

    Rules  map[string]interface{}

    Meta   map[string]interface{}
}
```

---

---

### 5.2 Types

---

#### Manual Collection

* explicitly assigned products

```sql
collection_products (
  collection_id TEXT,
  product_id TEXT
)
```

---

---

#### Dynamic Collection

* rule-based

Example:

```json
{
  "price": { "lt": 50 },
  "category": "sale"
}
```

---

👉 Evaluated via:

* query builder
* or plugin

---

---

## 6. Collection API

---

```http
GET /collections
GET /collections/{id}
GET /collections/{id}/products
```

---

---

## 7. Integration with Composition Pipeline

---

### Category Page (PLP)

---

```go
ListingContext {
    CategoryID string
    Products   []Product
}
```

---

Plugins can:

* modify filters
* inject banners
* reorder products

---

---

### Collection Page

Same as category, but:

* source = collection rules

---

---

## 8. Sorting & Filtering

---

Basic support:

* price
* name
* created_at

---

Extended via plugins:

* relevance
* popularity
* custom attributes

---

---

## 9. SEO (Minimal)

---

Category fields:

```go
Meta: {
  "title": "...",
  "description": "..."
}
```

---

---

## 10. Events

---

* `category.created`
* `category.updated`
* `collection.created`

---

---

## 11. Extensibility

---

Plugins can:

* modify collection rules
* override product selection
* add filters

---

Example:

```go
RegisterCollectionResolver("dynamic", MyResolver{})
```

---

---

## 12. Storage (Postgres)

---

### Categories

```sql
categories (
  id UUID PRIMARY KEY,
  parent_id TEXT,
  name TEXT,
  slug TEXT,
  position INT,
  meta JSONB
)
```

---

---

### Product ↔ Category

```sql
product_categories (
  product_id TEXT,
  category_id TEXT
)
```

---

---

### Collections

```sql
collections (
  id UUID PRIMARY KEY,
  name TEXT,
  slug TEXT,
  type TEXT,
  rules JSONB,
  meta JSONB
)
```

---

---

## 13. Non-Goals (v0)

---

* no category permissions
* no multi-tree (single tree only)
* no advanced rule engine
* no search indexing

---

---

## 14. Summary

Categories & Collections v0 provide:

> a clean separation between navigation (categories) and merchandising logic (collections).

This enables:

* simple browsing
* flexible grouping
* future expansion without complexity explosion

---
