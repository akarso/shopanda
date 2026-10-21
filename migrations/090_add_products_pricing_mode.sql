-- PR-1062 (Phase 12): bundle pricing mode.
-- DEFAULT 'fixed' backfills every existing row (constant DEFAULT on ADD COLUMN).
-- CHECK allows only the two tokens. The rule "dynamic only when type = bundle"
-- stays in catalog.Product.Validate — same split as composition type rules.
--
-- ADD CONSTRAINT ... NOT VALID takes ACCESS EXCLUSIVE briefly; VALIDATE
-- must not share this file — see 091. Allowed values must stay in sync
-- with catalog.AllPricingModes().

ALTER TABLE products ADD COLUMN pricing_mode TEXT NOT NULL DEFAULT 'fixed';

ALTER TABLE products ADD CONSTRAINT products_pricing_mode_check
    CHECK (pricing_mode IN ('fixed', 'dynamic')) NOT VALID;
