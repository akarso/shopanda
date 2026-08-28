-- Supports the read queries added in PR-1028 (job introspection):
-- List's "ORDER BY created_at DESC, id DESC" (every call, regardless of
-- filter — the id tie-breaker keeps offset pagination stable across pages
-- when multiple jobs share a created_at value) and its optional
-- type/status filters, and CountsByStatus's "GROUP BY status". The
-- existing idx_jobs_dequeue index only covers the dequeue hot path
-- (status = 'pending'), so none of these read queries had any index
-- support before this migration.
--
-- NOTE: shopanda migrations run inside a transaction (see
-- migrate.applyMigrationContent), so CREATE INDEX CONCURRENTLY is not
-- available here — same constraint documented in
-- 050_payments_admin_list_indexes.sql. These builds take a ShareLock on
-- jobs during creation, blocking INSERT/UPDATE/DELETE (i.e. Enqueue,
-- Dequeue, Complete, Fail) for that duration. Run as part of normal
-- deploy when brief index-build locking is acceptable; for a large/hot
-- production jobs table, apply equivalent indexes manually with
-- CONCURRENTLY during a maintenance window instead.
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs (created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_type ON jobs (type);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs (status);
