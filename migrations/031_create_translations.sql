-- System translations table: key + language → value.
CREATE TABLE translations (
    key      TEXT NOT NULL,
    language TEXT NOT NULL,
    value    TEXT NOT NULL,
    PRIMARY KEY (key, language)
);

-- Add default language to stores.
ALTER TABLE stores ADD COLUMN language TEXT NOT NULL DEFAULT 'en';
