# 🔎 Search System — v0 Specification (Postgres-first, Extensible)

## 1. Overview

Search system provides:

* product search
* filtering and sorting
* extensible backend (Postgres by default)
* optional external engines (Meilisearch, etc.)

Design goals:

* zero-dependency default
* pluggable search engines
* consistent API regardless of backend
* good enough performance out of the box

---

## 2. Core Concepts

---

### 2.1 Search Query

```go id="f7y9dz"
type SearchQuery struct {
    Text       string

    Filters    map[string]interface{}

    Sort       string

    Limit      int
    Offset     int

    Meta       map[string]interface{}
}
```

---

---

### 2.2 Search Result

```go id="4v2r8d"
type SearchResult struct {
    Products   []Product

    Total      int

    Facets     map[string][]FacetValue
}
```

---

### 2.3 Facet

```go id="tx8qbb"
type FacetValue struct {
    Value string
    Count int
}
```

---

## 3. Search Engine Interface

---

```go id="b4b8hy"
type SearchEngine interface {
    Name() string

    IndexProduct(p Product) error

    Search(query SearchQuery) (SearchResult, error)
}
```

---

---

## 4. Default Implementation (Core)

---

### PostgresSearchEngine

Uses:

* full-text search (`tsvector`)
* SQL filtering

---

### Example Query

```sql id="wdn5wv"
SELECT *
FROM products
WHERE to_tsvector(name || ' ' || description)
      @@ plainto_tsquery($1)
LIMIT $2 OFFSET $3;
```

---

---

## 5. Indexing Strategy

---

### v0:

* index on write (simple)
* optional batch reindex (CLI)

---

### CLI:

```bash id="t7h7sm"
app search:reindex
```

---

---

## 6. Filtering

---

Basic filters:

* category
* price range
* attributes (via JSONB)

---

Example:

```json id="3u3z9r"
{
  "filters": {
    "category": "shoes",
    "price": { "lt": 100 }
  }
}
```

---

---

## 7. Sorting

---

Supported:

* price
* name
* created_at

---

Extensible via plugins:

* relevance
* popularity
* custom ranking

---

---

## 8. Facets (Basic)

---

Example response:

```json id="g3sk0p"
{
  "facets": {
    "color": [
      { "value": "red", "count": 10 },
      { "value": "blue", "count": 5 }
    ]
  }
}
```

---

---

## 9. API

---

```http id="f1yzp6"
GET /search?q=shoes&limit=20
```

---

Supports:

* filters
* sorting
* pagination

---

---

## 10. Integration with Collections

---

Dynamic collections use:

```go id="3g4m6v"
SearchEngine.Search(query)
```

---

👉 Same engine powers:

* search page
* category listing (optional)
* collections

---

---

## 11. External Engines (Plugins)

---

### Example: Meilisearch

```go id="ktc7mv"
type MeiliSearchEngine struct{}
```

---

Registered via:

```go id="7t9xgr"
RegisterSearchEngine("meilisearch", MeiliSearchEngine{})
```

---

---

## 12. Engine Selection

---

Configured via:

```yaml id="d77pwb"
search:
  engine: postgres
```

---

---

## 13. Event Integration

---

* `product.created` → index
* `product.updated` → reindex
* `product.deleted` → remove

---

---

## 14. Performance Considerations

---

### Postgres:

* good for small/medium stores
* simple deployment

---

### External engine:

* better relevance
* faster queries at scale

---

---

## 15. Extensibility

---

Plugins can:

* override ranking
* add filters
* modify query

---

Example:

```go id="8z2jgo"
RegisterSearchModifier(func(q *SearchQuery) {
    q.Filters["in_stock"] = true
})
```

---

---

## 16. Storage (Postgres)

---

### Products table extension

```sql id="2z3vka"
ALTER TABLE products ADD COLUMN search_vector tsvector;
```

---

---

## 17. Non-Goals (v0)

---

* no typo tolerance (unless external engine)
* no AI search
* no personalization
* no analytics

---

---

## 18. Summary

Search v0 provides:

> a simple, pluggable search system with Postgres as the default and external engines as optional upgrades.

It enables:

* immediate usability
* scalability path
* consistent search interface

---
