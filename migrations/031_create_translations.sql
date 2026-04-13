-- System translations table: key + language → value.
CREATE TABLE translations (
    key      TEXT NOT NULL,
    language TEXT NOT NULL,
    value    TEXT NOT NULL,
    PRIMARY KEY (key, language)
);

-- Index on language for ListByLanguage queries.
CREATE INDEX idx_translations_language ON translations (language);

-- Add default language to stores.
ALTER TABLE stores ADD COLUMN language TEXT NOT NULL DEFAULT 'en';
