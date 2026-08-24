-- Prevents a retried admin store-credit issuance from crediting twice.
-- idempotency_key is client-supplied and optional: NULL for redeem entries
-- (already keyed by order_id) and for any issue call that doesn't supply one.

ALTER TABLE store_credit_ledger ADD COLUMN idempotency_key TEXT;

CREATE UNIQUE INDEX idx_store_credit_ledger_idempotency
    ON store_credit_ledger (customer_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
