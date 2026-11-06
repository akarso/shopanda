-- Placeholder meta table for sitekit (product-init skeleton).
-- Replace or extend with real product schema. Versions are tracked as
-- plugin:sitekit:001_create_meta.sql
CREATE TABLE IF NOT EXISTS sitekit_meta (
    key text PRIMARY KEY,
    value text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now()
);
