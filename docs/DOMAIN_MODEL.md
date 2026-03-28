# 🧱 Domain Model & Database Schema

## 1. Design Principles

* minimal but extensible
* normalized where it matters, flexible where needed
* avoid premature abstraction
* use PostgreSQL features (JSONB, constraints, indexes)
* keep core tables stable

---

## 2. Core Domains

### 2.1 Catalog

* products
* variants
* attributes

### 2.2 Pricing

* base prices
* price overrides

### 2.3 Inventory

* stock tracking
* reservations

---

## 3. Products & Variants

---

### 3.1 Products

Represents a logical product (e.g. "T-Shirt")

```sql
CREATE TABLE products (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    slug            TEXT UNIQUE NOT NULL,
    description     TEXT,
    status          TEXT NOT NULL DEFAULT 'draft',
    attributes      JSONB DEFAULT '{}'::jsonb,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW()
);
```

---

### 3.2 Variants

Sellable units (e.g. size M, color red)

```sql
CREATE TABLE variants (
    id              TEXT PRIMARY KEY,
    product_id      TEXT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    sku             TEXT UNIQUE NOT NULL,
    name            TEXT,
    attributes      JSONB DEFAULT '{}'::jsonb,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW()
);
```

---

### 3.3 Attributes Strategy

* stored as JSONB:

```json
{
  "size": "M",
  "color": "red"
}
```

* allows flexibility without schema explosion
* indexed selectively if needed

---

## 4. Pricing

---

### 4.1 Base Prices

```sql
CREATE TABLE prices (
    id              TEXT PRIMARY KEY,
    variant_id      TEXT NOT NULL REFERENCES variants(id) ON DELETE CASCADE,
    currency        TEXT NOT NULL,
    amount          NUMERIC(12,2) NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW()
);
```

---

### 4.2 Price Rules (Optional, extend later)

```sql
CREATE TABLE price_rules (
    id              TEXT PRIMARY KEY,
    name            TEXT,
    priority        INT NOT NULL DEFAULT 0,
    conditions      JSONB,
    actions         JSONB,
    active          BOOLEAN DEFAULT TRUE
);
```

---

👉 Rules are intentionally JSON-driven:

* handled in application layer
* allows plugins to extend logic

---

## 5. Inventory

---

### 5.1 Stock Levels

```sql
CREATE TABLE inventory (
    variant_id      TEXT PRIMARY KEY REFERENCES variants(id) ON DELETE CASCADE,
    quantity        INT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW()
);
```

---

### 5.2 Reservations

```sql
CREATE TABLE inventory_reservations (
    id              TEXT PRIMARY KEY,
    variant_id      TEXT NOT NULL REFERENCES variants(id),
    quantity        INT NOT NULL,
    order_id        TEXT,
    expires_at      TIMESTAMP,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW()
);
```

---

### Reservation Model

* stock is reduced in two steps:

  1. reservation
  2. confirmation

---

### Available Stock Calculation:

```sql
available = quantity - SUM(active reservations)
```

---

## 6. Relationships Overview

```id="p1g8oz"
products (1) ──── (N) variants
variants (1) ──── (N) prices
variants (1) ──── (1) inventory
variants (1) ──── (N) reservations
```

---

## 7. Indexing Strategy

Recommended indexes:

```sql
CREATE INDEX idx_variants_product_id ON variants(product_id);
CREATE INDEX idx_prices_variant_id ON prices(variant_id);
CREATE INDEX idx_inventory_res_variant_id ON inventory_reservations(variant_id);
```

Optional (JSONB):

```sql
CREATE INDEX idx_variants_attributes ON variants USING GIN (attributes);
```

---

## 8. Extensibility Strategy

---

### 8.1 JSONB Fields

Used for:

* custom attributes
* plugin data

---

### 8.2 Plugin-Owned Tables

Plugins can create their own tables:

Example:

```sql
stripe_payments
custom_discounts
```

---

### 8.3 No Core Table Mutation

Plugins must NOT:

* alter core tables
* add columns to core schema

---

## 9. Events Integration

Each domain emits events:

### Catalog:

* `catalog.product.created`
* `catalog.product.updated`

### Variants:

* `catalog.variant.created`

### Pricing:

* `pricing.updated`

### Inventory:

* `inventory.reserved`
* `inventory.released`

---

## 10. Data Integrity Rules

* foreign keys enforced
* no orphan records
* reservations must expire or resolve

---

## 11. Future Extensions

* multi-warehouse inventory
* multi-currency pricing rules
* localized product data
* product collections / categories

---

## 12. Non-Goals (MVP)

* complex promotions engine
* bundle products
* subscription logic

---

## 13. Summary

This model provides:

* a stable core schema
* flexible extension points
* minimal complexity
* real-world capability

> Start simple. Extend via events and plugins—not schema mutations.

---
