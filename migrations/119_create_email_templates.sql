-- Merchant email template overrides (PR-1212).
-- Resolution at render: DB exact locale → DB empty locale (store default) → file → builtin.
-- Body is inner HTML; _layout.html still wraps after render.

CREATE TABLE email_templates (
    id         UUID PRIMARY KEY,
    store_id   TEXT NOT NULL REFERENCES stores (id) ON DELETE CASCADE,
    name       TEXT NOT NULL CHECK (
        char_length(name) <= 64
        AND char_length(btrim(name)) > 0
        AND name ~ '^[a-z][a-z0-9_]*$'
    ),
    locale     TEXT NOT NULL DEFAULT '' CHECK (char_length(locale) <= 32),
    subject    TEXT NOT NULL CHECK (char_length(subject) <= 998),
    body       TEXT NOT NULL CHECK (char_length(body) <= 1048576),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT CHECK (updated_by IS NULL OR char_length(updated_by) <= 64),
    UNIQUE (store_id, name, locale)
);

CREATE INDEX idx_email_templates_store_name ON email_templates (store_id, name);
