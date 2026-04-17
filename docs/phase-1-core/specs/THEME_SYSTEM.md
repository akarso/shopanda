# 🎨 Theme System — v1 Specification (SSR as First-Class Client)

## 1. Overview

Theme system provides **server-side rendering (SSR)** as the default storefront.

It is:

* simple
* optional
* replaceable

---

### Key Principle

> SSR is a **client of the application layer**, not a core dependency.

---

This means:

* same data contracts power both SSR and API
* SSR can be disabled without affecting backend logic
* system remains fully headless-compatible

---

## 2. Goals

* render HTML from backend (default experience)
* consume application view models (composition pipeline)
* require zero frontend setup
* remain fully optional (headless mode)

---

## 3. Non-Goals (v1)

* no block rendering system yet
* no slot system yet
* no theme inheritance
* no multi-theme switching
* no asset pipeline (SCSS/JS)
* no CMS features

---

## 4. Rendering Architecture

---

```text
HTTP → handler → composition/query → view model → template → HTML
```

---

### Important

SSR must use:

* composition pipeline
* query layer

---

### SSR must NOT:

❌ query database directly
❌ implement business logic
❌ call REST API internally

---

---

## 5. Frontend Modes

---

Configured via:

```yaml
frontend:
  enabled: true
  mode: ssr | headless | hybrid
```

---

### Modes

---

#### `ssr` (default)

* HTML rendered on server
* API also available

---

#### `headless`

* SSR disabled
* API only
* external frontend required

---

#### `hybrid` (future)

* mix SSR + API-driven UI

---

---

## 6. Directory Structure

---

```plaintext
/themes
  /default
    /templates
      product.html
      layout.html
    theme.yaml
```

---

---

## 7. Theme Definition

---

### `theme.yaml`

```yaml
name: default
version: 0.1.0
```

---

---

## 8. Template Engine

---

Use Go standard library:

```go
html/template
```

---

Rules:

* no custom templating language
* no logic-heavy templates
* templates render only

---

---

## 9. Template Loading

---

```go
func LoadTemplates(path string) (*template.Template, error)
```

---

Example:

```go
tmpl := template.Must(template.ParseGlob("themes/default/templates/*.html"))
```

---

---

## 10. Renderer

---

```go
func Render(w http.ResponseWriter, name string, data interface{}) error
```

---

Example:

```go
Render(w, "product.html", ctx)
```

---

---

## 11. Layout Support (Basic)

---

### `layout.html`

```html
<!DOCTYPE html>
<html>
<head>
    <title>{{ template "title" . }}</title>
</head>
<body>
    {{ template "content" . }}
</body>
</html>
```

---

### `product.html`

```html
{{ define "title" }}{{ .Product.Name }}{{ end }}

{{ define "content" }}
<h1>{{ .Product.Name }}</h1>
<p>Price: {{ .Product.Price }}</p>
{{ end }}

{{ template "layout.html" . }}
```

---

---

## 12. Data Contract (CRITICAL)

---

Templates receive **view models from application layer**.

Example:

```go
type ProductPageView struct {
    Product *Product
    Price   PriceView
}
```

---

### Source

```go
view := GetProductPageView(productID)
```

---

👉 Same view model is used by:

* SSR (templates)
* API (JSON responses)

---

---

## 13. HTTP Integration

---

```go
func ProductHandler(w http.ResponseWriter, r *http.Request) {
    view := GetProductPageView(id)

    Render(w, "product.html", view)
}
```

---

---

## 14. Configuration

---

```yaml
frontend:
  enabled: true
  mode: ssr
  theme: default
```

---

---

## 15. Extensibility (Future)

---

Prepared for:

* block rendering
* slot system
* multiple themes
* asset handling (CSS/JS)
* theme overrides

---

---

## 16. Constraints

---

* rendering is synchronous
* no JS required
* no framework assumptions
* SSR must remain optional

---

---

## 17. Summary

Theme system v1 provides:

> a minimal SSR layer that acts as a client of the application layer, not a core dependency.

It ensures:

* simple out-of-the-box storefront
* API-first architecture compatibility
* seamless transition to headless mode

---

## Guiding Principle

> SSR is default — but never required.

---
