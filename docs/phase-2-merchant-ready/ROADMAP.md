# Phase 2 — Merchant-Ready Roadmap

## Strategy

* Transform the headless commerce engine into a deployable, merchant-usable product
* Each PR touches one responsibility — same discipline as Phase 1
* Plugins for external services (Stripe, S3, Meilisearch) — core stays clean
* Admin and storefront are optional modules (headless-first remains the default)
* Build from infrastructure outward: DevOps → integrations → UI → docs

---

## Section 1 — DevOps & Setup

Goal: one-command local setup, containerized deployment.

| PR  | Title                          | Scope                                                        |
| --- | ------------------------------ | ------------------------------------------------------------ |
| 200 | `.env.example` + config docs  | Annotated env template, `configs/config.example.yaml`, all vars documented |
| 201 | Dockerfile (multi-stage)       | Build stage + scratch/alpine runtime, health check, non-root user |
| 202 | docker-compose.yml             | App + Postgres + optional Redis/Meilisearch, volume mounts, `.env` integration |
| 203 | Setup CLI command              | `app setup` — interactive: creates config, runs migrations, seeds, verifies health |

Spec: [`docs/phase-2-merchant-ready/specs/DEVOPS_SETUP.md`](docs/phase-2-merchant-ready/specs/DEVOPS_SETUP.md)

---

## Section 2 — Email Templates

Goal: file-based, styled HTML email templates replacing hardcoded strings.

| PR  | Title                          | Scope                                                        |
| --- | ------------------------------ | ------------------------------------------------------------ |
| 204 | File-based template loader     | Load templates from `templates/emails/`, override resolution (DB → file), layout support |
| 205 | Core email templates           | Order confirmation, password reset, order shipped — responsive HTML with layout |
| 206 | Invoice email + attachment     | Invoice PDF attached to order confirmation, `invoice.created` event trigger |

Spec: [`docs/phase-2-merchant-ready/specs/EMAIL_TEMPLATES.md`](docs/phase-2-merchant-ready/specs/EMAIL_TEMPLATES.md)

---

## Section 3 — Stripe Payment Provider

Goal: production payment processing via Stripe.

| PR  | Title                          | Scope                                                        |
| --- | ------------------------------ | ------------------------------------------------------------ |
| 207 | Stripe provider plugin         | `PaymentProvider` implementation, PaymentIntent creation, client secret flow |
| 208 | Stripe webhook handler         | Signature verification, `payment_intent.succeeded` / `failed` → update payment status |
| 209 | Stripe refunds                 | Refund initiation via admin endpoint, `charge.refunded` webhook handling |

Spec: [`docs/phase-2-merchant-ready/specs/STRIPE_PAYMENT.md`](docs/phase-2-merchant-ready/specs/STRIPE_PAYMENT.md)

---

## Section 4 — Shipping Zones & Rates

Goal: region-aware shipping with weight-based calculation.

| PR  | Title                          | Scope                                                        |
| --- | ------------------------------ | ------------------------------------------------------------ |
| 210 | Shipping zones + rate tables   | Zone entity (list of countries), rate tiers per zone, DB storage + migration |
| 211 | Weight-based rate calculation  | Product weight/dimensions fields, weight-based rate lookup in `GetRates()` |
| 212 | Free shipping threshold        | Configurable threshold per zone, pricing pipeline step to apply free shipping |

Spec: [`docs/phase-2-merchant-ready/specs/SHIPPING_ZONES.md`](docs/phase-2-merchant-ready/specs/SHIPPING_ZONES.md)

---

## Section 5 — Media Processing

Goal: production-quality image handling with cloud storage option.

| PR  | Title                          | Scope                                                        |
| --- | ------------------------------ | ------------------------------------------------------------ |
| 213 | Image resize + thumbnails      | `ImageProcessor` interface, resize on upload, thumbnail presets (small/medium/large) |
| 214 | WebP conversion + optimization | Convert uploads to WebP, quality config, `<picture>` srcset support in asset URLs |
| 215 | S3 storage adapter             | `Storage` implementation for S3-compatible backends, config for endpoint/bucket/region |

Spec: [`docs/phase-2-merchant-ready/specs/MEDIA_PROCESSING.md`](docs/phase-2-merchant-ready/specs/MEDIA_PROCESSING.md)

---

## Section 6 — Meilisearch Integration

Goal: fast, typo-tolerant product search.

| PR  | Title                          | Scope                                                        |
| --- | ------------------------------ | ------------------------------------------------------------ |
| 216 | Meilisearch adapter            | `SearchEngine` implementation, index sync on product events, faceted search |
| 217 | Autocomplete endpoint          | `GET /search/suggest?q=...`, prefix search, result limit, debounce-friendly |

Spec: [`docs/phase-2-merchant-ready/specs/MEILISEARCH.md`](docs/phase-2-merchant-ready/specs/MEILISEARCH.md)

---

## Section 7 — Admin Dashboard

Goal: embedded admin SPA consuming schema endpoints.

| PR  | Title                          | Scope                                                        |
| --- | ------------------------------ | ------------------------------------------------------------ |
| 218 | Admin SPA scaffold             | Embedded static files (Go `embed`), served at `/admin`, login page, layout shell |
| 219 | Dashboard overview             | Order count, revenue summary, recent orders, low stock alerts — API + UI |
| 220 | Product management pages       | List (grid from schema), create/edit (form from schema), variant inline editing |
| 221 | Order management pages         | Order list + detail, status transitions, payment/shipping info display |
| 222 | Media library                  | Upload UI, grid view, image preview, attach to product flow |
| 223 | Settings page                  | Config editor consuming DB config API, store info, SMTP test, grouped sections |

Spec: [`docs/phase-2-merchant-ready/specs/ADMIN_DASHBOARD.md`](docs/phase-2-merchant-ready/specs/ADMIN_DASHBOARD.md)

---

## Section 8 — Storefront Templates

Goal: basic but complete server-rendered storefront using the theme engine.

| PR  | Title                          | Scope                                                        |
| --- | ------------------------------ | ------------------------------------------------------------ |
| 224 | Base layout + CSS              | `layout.html` with header/nav/footer, minimal CSS (classless or Pico), asset pipeline |
| 225 | Product listing page (PLP)     | Category products, pagination, sort, grid/list toggle |
| 226 | Category navigation            | Category tree in nav, breadcrumbs, category landing page |
| 227 | Cart + mini-cart               | Cart page with quantity update/remove, mini-cart in header, HTMX for interactivity |
| 228 | Checkout flow                  | Multi-step: address → shipping → payment → confirm, form validation |
| 229 | Account pages                  | Login, register, order history, profile edit, password change |

Spec: [`docs/phase-2-merchant-ready/specs/STOREFRONT.md`](docs/phase-2-merchant-ready/specs/STOREFRONT.md)

---

## Section 9 — Documentation & Guides

Goal: non-developers can deploy and operate the store.

| PR  | Title                          | Scope                                                        |
| --- | ------------------------------ | ------------------------------------------------------------ |
| 230 | Merchant guide                 | Adding products, managing orders, configuring store, using admin UI |
| 231 | Deployment guide               | Docker setup, cloud platforms (Railway/Fly.io/DigitalOcean), bare metal, TLS |
| 232 | Developer extension guide      | Creating plugins, adding payment/shipping providers, custom pipeline steps |

Spec: [`docs/phase-2-merchant-ready/specs/DOCUMENTATION.md`](docs/phase-2-merchant-ready/specs/DOCUMENTATION.md)

---

## Milestone Summary

| Milestone                    | After PR | What works                                      |
| ---------------------------- | -------- | ----------------------------------------------- |
| **Containerized**            | 203      | `docker-compose up` → running store             |
| **Styled emails**            | 206      | File-based HTML templates, invoice attachment    |
| **Real payments**            | 209      | Stripe checkout, webhooks, refunds              |
| **Smart shipping**           | 212      | Zone rates, weight-based, free shipping rules   |
| **Production media**         | 215      | Resize, WebP, S3 storage                        |
| **Fast search**              | 217      | Meilisearch with autocomplete                   |
| **Admin UI**                 | 223      | Full admin dashboard for merchants              |
| **Complete storefront**      | 229      | Browsable, shoppable SSR storefront             |
| **Documented**               | 232      | Merchant, deployment, and developer guides      |

---

## Dependencies

```text
Section 1 (DevOps) → no deps, start immediately
Section 2 (Email)  → builds on existing notification system
Section 3 (Stripe) → builds on existing payment domain
Section 4 (Shipping) → builds on existing shipping domain
Section 5 (Media)  → builds on existing storage interface
Section 6 (Meilisearch) → builds on existing search interface
Section 7 (Admin)  → needs schema endpoints (exist), all CRUD APIs (exist)
Section 8 (Storefront) → needs theme engine (exists), all public APIs (exist)
Section 9 (Docs)   → after Sections 1-8 are stable
```

---

## Rules

* Same discipline as Phase 1: each PR compiles, one responsibility, reviewable in ~15 min
* Plugins (Stripe, Meilisearch, S3) must be isolated — no core modifications
* Admin and storefront are optional — headless API must work without them
* All frontend work (admin + storefront) MUST follow [`docs/phase-2-merchant-ready/specs/TEMPLATING_GUIDELINES.md`](docs/phase-2-merchant-ready/specs/TEMPLATING_GUIDELINES.md): SSR-first, no build step, no npm, progressive enhancement, vanilla JS
* No JavaScript build toolchain required for admin (use Go `embed` + vanilla JS)
* Storefront uses native JS for interactivity (htmx optional, vendored) — no SPA framework
