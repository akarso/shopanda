# 🧩 Layout System — v0 Specification

## 1. Overview

Provides:

* structural layout definition
* slot-based rendering
* predictable extension points

---

## 2. Layout

---

Layout defines:

* page type
* available slots

---

---

## 3. Slots

---

Examples:

```text
header
footer
product.main
product.sidebar
```

---

---

## 4. Blocks

---

```go
type Block struct {
    Slot string
    Type string
}
```

---

Blocks are assigned to slots.

---

---

## 5. Rendering

---

Templates render slots:

```html
{{ renderSlot "product.main" }}
```

---

---

## 6. Themes

---

Themes define:

* templates
* slot placement

---

---

## 7. Admin

---

Admin uses same layout system with different slots.

---

---

## 8. Constraints

---

* no layout configuration files
* no dynamic layout engine
* slots defined in code

---

---

## 9. Summary

Layout system provides:

> a simple and predictable way to structure pages using slots and block composition.

---
