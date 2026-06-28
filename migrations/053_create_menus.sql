-- Navigation menus (header/footer) and nested menu items.
CREATE TABLE IF NOT EXISTS menus (
    id         UUID PRIMARY KEY,
    code       TEXT NOT NULL UNIQUE,
    title      TEXT NOT NULL,
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS menu_items (
    id          UUID PRIMARY KEY,
    menu_id     UUID NOT NULL REFERENCES menus (id) ON DELETE CASCADE,
    parent_id   UUID REFERENCES menu_items (id) ON DELETE CASCADE,
    label       TEXT NOT NULL,
    link_type   TEXT NOT NULL CHECK (link_type IN ('url', 'category', 'page')),
    link_target TEXT NOT NULL DEFAULT '',
    position    INT NOT NULL DEFAULT 0,
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_menu_items_menu_id ON menu_items (menu_id);
CREATE INDEX IF NOT EXISTS idx_menu_items_parent_id ON menu_items (parent_id);
CREATE INDEX IF NOT EXISTS idx_menus_code ON menus (code);

-- Seed default header and footer menus (items managed via admin API).
INSERT INTO menus (id, code, title)
VALUES
    ('00000000-0000-4000-8000-000000000511', 'header', 'Header Navigation'),
    ('00000000-0000-4000-8000-000000000512', 'footer', 'Footer Navigation')
ON CONFLICT (code) DO NOTHING;
