# 🏪 Storefront Templates — Specification

## 1. Overview

Complete server-rendered storefront using the existing theme engine:

* all essential shopping pages
* minimal CSS (classless framework)
* HTMX for interactivity (no JavaScript framework)
* progressive enhancement (works without JS)

Design goals:

* functional, not fancy — a working store, not a design showcase
* server-rendered via existing composition pipeline
* HTMX for dynamic parts (cart update, add-to-cart)
* accessible and semantic HTML
* easy to re-theme (override templates)

---

## 2. Base Layout + CSS (PR-224)

---

### 2.1 Layout Structure

```text
┌───────────────────────────────────────┐
│  Logo        Search      Cart (3)     │
│  Nav: Home | Categories | Account     │
├───────────────────────────────────────┤
│                                       │
│           Page Content                │
│                                       │
├───────────────────────────────────────┤
│  Footer: Links | Legal | © 2026       │
└───────────────────────────────────────┘
```

---

### 2.2 Template Inheritance

```text
templates/
  layout.html         ← base layout (header, nav, footer)
  product_list.html   ← extends layout
  product_detail.html ← extends layout
  category.html       ← extends layout
  cart.html           ← extends layout
  checkout/
    address.html
    shipping.html
    payment.html
    confirm.html
  account/
    login.html
    register.html
    orders.html
    profile.html
  error/
    404.html
    500.html
```

---

### 2.3 CSS Strategy

* Classless CSS framework (Pico CSS or similar, bundled not CDN)
* Stored in `themes/default/static/css/style.css`
* Served via `/static/` route
* Custom overrides in `themes/default/static/css/custom.css`
* Total CSS < 20KB

---

### 2.4 Asset Serving

```go
router.Handle("/static/*", http.StripPrefix("/static/",
    http.FileServer(http.Dir("themes/default/static"))))
```

---

## 3. Product Listing Page (PR-225)

---

### 3.1 Route

```text
GET /products          → all products
GET /categories/{slug} → products in category (PR-226)
GET /search?q=...      → search results (reuses same template)
```

---

### 3.2 Template Data

Via composition pipeline (`ListingContext`):

```go
type PLPData struct {
    Products   []ProductCard
    Pagination Pagination
    Sort       string
    Filters    map[string][]string
    Query      string // for search
}

type ProductCard struct {
    Name      string
    Slug      string
    Price     FormattedPrice
    Image     string
    InStock   bool
}
```

---

### 3.3 Layout

```text
┌──────────────────────────────────────┐
│ [Grid] [List]    Sort: [Price ▼]     │
├──────────────────────────────────────┤
│ ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐    │
│ │ IMG │ │ IMG │ │ IMG │ │ IMG │    │
│ │Name │ │Name │ │Name │ │Name │    │
│ │€29  │ │€49  │ │€19  │ │€39  │    │
│ └─────┘ └─────┘ └─────┘ └─────┘    │
│                                      │
│ ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐    │
│ │ ... │ │ ... │ │ ... │ │ ... │    │
│ └─────┘ └─────┘ └─────┘ └─────┘    │
│                                      │
│       [← Prev]  1 2 3  [Next →]     │
└──────────────────────────────────────┘
```

---

### 3.4 Pagination

Query params: `?page=2&per_page=12&sort=price_asc`

---

## 4. Category Navigation (PR-226)

---

### 4.1 Navigation Bar

Category tree rendered in header nav:

```html
<nav>
  <ul>
    <li><a href="/categories/electronics">Electronics</a></li>
    <li><a href="/categories/clothing">Clothing</a></li>
  </ul>
</nav>
```

---

### 4.2 Category Page

Route: `GET /categories/{slug}`

Same product grid as PLP, filtered by category. Includes:

* Category name + description
* Breadcrumbs: Home → Electronics → Headphones
* Sub-category links (if any)

---

### 4.3 Breadcrumbs

```go
type Breadcrumb struct {
    Name string
    URL  string
}
```

Rendered from category tree path.

---

## 5. Cart + Mini-Cart (PR-227)

---

### 5.1 Mini-Cart (Header)

HTMX-powered, updates without page reload:

```html
<span hx-get="/fragments/cart-count"
      hx-trigger="cart-updated from:body"
      hx-swap="innerHTML">
  Cart (0)
</span>
```

---

### 5.2 Add to Cart

On product page:

```html
<button hx-post="/cart/add"
        hx-vals='{"product_id": "...", "variant_id": "...", "quantity": 1}'
        hx-trigger="click"
        hx-on::after-request="htmx.trigger(document.body, 'cart-updated')">
  Add to Cart
</button>
```

---

### 5.3 Cart Page

Route: `GET /cart`

```text
┌──────────────────────────────────────────┐
│ Shopping Cart                            │
├──────┬────────┬─────┬───────┬───────────┤
│ Item │ Price  │ Qty │ Total │           │
├──────┼────────┼─────┼───────┼───────────┤
│ Shoe │ €49    │ [2] │ €98   │ [Remove]  │
│ Hat  │ €19    │ [1] │ €19   │ [Remove]  │
├──────┴────────┴─────┼───────┼───────────┤
│            Subtotal │ €117  │           │
│                     │       │           │
│           [Continue Shopping] [Checkout] │
└─────────────────────┴───────┴───────────┘
```

Quantity update and remove use HTMX to avoid full page reloads.

---

## 6. Checkout Flow (PR-228)

---

### 6.1 Multi-Step Flow

```text
/checkout/address  → /checkout/shipping → /checkout/payment → /checkout/confirm
```

Each step is a separate page. Progress indicator at top.

---

### 6.2 Address Step

```text
First Name, Last Name
Street, City, Postcode, Country (select)
→ [Continue to Shipping]
```

---

### 6.3 Shipping Step

Display available rates (from `GET /shipping/rates`):

```text
○ Standard Shipping — €5.00
○ Express Shipping — €12.00
○ Free Shipping (orders over €50) — €0.00
→ [Continue to Payment]
```

---

### 6.4 Payment Step

For Stripe: render Stripe Elements (card input).
For manual: display bank transfer instructions.

```text
Card Number: [________________]
Expiry:      [__/__]  CVC: [___]
→ [Place Order]
```

---

### 6.5 Confirmation

```text
✓ Order Placed!

Order #ORD-001
Total: €122.00

A confirmation email has been sent to john@example.com.

[View Order] [Continue Shopping]
```

---

## 7. Account Pages (PR-229)

---

### 7.1 Login

```text
GET /account/login

Email:    [________________]
Password: [________________]
[Login]

Don't have an account? [Register]
```

---

### 7.2 Register

```text
GET /account/register

First Name: [________]  Last Name: [________]
Email:      [________________]
Password:   [________________]
[Create Account]
```

---

### 7.3 Order History

```text
GET /account/orders

┌──────────┬────────────┬─────────┬────────┐
│ Order    │ Date       │ Total   │ Status │
├──────────┼────────────┼─────────┼────────┤
│ ORD-003  │ 2026-04-15 │ €122.00 │ Paid   │
│ ORD-001  │ 2026-04-10 │ €49.99  │ Shipped│
└──────────┴────────────┴─────────┴────────┘
```

Click → order detail page.

---

### 7.4 Profile

```text
GET /account/profile

Email:      john@example.com
First Name: [John____]
Last Name:  [Doe_____]
[Save]

[Change Password]
[Delete Account]
```

---

## 8. HTMX Integration

---

### Fragment Routes

Server-side routes returning HTML fragments (not full pages):

```text
GET  /fragments/cart-count        → "Cart (3)"
GET  /fragments/mini-cart         → cart items HTML
POST /fragments/cart/add          → updated mini-cart
POST /fragments/cart/update       → updated cart row
POST /fragments/cart/remove       → empty (row removed)
```

These are internal routes, not API endpoints. They return HTML, not JSON.

---

## 9. Non-Goals (v0)

* No product reviews / ratings
* No wishlist
* No product comparison
* No JavaScript SPA (server-rendered only)
* No real-time inventory display
* No social login (Google, Facebook)
