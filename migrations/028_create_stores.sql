CREATE TABLE stores (
    id         TEXT PRIMARY KEY,
    code       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    currency   TEXT NOT NULL,
    country    TEXT NOT NULL,
    domain     TEXT NOT NULL DEFAULT '',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_stores_domain ON stores (domain) WHERE domain != '';
CREATE UNIQUE INDEX idx_stores_default ON stores (is_default) WHERE is_default = TRUE;
