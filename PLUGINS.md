# Plugin Authoring Guide

Shopanda extends through **in-process Go plugins** — no separate services, no dynamic discovery in v1.

This document describes the **three-tier model**, extension boundaries, and what authors can do today. For step-by-step integration, see [Developer Guide](docs/guides/DEVELOPER.md). For the original Phase 1 design spec, see [docs/phase-1-core/specs/PLUGINS.md](docs/phase-1-core/specs/PLUGINS.md).

---

## Three tiers

| Tier | Package | Enablement | Purpose |
| --- | --- | --- | --- |
| **Core** | `internal/*` | always on | Domain, application, default Postgres adapters |
| **Core plugin** | `plugins/core/*` | config driver switches | Optional backends (Meilisearch, Redis, RabbitMQ, Stripe, S3, …) |
| **External plugin** | author module, e.g. `plugins/example/` | compile-time register + config | Custom pipeline steps, events, permissions |

All tiers implement the same interface:

```go
type Plugin interface {
    Name() string
    Init(app *App) error
}
```

Registration is **compile-time**:

- Core plugins: `plugins/core/register.go` (called from `cmd/api/register_plugins.go`)
- External plugins: add `registry.Register(yours.New())` in `cmd/api/register_plugins.go`

There is no runtime plugin discovery, `.so` loading, or marketplace in the current release.

---

## Core vs external boundaries

### Core plugins SHOULD

- Implement infrastructure ports (search, cache, queue, storage, payment)
- Live under `plugins/core/`
- Activate from a single driver switch per resource slot
- Fail init gracefully without crashing startup

### External plugins SHOULD

- Extend behavior via `plugin.App` hooks (pipelines, events, permissions)
- Use a vendor-prefixed unique name (e.g. `acme/loyalty`)
- Avoid modifying `internal/domain` or bypassing domain ports
- Keep optional config under `plugins.<name>.*` in YAML/env

### External plugins MUST NOT

- Override core logic or mutate core schema
- Access the database directly outside domain repositories
- Register competing infrastructure backends (use or contribute a core plugin instead)
- Block request lifecycle — use async events or jobs for slow work

---

## Extension mechanisms

Available today through `plugin.App`:

| Mechanism | API | Use for |
| --- | --- | --- |
| **Pricing pipeline** | `RegisterPricingStep` | Fees, discounts, custom line adjustments |
| **Checkout workflow** | `RegisterCheckoutStep` | Extra validation or side effects during checkout |
| **Composition pipelines** | `RegisterCompositionStep("pdp"\|"plp", …)` | Enrich API/storefront product responses |
| **Events** | `Bus.On` / `Bus.OnAsync` | React to domain changes |
| **Permissions** | `RegisterPermission` | Admin RBAC strings |
| **Admin config** | `RegisterConfig` | Simple settings on Integrations page (`GET/PUT /admin/config?group=plugins`) |

Core plugins additionally expose providers on `plugin.App` during init (search engine, job queue, cache store, media storage, payment registry entries) which `main.go` resolves after `InitAll`.

---

## Enable core plugins

Change config — no code changes required for shipped core plugins:

```yaml
search:
  engine: meilisearch          # default: postgres
cache:
  driver: redis                # default: postgres
queue:
  driver: rabbitmq             # default: postgres; mutually exclusive with redis
storage:
  driver: s3                   # default: local
payment:
  stripe:
    enabled: true
```

See `configs/config.example.yaml` and [Deployment Guide](docs/guides/DEPLOYMENT.md).

---

## Add an external plugin

1. Create a package implementing `plugin.Plugin` (copy from [`plugins/example/`](plugins/example/)).
2. Register extensions in `Init` via `plugin.App`.
3. Add `registry.Register(yours.New())` in `cmd/api/register_plugins.go` (optionally behind a config flag).
4. Rebuild and restart.

Example registration:

```go
// cmd/api/register_plugins.go
func registerPlugins(registry *plugin.Registry, cfg *config.Config) {
    core.Register(registry, cfg)
    if cfg.Plugins.Example.Enabled {
        registry.Register(example.New())
    }
}
```

---

## What still requires main.go changes

Honest list of gaps for external authors:

| Task | Where to wire |
| --- | --- |
| Register external plugin | `cmd/api/register_plugins.go` |
| New payment webhook HTTP route | `cmd/api/main.go` route table |
| New CLI subcommand | `cmd/api/main.go` subcommand switch + `printHelp()` |
| New infrastructure backend | contribute under `plugins/core/` + driver switch |

---

## Deferred capabilities

Not implemented; do not assume these exist:

- Go plugin `.so` dynamic loading
- Plugin marketplace or version resolver
- Hot reload
- Plugin-registered CLI commands (planned Phase 5 — PR-541)

Plugin settings (string, int, bool) can be registered with `RegisterConfig` and edited on the admin Integrations page when the plugin is enabled at boot.

**Phase 5 platform work:** merchant webhooks, Kafka/SQS queue plugins, GraphQL stretch — see [Phase 5 Roadmap](docs/phase-5-maturity/ROADMAP.md) Track E.

---

## Reference links

- [Developer Guide](docs/guides/DEVELOPER.md) — architecture, examples, API usage
- [Example external plugin](plugins/example/README.md) — pricing step, event listener, permission
- [Phase 5 Roadmap](docs/phase-5-maturity/ROADMAP.md) — returns, segments, EU compliance, platform stretch
- [Phase 4 Roadmap — three tiers](docs/phase-4-refactoring/ROADMAP.md#target-architecture-three-tiers)
- [C4 component diagram](docs/diagrams/c4-component.md) — registry wiring
- [Phase 1 authoring spec (historical)](docs/phase-1-core/specs/PLUGINS.md)

---

## Guiding principle

> Core defines contracts. Core plugins provide optional infrastructure. External plugins extend behavior without modifying core.

If a feature can be a plugin, it should not be in core. If a plugin requires infrastructure, it must be optional.
