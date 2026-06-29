-- Track abandoned-cart recovery emails sent to signed-in customers.
ALTER TABLE carts
    ADD COLUMN IF NOT EXISTS recovery_email_sent_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_carts_recovery_candidates
    ON carts (updated_at)
    WHERE status = 'active'
      AND customer_id IS NOT NULL
      AND recovery_email_sent_at IS NULL;
