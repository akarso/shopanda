-- PR-1075 (Phase 12 Track E): DownloadGrant lifecycle for paid downloadable lines.
-- Spec drafted the table singular (download_grant); plural matches
-- downloadable_files / product_assignments / order_items.
-- order_item_id is opaque TEXT (orderID/variantID), same convention as
-- product_assignments — order_items has a composite PK and no unique id.
-- customer_id may be empty for guest checkouts; delivery auth / rebind is
-- PR-1077+. asset_id RESTRICT so a grant cannot lose its blob while live.
-- UNIQUE (order_item_id, asset_id) makes order.paid issuance idempotent.
-- Product-level expiry/limit overrides live in downloadable_grant_settings
-- (NULL column = use catalog.downloadable global config).

CREATE TABLE IF NOT EXISTS download_grants (
    id             UUID PRIMARY KEY,
    order_item_id  TEXT NOT NULL,
    customer_id    TEXT NOT NULL DEFAULT '',
    asset_id       TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
    expires_at     TIMESTAMPTZ NOT NULL,
    download_limit INT NOT NULL,
    download_count INT NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT download_grants_order_item_not_empty
        CHECK (btrim(order_item_id) <> ''),
    CONSTRAINT download_grants_asset_not_empty
        CHECK (btrim(asset_id) <> ''),
    CONSTRAINT download_grants_limit_range
        CHECK (download_limit >= 1 AND download_limit <= 10000),
    CONSTRAINT download_grants_count_range
        CHECK (download_count >= 0 AND download_count <= download_limit),
    CONSTRAINT download_grants_order_item_asset_unique
        UNIQUE (order_item_id, asset_id)
);

CREATE INDEX IF NOT EXISTS idx_download_grants_customer
    ON download_grants (customer_id)
    WHERE customer_id <> '';

CREATE INDEX IF NOT EXISTS idx_download_grants_order_item
    ON download_grants (order_item_id);

CREATE INDEX IF NOT EXISTS idx_download_grants_asset
    ON download_grants (asset_id);

CREATE TABLE IF NOT EXISTS downloadable_grant_settings (
    product_id     UUID PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
    expiry_days    INT,
    download_limit INT,
    CONSTRAINT downloadable_grant_settings_expiry_range
        CHECK (expiry_days IS NULL OR (expiry_days >= 1 AND expiry_days <= 3650)),
    CONSTRAINT downloadable_grant_settings_limit_range
        CHECK (download_limit IS NULL OR (download_limit >= 1 AND download_limit <= 10000))
);
