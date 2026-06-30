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
| Legal | GDPR consent, data export/delete, Omnibus price indication on PDP/PLP (PR-530); WEEE/EPR/GPSR planned Phase 5 |
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

## Extension model (three tiers)

Shopanda separates **core**, **core plugins**, and **external plugins**. All three use the same in-process `plugin.Plugin` contract; what differs is who ships the code and how it is enabled.

```text
┌─────────────────────────────────────────────────────────────┐
│  Tier 1 — Core (always on)                                  │
│  Domain · application · Postgres repos · default adapters     │
│  Postgres search/cache/queue · manual pay · local storage   │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│  Tier 2 — Core plugins (shipped in repo, config-gated)    │
│  Meilisearch · Redis cache/queue · RabbitMQ · Stripe · S3   │
│  Registered via plugins/core; one backend per resource slot │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│  Tier 3 — External plugins (author-owned)                   │
│  Custom pricing/checkout steps · events · permissions       │
│  Compile-time register in cmd/api/register_plugins.go       │
└─────────────────────────────────────────────────────────────┘
```

| Tier | Location | Enabled by | Typical use |
| --- | --- | --- | --- |
| **Core** | `internal/` | always | Commerce engine, default Postgres stack |
| **Core plugin** | `plugins/core/` | config driver switches (`search.engine`, `cache.driver`, `queue.driver`, etc.) | Optional backends shipped with the repo |
| **External plugin** | e.g. `plugins/example/` or your own module | compile-time import + config flag | Custom business rules without forking core |
| **B2B module (commercial)** | `plugins/b2b/` | compile-time import + license key | Wholesale, groups, quotes — see [Commercial licensing](docs/COMMERCIAL.md) |

```go
type Plugin interface {
    Name() string
    Init(app *App) error
}
```

Extension mechanisms (events, pricing/checkout/composition pipelines, permissions) are wired through `PluginRegistry` at startup. There is no dynamic plugin discovery yet — core and external plugins are registered explicitly at compile time.

See [Developer Guide](docs/guides/DEVELOPER.md) for how to enable core plugins and add an external plugin. Reference implementation: [`plugins/example/`](plugins/example/).

**Not yet supported:** `.so` dynamic loading, plugin marketplace.

Full Phase 4 plan: [Phase 4 Roadmap](docs/phase-4-refactoring/ROADMAP.md) (shipped). Phase 5 ([Mature Commerce](docs/phase-5-maturity/ROADMAP.md)) is shipped. Next: [Phase 6 — Merchant-Complete Admin](docs/phase-6-merchant-complete/ROADMAP.md).

## Default stack

Shopanda uses a minimal default stack (Tier 1 only — no optional services required):

| Component | Default | Optional core plugin |
| --- | --- | --- |
| **Language** | Go | — |
| **Database** | PostgreSQL | — |
| **Queue** | PostgreSQL job queue | Redis or RabbitMQ (`queue.driver`) |
| **Search** | PostgreSQL full-text (tsvector) | Meilisearch (`search.engine`) |
| **Cache** | PostgreSQL UNLOGGED table | Redis (`cache.driver`) |
| **Storage** | Local filesystem | S3-compatible (`storage.driver`) |
| **Payments** | Manual (offline) | Stripe (`payment.stripe.enabled`) |
| **Email** | SMTP + async jobs | — |
| **Cron** | In-process scheduler | — |
| **Storefront** | SSR templates | — |
| **Data exchange** | CSV import/export | — |

Enable optional backends via YAML or environment variables — see [Deployment Guide](docs/guides/DEPLOYMENT.md) and `configs/config.example.yaml`.

## Early-stage note

Phases 1–5 are shipped: merchant admin for catalog, orders, promotions, inventory, returns, EU compliance (Omnibus, WEEE, GPSR), webhooks, and platform plugins (Kafka/SQS, plugin CLI). **Phase 6** ([roadmap](docs/phase-6-merchant-complete/ROADMAP.md)) closes admin UI debt — navigation, content blocks, webhooks, store credit, bulk prices, and merchant docs.

The long-term goal remains a commerce engine that is:

- fast,
- easy to self-host,
- easy to extend,
- and simple enough that teams do not need an army of developers to run it.

## Quick start

```bash
git clone https://github.com/akarso/shopanda.git
cd shopanda
./install.sh
# or: cp .env.example .env
go build -o app ./cmd/api
./app setup
./app dev
```

`./install.sh` is an interactive helper that writes a complete `.env` file. If you prefer to edit configuration manually, skip it and copy `.env.example` yourself.

For a complete operator-focused setup, including Docker, health checks, and environment variables, see [docs/guides/DEPLOYMENT.md](docs/guides/DEPLOYMENT.md).

### CLI commands

| Command | Description |
|---|---|
| `help` | Show built-in command help |
| `setup` | Run first-time setup (connectivity check, migrations, seed) |
| `migrate` | Run database schema migrations |
| `dev` | Start HTTP server with embedded worker and scheduler (local development) |
| `serve` | Start the HTTP server with embedded worker (default) |
| `worker` | Start the background job worker |
| `scheduler` | Start the cron scheduler |
| `seed` | Run seed data framework |
| `search:reindex` | Re-index all products in the search engine |
| `config:export` | Export configuration to stdout as YAML |
| `config:import <file.yaml>` | Import configuration from YAML |
| `import:products <file.csv>` | Bulk import products from CSV |
| `export:products <file.csv>` | Export products and variants to CSV |
| `import:stock <file.csv>` | Import stock quantities from CSV |
| `export:stock <file.csv>` | Export stock quantities to CSV |
| `import:customers <file.csv>` | Import customers from CSV |
| `export:customers <file.csv>` | Export customers to CSV |
| `import:attributes <file.csv>` | Import attribute & group definitions from CSV |
| `export:attributes <file.csv>` | Export attribute & group definitions to CSV |
| `import:categories <file.csv>` | Import category tree from CSV |
| `export:categories <file.csv>` | Export category tree to CSV |
| `import:prices <file.csv>` | Import prices from CSV |
| `export:prices <file.csv>` | Export prices to CSV |

Run `./app help` for the live command list from the current binary.

## Documentation

Current guides live in [`docs/guides/`](docs/guides/):

### Guides

- [Merchant Guide](docs/guides/MERCHANT.md) — day-to-day store operations, admin UI, orders, and catalog workflows
- [Deployment Guide](docs/guides/DEPLOYMENT.md) — Docker, bare metal, cloud deployment, TLS, backups, and monitoring
- [Developer Guide](docs/guides/DEVELOPER.md) — three-tier plugin model, extension points, events, pipelines, and API integration
- [Plugin Authoring Guide](PLUGINS.md) — core vs external boundaries, extension mechanisms, deferred capabilities

### Planning & Reference

- [Commercial Licensing](docs/COMMERCIAL.md) — open core (GPL) vs paid B2B module
- [Phase 6 Roadmap](docs/phase-6-merchant-complete/ROADMAP.md) — **active** merchant-complete admin (navigation, blocks, webhooks, store credit, bulk prices)
- [Phase 5 Roadmap](docs/phase-5-maturity/ROADMAP.md) — mature commerce milestones (shipped)
- [EU Compliance Reference](docs/phase-5-maturity/specs/COMPLIANCE_EU.md) — Omnibus, WEEE, EPR, GPSR mapping to planned work
- [Phase 4 Roadmap](docs/phase-4-refactoring/ROADMAP.md) — product-complete milestones (shipped)
- [Runtime Modes](docs/phase-4-refactoring/specs/RUNTIME_MODES.md) — dev vs production process layout (`serve`, `worker`, `scheduler`, `app dev`)
- [Phase 1 Roadmap](docs/phase-1-core/ROADMAP.md) — core platform milestones and archived planning context
- [Phase 2 Roadmap](docs/phase-2-merchant-ready/ROADMAP.md) — merchant-ready milestones and implementation history
- [Phase 3 Roadmap](docs/phase-3-testing/ROADMAP.md) — runtime refactor tracks (guest checkout, admin hardening, account UX); archived
- [C4 Context Diagram](docs/diagrams/c4-context.md) — system context
- [C4 Container Diagram](docs/diagrams/c4-container.md) — runtime containers
- [C4 Component Diagram](docs/diagrams/c4-component.md) — major component boundaries
- [C4 Code Diagram](docs/diagrams/c4-code.md) — code-level structure

Historical phase specs and implementation notes remain under:

- [`docs/phase-1-core/specs/`](docs/phase-1-core/specs/) for core design specs
- [`docs/phase-2-merchant-ready/specs/`](docs/phase-2-merchant-ready/specs/) for merchant-ready specs
- [`docs/phase-1-core/prs/`](docs/phase-1-core/prs/) and [`docs/phase-2-merchant-ready/prs/`](docs/phase-2-merchant-ready/prs/) for implementation notes

## License

See [LICENSE](LICENSE).