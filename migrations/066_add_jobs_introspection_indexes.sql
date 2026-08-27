-- Supports the read queries added in PR-1028 (job introspection):
-- List's "ORDER BY created_at DESC, id DESC" (every call, regardless of
-- filter — the id tie-breaker keeps offset pagination stable across pages
-- when multiple jobs share a created_at value) and its optional
-- type/status filters, and CountsByStatus's "GROUP BY status". The
-- existing idx_jobs_dequeue index only covers the dequeue hot path
-- (status = 'pending'), so none of these read queries had any index
-- support before this migration.
CREATE INDEX idx_jobs_created_at ON jobs (created_at DESC, id DESC);
CREATE INDEX idx_jobs_type ON jobs (type);
CREATE INDEX idx_jobs_status ON jobs (status);
