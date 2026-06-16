-- Add an explicit language assignment to CMS pages.
-- Empty string means unspecified/default language. Pages remain one record per
-- language (no multi-locale translation model yet); slug stays globally unique.
ALTER TABLE pages ADD COLUMN IF NOT EXISTS language TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_pages_language ON pages (language);
