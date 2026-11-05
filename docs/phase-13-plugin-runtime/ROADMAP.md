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
| Plugin schema | ~~B2B `RunFS` in `Init`~~ | `RunPluginFS` + advisory lock; `plugin.Migrator`; `app migrate` / `setup` apply without Init; InitAll applies before Init |
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
| PR-1084 | `RunPluginFS` + lock + namespaced versions | **done** — New runner: record `plugin:{name}:{basename}`; **one transaction + xact lock per file** (PgBouncer-safe; preserves 003/004 lock splits); claim-then-exec; strict name charset; plugin filename hygiene. Existing `Run` / `RunFS` unchanged. |
| PR-1085 | Move B2B onto `RunPluginFS` | **done** — B2B `Init` uses the namespaced runner. `AdoptBareBasenames` remaps the frozen legacy set (001–004) under the same per-file lock/tx; dual-writes bare keys on fresh apply; allowlist must match the embed FS; lock wait only (no DDL wall-clock cap). |
| PR-1086 | `Migrator` + `app migrate` | **done** — `plugin.Migrator` (`Migration()` one FS). migrate without InitAll; `InitAllContext` apply/verify before Init; depends_on fatal with Migrator; serve fails on MigrateFailed; panics recovered. |

---

## Track B — On-demand sync jobs (PR-1087–1088)

**Goal:** enqueue existing sync job types from plugin code, without a second job API.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1087 | `manual` trigger + `Enqueue` + queue bind | **done** — `extapi.Manual()`; `Integration.Enqueue`; `BindQueue` after resolve (serve/worker/scheduler/CLIs). Unknown name / unbound queue fail clearly. |
| PR-1088 | SDK + docs + example | **done** — `pluginsdk` `RegisterManual` / `Enqueue`; docs; `plugins report` `trigger=…`; example uses `RegisterOnEvent`. |

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
| 1084 | A | [oss] | **done** — Namespaced plugin migrate runner + advisory lock |
| 1085 | A | [oss] | **done** — B2B uses runner; adopt frozen bare basenames |
| 1086 | A | [oss] | **done** — `Migrator` + `app migrate` applies plugin SQL (no InitAll) |
| 1087 | B | [oss] | **done** — Manual sync trigger + `Enqueue` + queue bind |
| 1088 | B | [oss] | **done** — SDK, docs, example, plugins report |

PR specs: [`prs/`](prs/).

---

## Relationship to prior phases

| Phase | Focus | Status |
| --- | --- | --- |
| [Phase 8](../phase-8-integrator-platform/ROADMAP.md) | `RegisterSyncJob` cron/event | Shipped |
| [Phase 11](../phase-11-jobs-search-cache/ROADMAP.md) | Jobs admin for any queued type | Shipped |
| [Phase 12](../phase-12-product-composition/ROADMAP.md) | Catalog types & composition | In progress — no dependency |
| **Phase 13** | Plugin schema + on-demand sync | **Done** (PR-1084–1088) |
