# 🎨 Frontend & Templating Guidelines — v0

## 1. Overview

This document defines the **rules of conduct** for building frontend (storefront + admin) in the system.

Goal:

> Provide a **merchant-ready UI** while preserving:

* simplicity
* zero-build workflow
* SSR-first architecture

---

## 2. Core Principles

---

### 2.1 Server-Side First

* All pages MUST render meaningful HTML on first request
* JS MUST NOT be required for core functionality

---

### 2.2 Progressive Enhancement

* JS enhances UX
* System must work without JS

---

### 2.3 No Build Step

* No npm
* No bundlers
* No transpilation

---

### 2.4 Minimal Dependencies

Allowed:

* single-file libraries (vendored)
* optional enhancements

Forbidden:

* large dependency trees
* frameworks requiring build step

---

### 2.5 Unified Rendering

* Core and plugins use same rendering system
* No special frontend paths for core

---

## 3. Technology Choices

---

### 3.1 JavaScript

Primary:

* native JS (ES6+)

Optional:

* htmx (vendored)

Forbidden:

* Inertia.js
* React / Vue / Angular (in core)

---

### 3.2 CSS

Allowed:

* plain CSS
* Pico.css (optional)

Optional:

* SCSS (compiled outside core)

---

## 4. Project Structure

---

```text
/themes/
  default/
    templates/
    assets/
      js/
        core.js
        htmx.min.js (optional)
      css/
    blocks/
  admin/
    templates/
    assets/
```

---

## 5. JavaScript Guidelines

---

### 5.1 Core JS Layer

Provide minimal utilities:

```js
function api(url, options = {}) {
  return fetch(url, {
    headers: { "Content-Type": "application/json" },
    ...options
  }).then(r => r.json())
}
```

---

### 5.2 Responsibilities

JS may:

* enhance forms
* fetch dynamic data
* update small UI fragments

JS must NOT:

* control routing
* replace SSR
* hold critical business logic

---

### 5.3 Event Handling

Prefer:

* native DOM events
* simple listeners

Avoid:

* global state managers
* complex client-side orchestration

---

## 6. htmx Usage (Optional)

---

Use htmx for:

* form submissions
* partial updates
* admin UI interactions

Example:

```html
<button hx-post="/cart/add" hx-swap="outerHTML">
  Add to cart
</button>
```

---

Constraints:

* must degrade gracefully without JS
* must not replace full page rendering

---

## 7. Templates

---

### 7.1 Rules

* templates must be simple
* no heavy logic
* no data fetching

---

### 7.2 Data Source

Templates receive:

* fully prepared view models

---

### 7.3 Example

```html
<h1>{{ .Product.Name }}</h1>
<p>{{ .Price.Formatted }}</p>
```

---

## 8. Layout & Slots

---

* layouts define structure
* slots define insertion points

Example:

```html
{{ renderSlot "product.main" }}
{{ renderSlot "product.sidebar" }}
```

---

Plugins may inject blocks into slots.

---

## 9. Storefront Guidelines

---

* prioritize performance
* minimize JS usage
* use API only for dynamic fragments

Examples:

* cart count
* stock updates

---

## 10. Admin Guidelines

---

Admin may:

* use more JS than storefront
* rely on htmx for productivity

Still must:

* avoid heavy frameworks
* remain SSR-first

---

## 11. Assets

---

### 11.1 Delivery

* served via CDN if configured
* no bundling required

---

### 11.2 Versioning

* use file hashing or version suffix

---

## 12. Accessibility & UX

---

* forms must work without JS
* navigation must be functional without JS
* avoid JS-only interactions

---

## 13. Anti-Patterns

---

Forbidden:

* SPA architecture in core
* hydration-heavy UI
* client-side routing
* dependency explosion

---

## 14. Extensibility

---

Plugins may:

* add JS files
* add blocks
* enhance templates

But must follow:

* same principles
* no build step requirement

---

## 15. Summary

---

Frontend system is:

> **SSR-first, dependency-light, progressively enhanced**

This ensures:

* fast performance
* easy deployment
* long-term maintainability

---
