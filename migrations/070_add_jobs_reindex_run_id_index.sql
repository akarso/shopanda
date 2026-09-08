-- Supports ReindexJobFinder.FindReindexJobByRunID (PR-1048):
-- "WHERE type = 'search.reindex' AND payload->>'run_id' = $1". Without
-- this, idx_jobs_type (066) narrows to search.reindex rows but the
-- payload->>'run_id' extraction itself is unindexed — fine at today's
-- volume, but worth closing before the jobs table grows large enough for
-- a per-type scan to matter.
--
-- Same CONCURRENTLY caveat as 066_add_jobs_introspection_indexes.sql:
-- migrations run inside a transaction here, so this takes a ShareLock on
-- jobs during creation. Apply manually with CONCURRENTLY during a
-- maintenance window instead, for a large/hot production jobs table.
CREATE INDEX IF NOT EXISTS idx_jobs_reindex_run_id ON jobs ((payload->>'run_id'))
    WHERE type = 'search.reindex';
