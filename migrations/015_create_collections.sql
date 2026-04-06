CREATE TABLE IF NOT EXISTS collections (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL UNIQUE,
    type       TEXT NOT NULL CHECK (type IN ('manual', 'dynamic')),
    rules      JSONB NOT NULL DEFAULT '{}',
    meta       JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS collection_products (
    collection_id TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    product_id    TEXT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    PRIMARY KEY (collection_id, product_id)
);

CREATE INDEX IF NOT EXISTS collection_products_product_idx ON collection_products (product_id);
