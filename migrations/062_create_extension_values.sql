-- Extension field values keyed by target and field code.

CREATE TABLE extension_values (
    target_type TEXT NOT NULL,
    target_id   TEXT NOT NULL,
    field_code  TEXT NOT NULL,
    value       JSONB NOT NULL,
    updated_by  TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (target_type, target_id, field_code)
);

CREATE INDEX idx_extension_values_target ON extension_values (target_type, target_id);
