# 🎨 Frontend Guide — Strategy & Structure (FRONTEND.md, v0)

## 1. Overview

Defines frontend approach:

* SSR-first
* minimal JavaScript
* theme-based rendering
* optional headless mode

---

## 2. Core Philosophy

---

> Backend decides WHAT
> Frontend decides HOW and WHERE

---

---

## 3. Rendering Model

---

### Default: Server-Side Rendering (SSR)

* Go templates
* HTML generated on server
* no JS required

---

---

## 4. Themes

---

Location:

```plaintext
/themes/default
/themes/_starter
```

---

Structure:

```plaintext
/templates
/static/css
/static/js
theme.yaml
layout.yaml
```

---

---

## 5. Templates

---

* use Go `html/template`
* keep logic minimal
* no heavy templating systems

---

---

## 6. Blocks & Slots (Future Integration)

---

* backend → provides blocks
* theme → defines slots

---

Example:

```html
<div data-slot="product.sidebar"></div>
```

---

---

## 7. JavaScript Strategy

---

### Default:

* vanilla JS
* progressive enhancement
* vendored **htmx 1.9.12** in `themes/default/static/js/htmx.min.js` (do not chase 2.x)

---

### Optional (theme author's choice, not core):

* Alpine or similar for local UI state
* Headless: any stack; see [HEADLESS.md](../../../guides/HEADLESS.md) and `examples/headless-vanilla`

Forbidden in core: React/Vue/Angular storefront, npm build step

---

---

## 8. Styling

---

* plain CSS works
* SCSS optional
* no build system required for v0

---

---

## 9. Headless Mode (Optional)

---

* use REST API
* frontend fully decoupled

---

---

## 10. Do NOT Do

---

❌ no React SSR in core
❌ no hydration by default
❌ no heavy frontend frameworks required

---

---

## 11. Extensibility

---

Themes can:

* override templates
* define layout
* render blocks

---

---

## 12. Future Extensions

---

* block rendering system
* asset pipeline
* theme inheritance

---

---

## 13. Guiding Principle

---

> Keep frontend simple. Complexity must be optional.

---
