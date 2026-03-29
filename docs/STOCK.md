# 📦 Inventory / Stock Management — v0 Specification

## 1. Overview

Provides:

* stock tracking per SKU
* reservation system
* API and CSV integration

---

## 2. Data Model

---

### Inventory

```sql
variant_id UUID PRIMARY KEY,
quantity INT NOT NULL
```

---

### Reservations

```sql
variant_id UUID,
quantity INT,
reference_id UUID
```

---

---

## 3. Availability

---

```text
available = quantity - reserved
```

---

---

## 4. Operations

---

* check availability
* reserve stock
* release reservation
* update stock

---

---

## 5. API

---

```http
GET /inventory/{variant_id}
POST /inventory/{variant_id}
POST /inventory/bulk
```

---

---

## 6. Import / Export

---

```bash
app import:inventory
app export:inventory
```

---

---

## 7. Checkout Integration

---

```text
validate → reserve → order
```

---

---

## 8. Constraints

---

* no multi-warehouse (v0)
* no complex allocation
* strong consistency required

---

---

## 9. Extensibility

---

* external WMS
* advanced inventory logic

---

---

## 10. Summary

Inventory system provides:

> a simple and reliable mechanism for managing stock using reservations and consistent updates.

---
