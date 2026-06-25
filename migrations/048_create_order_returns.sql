-- Order returns (RMA) for post-sale merchandise returns.

CREATE TABLE order_returns (
    id           UUID PRIMARY KEY,
    order_id     UUID NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    customer_id  TEXT NOT NULL DEFAULT '',
    reason       TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'requested'
                 CHECK (status IN ('requested', 'approved', 'received', 'refunded', 'rejected', 'cancelled')),
    currency     TEXT NOT NULL,
    restocked_at TIMESTAMPTZ,
    refunded_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_order_returns_order ON order_returns (order_id);
CREATE INDEX idx_order_returns_status ON order_returns (status);

CREATE TABLE order_return_items (
    return_id   UUID NOT NULL REFERENCES order_returns(id) ON DELETE CASCADE,
    variant_id  TEXT NOT NULL,
    sku         TEXT NOT NULL,
    name        TEXT NOT NULL,
    quantity    INT NOT NULL CHECK (quantity > 0),
    unit_price  BIGINT NOT NULL CHECK (unit_price >= 0),
    currency    TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (return_id, variant_id)
);
