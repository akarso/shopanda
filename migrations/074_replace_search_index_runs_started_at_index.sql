-- Code review fix (PR-1038, round 3): SearchIndexRunRepo.List's query now
-- orders by "started_at DESC, id DESC" instead of "started_at DESC" alone
-- — concurrent triggers can share the same microsecond-resolution
-- timestamp, leaving their relative order undefined and letting an
-- offset-based client skip or duplicate runs between pages (matching
-- job_queue.go's JobQueue.List precedent, which orders by
-- "created_at DESC, id DESC" for the same reason). The plain index from
-- 073_add_search_index_runs_started_at_index.sql no longer matches the
-- query's sort order, so it's replaced with a composite one here rather
-- than edited in place — 073 already shipped, and this repo's migrations
-- are forward-only.
--
-- Same CONCURRENTLY caveat as 073 and its own predecessors: migrations run
-- inside a transaction here, so this takes a ShareLock on
-- search_index_runs for the duration of both the DROP and the CREATE.
-- Apply manually with CONCURRENTLY during a maintenance window instead,
-- for a large/hot production table.
DROP INDEX IF EXISTS idx_search_index_runs_started_at;
CREATE INDEX IF NOT EXISTS idx_search_index_runs_started_at_id ON search_index_runs (started_at DESC, id DESC);
