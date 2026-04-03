CREATE TABLE password_reset_tokens (
    id          UUID PRIMARY KEY,
    customer_id UUID NOT NULL REFERENCES customers(id),
    token_hash  TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_password_reset_tokens_hash ON password_reset_tokens(token_hash);
CREATE INDEX idx_password_reset_tokens_customer ON password_reset_tokens(customer_id);
