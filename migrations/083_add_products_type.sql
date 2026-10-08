-- PR-1049 (Phase 12): product type taxonomy column.
-- DEFAULT 'simple' backfills every existing row with zero behavior change
-- (constant DEFAULT on ADD COLUMN fills existing rows; there are no
-- pre-existing "bad" type values for VALIDATE to discover).
--
-- Each migration file runs in one transaction (applyMigrationContent).
-- ADD CONSTRAINT ... NOT VALID takes ACCESS EXCLUSIVE briefly; VALIDATE
-- must not share this file — see 084 (SHARE UPDATE EXCLUSIVE scan after
-- stronger locks are released). The split still marks the CHECK
-- convalidated without holding ACCESS EXCLUSIVE for the scan.
-- Allowed values must stay in sync with catalog.AllTypes().

ALTER TABLE products ADD COLUMN type TEXT NOT NULL DEFAULT 'simple';

ALTER TABLE products ADD CONSTRAINT products_type_check
    CHECK (type IN (
        'simple',
        'virtual',
        'bundle',
        'grouped',
        'configurable',
        'downloadable'
    )) NOT VALID;
