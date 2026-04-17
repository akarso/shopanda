# 🎨 Rendering & Theme System — v1 Specification

## 1. Overview

Provides:

* unified rendering system
* shared pipeline for core and plugins
* SSR + API support

---

## 2. Rendering Flow

---

```text
request → context → composition → view model → render
```

---

---

## 3. View Model

---

Typed struct passed to templates.

---

---

## 4. Themes

---

* template-based
* located in `/themes/<name>`

---

---

## 5. Blocks

---

```go
type Block struct {
    Type string
    Data map[string]any
}
```

---

Used for extensibility.

---

---

## 6. Core Pages

---

* product
* category
* CMS

---

All use composition pipeline.

---

---

## 7. Admin

---

Admin uses same rendering system.

---

---

## 8. API

---

Same composition pipeline, different output (JSON).

---

---

## 9. Constraints

---

* no separate rendering paths
* no controller-specific rendering
* no layout engines

---

---

## 10. Summary

Rendering system provides:

> a unified and extensible way to render all application views using composition pipelines and themes.

---
