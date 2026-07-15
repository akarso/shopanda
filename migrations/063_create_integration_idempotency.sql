-- Inbound integration idempotency keys for ERP-safe retries.

CREATE TABLE integration_idempotency (
    plugin_slug     VARCHAR(64)  NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL,
    method          VARCHAR(16)  NOT NULL,
    path            TEXT         NOT NULL,
    request_hash    CHAR(64)     NOT NULL,
    status_code     INT          NOT NULL DEFAULT 0,
    response_body   BYTEA        NOT NULL DEFAULT '',
    completed       BOOLEAN      NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ  NOT NULL,
    PRIMARY KEY (plugin_slug, idempotency_key)
);

CREATE INDEX idx_integration_idempotency_expires ON integration_idempotency (expires_at);
