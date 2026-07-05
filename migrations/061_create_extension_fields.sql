-- Extension field definitions (namespaced custom fields for entities and contexts).

CREATE TABLE extension_fields (
    code        TEXT PRIMARY KEY,
    definition  JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);

CREATE INDEX idx_extension_fields_active_scope
    ON extension_fields ((definition->>'scope'))
    WHERE deleted_at IS NULL;
