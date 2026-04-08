-- 019_create_cache.sql

CREATE UNLOGGED TABLE cache (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_cache_expires_at ON cache (expires_at) WHERE expires_at IS NOT NULL;
