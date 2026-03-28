# 🚚 Shipping Domain — v0 Specification

## 1. Overview

Shipping module handles:

* shipping methods
* shipping rate calculation
* shipment assignment (basic)
* integration with checkout

Design goals:

* simple default behavior
* extensible for real-world complexity
* supports multiple shipping strategies
* integrates with pricing and checkout workflow

---

## 2. Core Concepts

---

### 2.1 Shipping Address

```go
type Address struct {
    FirstName string
    LastName  string

    Street    string
    City      string
    Postcode  string

    Country   string
}
```

---

### 2.2 Shipment (basic)

Represents a shipping unit.

```go
type Shipment struct {
    ID        string
    OrderID   string

    Address   Address

    Method    string

    Cost      Money
    Currency  string

    Meta      map[string]interface{}
}
```

---

👉 v0 assumption:

* **one shipment per order**

---

### 2.3 Shipping Method

```go
type ShippingMethod struct {
    Code        string
    Name        string

    Description string

    Active      bool
}
```

---

---

## 3. Shipping Rate

---

```go
type ShippingRate struct {
    MethodCode string

    Price      Money
    Currency   string

    Meta       map[string]interface{}
}
```

---

---

## 4. Shipping Provider Interface

---

```go
type ShippingProvider interface {
    Name() string

    GetRates(ctx ShippingContext) ([]ShippingRate, error)
}
```

---

### ShippingContext

```go
type ShippingContext struct {
    Items      []CartItem

    Address    Address

    Currency   string

    Meta       map[string]interface{}
}
```

---

---

## 5. Core Flow

---

### 5.1 Get Shipping Methods

```http
POST /shipping/rates
```

---

### Input:

```json
{
  "cart_id": "cart_123",
  "address": { ... }
}
```

---

### Flow:

1. load cart items
2. build ShippingContext
3. call providers
4. aggregate rates
5. return available methods

---

---

## 6. Default Provider (Core)

---

### Flat Rate Provider

```go
type FlatRateProvider struct{}
```

---

Behavior:

* always returns one method
* fixed price

---

👉 Ensures:

* system works out of the box

---

---

## 7. Multiple Providers

---

System supports:

* flat rate (core)
* table rates (plugin)
* carrier APIs (plugin)

---

```go
RegisterShippingProvider("flat", FlatRateProvider{})
RegisterShippingProvider("dhl", DHLProvider{})
```

---

---

## 8. Selection

---

Customer selects method:

```json
{
  "method": "flat"
}
```

---

Stored in:

* cart
* later copied to order

---

---

## 9. Checkout Integration

---

### Step:

```text
select_shipping → reserve_inventory → create_order
```

---

Shipping cost:

* added via pricing pipeline

---

---

## 10. Pricing Integration

---

Shipping cost is treated as:

```go
Adjustment{
    Type:   "shipping",
    Amount: Money{Amount: 1000, Currency: "EUR"},
}
```

---

---

## 11. Real-World Extensions

---

### 11.1 Multi-shipping (future)

* split cart into multiple shipments
* each shipment has:

  * own address
  * own method

---

### 11.2 Virtual Products

* mark item as `requires_shipping = false`
* exclude from shipping context

---

### 11.3 Billing vs Shipping Address

* separate fields in checkout context
* shipping uses shipping address only

---

### 11.4 Gift Shipping

* additional fields in address/meta
* handled via plugins

---

### 11.5 Carrier Integration

Plugins can:

* call external APIs
* calculate dynamic rates

---

---

## 12. Events

---

* `shipping.rates.requested`
* `shipping.method.selected`
* `shipment.created`

---

---

## 13. Storage (Postgres)

---

### Shipments

```sql
shipments (
  id UUID PRIMARY KEY,
  order_id TEXT,
  method TEXT,
  cost NUMERIC,
  currency TEXT,
  address JSONB,
  meta JSONB
)
```

---

---

## 14. Extensibility

---

Plugins can:

* add providers
* modify rates
* filter methods

---

Example:

```go
RegisterShippingProvider("ups", UPSProvider{})
```

---

---

## 15. Constraints

---

* providers must be deterministic
* no side effects during rate calculation
* must be idempotent

---

---

## 16. Non-Goals (v0)

---

* no shipment tracking
* no label generation
* no multi-warehouse logic
* no delivery scheduling

---

---

## 17. Summary

Shipping v0 provides:

> a simple, provider-based system for calculating and selecting shipping methods, designed to scale into complex logistics scenarios via plugins.

It ensures:

* drop-in usability (flat rate)
* extensibility (providers)
* compatibility with pricing and checkout

---
