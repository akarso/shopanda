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
- **Queue:** PostgreSQL-backed job queue with worker and retry logic
- **Search:** PostgreSQL full-text search (tsvector)
- **Storage:** Local filesystem by default
- **Email:** SMTP mailer with async job-based delivery
- **Cron:** In-process scheduler for recurring tasks
- **Themes:** Server-side rendered templates with layout support
- **Data exchange:** CSV import/export for products, stock, and customers

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
./app serve
```

### CLI commands

| Command | Description |
|---|---|
| `migrate` | Run database schema migrations |
| `serve` | Start the HTTP server (default) |
| `import:products <file.csv>` | Bulk import products from CSV |
| `export:products <file.csv>` | Export products and variants to CSV |
| `import:stock <file.csv>` | Import stock quantities from CSV |
| `export:stock <file.csv>` | Export stock quantities to CSV |
| `import:customers <file.csv>` | Import customers from CSV |
| `export:customers <file.csv>` | Export customers to CSV |
| `config:import <file.yaml>` | Import configuration from YAML |
| `config:export <file.yaml>` | Export configuration to YAML |
| `scheduler` | Run background job scheduler |

See the full [CLI Commands](docs/CLI_COMMANDS.md) doc for details.

## Documentation

All specs live in [`docs/`](docs/):

### Architecture & Design

- [Foundation](docs/FOUNDATION.md) — vision and principles
- [Domain Model](docs/DOMAIN_MODEL.md) — schema and entities
- [Project Structure](docs/PROJECT_STRUCTURE.md) — code organization
- [Web API](docs/WEB_API.md) — REST endpoints
- [Event System](docs/EVENT_SYSTEM.md) — pub/sub events
- [Plugin Guide](docs/PLUGIN_LIFECYCLE.md) — plugin lifecycle
- [Backend Guide](docs/BACKEND.md) — implementation patterns
- [Frontend Guide](docs/FRONTEND.md) — rendering strategy

### Commerce Domains

- [Pricing Pipeline](docs/PRICING_PIPELINE.md) — deterministic pricing
- [Checkout Workflow](docs/CHECKOUT_WORKFLOW.md) — ordered checkout flow
- [Order & Cart](docs/ORDER_CART_DOMAIN.md) — cart and order domain
- [Composition Pipeline](docs/COMPOSITION_PIPELINE.md) — PDP/PLP response enrichment
- [Inventory](docs/STOCK.md) — stock management
- [Taxes](docs/TAXES.md) — VAT calculation
- [Promotions](docs/PROMOTIONS.md) — discounts and coupons
- [Rules](docs/RULES.md) — condition primitives
- [Invoicing](docs/INVOICING.md) — invoice generation

### Infrastructure & Operations

- [Admin Schemas](docs/ADMIN_SCHEMA.md) — schema-driven forms and grids
- [Theme System](docs/THEME_SYSTEM.md) — SSR theme engine
- [Data Exchange](docs/DATA_EXCHANGE.md) — CSV import/export
- [CLI Commands](docs/CLI_COMMANDS.md) — available commands
- [Search](docs/SEARCH_SYSTEM.md) — full-text search
- [Mailer](docs/MAILER.md) — email delivery
- [Media](docs/MEDIA_ASSETS.md) — file storage
- [Jobs & Queue](docs/JOBS_QUEUE_SYSTEM.md) — async job processing
- [Cron](docs/CRON.md) — scheduled tasks
- [Caching](docs/CACHING.md) — key-value cache
- [Configuration](docs/CONFIGURATION_SYSTEM.md) — runtime config
- [Routing](docs/ROUTING.md) — URL rewrites
- [CMS](docs/CMS.md) — content pages

### Cross-Cutting

- [SEO](docs/SEO.md) — structured data and discoverability
- [Multi-Store](docs/STORES_CURRENCIES.md) — stores and currencies
- [Localization](docs/I18N.md) — translations
- [Legal](docs/LEGAL.md) — GDPR and compliance
- [Performance](docs/PERFORMANCE.md) — CDN and caching

## License

See [LICENSE](LICENSE).