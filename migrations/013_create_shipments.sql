CREATE TABLE IF NOT EXISTS shipments (
    id            TEXT PRIMARY KEY,
    order_id      TEXT NOT NULL UNIQUE,
    method        TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'shipped', 'delivered', 'cancelled')),
    cost          BIGINT NOT NULL DEFAULT 0 CHECK (cost >= 0),
    currency      TEXT NOT NULL CHECK (length(currency) = 3),
    tracking_number TEXT,
    provider_ref  TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX shipments_status_idx ON shipments (status);
