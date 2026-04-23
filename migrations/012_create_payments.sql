CREATE TABLE IF NOT EXISTS payments (
    id          TEXT PRIMARY KEY,
    order_id    UUID NOT NULL REFERENCES orders(id),
    method      TEXT NOT NULL CHECK (method IN ('manual')),
    status      TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'completed', 'failed', 'refunded')),
    amount      BIGINT NOT NULL CHECK (amount > 0),
    currency    TEXT NOT NULL CHECK (length(currency) = 3),
    provider_ref TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payments_order_id_unique UNIQUE (order_id)
);

CREATE INDEX IF NOT EXISTS payments_status_idx ON payments (status);
