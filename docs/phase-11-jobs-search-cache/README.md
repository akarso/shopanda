# Phase 11 — Jobs, Search & Cache

Make background jobs, search indexing, and caching reachable by an operator — admin API, CLI, and GUI for all three — and add the caching capability Shopanda doesn't have yet: tag-based invalidation and a full-page cache.

## Why this phase

A prior-conversation audit of these three subsystems found the domain layer generally more capable than what's actually wired up or exposed:

- **Jobs/cron:** a real Postgres-backed queue and a real cron scheduler exist, but there is zero admin surface — no way to list, retry, or trigger anything without direct SQL or a restart. Worse: `ReleaseExpiredBefore` (expired-reservation cleanup) exists as a repository method and is **never called from anywhere** — dead code since it was written.
- **Search:** reindexing is CLI-only, always a full table scan, has no progress reporting, and never runs automatically — the index goes stale after every product edit until someone remembers to run it by hand. Products can belong to multiple categories in the domain model, but the search index only stores one.
- **Caching:** invalidation is prefix-only (adequate for the one real consumer it has today), there's no tag-based invalidation, no in-memory tier, no full-page cache, and no admin visibility into the cache at all.

This phase closes those gaps as one coherent piece of work, not three unrelated patches — reindex and full-page-cache warming both run as jobs through the same queue Track A makes observable; the full-page cache in Track D is built on the tag invalidation Track C ships first.

## Design principles carried through every track

- **Background work goes through the existing job queue.** No parallel execution mechanism for reindexing or cache warming.
- **Admin reachability means all three: API, CLI, and GUI** — not just an endpoint with no UI, or a CLI command nobody outside ops knows exists.
- **Full-page cache is opt-in per route template (allowlist), not opt-out per route (denylist alone).** A new storefront route defaults to uncached until someone deliberately adds it to the cacheable set. Getting this backwards is how a cart or account page ends up cached by accident.
- **No session, cart, or customer-identifying data ever enters a shared cache key or a cached response body.** Personalized fragments are always separately fetched, never baked into the cached shell.

See [`ROADMAP.md`](ROADMAP.md) for the full track breakdown, sequencing, and PR specs under [`prs/`](prs/).

## Status

**In progress.** Track A (jobs & scheduling admin, PR-1027–1032) is done. Tracks B–D (PR-1033–1047) are still planned.

## Relationship

| Phase | Focus | Status |
| --- | --- | --- |
| Phase 10 | Platform excellence (quality / security / ops / architecture) | Shipped (PR-1000–1026; PR-1003 the only open item) |
| **Phase 11** | Jobs, search & cache — admin reachability + full-page cache | **In progress** |
