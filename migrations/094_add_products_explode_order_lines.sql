-- PR-1064 (Phase 12): per-bundle order line explosion.
-- DEFAULT false backfills every existing row (constant DEFAULT on ADD COLUMN).
-- true is allowed only for a bundle with dynamic pricing and dynamic stock.
-- That combination stays in catalog.Product.Validate. A boolean needs no CHECK
-- and no separate VALIDATE migration.

ALTER TABLE products ADD COLUMN explode_order_lines BOOLEAN NOT NULL DEFAULT false;
