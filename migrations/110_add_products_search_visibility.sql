-- Denormalized search-index visibility axes written by SearchEngine.IndexProduct (PR-1079).
-- DEFAULT true preserves pre-reindex behavior (status=active FTS still works); operators must full-reindex after deploy.

ALTER TABLE products
    ADD COLUMN IF NOT EXISTS search_visible_in_catalog BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS search_visible_in_search BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS search_visible_individually BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS search_purchasable BOOLEAN NOT NULL DEFAULT true;
