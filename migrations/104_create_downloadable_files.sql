-- PR-1074 (Phase 12 Track E): downloadable product → media asset attachments.
-- product_id must reference a Type = downloadable product (enforced in application).
-- asset_id references the existing media assets table (local or S3-backed storage).
-- Asset delete is RESTRICT so an attached file cannot be removed until unlinked.
-- Product delete cascades attachment rows.

CREATE TABLE IF NOT EXISTS downloadable_files (
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    asset_id   TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
    position   INT  NOT NULL DEFAULT 0 CHECK (position >= 0 AND position <= 9999),
    label      TEXT NOT NULL DEFAULT '' CHECK (char_length(label) <= 200),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (product_id, asset_id)
);

CREATE INDEX IF NOT EXISTS idx_downloadable_files_asset
    ON downloadable_files (asset_id);

CREATE INDEX IF NOT EXISTS idx_downloadable_files_product_position
    ON downloadable_files (product_id, position ASC, created_at ASC, asset_id ASC);
