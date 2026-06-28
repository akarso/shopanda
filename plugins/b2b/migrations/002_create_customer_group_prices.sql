CREATE TABLE customer_group_prices (
    id         UUID PRIMARY KEY,
    group_id   UUID NOT NULL REFERENCES customer_groups(id) ON DELETE CASCADE,
    variant_id UUID NOT NULL REFERENCES variants(id) ON DELETE CASCADE,
    store_id   TEXT NOT NULL DEFAULT '',
    currency   TEXT NOT NULL CHECK (length(currency) = 3 AND currency = upper(currency)),
    amount     BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (group_id, variant_id, currency, store_id)
);
