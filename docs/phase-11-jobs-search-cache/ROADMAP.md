# Phase 11 — Jobs, Search & Cache

## Strategy

Phase 10 found the gap between what the domain layer *can* do and what an operator can actually *reach*: incremental search indexing and per-key cache TTLs already exist as primitives, but nothing wires them up, and neither jobs, search, nor cache has any admin surface (HTTP or CLI) at all. This phase closes that gap for three subsystems that share the same shape — background work, resolved via the existing `jobs.Queue`/`Worker`, made observable and controllable through admin API + CLI + GUI — plus a caching upgrade (tags, an in-memory tier, and a full-page cache with an ESI-equivalent hole mechanism) that the current single-purpose invalidation subscriber can't support.

- **Refactor and extend, do not replace** — the existing `jobs.Queue`/`Worker`, `cron.Scheduler`, `cache.Cache`, `search.SearchEngine` ports stay; this phase adds admin reachability and fills concrete gaps (dead-code reservation expiry, no on-save indexing, no tag invalidation, no full-page cache), it does not redesign them.
- **Reindex and full-page cache are both background work** — modeled as jobs through the existing queue, not a parallel execution mechanism. Track A (jobs admin) is a deliberate foundation: Track B's reindex progress endpoint and Track D's cache-warm-on-invalidation both reuse it instead of inventing their own job/run tracking.
- **One PR = one responsibility**, reviewable in ~10–20 minutes, same discipline as Phase 10.
- PR specs live under `prs/` (**PR-1027+**, continuing Phase 10's numbering).

Each PR is tagged **`[oss]`** unless noted.

---

## What ships, by subsystem

| Subsystem | Today | End of this phase |
| --- | --- | --- |
| Jobs/cron | No admin API, no CLI, no GUI; job/schedule state visible only via logs and direct SQL. `ReleaseExpiredBefore` for expired reservations is dead code — never called. | Admin API + CLI + GUI to list/retry/cancel jobs and list/trigger schedules. Reservation expiry actually runs. |
| Search indexing | `search:reindex` CLI only, always full-table, no admin API, no progress, no on-save updates, products only (and only a single `CategoryID`, though products can belong to many categories). | Admin API to trigger full/partial/single-item reindex with a progress endpoint; automatic on-save updates; categories indexed as their own entity; product↔category many-to-many correctly reflected. |
| Caching | Prefix-only invalidation, one real consumer (product cache, currently unused — nothing populates it), no full-page cache, no admin visibility, in-process-only rate limiting. | Tag-based invalidation (prefix stays for the simple case), an in-memory L1 tier for hot low-churn reads, a full-page cache for cacheable storefront routes with a strict never-cache denylist and an ESI-equivalent fragment mechanism for customer-specific content, admin API + CLI + GUI, and a documented rate-limit/TTL policy. |

---

## Tracks

| Track | Goal | PR range |
| --- | --- | --- |
| **A** | Jobs & scheduling admin | PR-1027–1032 |
| **B** | Search indexing | PR-1033–1038 |
| **C** | Caching foundation | PR-1039–1043 |
| **D** | Full-page cache & fragments | PR-1044–1047 |

**Ordering rule:** Track A before Track B (reindex progress reuses job introspection) and before Track D (cache-warm/purge admin reuses job introspection for async purge jobs). Track C before Track D (full-page cache is built on tag invalidation and needs the L1 tier's invalidation-propagation story settled first). Track B is independent of C/D and can run in parallel with either once A is done.

```text
1027 (fix expiry gap — standalone, ship first)
   │
1028 → 1029 → 1030 → 1031 → 1032        (Track A: jobs/scheduler admin)
   │                    │
   │                    ├──> 1033 → 1034 → 1035 → 1036 → 1037 → 1038   (Track B: search)
   │                    │
   └──> 1039 → 1040 → 1041 → 1042 → 1043                              (Track C: cache foundation)
                          │
                          └──> 1044 → 1045 → 1046 → 1047               (Track D: full-page cache)
```

---

## Track A — Jobs & scheduling admin (PR-1027–1032)

**Goal:** make background work observable and controllable without direct SQL.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1027 | Fix reservation expiry gap | **Done.** Wired the already-existing `ReleaseExpiredBefore` into a job handler + cron registration (`*/15 * * * *`, matching the 15-minute reservation TTL already documented in RUNBOOK.md). Standalone; shipped with zero dependency on the rest of this phase. |
| PR-1028 | Job introspection | **Done.** Read-only application service over the `jobs` table: list (filter by type/status), get by ID, status counts. No HTTP yet — this is the shared foundation Track B and Track D's progress/purge tracking build on. Also closed a gap the original spec didn't know was there: a failed job's error message was never persisted anywhere at all (not even "the last one") — added a `last_error` column so `Get` has something real to show. |
| PR-1029 | Jobs admin API | `GET /admin/jobs`, `GET /admin/jobs/{id}`, `POST /admin/jobs/{id}/retry` (requeue a `failed` job, resets `attempts`), `POST /admin/jobs/{id}/cancel` (only `pending`, not `processing` — no in-flight cancellation). New `jobs.read`/`jobs.write` permissions. Audit log entries for retry/cancel. |
| PR-1030 | Scheduler admin | Expose the `cron.Scheduler`'s registered specs (name, cron expression, next run time) via a catalog, mirroring the existing hooks/slots catalog pattern. `GET /admin/schedules`, `POST /admin/schedules/{name}/trigger` (fires the registered fn immediately, same as a real tick). Enable/disable requires a small persisted flag (new `scheduler_overrides` table, checked before `Scheduler.run` fires a task) — schedules are code-registered today with no runtime toggle. |
| PR-1031 | Jobs + scheduler admin GUI | New screens in the existing bundled admin SPA (`internal/interfaces/http/admin/dist`): job list/detail/retry/cancel, schedule list/trigger-now. Same schema-driven forms-and-grids pattern as the rest of the admin panel — no new frontend framework. |
| PR-1032 | Jobs/scheduler CLI | `app jobs:list`, `jobs:show <id>`, `jobs:retry <id>`, `schedule:list`, `schedule:trigger <name>` — for ops/scripting without the GUI, and for the CI/deploy scripts that will want to trigger a reindex or cache warm without going through HTTP. |

---

## Track B — Search indexing (PR-1033–1038)

**Goal:** reindex without a full table scan, reindex from the admin panel with visible progress, keep the index fresh automatically, and index the entities that are actually missing.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1033 | Reindex as a job | Model `search:reindex` as a queued `search.reindex` job (reusing Track A's `Queue`/`Worker`), with a persisted `search_index_runs` row (scope, started/finished, counts, error) updated as the worker processes it. The CLI command becomes a thin enqueue-and-optionally-wait wrapper instead of doing the work inline. |
| PR-1034 | Partial & scoped reindex | Scope a reindex run to explicit product IDs, explicit category IDs, or "changed since &lt;timestamp&gt;" (using `updated_at`, already present on `products`). Document the threshold heuristic: below ~20% of the catalog changed, scoped reindex is cheaper; above that, per-document overhead (index round-trips) exceeds the cost of one full scan — the reindex service picks full-scan automatically past the threshold rather than trusting the caller's guess. See "Design notes" below. |
| PR-1035 | Reindex admin API + progress | `POST /admin/search/reindex` (`{"scope": "all"}` \| `{"scope": "products", "ids": [...]}` \| `{"scope": "categories", "ids": [...]}` \| `{"scope": "since", "since": "<RFC3339>"}`), `GET /admin/search/reindex/{runID}` for progress (reuses Track A's job-status shape — a reindex run *is* a job). A single explicit product/category ID list of size 1 ("reindex this one now") skips the queue and calls `IndexProduct`/`IndexCategory` synchronously, returning immediately — genuinely "now," not "queued, poll for it." |
| PR-1036 | On-save incremental indexing | Event bus subscribers (same shape as `internal/application/cache/invalidation.go`) on product/price/stock/category-assignment change events → enqueue a single-item (or small-batch, debounced over a short window) `search.reindex` job scoped to the affected product/category IDs. This is the "on save" mode; PR-1030's schedule-trigger and PR-1035's manual trigger are the other two. |
| PR-1037 | Category indexing + relationship fix | Add `IndexCategory`/`RemoveCategory` to the `SearchEngine` port (both implementations); index categories as their own searchable/filterable entity. Fix `search.Product.CategoryID` (singular) → `CategoryIDs []string` — the catalog domain already supports many-to-many product↔category assignment (`product_categories` junction table, `AssignCategory`/`RemoveCategory`/`ListCategoryIDsByProduct`); the search index has silently been unable to represent a product in more than one category since day one. Category-assignment changes feed PR-1036's subscribers. |
| PR-1038 | Search admin GUI | Reindex trigger with a scope picker (all / products / categories / since-date), progress bar reusing Track A's job-status UI, run history table, and a per-row "reindex now" action on the product/category admin grids. |

### Design notes: what else is worth indexing

Beyond products and categories (PR-1037), two more entities exist in the domain and are plausible future search/filter targets, but are **out of scope for this phase** — call them out now so they're a deliberate decision, not an oversight:

- **CMS pages/content blocks** (`internal/domain/cms`) — searchable storefront content exists, but there's no evidence of a storefront "search everything" UI today (search is product-scoped); indexing CMS content without a consumer is speculative. Revisit if/when a unified storefront search box ships.
- **Customer/order search** (admin-side lookup) — currently served by direct Postgres queries in the admin API, which is adequate at expected admin-panel query volumes; moving it onto `SearchEngine` would only pay off at a scale this platform isn't targeting yet.

---

## Track C — Caching foundation (PR-1039–1043)

**Goal:** the primitives Track D's full-page cache needs, plus the admin visibility that's missing today for the cache that already exists.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1039 | Tag-based invalidation | Extend `cache.Cache`: `SetWithTags(key, value, ttl, tags ...string) error`, `DeleteByTag(ctx, tag string) error`. Postgres implementation: a `cache_tags(tag, key)` join table, `DeleteByTag` does a single `DELETE ... WHERE tag = $1` join. Redis implementation: a `SET` per tag (`tag:<name>` → member keys), `SMEMBERS` + pipelined `DEL` + tag-set cleanup. `DeleteByPrefix` stays for the simple per-entity case (existing product-cache invalidation keeps working unchanged); tags are for cross-cutting invalidation (Track D's "this CMS block appears on 40 cached pages" case) that prefix can't express. |
| PR-1040 | In-memory (L1) cache tier | A bounded, TTL+LRU process-local cache in front of the existing backend (now L2). Scoped explicitly to **low-churn, read-heavy, tolerant-of-bounded-staleness** data — category tree, permission catalog, plugin config, active promotion rule set — never anything invalidation-sensitive at request-serving latency without a propagation story. Cross-instance staleness is handled by (a) a short L1 TTL (seconds, not minutes) as the safety net, plus (b) an invalidation broadcast: `DeleteByTag`/`DeleteByPrefix` also publishes on the existing event bus's async path so every instance's L1 evicts, not just the one that triggered it. Document explicitly what must **never** go in L1: anything gated by RBAC per-request, anything with a tag that changes more than a few times a minute. |
| PR-1041 | Rate limiting hardening | Today's `ratelimit.Limiter` is in-process only — correct for a single instance, silently wrong (each instance gets its own bucket) for horizontal scaling. Add a Redis-backed sliding-window limiter as an opt-in alternative (`rate_limit.driver: memory\|redis`, default `memory` — zero new dependency for the common case), reusing the `cache.Cache` Redis backend's connection rather than a second Redis client. Document TTL/window best practices: sliding window over fixed window (avoids the burst-at-boundary problem), a distinct budget for auth endpoints vs general API (auth lockout already does this correctly — extend the same reasoning to the general limiter), and why probe/webhook endpoints need their own budget (already true today for `/readyz`; audit whether it should also be true for other automated-traffic paths). |
| PR-1042 | Cache admin API + CLI | `GET /admin/cache/stats` (per cache-type key counts/hit-miss where the backend can report it — Postgres backend gets an approximate count via `SELECT count(*)`, Redis gets `INFO`/`DBSIZE`), `POST /admin/cache/clear` (`{"prefix": "..."}` \| `{"tag": "..."}` \| `{"key": "..."}` \| `{"all": true}`, the last one gated behind a distinct, more sensitive permission and always audit-logged). CLI: `app cache:stats`, `cache:clear --prefix|--tag|--key|--all`. |
| PR-1043 | Cache admin GUI | Stats dashboard (per cache type), manual clear form (prefix/tag/key/all) with a confirmation step before `--all`. |

---

## Track D — Full-page cache & fragments (PR-1044–1047)

**Goal:** cache fully-rendered storefront HTML for the pages that are safe to cache, without ever serving one shopper's cart, session, or personal data to another.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1044 | Full-page cache core + cacheability policy | Cache rendered HTML (post-`html/template` execution, pre-write) in the `cache.Cache` backend, keyed by `route template + vary key` where vary = `{store, language, currency, auth-state}` — `auth-state` is a **coarse** `guest`/`authenticated` flag, never a session or customer ID (that would explode the key space and defeat the cache; personalized content is a fragment, see PR-1045). Every cached page is written with `SetWithTags` (PR-1039) tagged by the product/category/CMS-block IDs it rendered. **Cacheable:** PDP, PLP/category listing, CMS pages, home. **Never cached, enforced twice (allowlist of cacheable route templates AND a denylist check that fails closed on anything not explicitly allowlisted):** cart, mini-cart, checkout (all steps), account/order pages, admin — anything auth-scoped or containing a CSRF token, session state, or per-customer data. CSRF tokens specifically must never be baked into cached HTML — they need to already be a fragment (PR-1045) or client-fetched, or every visitor gets served the same stale token. |
| PR-1045 | Fragment mechanism (ESI-equivalent) | Shopanda has no edge/CDN/Varnish layer, so this is an **app-level** hole mechanism, not literal ESI: a template helper renders a placeholder (`<div hx-get="/fragment/minicart" hx-trigger="load">`) in an otherwise-cacheable page; the fragment endpoint itself is always `Cache-Control: no-store`, reads real session/cart state, and returns a small HTML snippet that htmx swaps in after the cached shell loads — same effect as Varnish resolving an ESI include, minus the edge-layer round trip savings (a fair tradeoff: no infra dependency, one extra client-side request for a handful of small always-fresh regions per page). Covers: mini-cart count, "recently viewed," personalized greeting, wishlist indicator, CSRF token. |
| PR-1046 | Invalidation wiring + stampede guard | Event subscribers (product/category/price/stock/CMS change events) call `DeleteByTag` (PR-1039) with the changed entity's ID — every cached page that rendered that product/category/block is purged, not just the entity's own record. Admin/CLI purge-by-URL escape hatch for anything a tag missed. Cache-stampede guard on miss: a per-key in-flight lock (`singleflight`-style, backed by the L1 tier from PR-1040 or a lightweight Postgres advisory lock) so a popular page invalidated under load gets rendered once, not once per concurrent request. |
| PR-1047 | Observability + rollup | Hit/miss/bypass counters (Prometheus, matching Phase 10's existing metrics conventions), RUNBOOK.md entries ("full-page cache serving stale content" / "a page that should be cached isn't" — symptom → check → fix, same style as Phase 10's incident-response section), cache admin GUI page for FPC-specific stats (hit rate by route template, current cached-page count). |

---

## Out of scope (explicit)

| Item | Why deferred |
| --- | --- |
| A real edge/CDN/Varnish deployment | This phase is an app-level full-page cache; fronting it with a real edge cache later is additive, not blocked by anything here |
| Multi-store catalog/search-index partitioning | No `store_id` on `products`/`categories` today (only on prices/tax) — partitioning the catalog itself is a bigger, separate change; this phase's reindex scoping is by product/category ID and timestamp, not by store |
| CDC/outbox-based cross-service invalidation | Tag invalidation + an event-bus broadcast for L1 propagation covers this phase's needs; a durable outbox is worth revisiting only if invalidation events start being lost across restarts, which the current in-process bus doesn't guarantee against a hard crash (existing limitation, not introduced here — see RUNBOOK.md's "Event bus drain" section) |
| Personalization / A/B testing beyond guest-vs-authenticated | No such system exists yet; the vary-key design in PR-1044 intentionally stays coarse rather than guessing at a future requirement |
| A new admin frontend framework | Extends the existing bundled vanilla SPA (`internal/interfaces/http/admin/dist`), does not replace it |
| Distributed scheduler / leader election | `scheduler` remains a single active instance, matching today's architecture; running two `scheduler` processes today double-fires every cron tick — that pre-existing constraint isn't addressed here |
| GraphQL (or any non-REST surface) for the new admin endpoints | Matches the existing admin API convention — REST only |
| Cache warming (pre-populating the full-page cache before first request) | Real but genuinely optional; a cold cache degrades gracefully to render-on-demand. Flagged as a natural PR-1048+ follow-up, not required for this phase to bring value |

---

## Effort estimate (calendar, one focused engineer)

| Track | Effort | Notes |
| --- | --- | --- |
| A | 1–1.5 weeks | Foundation for B and D's progress/purge tracking |
| B | 1.5–2 weeks | PR-1037's `CategoryIDs` migration touches the search index schema; plan a reindex as part of that PR's rollout |
| C | 1.5–2 weeks | PR-1040 (L1 tier) is the highest-judgment PR in the phase — get the "what's safe to cache in-process" boundary reviewed carefully |
| D | 2–2.5 weeks | PR-1044/1045 are the highest-risk PRs in the phase (serving one shopper's data to another is the failure mode to design against, not just implement against) |

**Total:** ~6–8 weeks. Track B can run in parallel with C/D once Track A ships, so wall-clock can compress toward the lower end with two engineers.

---

## PR index (quick reference)

| PR | Track | Status |
| --- | --- | --- |
| 1027 | — | done |
| 1028 | A | done |
| 1029 | A | done |
| 1030–1032 | A | planned |
| 1033–1038 | B | planned |
| 1039–1043 | C | planned |
| 1044–1047 | D | planned |

PR specs: [`prs/`](prs/).

---

## Relationship to prior phases

| Phase | Focus | Status |
| --- | --- | --- |
| Phase 9 | Integrator backlog + merchant discovery | Shipped (PR-856–908) |
| Phase 10 | Platform excellence (quality / security / ops / architecture) | Shipped (PR-1000–1026; PR-1003 the only open item, unrelated to this phase) |
| **Phase 11** | Jobs, search & cache — admin reachability + full-page cache | **In progress** |
