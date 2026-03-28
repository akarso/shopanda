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
| Cart & Orders | Mutable cart → immutable order with inventory reservations |
| Customers & Auth | Email/password, token-based, extensible via plugins |
| Payments | Provider-agnostic with manual default |
| Shipping | Flat rate default, pluggable providers |
| Search | Postgres full-text default, pluggable engines |
| Media | Local storage default, CDN-ready |
| Admin | Schema-driven forms and grids |

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

## Tech Stack

- **Language:** Go
- **Database:** PostgreSQL
- **Queue:** Postgres-backed (default)
- **Search:** Postgres full-text (default)
- **Storage:** Local filesystem (default)

Everything optional (Redis, Meilisearch, S3, CDN) is added via plugins.

## License

See [LICENSE](LICENSE).
