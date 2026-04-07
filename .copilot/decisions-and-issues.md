# Shopanda Known Issues & Decisions

## Known Issues
- product_categories.product_id is TEXT but products.id is UUID (migration 014) — type mismatch, pre-existing from PR-050
- collection_products.product_id was fixed to UUID in PR-052 review, but product_categories was not touched (out of scope)

## Architectural Decisions
- Jobs queue (PR-056) uses `FOR UPDATE SKIP LOCKED` for concurrent-safe dequeue — no row-level blocking between workers
- Jobs retry uses exponential backoff (PR-057): base=5s, 2^attempt growth, capped at 5min, ±25% jitter
- Worker lives in domain layer (`internal/domain/jobs/`) alongside Queue interface — it coordinates domain logic, not infrastructure
- Search domain (PR-053) uses its own `search.Product` type instead of importing `catalog.Product` — hexagonal isolation, no cross-domain imports in domain layer
- PostgresSearchEngine (PR-054) uses trigger-based indexing: trigger auto-updates search_vector on name/description changes, IndexProduct exists for explicit reindex
- RemoveProduct sets search_vector to NULL rather than deleting the row — keeps search engine decoupled from product lifecycle
- Category facets only in v0 — attribute-based JSONB facets deferred to avoid query complexity
- Collection AddProduct uses INSERT...SELECT with type='manual' guard (PR-052) — enforces manual-only at DB level in single query
- Collection Update wraps in TX to check assignment count before allowing type change to dynamic (PR-052)
- Category tree built with pointer-based 2-pass algorithm: create nodes map → attach children → collect roots (PR-051)
- Composition pipeline steps use Apply(), not Execute() — different from checkout steps which use Execute()
- Pricing steps use Apply(ctx context.Context, pctx *PricingContext) — takes Go context AND pricing context
- Checkout steps use Execute(ctx *Context) — takes only checkout context (no Go context param)
- Plugin registration uses `any` type for steps; main.go does type assertion when extracting

## Review Decisions (Rejected Findings)
- PR-054 R2: "Migrate product_categories.product_id to UUID" — rejected. Pre-existing type mismatch from PR-050, out of scope. `::text` casts removed instead (PostgreSQL implicit cast works, consistent with product_repo.go).
- PR-051 R2: "Extract queryAndScanProducts helper" — rejected. Only 2 callsites with distinct error context strings. Helper would lose debugging granularity.
- PR-047: All 7 findings rejected (checkout steps implementation choices)

## PRs Complete (with review status)
- 001-053 all implemented and reviewed
- Phase 13 (Catalog Organization: categories + collections) — complete
- Phase 14 (Search) — PR-053 done, PR-054 done, PR-055 done
- Phase 15 (Jobs) — PR-056 done, PR-057 done
