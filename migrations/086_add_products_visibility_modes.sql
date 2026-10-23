-- PR-1055 (Phase 12): per-axis visibility override modes on products.
-- DEFAULT 'auto' backfills existing rows; AutoBasis computation is application-side.
-- Allowed values must stay in sync with catalog.AllVisibilityModes().
--
-- Single ALTER TABLE so ADD COLUMN + CHECK share one ACCESS EXCLUSIVE window
-- (same pattern as 083 type column). VALIDATE is a separate migration (087)
-- so the scan is not under ACCESS EXCLUSIVE.
-- Deploy: apply migrations before rolling out app binaries that SELECT these columns.

ALTER TABLE products
    ADD COLUMN visibility_catalog_mode TEXT NOT NULL DEFAULT 'auto',
    ADD COLUMN visibility_search_mode TEXT NOT NULL DEFAULT 'auto',
    ADD COLUMN visibility_individually_mode TEXT NOT NULL DEFAULT 'auto',
    ADD COLUMN visibility_purchasable_mode TEXT NOT NULL DEFAULT 'auto',
    ADD CONSTRAINT products_visibility_modes_check
        CHECK (
            visibility_catalog_mode IN ('auto', 'visible', 'hidden')
            AND visibility_search_mode IN ('auto', 'visible', 'hidden')
            AND visibility_individually_mode IN ('auto', 'visible', 'hidden')
            AND visibility_purchasable_mode IN ('auto', 'visible', 'hidden')
        ) NOT VALID;
