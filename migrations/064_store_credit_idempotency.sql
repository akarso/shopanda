-- Prevents a retried admin store-credit issuance from crediting twice.
-- idempotency_key is client-supplied and optional: NULL for every redeem
-- entry (redeem is not deduplicated — order_id has no unique constraint
-- and, for a checkout retry, changes on every attempt, so it can't serve
-- as a replay-detection key either) and for any issue call that doesn't
-- supply one.

-- VARCHAR(255) matches integration_idempotency.idempotency_key (migration
-- 063) — same concept (a client-supplied replay-detection token), same
-- bound. The empty-string check guards the column itself: application
-- code already maps an empty key to NULL via NULLIF before insert, but an
-- empty (as opposed to NULL) value would otherwise silently defeat the
-- "NULL means no key" convention the partial unique index below relies on.
ALTER TABLE store_credit_ledger ADD COLUMN idempotency_key VARCHAR(255)
    CHECK (idempotency_key IS NULL OR idempotency_key <> '');

CREATE UNIQUE INDEX idx_store_credit_ledger_idempotency
    ON store_credit_ledger (customer_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
