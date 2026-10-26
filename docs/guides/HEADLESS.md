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
- UUID-shaped keys try ID first, then slug if that ID is missing (products, categories, and nested variant/review routes)
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

Same-origin or a BFF/proxy is the default advice. CORS is applied only to storefront API prefixes (`/api/v1/products`, `/api/v1/account`, `/api/v1/auth`, …). Admin, setup, webhooks, `/docs`, and `/storefront/*` (SSR cookie-cart JSON) never get CORS headers.

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
- `/api/v1/*` is not CSRF-checked. CSRF is for HTML `/account/` and `/checkout/` forms, and for `/storefront/*` JSON.
- Headless clients store `cart_id` themselves. The SSR cart cookie is not used.
- Cookie session (`shopanda_storefront_session`) is for the HTML storefront. Do not turn on CORS credentials to reuse it from another origin.

## Same-origin storefront JS (hybrid)

A configurator (or any same-origin script) that should land on SSR `/cart` and `/checkout` must mutate the **storefront** cart, not `POST /api/v1/carts`.

These routes exist only when the HTML storefront is mounted (`frontend.enabled: true` and `frontend.mode` is not `headless`). On an API-only process they 404. They are not in OpenAPI — they are the SSR cookie-cart seam, not `/api/v1`.

```http
GET /storefront/csrf
POST /storefront/cart/items
PUT /storefront/cart/items/{variantId}
DELETE /storefront/cart/items/{variantId}
Content-Type: application/json   # required for POST and PUT
X-CSRF-Token: <token from GET /storefront/csrf>
```

```json
{"variant_id":"…","quantity":1,"extensions":[{"field_code":"acme.print.design","value":{}}]}
```

Omit `quantity` to add 1. Explicit `null`, `0`, or a negative quantity is 422 and does not mint a guest cart. PUT `quantity` is required and must be a positive integer; omitted, `null`, `0`, and negative are 422 `quantity must be positive`. DELETE has no body.

Responses use the standard envelope (`{"data":...,"error":null}`):

```json
{"data":{"csrf_token":"..."},"error":null}
```

```json
{"data":{"item_count":1,"total_quantity":1,"cart_url":"/cart"},"error":null}
```

POST sets `shopanda_storefront_cart` for guests only after a successful add. A failed first guest add does not persist a reachable cart: the empty row is discarded and no cookie is set. The body never includes `cart_id`. Signed-in requests mutate the customer active cart and ignore a leftover guest cookie. CSRF failures (missing or wrong `X-CSRF-Token`) are JSON 403 `{"data":null,"error":{"code":"forbidden",...}}` — query-string `csrf_token` is not accepted on `/storefront/*`. Extension and `cart.validate` failures are 422 with the real code (`unknown_field_code`, `field_validation_failed`, `cart_validation_failed`); a private field is 403 `forbidden_private_field`. Concurrent cart writes are 409 `conflict`. Unexpected service errors are a generic JSON 500.

`/storefront/*` is not on the CORS allowlist — serve the script from the Shopanda origin (or a same-host reverse-proxy path).

HTML `POST /cart/add`, `/cart/update`, and `/cart/remove` are unchanged and are not CSRF-gated.

## Claim (JSON or form)

`claim-search` accepts `contact_email`. `claim-register` is **register-and-link** (new account), not login-and-link. Fields: `order_id`, `claim_token`, `email`, `password`, optional `first_name` / `last_name`. `Content-Type: application/json` or `application/x-www-form-urlencoded`.

Headless claim flows require `frontend.claim_url` (env `SHOPANDA_FRONTEND_CLAIM_URL`). Set this to the shopper app's claim page (for example `https://www.example.com/account/orders/claim`). Claim emails link to this page with `?claim_token=` appended. Without this setting, the headless server cannot send the email containing the token required by `claim-register`. SSR defaults to `{public_base_url}/account/orders/claim`.

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
