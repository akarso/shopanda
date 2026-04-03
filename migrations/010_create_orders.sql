CREATE TABLE orders (
    id          UUID PRIMARY KEY,
    customer_id TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'confirmed', 'paid', 'cancelled', 'failed')),
    currency    TEXT NOT NULL,
    total_amount BIGINT NOT NULL DEFAULT 0 CHECK (total_amount >= 0),
    total_currency TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_orders_customer ON orders (customer_id);
CREATE INDEX idx_orders_status ON orders (status);

CREATE TABLE order_items (
    order_id    UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    variant_id  TEXT NOT NULL,
    sku         TEXT NOT NULL,
    name        TEXT NOT NULL,
    quantity    INT NOT NULL CHECK (quantity > 0),
    unit_price  BIGINT NOT NULL CHECK (unit_price >= 0),
    currency    TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (order_id, variant_id)
);
