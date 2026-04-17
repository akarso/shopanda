# 🖥️ Admin Dashboard — Specification

## 1. Overview

Embedded admin SPA consuming existing schema and CRUD endpoints:

* served from the Go binary via `embed`
* schema-driven forms and grids
* no external build toolchain required
* lightweight JavaScript (vanilla + HTMX or Alpine.js)

Design goals:

* zero external dependencies to build or run
* schema-driven — new entities get UI automatically
* functional over beautiful (utility, not showcase)
* respects existing RBAC permissions

---

## 2. SPA Scaffold (PR-218)

---

### 2.1 Embedding

```go
//go:embed admin/dist/*
var adminFS embed.FS
```

Served at `/admin` via Go HTTP handler.

---

### 2.2 Routes (client-side)

```text
/admin                    → Login (if not authenticated)
/admin/dashboard          → Overview
/admin/products           → Product grid
/admin/products/{id}      → Product edit form
/admin/products/new       → Product create form
/admin/orders             → Order grid
/admin/orders/{id}        → Order detail
/admin/media              → Media library
/admin/settings           → Settings page
```

---

### 2.3 Auth Flow

1. Admin navigates to `/admin`
2. Login form → `POST /api/v1/auth/login`
3. JWT stored in `localStorage`
4. All API requests include `Authorization: Bearer {token}`
5. 401 response → redirect to login

---

### 2.4 Layout Shell

```text
┌──────────────────────────────────────┐
│  Logo           Admin Name  [Logout] │
├──────────┬───────────────────────────┤
│          │                           │
│  Sidebar │     Content Area          │
│          │                           │
│  • Dash  │                           │
│  • Prods │                           │
│  • Orders│                           │
│  • Media │                           │
│  • Config│                           │
│          │                           │
└──────────┴───────────────────────────┘
```

---

### 2.5 Technology Choice

* **HTML + CSS**: minimal, classless CSS framework (e.g., Pico CSS via CDN)
* **Interactivity**: HTMX for server-driven updates OR Alpine.js for client-side state
* **No npm/webpack/vite**: files are static HTML/JS/CSS, committed to repo
* **Build**: none required — `go:embed` includes the files as-is

---

## 3. Dashboard Overview (PR-219)

---

### 3.1 Stats Cards

```text
┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐
│ Orders   │ │ Revenue  │ │ Products │ │ Low Stock│
│ Today: 5 │ │ €1,234   │ │ 142      │ │ 3 items  │
└──────────┘ └──────────┘ └──────────┘ └──────────┘
```

---

### 3.2 API Endpoints (New)

```http
GET /api/v1/admin/stats/overview
```

Response:

```json
{
  "orders_today": 5,
  "revenue_today": { "amount": 123400, "currency": "EUR" },
  "total_products": 142,
  "low_stock_count": 3,
  "recent_orders": [...]
}
```

---

### 3.3 Recent Orders Table

Last 10 orders with: ID, customer, total, status, date.

---

## 4. Product Management (PR-220)

---

### 4.1 Product Grid

Consumes `GET /api/v1/admin/grids/products` schema:

* Columns from schema definition
* Pagination, sort, search
* Bulk actions: activate/deactivate (future)

Data from: `GET /api/v1/admin/products?page=1&per_page=20&sort=created_at&order=desc`

---

### 4.2 Product Form

Consumes `GET /api/v1/admin/forms/products` schema:

* Fields rendered by type (text, number, select, checkbox, textarea)
* Variant inline editing (sub-table within product form)
* Image upload integration (select from media library)
* Save → `POST /api/v1/admin/products` or `PUT /api/v1/admin/products/{id}`

---

### 4.3 Schema-Driven Rendering

```js
function renderField(field) {
    switch (field.type) {
        case "text":     return `<input type="text" name="${field.name}">`;
        case "number":   return `<input type="number" name="${field.name}">`;
        case "select":   return renderSelect(field);
        case "checkbox": return `<input type="checkbox" name="${field.name}">`;
        case "textarea": return `<textarea name="${field.name}"></textarea>`;
    }
}
```

---

## 5. Order Management (PR-221)

---

### 5.1 Order Grid

* Columns: ID, customer, total, status, payment status, date
* Filter by status
* Click → order detail

---

### 5.2 Order Detail

```text
┌─ Order #ORD-001 ──────────────────────────┐
│ Status: [paid] → [Change Status ▼]        │
│                                            │
│ Customer: John Doe (john@example.com)      │
│ Date: 2026-04-15                           │
│                                            │
│ Items:                                     │
│ ┌──────────┬─────┬──────┬───────┐         │
│ │ Product  │ SKU │ Qty  │ Price │         │
│ ├──────────┼─────┼──────┼───────┤         │
│ │ T-Shirt  │ TS1 │ 2    │ €50   │         │
│ └──────────┴─────┴──────┴───────┘         │
│                                            │
│ Subtotal: €50    Shipping: €5              │
│ Tax: €11.55      Total: €66.55             │
│                                            │
│ Shipping: Standard (DE)                    │
│ Payment: Stripe (pi_xxx) — paid            │
└────────────────────────────────────────────┘
```

---

### 5.3 Status Transitions

Dropdown with allowed next statuses based on current state.
Uses: `PUT /api/v1/admin/orders/{id}` with `{ "status": "shipped" }`.

---

## 6. Media Library (PR-222)

---

### 6.1 Grid View

* Thumbnail grid of all uploaded assets
* Upload button → drag-and-drop or file picker
* Click to preview / copy URL
* Delete with confirmation

---

### 6.2 Upload Flow

```text
1. Select file(s)
2. POST /api/v1/admin/media (multipart)
3. Show upload progress
4. Display new asset in grid
```

---

### 6.3 Image Picker (for product forms)

Modal overlay showing media grid. Click to select → inserts asset ID into product form.

---

## 7. Settings Page (PR-223)

---

### 7.1 Sections

```text
Store Info:     name, URL, address, logo
Email:          SMTP host, port, from address, [Test] button
Media:          storage type, path/bucket, base URL
Currency:       default currency, display format
Tax:            default tax class, included/excluded
```

---

### 7.2 Implementation

* Reads from: `GET /api/v1/admin/config?group=store`
* Writes to: `PUT /api/v1/admin/config` (DB config layer)
* SMTP test: `POST /api/v1/admin/config/test-email` (new endpoint)

---

## 8. Non-Goals (v0)

* No drag-and-drop page builder
* No analytics / charts / reporting
* No customer management UI (API-only for now)
* No real-time updates (WebSocket)
* No dark mode
* No mobile-optimized admin layout
