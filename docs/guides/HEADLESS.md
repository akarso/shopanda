# Headless storefront

Shopanda’s application layer is shared by the built-in SSR theme and public REST. A custom frontend is a **client of the API**, not a theme.

## Enable API-only

```yaml
frontend:
  enabled: false          # default — no HTML storefront
  # mode: headless        # also skips HTML when enabled is true
```

`frontend.enabled` is the real switch. `mode: headless` exists so the documented mode key is not a no-op.

## What works today

- Catalog: `GET /api/v1/products`, `GET /api/v1/products/{idOrSlug}`
- Nested catalog reads also accept the parent id or slug: `GET /api/v1/products/{idOrSlug}/variants`, `GET /api/v1/products/{idOrSlug}/reviews`
- Categories: `GET /api/v1/categories`, `GET /api/v1/categories/{idOrSlug}` (and `.../products`)
- Search: `GET /api/v1/search`, `GET /api/v1/search/suggest`
- CMS: pages, menus, content-blocks
- Cart + coupons + `POST /api/v1/checkout` (one-shot, guest or signed-in)
- JWT auth, orders, returns, consent/GDPR, store credit
- Saved addresses: `/api/v1/account/addresses`
- Guest claim: `POST /api/v1/orders/claim-search` and `POST /api/v1/orders/claim-register` (JSON or form)

## Catalog JSON field names

Default product/category payloads keep **Go exported names** (`ID`, `Name`, `Slug`) so existing clients do not break.

Opt in to snake_case maps (`id`, `name`, `slug`) with `?fields=snake` or header `X-Shopanda-Field-Names: snake`. Snake product maps omit merchant internals (`pricing_mode`, `stock_mode`, `explode_order_lines`). `blocks` and `meta` stay composition output either way. Search was already snake_case. Variant objects are still domain encoding.

## Browser CORS

Same-origin or a BFF/proxy is the default advice. CORS is applied only to storefront API prefixes (`/api/v1/products`, `/api/v1/account`, `/api/v1/auth`, …). Admin, setup, webhooks, and `/docs` never get CORS headers.

```yaml
http:
  cors:
    allowed_origins:
      - https://www.example.com
    # allow_credentials: false   # default
```

Env: `SHOPANDA_HTTP_CORS_ALLOWED_ORIGINS=https://www.example.com,http://127.0.0.1:4173`

- Empty allowlist = no CORS headers (current default).
- `*` is anonymous only: `Access-Control-Allow-Origin: *` and credentials stay off. Combining `*` with `allow_credentials: true` disables the wildcard.
- `allow_credentials: true` requires an explicit origin list (no `*`). `/api/v1` is not CSRF-checked; do not enable cookie credentials unless you also add CSRF (or stay on Bearer).
- Unmatched `OPTIONS` falls through (rate limit / 405). Only a matching origin (or anonymous `*`) answers preflight with 204.

## Auth and cart

- Login returns a JWT. Send `Authorization: Bearer <token>`.
- `/api/v1/*` is not CSRF-checked. CSRF is for HTML `/account/` and `/checkout/` forms.
- Headless clients store `cart_id` themselves. The SSR cart cookie is not used.
- Cookie session (`shopanda_storefront_session`) is for the HTML storefront. Do not turn on CORS credentials to reuse it from another origin.

## Claim (JSON or form)

`claim-search` accepts `contact_email`. `claim-register` is **register-and-link** (new account), not login-and-link. Fields: `order_id`, `claim_token`, `email`, `password`, optional `first_name` / `last_name`. `Content-Type: application/json` or `application/x-www-form-urlencoded`.

Claim emails link to `frontend.claim_url` (env `SHOPANDA_FRONTEND_CLAIM_URL`), not `server.public_base_url`. Set this to the shopper app's claim page (for example `https://www.example.com/account/orders/claim`). The token is appended as `?claim_token=`. Without it, a headless API host will not send a working HTML claim link — use `claim-register` from the client, or set the URL.

## Do not use

- `/fragments/*` — HTML holes for full-page cache + htmx
- `/api/v1/admin/*` — operator surface
- GraphQL as a storefront API (optional, thin, no composition yet)

## Primer

[`examples/headless-vanilla`](../../examples/headless-vanilla) — one HTML file + `fetch`. Not an SDK.

## Still HTML-only or incomplete

- Multi-step checkout (address → shipping → payment → confirm)
- Guest shipping quotes before an order exists
- Email verify / email change / some security step-up pages
- Bundle/grouped PDP composition on public REST (admin APIs exist)

Treat those as platform gaps, not theme work.

Live contract: `/docs` and [`openapi.yaml`](../../openapi.yaml).
