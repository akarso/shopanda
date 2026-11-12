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

Opt in to snake_case maps (`id`, `name`, `slug`) with `?fields=snake` or header `X-Shopanda-Field-Names: snake`. Snake product maps omit merchant internals (`pricing_mode`, `stock_mode`, `explode_order_lines`). `blocks` and `meta` stay composition output either way. Search was already snake_case. Variant objects are still domain encoding. Price and availability are siblings, not fields on those entities — see below.

## Price and availability

Catalog reads attach a sibling offer. Search does not. `amount` is minor units of the request currency, the same scale as cart `unit_price`. `null` means this currency has no price. `0` is a real free price. `variant_id` is present only when `amount` is set.

`in_stock` is on-hand quantity greater than zero, or a dynamic-stock bundle rollup. Checkout reservations already reduce on-hand quantity, so the flag does not subtract them again. It is not the cart's purchasable gate: the cart also checks visibility, a price in the cart currency, linked-parent rules, and downloadable files. A virtual or downloadable product with no stock row is out of stock, the same quantity rule the cart uses.

Currency and store come from the request host. With no store, currency is EUR and the price is the global row. The price repository falls back from the store row to the global row in that one read.

| Read | Where |
| --- | --- |
| `GET /api/v1/products` | `meta.offers[<product id>]` |
| `GET /api/v1/categories/{id}/products` | `meta.offers[<product id>]` |
| `GET /api/v1/products/{id}` | `meta.offer` |
| `GET /api/v1/products/{id}/variants` | `offers[<variant id>]` |
| `GET /api/v1/products/{id}/variants/{variantId}` | `offer` |

```json
{"variant_id":"v1","amount":1999,"currency":"EUR","in_stock":true}
```

Product detail computes that offer once. JSON-LD copies it and emits an Offer only when `amount` is set. The structured-data price is that card price: the cheapest of the considered variants. `availability` is the card's `in_stock`, which can come from a different variant. There is no `AggregateOffer`. A stock-only snapshot (amount null, in stock true) produces no Offer. Omnibus price indication still skips dynamic headlines. A product card uses the cheapest of the oldest 100 variants in that currency. A newer variant past that cap is not considered, and the response does not mark the cap. `in_stock` on the card is true when any of those variants has on-hand quantity, which may not be the cheapest one. A grouped parent has `amount: null` and `in_stock: false`. A dynamic-price bundle is the sum of required active components, not its own price row. A dynamic-stock bundle uses the component rollup, not its own stock row. Variant offers for a dynamic-price bundle resolve each requested variant. JSON-LD prices use the same two-decimal major-unit formatting as Omnibus (`amount / 100`).

`GET /api/v1/products` passes the request store, currency, and country into the listing pipeline. Price indication on that list uses the store currency. Category product lists call the same offer resolver and do not run the rest of the listing pipeline, so they have no price indication and no plugin listing steps.

Guest `GET` responses for products, categories, and search send `Cache-Control: public, max-age=300`. Nothing purges that cache when a price or stock row changes. Treat offers as advisory for up to five minutes.

A failed batch read of variants, prices, or stock on a product page is logged and `meta.offer` is left out. The page still renders. The guest cache does not store a fake "no price, out of stock" offer. JSON-LD emits no Offer, and price indication does not read variants or prices again, so there is no Omnibus block for that product. A grouped parent or a product with no price row still has `meta.offer` and `amount: null`. The same batch failure on a product list or a category product list fails the request. A failure while resolving one dynamic bundle is logged once for that product. That card keeps `in_stock` when stock resolved, and `amount` stays null. A validation or not-found error from a bundle rollup is one warning with the product id, and the card shows no price. A cancelled request is not logged. Admin product and category reads run under operator scope and do not include offers. Plugin PDP and PLP steps run after the offer step and must not replace `meta.offer` or `meta.offers`.

Search hits stay name, slug, description, and attributes. Use a catalog read for price and stock.

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
- Bundle and grouped member lists on public REST (admin composition APIs exist). Price and stock for those parents are on `offer` / `offers`.

Treat those as platform gaps, not theme work.

Live contract: `/docs` and [`openapi.yaml`](../../openapi.yaml).
