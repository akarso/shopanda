-- Indexes for admin payment ledger list queries.

CREATE INDEX IF NOT EXISTS idx_payments_created_at ON payments (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payments_status_created_at ON payments (status, created_at DESC);
