-- PR-1051 (Phase 12): index for admin product list ?type= filter.
-- Matches ProductRepo.List: WHERE type = $n ORDER BY created_at DESC LIMIT/OFFSET.
-- Composite (type, created_at DESC) serves the equality + sort access path;
-- a single-column type index is too weak on large catalogs (low cardinality).

CREATE INDEX IF NOT EXISTS idx_products_type_created_at
    ON products (type, created_at DESC);
