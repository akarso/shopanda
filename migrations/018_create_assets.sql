CREATE TABLE IF NOT EXISTS assets (
    id         TEXT PRIMARY KEY,
    path       TEXT NOT NULL,
    filename   TEXT NOT NULL,
    mime_type  TEXT NOT NULL,
    size       BIGINT NOT NULL,
    meta       JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_assets_created_at ON assets (created_at DESC);
