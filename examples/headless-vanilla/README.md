# Headless vanilla primer

A single-page example that talks to Shopanda REST with `fetch`. No Node, no bundler, no framework.

This is a teaching sample, not a storefront SDK. Use it to see login, cart, and product-by-slug. Fragments (`/fragments/*`) and htmx are SSR-only — ignore them here.

## Run

1. Start Shopanda with the storefront off (the default):

   ```bash
   export SHOPANDA_FRONTEND_ENABLED=false
   export SHOPANDA_HTTP_CORS_ALLOWED_ORIGINS=http://127.0.0.1:4173
   ```

   Start Shopanda in the same shell so it receives both values.

2. Serve this folder on another origin (or same origin behind a reverse proxy):

   ```bash
   python3 -m http.server 4173
   ```

3. Open `http://127.0.0.1:4173` and set the API base (for example `http://127.0.0.1:8080`).

Same-origin or a BFF/proxy is the safer default. CORS is only required when the browser calls the API directly.

## What it calls

| Action | Endpoint |
| --- | --- |
| Resolve slug | `GET /{path}` rewrite, then `GET /api/v1/products/{id}` (id or slug both work) |
| Product | `GET /api/v1/products/{idOrSlug}` (nested `/variants` and `/reviews` accept the same key) |
| Search | `GET /api/v1/search?q=` |
| Cart | `POST /api/v1/carts`, `POST /api/v1/carts/{id}/items` |
| Login | `POST /api/v1/auth/login` → Bearer token |
| Addresses | `GET /api/v1/account/addresses` (after login) |

Keep `cart_id` in `localStorage` yourself. Keep the JWT in page memory after login (a refresh clears it). There is no Shopanda cookie for headless clients.

## Still missing vs the SSR storefront

- Multi-step checkout (JSON checkout is one `POST /api/v1/checkout`)
- Guest shipping quotes before an order exists
- Several account HTML-only flows (email verify/change)
