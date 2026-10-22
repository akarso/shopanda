-- PR-1063 (Phase 12): bundle stock mode.
-- DEFAULT 'fixed' backfills every existing row (constant DEFAULT on ADD COLUMN).
-- CHECK allows only the two tokens. The rule "dynamic only when type = bundle"
-- stays in catalog.Product.Validate — same split as composition type rules.
--
-- ADD CONSTRAINT ... NOT VALID takes ACCESS EXCLUSIVE briefly; VALIDATE
-- must not share this file — see 093. Allowed values must stay in sync
-- with catalog.AllStockModes().

ALTER TABLE products ADD COLUMN stock_mode TEXT NOT NULL DEFAULT 'fixed';

ALTER TABLE products ADD CONSTRAINT products_stock_mode_check
    CHECK (stock_mode IN ('fixed', 'dynamic')) NOT VALID;
