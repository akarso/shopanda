CREATE TABLE store_credit_accounts (
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    currency    TEXT NOT NULL CHECK (length(currency) = 3 AND currency = upper(currency)),
    balance     BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (customer_id, currency)
);

CREATE TABLE store_credit_ledger (
    id          UUID PRIMARY KEY,
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    currency    TEXT NOT NULL,
    amount      BIGINT NOT NULL CHECK (amount > 0),
    kind        TEXT NOT NULL CHECK (kind IN ('issue', 'redeem')),
    order_id    UUID REFERENCES orders(id) ON DELETE SET NULL,
    note        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_store_credit_ledger_customer ON store_credit_ledger (customer_id, created_at DESC);

ALTER TABLE orders ADD COLUMN store_credit_amount BIGINT NOT NULL DEFAULT 0
    CHECK (store_credit_amount >= 0 AND store_credit_amount <= total_amount);
