# 📦 Data Exchange — Import / Export Specification (v0)

## 1. Overview

Provides:

* product import/export
* attribute management
* CSV-based data exchange

---

## 2. Design

---

Import/export is implemented as a **pipeline**:

```text
parse → map → validate → persist
```

---

---

## 3. Supported Entities (v0)

---

* products
* attributes
* attribute groups

---

---

## 4. Format

---

Default:

* CSV

---

Example:

```csv
sku,name,price,color
SKU1,Shirt,19.99,red
```

Product CSV (current): required `name,slug,sku`; optional create-only `type`, `status` (`draft|active|archived`), bundle mode columns, `requires_linked_parent`, and visibility axes `visibility_catalog` / `visibility_search` / `visibility_individually` / `visibility_purchasable` (`auto|visible|hidden`). Downloadable + `active` on create is rejected — import as draft, attach files via `import:downloadable-files`, activate in admin.

---

---

## 5. Import

---

CLI:

```bash
app import:products file.csv
```

---

Optional:

```bash
app import:products --async file.csv
```

---

---

## 6. Export

---

CLI:

```bash
app export:products
```

---

---

## 7. Validation

---

* strict validation
* fail on error
* detailed error reporting

---

---

## 8. Attributes

---

```go
type Attribute struct {
    Code string
    Type string
}
```

---

---

## 9. Extensibility

---

Plugins may:

* add import mappings
* add attributes
* modify validation

---

---

## 10. Constraints

---

* flat data structure
* no dynamic schema changes at runtime
* no EAV

---

---

## 11. Summary

Data exchange v0 provides:

> a simple, predictable system for importing and exporting product data without unnecessary complexity.

---
