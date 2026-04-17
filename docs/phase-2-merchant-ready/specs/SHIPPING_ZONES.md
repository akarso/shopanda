# 🚚 Shipping Zones & Rates — Specification

## 1. Overview

Replaces flat-rate-only shipping with:

* geographic shipping zones
* tiered rate tables per zone
* weight-based calculation
* free shipping threshold

Design goals:

* backward-compatible (flat rate still works)
* zone model covers most real-world scenarios
* extensible for carrier API plugins later
* integrates with existing checkout workflow

---

## 2. Shipping Zones (PR-210)

---

### 2.1 Zone Entity

```go
type Zone struct {
    ID        string
    Name      string    // "Domestic", "EU", "International"
    Countries []string  // ISO 3166-1 alpha-2 codes
    Priority  int       // higher priority wins on overlap
    Active    bool
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

---

### 2.2 Rate Tier

```go
type RateTier struct {
    ID         string
    ZoneID     string
    MinWeight  float64   // kg, 0 = no minimum
    MaxWeight  float64   // kg, 0 = no maximum
    Price      Money
    Currency   string
}
```

---

### 2.3 Zone Resolution

```text
1. Match customer country to zone (by Countries list)
2. If multiple zones match, use highest Priority
3. If no zone matches, fall back to default zone (if configured)
4. If no default, return error "no shipping available"
```

---

### 2.4 Database Schema

```sql
CREATE TABLE shipping_zones (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    countries  TEXT[] NOT NULL,
    priority   INT NOT NULL DEFAULT 0,
    active     BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE shipping_rate_tiers (
    id         TEXT PRIMARY KEY,
    zone_id    TEXT NOT NULL REFERENCES shipping_zones(id),
    min_weight NUMERIC NOT NULL DEFAULT 0,
    max_weight NUMERIC NOT NULL DEFAULT 0,
    price      BIGINT NOT NULL,
    currency   TEXT NOT NULL DEFAULT 'EUR'
);
```

---

### 2.5 Admin Endpoints

```http
GET    /api/v1/admin/shipping/zones
POST   /api/v1/admin/shipping/zones
PUT    /api/v1/admin/shipping/zones/{id}
DELETE /api/v1/admin/shipping/zones/{id}

GET    /api/v1/admin/shipping/zones/{id}/rates
POST   /api/v1/admin/shipping/zones/{id}/rates
PUT    /api/v1/admin/shipping/rates/{id}
DELETE /api/v1/admin/shipping/rates/{id}
```

---

## 3. Weight-Based Calculation (PR-211)

---

### 3.1 Product Weight

Add to variant entity:

```go
type Variant struct {
    // ... existing fields
    Weight     float64 // kg
    Length     float64 // cm (optional, future)
    Width      float64 // cm (optional, future)
    Height     float64 // cm (optional, future)
}
```

Migration: `ALTER TABLE variants ADD COLUMN weight NUMERIC DEFAULT 0`

---

### 3.2 Rate Lookup

```go
func (p *ZoneProvider) GetRates(ctx ShippingContext) ([]ShippingRate, error) {
    // 1. Resolve zone from address.Country
    // 2. Sum item weights
    // 3. Find rate tier matching total weight
    // 4. Return rate
}
```

---

### 3.3 Weight Calculation

```text
totalWeight = sum(item.variant.weight * item.quantity)
```

Tier matching:

```text
tier where min_weight <= totalWeight AND (max_weight == 0 OR max_weight >= totalWeight)
```

---

## 4. Free Shipping Threshold (PR-212)

---

### 4.1 Configuration

Per-zone setting:

```go
type Zone struct {
    // ... existing fields
    FreeShippingThreshold Money  // 0 = disabled
}
```

---

### 4.2 Application

In `GetRates()`:

```text
if cart subtotal >= zone.FreeShippingThreshold:
    return ShippingRate{ Price: 0, Method: "free_shipping" }
```

Free shipping appears as a separate rate option alongside paid rates.

---

### 4.3 Display

API response includes both options when threshold is met:

```json
{
  "rates": [
    { "method": "standard", "price": 500, "currency": "EUR" },
    { "method": "free_shipping", "price": 0, "currency": "EUR", "label": "Free shipping (orders over €50)" }
  ]
}
```

---

## 5. Provider Composition

---

### Existing flat rate provider remains as fallback:

```go
providers := []ShippingProvider{
    zoneProvider,     // zone-based (primary)
    flatRateProvider, // fallback if no zones configured
}
```

Zone provider takes priority. Flat rate returns results only when zone provider returns empty.

---

## 6. Non-Goals (v0)

* No carrier API integrations (FedEx, UPS, DHL) — future plugins
* No real-time rate fetching from external APIs
* No dimensional weight calculation
* No shipping label generation
* No tracking number management (beyond existing basic field)
