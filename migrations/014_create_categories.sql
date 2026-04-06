CREATE TABLE IF NOT EXISTS categories (
    id         TEXT PRIMARY KEY,
    parent_id  TEXT REFERENCES categories(id),
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL UNIQUE,
    position   INTEGER NOT NULL DEFAULT 0,
    meta       JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX categories_parent_id_idx ON categories (parent_id);
CREATE INDEX categories_slug_idx ON categories (slug);

CREATE TABLE IF NOT EXISTS product_categories (
    product_id  TEXT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    category_id TEXT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (product_id, category_id)
);

CREATE INDEX product_categories_category_idx ON product_categories (category_id);
