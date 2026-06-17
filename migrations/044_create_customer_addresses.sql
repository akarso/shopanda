-- Reusable shipping addresses saved to a customer's account.
-- Distinct from the immutable address snapshot persisted on orders.
CREATE TABLE customer_addresses (
    id          UUID NOT NULL PRIMARY KEY,
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    label       TEXT NOT NULL DEFAULT '',
    recipient   TEXT NOT NULL CHECK (length(btrim(recipient)) > 0),
    street      TEXT NOT NULL CHECK (length(btrim(street)) > 0),
    city        TEXT NOT NULL CHECK (length(btrim(city)) > 0),
    postcode    TEXT NOT NULL CHECK (length(btrim(postcode)) > 0),
    country     TEXT NOT NULL CHECK (length(btrim(country)) > 0),
    is_default  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_customer_addresses_customer ON customer_addresses (customer_id);

-- At most one default address per customer.
CREATE UNIQUE INDEX idx_customer_addresses_default
    ON customer_addresses (customer_id)
    WHERE is_default;
