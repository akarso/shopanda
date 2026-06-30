-- Merchant-configured outbound webhook endpoints.

CREATE TABLE webhook_endpoints (
    id          UUID PRIMARY KEY,
    url         TEXT NOT NULL,
    secret      TEXT NOT NULL,
    events      TEXT[] NOT NULL,
    active      BOOLEAN NOT NULL DEFAULT true,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhook_endpoints_active ON webhook_endpoints (active);
