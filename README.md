# Shopanda

A **developer-first ecommerce engine** built with Go and PostgreSQL.

Combines the extensibility of Magento, the simplicity of Shopware, and the performance of Go — delivered as a single binary.

## What It Is

- Event-driven ecommerce backend
- Hexagonal architecture (ports & adapters)
- Plugin-extensible without modifying core
- Single binary, runs on a VPS — no Kubernetes required

## What It Isn't

- Not a SaaS platform
- Not a low-code/no-code tool
- Not an enterprise-first system

## Core Domains

| Domain | Description |
|---|---|
| Catalog | Products, variants, categories, collections |
| Pricing | Deterministic pipeline (discounts, taxes, fees) |
| Taxes | Country-based VAT, exclusive/inclusive modes |
| Promotions | Catalog rules, cart rules, coupons |
| Cart & Orders | Mutable cart → immutable order with inventory reservations |
| Inventory | Stock tracking per SKU, reservation system |
| Customers & Auth | Email/password, token-based, extensible via plugins |
| Payments | Provider-agnostic with manual default |
| Shipping | Flat rate default, pluggable providers |
| Invoicing | Immutable invoices, credit notes, PDF export |
| Search | Postgres full-text default, pluggable engines |
| CMS | Simple content pages with URL routing |
| Media | Local storage default, CDN-ready |
| Admin | Schema-driven forms and grids |
| SEO | Structured data, sitemap, canonical URLs |
| Multi-Store | Store contexts, scoped pricing and tax |
| Localization | System + content translations, multi-language |
| Legal | GDPR, cookie consent, EU price indication |
| Mailer | Async email delivery, pluggable providers |

## Quick Start

```bash
git clone <repo>
cd shopanda
./app migrate
./app seed
./app serve
```

See the full [Runbook](RUNBOOK.md) for deployment, scaling, and operations.

## Architecture

```
interfaces → application → domain
                ↓
         infrastructure
```

All extensibility goes through four mechanisms:

1. **Events** — async/sync reactions
2. **Pipelines** — deterministic transformations (pricing)
3. **Workflows** — ordered stateful flows (checkout)
4. **Composition Pipelines** — API response building (PDP/PLP)

## Plugins

Plugins are in-process Go interfaces. No microservices, no HTTP overhead.

```go
type Plugin interface {
    Name() string
    Init(app *App) error
}
```

Plugins can register providers, extend pipelines, listen to events, and customize admin — but cannot mutate core.

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

## Tech Stack

- **Language:** Go
- **Database:** PostgreSQL
- **Queue:** Postgres-backed (default)
- **Search:** Postgres full-text (default)
- **Storage:** Local filesystem (default)

Everything optional (Redis, Meilisearch, S3, CDN) is added via plugins.

## License

See [LICENSE](LICENSE).
