# 🔍 Meilisearch Integration — Specification

## 1. Overview

Plugin implementing `SearchEngine` interface for Meilisearch:

* typo-tolerant full-text search
* faceted filtering
* autocomplete / suggestions
* real-time index sync via events

Design goals:

* drop-in replacement for Postgres search
* no core modifications
* event-driven index updates
* graceful degradation (fallback to Postgres if Meilisearch unavailable)

---

## 2. Meilisearch Adapter (PR-216)

---

### 2.1 Plugin Registration

```go
type MeilisearchEngine struct {
    client *meilisearch.Client
    index  string
}

func (e *MeilisearchEngine) Name() string { return "meilisearch" }
```

Registered via plugin system or config toggle:

```yaml
search:
  engine: meilisearch   # "postgres" (default) or "meilisearch"
  meilisearch:
    host: http://localhost:7700
    api_key: ${SHOPANDA_SEARCH_MEILI_KEY}
    index: products
```

---

### 2.2 SearchEngine Implementation

```go
func (e *MeilisearchEngine) Search(ctx context.Context, q SearchQuery) (*SearchResult, error)
func (e *MeilisearchEngine) Index(ctx context.Context, doc SearchDocument) error
func (e *MeilisearchEngine) Delete(ctx context.Context, id string) error
func (e *MeilisearchEngine) Reindex(ctx context.Context, docs []SearchDocument) error
```

---

### 2.3 Index Schema

```json
{
  "primaryKey": "id",
  "searchableAttributes": ["name", "description", "sku", "category_names"],
  "filterableAttributes": ["category_id", "price", "in_stock", "attributes"],
  "sortableAttributes": ["price", "name", "created_at"],
  "displayedAttributes": ["*"]
}
```

---

### 2.4 Document Mapping

```go
type SearchDocument struct {
    ID            string
    Name          string
    Description   string
    SKU           string
    CategoryID    string
    CategoryNames []string
    Price         int64       // cents, for filtering/sorting
    Currency      string
    InStock       bool
    Attributes    map[string]interface{}
    ImageURL      string
    Slug          string
    CreatedAt     int64       // unix timestamp
}
```

---

### 2.5 Event-Driven Sync

```go
On("product.created", func(e Event) { engine.Index(ctx, toDoc(e.Product)) })
On("product.updated", func(e Event) { engine.Index(ctx, toDoc(e.Product)) })
On("product.deleted", func(e Event) { engine.Delete(ctx, e.ProductID) })
On("stock.updated",   func(e Event) { engine.Index(ctx, toDoc(e.Product)) })
```

Index updates dispatched as async jobs for reliability.

---

### 2.6 Reindex Command

Existing `app search:reindex` command calls `engine.Reindex()`:

```text
1. Fetch all products from DB
2. Map to SearchDocument
3. Batch index via Meilisearch API
4. Report count + duration
```

---

## 3. Autocomplete Endpoint (PR-217)

---

### 3.1 Endpoint

```http
GET /api/v1/search/suggest?q=sne&limit=5
```

---

### 3.2 Response

```json
{
  "suggestions": [
    { "text": "Sneakers", "type": "product", "url": "/products/sneakers-white" },
    { "text": "Sneaker Care Kit", "type": "product", "url": "/products/sneaker-care-kit" }
  ]
}
```

---

### 3.3 Implementation

Uses Meilisearch prefix search with limited attributes:

```go
func (e *MeilisearchEngine) Suggest(ctx context.Context, prefix string, limit int) ([]Suggestion, error)
```

* Return product names + slugs
* Limit to `limit` results (default 5, max 10)
* Debounce-friendly: fast responses (< 50ms)

---

### 3.4 Fallback

If Meilisearch is unavailable, `Suggest()` returns empty list (no error). Full search falls back to Postgres implementation.

---

## 4. Faceted Search

---

### 4.1 Facets via Meilisearch

Existing `SearchResult.Facets` populated from Meilisearch facet distribution:

```json
{
  "facets": {
    "category_id": { "cat_1": 15, "cat_2": 8 },
    "attributes.color": { "red": 5, "blue": 3 }
  }
}
```

---

### 4.2 Filter Syntax

Query params mapped to Meilisearch filter syntax:

```text
GET /search?q=shoes&category=cat_1&price_min=2000&price_max=5000

→ Meilisearch filter: "category_id = cat_1 AND price >= 2000 AND price <= 5000"
```

---

## 5. Non-Goals (v0)

* No multi-index search (products only)
* No search analytics / popular queries
* No synonyms configuration UI (can set via Meilisearch API directly)
* No geo-search
* No personalized results
