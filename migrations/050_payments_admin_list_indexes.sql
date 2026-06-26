-- Indexes for admin payment ledger list queries.
--
-- NOTE: shopanda migrations run inside a transaction (see migrate.applyMigration),
-- so CREATE INDEX CONCURRENTLY is not available here. These builds take a
-- ShareLock on payments during creation. Run as part of normal deploy when
-- brief index-build locking is acceptable; for large/hot production tables,
-- apply equivalent indexes manually with CONCURRENTLY during a maintenance window.

CREATE INDEX IF NOT EXISTS idx_payments_created_at ON payments (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payments_status_created_at ON payments (status, created_at DESC);
