-- Content translations: per-entity, per-language, per-field translated values.
CREATE TABLE content_translations (
    entity_id TEXT NOT NULL,
    language  TEXT NOT NULL,
    field     TEXT NOT NULL,
    value     TEXT NOT NULL,
    PRIMARY KEY (entity_id, language, field)
);

-- Index for fetching all translated fields of an entity in a given language.
CREATE INDEX idx_content_translations_entity_language ON content_translations (entity_id, language);
