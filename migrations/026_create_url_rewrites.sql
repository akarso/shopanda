-- URL rewrite table for SEO-friendly URL resolution.
CREATE TABLE IF NOT EXISTS url_rewrites (
    path      TEXT PRIMARY KEY,
    type      TEXT NOT NULL,
    entity_id UUID NOT NULL
);

CREATE INDEX idx_url_rewrites_entity ON url_rewrites (entity_id);
