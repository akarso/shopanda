# Phase 13 — Plugin Runtime (schema + on-demand jobs)

Make plugin-owned tables and plugin-owned background work safe to copy before a real merchant site depends on them.

## Why this phase

Two plugin capabilities look present but are not site-ready:

- **Plugin schema** is a B2B-only pattern: `migrate.RunFS` against `app.Bootstrap.DB` during `Init`. Versions share core `schema_migrations` keyed by **filename basename only**. There is no advisory lock, so `serve` and `worker` can race DDL on boot. The next plugin that ships `001_*.sql` inherits a collision and a startup flake.
- **Plugin jobs** exist only as outbound **sync** jobs (`RegisterSyncJob` — cron or event → `integration.sync.{slug}.{name}`). That covers warehouse/PIM pull. It does not cover “ack the HTTP request, enqueue work, retry via the queue” from a plugin admin route or inbound webhook. Authors fall back to `Bus.OnAsync` bodies, which have no retry and no jobs-admin visibility.

This phase closes those two gaps as small increments on existing ports. It does **not** add a second job subsystem or a per-plugin database.

## Design principles

- **Namespace, don’t isolate.** Plugin migration versions become `plugin:{name}:{basename}` in the same `schema_migrations` table. Plugin job types stay `integration.sync.{slug}.{name}`.
- **Lock around DDL.** Plugin apply uses a PostgreSQL advisory lock so multi-process `Init` is safe.
- **`app migrate` is the deploy path.** `Init` remains an idempotent safety net (same lock), not the only way plugin SQL runs.
- **Extend `RegisterSyncJob`, don’t add `RegisterJobHandler`.** On-demand work is a third trigger kind (`manual`) plus `Enqueue` after the composition root binds the resolved queue. Phase 11 jobs admin already lists/retries any type on that queue.
- **One PR = one responsibility**, reviewable in ~10–20 minutes.

See [`ROADMAP.md`](ROADMAP.md) for tracks, sequencing, and PR specs under [`prs/`](prs/).

## Status

**Planned.** Site-ready gate is Track A + Track B (PR-1084–1088). Independent of Phase 12 catalog work; can run in parallel.

## Relationship

| Phase | Focus | Status |
| --- | --- | --- |
| Phase 8 | Integrator platform — `RegisterSyncJob`, outbound warehouse/PIM | Shipped (PR-840–843) |
| Phase 11 | Jobs admin (list / retry / cancel) + queue reachability | Shipped |
| Phase 12 | Product types & composition | In progress (unrelated catalog work) |
| **Phase 13** | Plugin-owned migrations + on-demand sync enqueue | **Planned** (do before a real site) |
