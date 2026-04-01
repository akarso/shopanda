CREATE TABLE prices (
    id          UUID PRIMARY KEY,
    variant_id  UUID NOT NULL REFERENCES variants(id) ON DELETE CASCADE,
    currency    TEXT NOT NULL CHECK (length(currency) = 3),
    amount      BIGINT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (variant_id, currency)
);

CREATE INDEX idx_prices_variant_id ON prices (variant_id);
