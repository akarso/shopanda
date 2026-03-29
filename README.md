# Shopanda

Shopanda is a **developer-first ecommerce engine** built with Go and PostgreSQL.

It is designed for teams that want a **lean, self-hosted commerce stack** with strong extensibility, minimal operational overhead, and a clean path for advanced customization.

## Why Shopanda

Traditional ecommerce platforms often trade flexibility for complexity. Shopanda aims for a different balance:

- **Fast and lightweight** — built to stay resource-efficient.
- **Self-hosted by default** — no SaaS lock-in.
- **Extensible without core hacks** — plugins, events, workflows, and pipelines.
- **PostgreSQL-first** — using the database heavily for search, queues, and core data operations.
- **Simple to operate** — runs as a single binary on straightforward infrastructure.
- **Built for technical teams** — enough power for serious commerce, without needing a large team to maintain it.

## What it is

- An event-driven ecommerce backend.
- A modular commerce core with plugin-based extensibility.
- A system designed around simple deployment and maintainability.
- A platform intended for technical teams, agencies, and self-hosted commerce projects.

## What it is not

- Not a SaaS platform.
- Not a low-code or no-code builder.
- Not enterprise bloatware.
- Not a framework that requires a large operational footprint.

## Core principles

Shopanda is being built around a few guiding principles:

1. **Performance first**  
   Keep the stack lean, avoid unnecessary overhead, and let PostgreSQL do more of the heavy lifting.

2. **Simple architecture**  
   Favor clear boundaries, predictable flows, and a single-binary deployment model.

3. **Extensibility without chaos**  
   Support customization through plugins and well-defined extension points instead of core modification.

4. **Operational minimalism**  
   Reduce infrastructure complexity and avoid requiring Kubernetes, microservices, or heavy background systems by default.

5. **Security through restraint**  
   Keep the core small and dependency-free where practical, so the default system stays easier to reason about and maintain.

## Core domains

Shopanda aims to provide a broad commerce foundation out of the box:

| Domain | Description |
|---|---|
| Catalog | Products, variants, categories, collections |
| Pricing | Deterministic pricing pipeline with discounts, taxes, and fees |
| Taxes | Country-based VAT, inclusive and exclusive pricing modes |
| Promotions | Catalog rules, cart rules, coupons |
| Cart & Orders | Mutable cart flow with immutable order creation and inventory reservations |
| Inventory | Stock tracking per SKU and reservation handling |
| Customers & Auth | Email/password and token-based auth with plugin extension points |
| Payments | Provider-agnostic payments with a manual default flow |
| Shipping | Flat rate default with pluggable shipping providers |
| Invoicing | Immutable invoices, credit notes, PDF export |
| Search | PostgreSQL full-text search by default, with optional search engine plugins |
| CMS | Simple content pages with routing |
| Media | Local storage by default, CDN-ready design |
| Admin | Schema-driven forms and grids |
| SEO | Structured data, sitemap generation, canonical URLs |
| Multi-Store | Store contexts with scoped pricing and tax rules |
| Localization | Translations for system and content, multi-language support |
| Legal | GDPR support, cookie consent, EU price indication |
| Mailer | Async email delivery with pluggable providers |

## Architecture

Shopanda follows a layered architecture:

```text
interfaces → application → domain
                 ↓
           infrastructure
```

Extensibility is built around four mechanisms:

- **Events** — async and sync reactions to system changes.
- **Pipelines** — deterministic transformations such as pricing.
- **Workflows** — ordered, stateful flows such as checkout.
- **Composition pipelines** — API response building for views such as PDP and PLP.

## Plugin model

Plugins are in-process Go interfaces.

```go
type Plugin interface {
    Name() string
    Init(app *App) error
}
```

Plugins can:

- Register providers.
- Extend pipelines.
- Listen to events.
- Customize admin behavior.
- Add optional integrations and advanced capabilities.

The core is designed to stay stable while plugins handle variation.

## Default stack

Shopanda uses a minimal default stack:

- **Language:** Go
- **Database:** PostgreSQL
- **Queue:** PostgreSQL-backed by default
- **Search:** PostgreSQL full-text by default
- **Storage:** Local filesystem by default

Optional infrastructure such as Redis, Meilisearch, S3, or CDN support can be added through plugins.

## Early-stage note

Shopanda is still early in its journey.

The long-term goal is to build a commerce engine that is:

- fast,
- easy to self-host,
- easy to extend,
- and simple enough that teams do not need an army of developers to run it.

## Quick start

```bash
git clone <repo>
cd shopanda
./app migrate
./app seed
./app serve
```

See the full [Runbook](RUNBOOK.md) for deployment, scaling, and operations.

## Documentation

All specs live in [`docs/`](docs/):

- [Foundation](docs/FOUNDATION.md) — vision and principles
- [Domain Model](docs/DOMAIN_MODEL.md) — schema and entities
- [Project Structure](docs/PROJECT_STRUCTURE.md) — code organization
- [Web API](docs/WEB_API.md) — REST endpoints
- [Plugin Guide](docs/PLUGINS.md) — authoring plugins
- [Backend Guide](docs/BACKEND.md) — implementation patterns
- [Frontend Guide](docs/FRONTEND.md) — rendering strategy
- [Taxes](docs/TAXES.md) — VAT calculation
- [Promotions](docs/PROMOTIONS.md) — discounts and coupons
- [Rules](docs/RULES.md) — condition primitives
- [Invoicing](docs/INVOICING.md) — invoice generation
- [Routing](docs/ROUTING.md) — URL rewrites
- [CMS](docs/CMS.md) — content pages
- [SEO](docs/SEO.md) — structured data and discoverability
- [Inventory](docs/STOCK.md) — stock management
- [Mailer](docs/MAILER.md) — email delivery
- [Multi-Store](docs/STORES_CURRENCIES.md) — stores and currencies
- [Localization](docs/I18N.md) — translations
- [Legal](docs/LEGAL.md) — GDPR and compliance
- [Performance](docs/PERFORMANCE.md) — CDN and caching

## License

See [LICENSE](LICENSE).