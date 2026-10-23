# Phase 13 — Plugin Runtime (schema + on-demand jobs)

## Strategy

Phase 8 shipped `RegisterSyncJob` and B2B shipped plugin tables via `migrate.RunFS`. Both are usable in demos and unsafe to copy into a merchant site: migration versions collide on basename, `Init` races DDL across processes, and plugins cannot enqueue work from their own HTTP handlers.

This phase is a **site-ready gate** — five small PRs, no new subsystem.

- **Track A first.** A second plugin with tables is already a production risk (B2B exists).
- **Track B extends sync jobs.** No generic `jobs.Handler` registry; add `manual` trigger + `Enqueue`.
- **One PR = one responsibility**, reviewable in ~10–20 minutes.
- PR specs under `prs/` (**PR-1084+**, after Phase 12’s reserved PR-1083). Planned specs have Summary / Why / Scope / Out of scope / Follow-up only. Validation and “what changed” are written when the PR is implemented.

Each PR is tagged **`[oss]`**.

---

## What ships, by subsystem

| Subsystem | Today | End of this phase |
| --- | --- | --- |
| Plugin schema | B2B calls `migrate.RunFS` in `Init`; versions = SQL basename; no lock; not part of `app migrate` | `RunPluginFS` records `plugin:{name}:{file}`; advisory lock; B2B moved over; `RegisterMigrations` + `app migrate` applies plugin SQL |
| Plugin jobs | Cron or event sync only; no queue on `Bootstrap`; `OnAsync` is the escape hatch | Same job types + worker path; `manual` trigger; `Enqueue` after queue bind; SDK + docs |

---

## Tracks

| Track | Goal | PR range | Site-ready? |
| --- | --- | --- | --- |
| **A** | Plugin-owned migrations | PR-1084–1086 | Required |
| **B** | On-demand sync enqueue | PR-1087–1088 | Required |

```text
1084 → 1085 → 1086        (Track A: plugin migrations)
                │
                └──> 1087 → 1088   (Track B: manual sync jobs)
```

Track B does not depend on Track A in code. Do A first anyway: B2B is already in-tree, and a site that adds a second plugin with tables hits the collision/race immediately.

---

## Site-ready gate

Do **not** start a real merchant site (or a site-specific plugin with its own tables / inbound “202 + process later” work) until all five PRs are `done`.

After the gate:

- A plugin may ship owned tables with unique tracking and safe multi-process apply.
- `app migrate` applies those tables in the same deploy step as core schema.
- A plugin may register a sync job with no cron/event and enqueue it from a route, hook, or listener. Failures retry on the existing queue and show up in Phase 11 jobs admin.

---

## Track A — Plugin-owned migrations (PR-1084–1086)

**Goal:** make the B2B pattern safe to copy.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1084 | `RunPluginFS` + lock + namespaced versions | New runner: record `plugin:{name}:{basename}` in `schema_migrations`; take a PostgreSQL advisory lock for the apply batch; reject empty plugin name and non-`.sql` hygiene. Existing `Run` / `RunFS` unchanged (core `migrations/`). |
| PR-1085 | Move B2B onto `RunPluginFS` | B2B `Init` uses the namespaced runner. One-time treat already-applied B2B basenames (`001_create_customer_groups.sql`, `002_create_customer_group_prices.sql`) as applied under the new keys so enablement does not re-exec DDL. |
| PR-1086 | `RegisterMigrations` + `app migrate` | Plugins register an `embed.FS` during `Init`. `app migrate` applies core dir then every registered plugin FS (same lock/namespace as 1084). `Init` stays an idempotent safety net. Document the deploy order. |

---

## Track B — On-demand sync jobs (PR-1087–1088)

**Goal:** enqueue existing sync job types from plugin code, without a second job API.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1087 | `manual` trigger + `Enqueue` + queue bind | `extapi.Manual()`; `Integration.Enqueue(ctx, name, payload)`; composition root binds the resolved `jobs.Queue` onto `plugin.App` after `InitAll` (serve, worker, CLI paths that already resolve a queue). Unknown name / unbound queue fail clearly. Worker handlers already come from `RegisterSyncJobHandlers`. |
| PR-1088 | SDK + docs + example | `pluginsdk` `RegisterManual` / `Enqueue`; PLUGINS.md / DEVELOPER.md / EXTENSION_POINTS.md / PLUGIN_COMPOSITION.md; `plugins report` lists `trigger=manual`. Point `plugins/example` off inline `OnAsync` work toward enqueue-or-document the anti-pattern. |

---

## Explicitly out of scope

| Deferred | Why not now |
| --- | --- |
| Generic `RegisterJobHandler` / free-form job types | Duplicates sync jobs; no second in-tree caller that cannot use `integration.sync.*` |
| Plugin `jobs.Queue` on `Bootstrap` as a raw port | Encourages bypassing namespacing; `Enqueue` on the integration handle is enough |
| Down / rollback migrations | Core does not have them either |
| Per-plugin database or `plugin_schema_migrations` table | Namespace in the existing table is sufficient |
| Plugin-specific jobs admin UI | Phase 11 list/retry/cancel already works by job type |
| `.so` loading, marketplace, hot reload | Unchanged deferred list |

---

## Validation target (phase, not per-PR)

- Two processes (`serve` + `worker`) starting together apply B2B schema once; neither plugin init fails on “relation already exists”.
- A second plugin FS with `001_create_customer_groups.sql` is tracked as a **different** version (`plugin:{other}:…`) and does not skip or re-apply B2B’s file by basename.
- `app migrate` on a fresh DB applies core + B2B (when the plugin is registered) before processes start.
- A plugin registers `Trigger: Manual()`, calls `Enqueue` from a handler after boot, worker runs it, `jobs:list --type=integration.sync.{slug}.{name}` shows the row.
- `Enqueue` before queue bind, or for an unregistered name, returns an error (no silent drop).

---

## PR index

| PR | Track | License | One-liner |
| --- | --- | --- | --- |
| 1084 | A | [oss] | Namespaced plugin migrate runner + advisory lock |
| 1085 | A | [oss] | B2B uses runner; backfill old version keys |
| 1086 | A | [oss] | `RegisterMigrations` + `app migrate` applies plugin SQL |
| 1087 | B | [oss] | Manual sync trigger + `Enqueue` + queue bind |
| 1088 | B | [oss] | SDK, docs, example, plugins report |

PR specs: [`prs/`](prs/).

---

## Relationship to prior phases

| Phase | Focus | Status |
| --- | --- | --- |
| [Phase 8](../phase-8-integrator-platform/ROADMAP.md) | `RegisterSyncJob` cron/event | Shipped |
| [Phase 11](../phase-11-jobs-search-cache/ROADMAP.md) | Jobs admin for any queued type | Shipped |
| [Phase 12](../phase-12-product-composition/ROADMAP.md) | Catalog types & composition | In progress — no dependency |
| **Phase 13** | Plugin schema + on-demand sync | **Planned** |
