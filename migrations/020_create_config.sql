-- 020_create_config.sql

CREATE TABLE config (
    key   TEXT PRIMARY KEY,
    value JSONB NOT NULL
);
