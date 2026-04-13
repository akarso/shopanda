-- Price history: records every price change for EU Omnibus compliance.
-- Enables "lowest price in last 30 days" display for discounted products.

CREATE TABLE price_history (
    id          UUID PRIMARY KEY,
    variant_id  UUID NOT NULL REFERENCES variants(id) ON DELETE CASCADE,
    store_id    TEXT NOT NULL DEFAULT '',
    currency    TEXT NOT NULL CHECK (length(currency) = 3 AND currency = upper(currency)),
    amount      BIGINT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_price_history_lookup
    ON price_history (variant_id, currency, store_id, recorded_at DESC);
