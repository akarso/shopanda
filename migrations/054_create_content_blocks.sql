-- Reusable CMS content blocks and page/layout placements.
CREATE TABLE IF NOT EXISTS content_blocks (
    id         UUID PRIMARY KEY,
    title      TEXT NOT NULL,
    block_type TEXT NOT NULL CHECK (block_type IN ('hero', 'rich_text', 'product_carousel')),
    config     JSONB NOT NULL DEFAULT '{}',
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS content_block_placements (
    id          UUID PRIMARY KEY,
    block_id    UUID NOT NULL REFERENCES content_blocks (id) ON DELETE CASCADE,
    target_type TEXT NOT NULL CHECK (target_type IN ('page', 'layout')),
    target_key  TEXT NOT NULL,
    position    INT NOT NULL DEFAULT 0,
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_content_blocks_active ON content_blocks (is_active);
CREATE INDEX IF NOT EXISTS idx_content_block_placements_target ON content_block_placements (target_type, target_key);
CREATE INDEX IF NOT EXISTS idx_content_block_placements_block ON content_block_placements (block_id);
